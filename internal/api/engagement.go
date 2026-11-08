package api

import (
	"context"
	"strings"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/internal/auth"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

type mission struct {
	Code, Title, Description, Route string
	Reward                          float64
	Target                          int
	Progress                        string // SQL returning an integer for store $1
}

var missions = []mission{
	{"add_products", "Add 10 products", "Build your catalog so the cashier can sell quickly.", "products", 5000, 10,
		`SELECT count(*) FROM products p JOIN branches b ON b.id = p.branch_id WHERE b.store_id = $1 AND p.deleted_at IS NULL`},
	{"first_sale", "Make your first sale", "Record a sale from the cashier app.", "sales", 2000, 1,
		`SELECT count(*) FROM sales s JOIN branches b ON b.id = s.branch_id WHERE b.store_id = $1`},
	{"store_location", "Set your store location", "Pin your store on the map so customers can find you.", "store", 2000, 1,
		`SELECT count(*) FROM settings st JOIN branches b ON b.id = st.branch_id WHERE b.store_id = $1 AND st.key = 'store' AND st.value->>'latitude' IS NOT NULL`},
	{"add_staff", "Invite a staff member", "Give your cashier their own login.", "staff", 3000, 1,
		`SELECT count(*) FROM users WHERE store_id = $1 AND role <> 'owner' AND deleted_at IS NULL`},
	{"verify_phone", "Verify your phone number", "Secure your account and receive important alerts.", "profile", 2000, 1,
		`SELECT count(*) FROM users WHERE store_id = $1 AND role = 'owner' AND phone_verified`},
	{"two_factor", "Turn on two-step verification", "Protect the owner account with an authenticator app.", "profile", 2000, 1,
		`SELECT count(*) FROM users WHERE store_id = $1 AND role = 'owner' AND two_factor_enabled`},
	{"hundred_sales", "Reach 100 sales", "Keep selling — rewards grow with you.", "report-daily-sales", 10000, 100,
		`SELECT count(*) FROM sales s JOIN branches b ON b.id = s.branch_id WHERE b.store_id = $1`},
}

func (a *API) missionRows(ctx context.Context, storeID int64) ([]fiber.Map, error) {
	claimed := map[string]bool{}
	rows, err := a.DB.Query(ctx, `SELECT code FROM mission_claims WHERE store_id=$1`, storeID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		claimed[c] = true
	}
	rows.Close()
	out := make([]fiber.Map, 0, len(missions))
	for i, m := range missions {
		var n int
		if err := a.DB.QueryRow(ctx, m.Progress, storeID).Scan(&n); err != nil {
			return nil, err
		}
		if n > m.Target {
			n = m.Target
		}
		status := "in_progress"
		switch {
		case claimed[m.Code]:
			status = "claimed"
		case n >= m.Target:
			status = "completed"
		}
		out = append(out, fiber.Map{"id": i + 1, "code": m.Code, "title": m.Title, "description": m.Description, "reward_type": "coin", "reward_amount": m.Reward,
			"progress": n, "target": m.Target, "status": status, "expires_at": nil, "action_route": m.Route})
	}
	return out, nil
}

func (a *API) registerEngagement() {
	a.addH("GET", "/missions", func(c fiber.Ctx) error {
		rows, err := a.missionRows(c.Context(), httpx.P(c).StoreID)
		if err != nil {
			return err
		}
		return httpx.OK(c, rows)
	})
	a.addH("POST", "/missions/:id/claim", func(c fiber.Ctx) error {
		id, err := httpx.ParamID(c, "id")
		if err != nil || int(id) > len(missions) {
			return httpx.NotFound("Mission")
		}
		p := httpx.P(c)
		rows, err := a.missionRows(c.Context(), p.StoreID)
		if err != nil {
			return err
		}
		m := rows[id-1]
		if m["status"] != "completed" {
			if m["status"] == "claimed" {
				return httpx.Conflict("This reward was already claimed.")
			}
			return httpx.Conflict("Finish the mission first.")
		}
		err = a.tx(c.Context(), func(tx pgx.Tx) error {
			if _, err := tx.Exec(c.Context(), `INSERT INTO mission_claims (store_id, code) VALUES ($1,$2)`, p.StoreID, m["code"]); err != nil {
				if db.IsUniqueViolation(err) {
					return httpx.Conflict("This reward was already claimed.")
				}
				return err
			}
			_, err := tx.Exec(c.Context(), `UPDATE stores SET coin_balance = coin_balance + $2 WHERE id=$1`, p.StoreID, m["reward_amount"])
			return err
		})
		if err != nil {
			return err
		}
		return httpx.OK(c, fiber.Map{"id": id, "status": "claimed", "reward_type": "coin", "reward_amount": m["reward_amount"]})
	})

	// referral
	a.addH("GET", "/referrals", a.getReferral)
	a.addH("POST", "/referrals/code", func(c fiber.Ctx) error {
		var in struct {
			Code string `json:"code"`
		}
		if err := httpx.Decode(c, &in); err != nil {
			return err
		}
		p := httpx.P(c)
		if !p.IsOwner() {
			return httpx.Forbidden("Only the owner can create the referral code.")
		}
		code := strings.ToUpper(strings.TrimSpace(in.Code))
		if code == "" {
			code = "LST" + strings.ToUpper(auth.RandomToken(3))
		}
		if len(code) < 4 || len(code) > 16 || strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
			return httpx.Invalid("code", "Use 4–16 letters or numbers.")
		}
		tag, err := a.DB.Exec(c.Context(), `UPDATE stores SET referral_code=$2 WHERE id=$1 AND referral_code IS NULL`, p.StoreID, code)
		if db.IsUniqueViolation(err) {
			return httpx.Invalid("code", "This code is already taken.")
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.Conflict("You already have a referral code.")
		}
		return httpx.OK(c, fiber.Map{"code": code, "share_url": a.Cfg.PublicURL + "/register?ref=" + code})
	})
	a.addH("POST", "/referrals/rewards/transfer-to-coin", func(c fiber.Ctx) error {
		p := httpx.P(c)
		var out map[string]any
		err := a.tx(c.Context(), func(tx pgx.Tx) error {
			var reward float64
			if err := tx.QueryRow(c.Context(), `SELECT reward_balance FROM stores WHERE id=$1 FOR UPDATE`, p.StoreID).Scan(&reward); err != nil {
				return err
			}
			if reward <= 0 {
				return httpx.Conflict("There are no rewards to transfer yet.")
			}
			var err error
			out, err = db.JSONRow(c.Context(), tx, `UPDATE stores SET coin_balance = coin_balance + reward_balance, reward_balance = 0 WHERE id=$1
				RETURNING $2::numeric AS transferred, coin_balance, reward_balance`, p.StoreID, reward)
			return err
		})
		if err != nil {
			return err
		}
		return httpx.OK(c, out)
	})

	// integrations
	a.addH("GET", "/integrations", a.listIntegrations)
	a.addH("POST", "/integrations/:code/activate", func(c fiber.Ctx) error {
		code := c.Params("code")
		if integrationByCode(code) == nil {
			return httpx.NotFound("Integration")
		}
		body, err := httpx.Body(c)
		if err != nil {
			return err
		}
		if !httpx.Bool(body, "accept_terms") {
			return httpx.Invalid("accept_terms", "Accept the terms of service to continue.")
		}
		settings := "{}"
		if s, ok := body["settings"].(map[string]any); ok && len(s) > 0 {
			b, _ := json.Marshal(s)
			settings = string(b)
		}
		status := "pending" // the partner confirms the connection
		if code == "jurnal" {
			status = "active"
		}
		row, err := db.JSONRow(c.Context(), a.DB, `INSERT INTO store_integrations (store_id, code, status, settings, connected_at) VALUES ($1,$2,$3,$4::jsonb, CASE WHEN $3='active' THEN now() END)
			ON CONFLICT (store_id, code) DO UPDATE SET status=EXCLUDED.status, settings=EXCLUDED.settings, connected_at=EXCLUDED.connected_at
			RETURNING code, status`, httpx.P(c).StoreID, code, status, settings)
		if err != nil {
			return err
		}
		row["redirect_url"] = nil
		return httpx.OK(c, row)
	})
	a.addH("POST", "/integrations/:code/deactivate", func(c fiber.Ctx) error {
		code := c.Params("code")
		if integrationByCode(code) == nil {
			return httpx.NotFound("Integration")
		}
		if _, err := a.DB.Exec(c.Context(), `UPDATE store_integrations SET status='inactive', connected_at=NULL WHERE store_id=$1 AND code=$2`, httpx.P(c).StoreID, code); err != nil {
			return err
		}
		return httpx.OK(c, fiber.Map{"code": code, "status": "inactive"})
	})

	// loans
	a.addH("GET", "/loans/providers", func(c fiber.Ctx) error { return httpx.OK(c, loanProviders) })
	a.addH("GET", "/loans/applications", func(c fiber.Ctx) error {
		raw, err := db.JSONRaw(c.Context(), a.DB, `SELECT id, provider_code, amount, tenor_months, purpose, status, created_at FROM loan_applications WHERE store_id=$1 ORDER BY created_at DESC`, httpx.P(c).StoreID)
		if err != nil {
			return err
		}
		return httpx.OK(c, raw)
	})
	a.addH("POST", "/loans/applications", a.applyLoan)

	// customer feedback collected by the cashier app or receipt link
	a.addH("POST", "/store-ratings", func(c fiber.Ctx) error {
		body, err := httpx.Body(c)
		if err != nil {
			return err
		}
		r, _ := httpx.Int(body, "rating")
		if r < 1 || r > 5 {
			return httpx.Invalid("rating", "Give 1 to 5 stars.")
		}
		var sale any
		if s, ok := httpx.Int(body, "sale_id"); ok {
			sale = s
		}
		tags := []string{}
		if arr, ok := body["tags"].([]any); ok {
			for _, t := range arr {
				if s, ok := t.(string); ok {
					tags = append(tags, s)
				}
			}
		}
		row, err := db.JSONRow(c.Context(), a.DB, `INSERT INTO store_ratings (branch_id, sale_id, customer_name, rating, comment, tags) VALUES ($1,$2,NULLIF($3,''),$4,NULLIF($5,''),$6)
			RETURNING id, rating, comment, tags, created_at`, httpx.P(c).BranchID, sale, httpx.Str(body, "customer_name"), r, httpx.Str(body, "comment"), tags)
		if err != nil {
			return err
		}
		return httpx.Created(c, row)
	})
}

func (a *API) getReferral(c fiber.Ctx) error {
	p := httpx.P(c)
	row, err := db.JSONRow(c.Context(), a.DB, `SELECT s.referral_code AS code,
		CASE WHEN s.referral_code IS NULL THEN NULL ELSE $2 || s.referral_code END AS share_url,
		(SELECT count(*) FROM stores r WHERE r.referred_by_store_id = s.id) AS referred_count,
		s.reward_balance,
		COALESCE((SELECT json_agg(json_build_object('store_name', r.name, 'joined_at', r.created_at,
		   'status', CASE WHEN r.plan = 'pro' THEN 'subscribed' ELSE 'registered' END,
		   'reward_amount', CASE WHEN r.plan = 'pro' THEN 25000 ELSE 0 END) ORDER BY r.created_at DESC)
		 FROM stores r WHERE r.referred_by_store_id = s.id), '[]') AS referred_businesses
		FROM stores s WHERE s.id = $1`, p.StoreID, a.Cfg.PublicURL+"/register?ref=")
	if err != nil {
		return err
	}
	return httpx.OK(c, row)
}

type integration struct {
	Code, Name, Description, Logo, Terms string
}

var integrations = []integration{
	{"dana", "DANA", "Accept DANA e-wallet payments at the cashier and online store.", "", "https://www.dana.id/terms"},
	{"jurnal", "Jurnal by Mekari", "Send sales, purchases and expenses to your accounting books automatically.", "", "https://www.jurnal.id/terms"},
	{"qris_dynamic", "Dynamic QRIS", "Show a QR code with the exact amount for every sale.", "", ""},
}

func integrationByCode(code string) *integration {
	for i := range integrations {
		if integrations[i].Code == code {
			return &integrations[i]
		}
	}
	return nil
}

func (a *API) listIntegrations(c fiber.Ctx) error {
	rows, err := a.DB.Query(c.Context(), `SELECT code, status, connected_at FROM store_integrations WHERE store_id=$1`, httpx.P(c).StoreID)
	if err != nil {
		return err
	}
	type st struct {
		status    string
		connected any
	}
	state := map[string]st{}
	for rows.Next() {
		var code string
		var s st
		if err := rows.Scan(&code, &s.status, &s.connected); err != nil {
			rows.Close()
			return err
		}
		state[code] = s
	}
	rows.Close()
	out := make([]fiber.Map, 0, len(integrations))
	for _, i := range integrations {
		s, ok := state[i.Code]
		if !ok {
			s.status = "inactive"
		}
		var terms any
		if i.Terms != "" {
			terms = i.Terms
		}
		out = append(out, fiber.Map{"code": i.Code, "name": i.Name, "description": i.Description, "logo_url": nil, "status": s.status, "terms_url": terms, "connected_at": s.connected})
	}
	return httpx.OK(c, out)
}

var loanProviders = []fiber.Map{
	{"code": "modalrakyat", "name": "Modal Rakyat", "description": "Working-capital loans for small shops, decided from your sales history.", "logo_url": nil,
		"min_amount": 1000000, "max_amount": 50000000, "tenors_months": []int{3, 6, 12}, "interest_info": "From 1.5% per month", "info_url": nil},
	{"code": "komunal", "name": "Komunal", "description": "Productive financing for growing businesses.", "logo_url": nil,
		"min_amount": 5000000, "max_amount": 200000000, "tenors_months": []int{6, 12, 24}, "interest_info": "From 1.2% per month", "info_url": nil},
	{"code": "koinworks", "name": "KoinWorks", "description": "Business loans with a fast online process.", "logo_url": nil,
		"min_amount": 10000000, "max_amount": 500000000, "tenors_months": []int{6, 12, 24}, "interest_info": "From 1% per month", "info_url": "http://bit.ly/koinbisniskw"},
}

func (a *API) applyLoan(c fiber.Ctx) error {
	var in struct {
		ProviderCode string  `json:"provider_code"`
		Amount       float64 `json:"amount"`
		TenorMonths  int     `json:"tenor_months"`
		Purpose      string  `json:"purpose"`
		Consent      bool    `json:"consent"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	var prov fiber.Map
	for _, p := range loanProviders {
		if p["code"] == in.ProviderCode {
			prov = p
		}
	}
	v := &httpx.Validation{}
	if prov == nil {
		v.Add("provider_code", "Choose a lending partner.")
	} else {
		if in.Amount < float64(prov["min_amount"].(int)) || in.Amount > float64(prov["max_amount"].(int)) {
			v.Add("amount", "The amount is outside this partner's range.")
		}
		ok := false
		for _, t := range prov["tenors_months"].([]int) {
			ok = ok || t == in.TenorMonths
		}
		if !ok {
			v.Add("tenor_months", "Choose one of the offered tenors.")
		}
	}
	if strings.TrimSpace(in.Purpose) == "" {
		v.Add("purpose", "Required")
	}
	if !in.Consent {
		v.Add("consent", "Consent is required to share your data with the partner.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	p := httpx.P(c)
	if !p.IsOwner() {
		return httpx.Forbidden("Only the owner can apply for a loan.")
	}
	row, err := db.JSONRow(c.Context(), a.DB, `INSERT INTO loan_applications (store_id, provider_code, amount, tenor_months, purpose) VALUES ($1,$2,$3,$4,$5) RETURNING id, status`,
		p.StoreID, in.ProviderCode, in.Amount, in.TenorMonths, strings.TrimSpace(in.Purpose))
	if err != nil {
		return err
	}
	row["redirect_url"] = prov["info_url"]
	a.activity(c, "loan_applied", "Applied for a loan with "+in.ProviderCode)
	return httpx.OK(c, row)
}
