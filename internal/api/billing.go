package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/repositories"
)

var payChannels = []string{"virtual_account", "qris", "e_wallet", "credit_card", "convenience_store", "manual_transfer"}

func validPayMethod(m string, allowCoin bool) bool {
	if allowCoin && m == "coin" {
		return true
	}
	return contains(payChannels, m)
}

type plan struct {
	Code   string
	Months int
	Price  float64
}

var plans = map[string]plan{
	"pro_monthly":   {"pro", 1, 99000},
	"pro_quarterly": {"pro", 3, 279000},
	"pro_yearly":    {"pro", 12, 999000},
}

type pluginInfo struct {
	Code, Name, Description string
	Features                []string
	Monthly                 float64
}

var pluginCatalog = []pluginInfo{
	{"business", "Business", "Customer types, promotions, points, commissions, service charge, rounding and multi-branch tools.", []string{"Customer types & special prices", "Promotions & loyalty points", "Staff commissions", "Service charge & rounding", "Branch products & transfer confirmation"}, 49000},
	{"ingredients", "Raw Materials", "Recipes that deduct raw-material stock when a product is sold.", []string{"Recipes per product", "Unit conversion", "Raw-material stock reports"}, 29000},
	{"desktop", "Desktop POS", "Run the cashier on Windows and macOS.", []string{"Windows & macOS cashier", "Receipt and label printers"}, 39000},
	{"server", "Local Server", "Keep selling when the internet is down; sync when it is back.", []string{"Offline mode for several devices", "Automatic sync"}, 59000},
	{"food_menu", "Food Menu", "Add-ons, modifiers and a kitchen display for restaurants.", []string{"Add-ons & modifiers", "Kitchen order tickets", "Table management"}, 29000},
	{"attendance", "Staff Attendance", "Clock-in and clock-out with photo and location.", []string{"Selfie clock-in", "GPS location", "Attendance reports"}, 25000},
}

func pluginByCode(code string) *pluginInfo {
	for i := range pluginCatalog {
		if pluginCatalog[i].Code == code {
			return &pluginCatalog[i]
		}
	}
	return nil
}

func (a *API) registerBilling() {
	a.addH("GET", "/billing/invoices", a.listInvoices)
	a.addH("GET", "/billing/invoices/export", a.exportInvoices)
	a.addH("POST", "/billing/subscriptions", a.subscribe)
	a.addH("POST", "/billing/coin-top-ups", a.coinTopUp)
	a.addH("POST", "/billing/vouchers/redeem", a.redeemVoucher)
	a.addH("POST", "/billing/invoices/:id/payment-proof", a.invoiceProof)
	a.addH("POST", "/billing/invoices/:id/cancel", a.cancelInvoice)
	a.addH("GET", "/billing/invoices/:id/pdf", a.invoicePDF)
	a.addH("GET", "/plugins", a.listPlugins)
	a.addH("POST", "/plugins/:code/subscribe", a.subscribePlugin)
	a.addH("POST", "/plugins/:code/trial", a.trialPlugin)
}

const invoiceCols = `id, invoice_number, kind, description, payment_method, amount, admin_fee, status, payment_url, payment_instructions, payment_proof_url, expires_at, paid_at, created_at`

// createInvoice charges coins immediately (status paid) or asks the payment provider for a link.
func (a *API) createInvoice(c fiber.Ctx, kind, desc, method string, amount float64, promo string, meta map[string]any) (map[string]any, error) {
	p := helper.P(c)
	var out map[string]any
	err := a.tx(c.Context(), func(tx pgx.Tx) error {
		ctx := c.Context()
		if promo != "" {
			var typ string
			var val float64
			err := tx.QueryRow(ctx, `UPDATE vouchers SET used_count = used_count + 1 WHERE upper(code)=upper($1) AND reward_type='discount_percent' AND used_count < max_uses AND (expires_at IS NULL OR expires_at > now())
				RETURNING reward_type, reward_amount`, promo).Scan(&typ, &val)
			if err != nil {
				return helper.Invalid("promo_code", "This promo code is not valid.")
			}
			amount = round2(amount * (100 - val) / 100)
		}
		number, err := a.nextNumber(ctx, tx, "INV", "invoices", "invoice_number", "store_id", p.StoreID)
		if err != nil {
			return err
		}
		mb, _ := json.Marshal(meta)
		var id string
		if method == "coin" {
			if kind == "coin_topup" {
				return helper.Invalid("payment_method", "Coins cannot be bought with coins.")
			}
			if err := spendCoins(ctx, tx, p.StoreID, amount); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `INSERT INTO invoices (store_id, invoice_number, kind, description, payment_method, amount, status, meta, paid_at) VALUES ($1,$2,$3,$4,'coin',$5,'paid',$6::jsonb,now()) RETURNING id`,
				p.StoreID, number, kind, desc, amount, string(mb)).Scan(&id); err != nil {
				return err
			}
			if err := a.applyInvoice(ctx, tx, id); err != nil {
				return err
			}
		} else {
			fee := 0.0
			if method == "credit_card" {
				fee = round2(amount * 0.029)
			} else if method != "manual_transfer" {
				fee = 4000
			}
			url, instructions, err := a.Payments.Create(ctx, number, amount+fee, method, helper.Lang(c))
			if err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `INSERT INTO invoices (store_id, invoice_number, kind, description, payment_method, amount, admin_fee, status, payment_url, payment_instructions, meta, expires_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,'pending',NULLIF($8,''),$9,$10::jsonb, now() + interval '24 hours') RETURNING id`,
				p.StoreID, number, kind, desc, method, amount, fee, url, instructions, string(mb)).Scan(&id); err != nil {
				return err
			}
		}
		out, err = repositories.JSONRow(ctx, tx, `SELECT `+invoiceCols+`, id AS invoice_id FROM invoices WHERE id=$1`, id)
		return err
	})
	return out, err
}

// applyInvoice performs what was bought once an invoice is paid.
func (a *API) applyInvoice(ctx context.Context, tx pgx.Tx, id string) error {
	var storeID string
	var kind string
	var amount float64
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT store_id, kind, amount, meta FROM invoices WHERE id=$1`, id).Scan(&storeID, &kind, &amount, &raw); err != nil {
		return err
	}
	meta := map[string]any{}
	_ = json.Unmarshal(raw, &meta)
	months := int(num(meta["months"]))
	switch kind {
	case "coin_topup":
		_, err := tx.Exec(ctx, `UPDATE stores SET coin_balance = coin_balance + $2 WHERE id=$1`, storeID, amount)
		return err
	case "subscription":
		_, err := tx.Exec(ctx, `UPDATE stores SET plan = $2, plan_expires_at = GREATEST(COALESCE(plan_expires_at, now()), now()) + make_interval(months => $3), updated_at=now() WHERE id=$1`,
			storeID, fmt.Sprint(meta["plan"]), months)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE branches SET subscription_expires_at = (SELECT plan_expires_at FROM stores WHERE id=$1) WHERE store_id=$1 AND is_head_office`, storeID)
		}
		return err
	case "plugin":
		_, err := tx.Exec(ctx, `INSERT INTO store_plugins (store_id, code, status, trial_used, expires_at) VALUES ($1,$2,'active',false, now() + make_interval(months => $3))
			ON CONFLICT (store_id, code) DO UPDATE SET status='active', expires_at = GREATEST(COALESCE(store_plugins.expires_at, now()), now()) + make_interval(months => $3)`,
			storeID, fmt.Sprint(meta["code"]), months)
		return err
	case "branch_renewal":
		ids := []string{}
		if arr, ok := meta["branch_ids"].([]any); ok {
			for _, x := range arr {
				if id, ok := x.(string); ok && helper.IsUUID(id) {
					ids = append(ids, id)
				}
			}
		}
		_, err := tx.Exec(ctx, `UPDATE branches SET subscription_expires_at = GREATEST(COALESCE(subscription_expires_at, now()), now()) + make_interval(months => $3) WHERE store_id=$1 AND id = ANY($2)`,
			storeID, ids, months)
		return err
	}
	return nil
}

// markInvoicePaid is used by the payment webhook and admin tools.
func (a *API) markInvoicePaid(ctx context.Context, number string) (bool, error) {
	paid := false
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		var id, storeID string
		var desc string
		err := tx.QueryRow(ctx, `UPDATE invoices SET status='paid', paid_at=now(), updated_at=now() WHERE invoice_number=$1 AND status='pending' RETURNING id, store_id, description`, number).Scan(&id, &storeID, &desc)
		if repositories.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		paid = true
		if err := a.applyInvoice(ctx, tx, id); err != nil {
			return err
		}
		a.notify(ctx, tx, storeID, nil, "back_office", "Payment received", "{item} has been paid.", "billing", "item", desc)
		return nil
	})
	return paid, err
}

func (a *API) markDepositPaid(ctx context.Context, ref string) (bool, error) {
	paid := false
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		var id, branchID string
		var amount float64
		err := tx.QueryRow(ctx, `SELECT id, branch_id, amount FROM ppob_deposits WHERE reference_id=$1 AND status='pending' FOR UPDATE`, ref).Scan(&id, &branchID, &amount)
		if repositories.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		bal, err := ppobBalance(ctx, tx, branchID)
		if err != nil {
			return err
		}
		paid = true
		_, err = tx.Exec(ctx, `UPDATE ppob_deposits SET status='success', balance_after=$2, updated_at=now() WHERE id=$1`, id, bal+amount)
		return err
	})
	return paid, err
}

// paymentWebhook receives provider callbacks: {"reference":"INV/…","status":"paid"} signed with
// X-Signature = hex(HMAC-SHA256(JWT_SECRET or PAYMENT_WEBHOOK_SECRET, raw body)).
func (a *API) paymentWebhook(c fiber.Ctx) error {
	secret := []byte(a.Cfg.JWTSecret)
	m := hmac.New(sha256.New, secret)
	m.Write(c.Body())
	if !hmac.Equal([]byte(hex.EncodeToString(m.Sum(nil))), []byte(strings.ToLower(c.Get("X-Signature")))) {
		return helper.Unauthorized("invalid_signature", "Invalid signature.")
	}
	var in struct {
		Reference string `json:"reference"`
		Status    string `json:"status"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	if in.Status != "paid" {
		return helper.OK(c, fiber.Map{"handled": false})
	}
	ok, err := a.markInvoicePaid(c.Context(), in.Reference)
	if err == nil && !ok {
		ok, err = a.markDepositPaid(c.Context(), in.Reference)
	}
	if err != nil {
		return err
	}
	return helper.OK(c, fiber.Map{"handled": ok})
}

func (a *API) invoiceWhere(c fiber.Ctx) (string, []any, error) {
	p := helper.P(c)
	start, end, err := helper.DateRange(c, a.Loc)
	if err != nil {
		return "", nil, err
	}
	return `store_id=$1 AND created_at::date BETWEEN $2::date AND $3::date AND ($4 = '' OR status = $4) AND ($5 = '' OR invoice_number ILIKE '%'||$5||'%' OR description ILIKE '%'||$5||'%')`,
		[]any{p.StoreID, start, end, c.Query("status"), c.Query("q")}, nil
}

func (a *API) listInvoices(c fiber.Ctx) error {
	p := helper.P(c)
	// expire stale pending invoices
	_, _ = a.DB.Exec(c.Context(), `UPDATE invoices SET status='expired' WHERE store_id=$1 AND status='pending' AND expires_at < now()`, p.StoreID)
	where, args, err := a.invoiceWhere(c)
	if err != nil {
		return err
	}
	pg := helper.Paging(c, 50)
	var total int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM invoices WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	rows, err := repositories.JSONRaw(c.Context(), a.DB, `SELECT `+invoiceCols+` FROM invoices WHERE `+where+fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d OFFSET %d`, pg.PerPage, pg.Offset), args...)
	if err != nil {
		return err
	}
	summary, err := repositories.JSONRow(c.Context(), a.DB, `SELECT CASE plan WHEN 'pro' THEN 'Pro' WHEN 'pro_trial' THEN 'Pro (trial)' ELSE 'Free' END AS plan_name, plan_expires_at, coin_balance FROM stores WHERE id=$1`, p.StoreID)
	if err != nil {
		return err
	}
	return helper.ListWith(c, rows, helper.NewMeta(pg, total), helper.Extras{Summary: summary})
}

func (a *API) exportInvoices(c fiber.Ctx) error {
	where, args, err := a.invoiceWhere(c)
	if err != nil {
		return err
	}
	rows, err := repositories.JSONRows(c.Context(), a.DB, `SELECT `+invoiceCols+` FROM invoices WHERE `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return err
	}
	return export.Write(c, "invoices", "Invoices", []export.Column{{Key: "created_at", Label: "Date", Format: "datetime"}, {Key: "invoice_number", Label: "Invoice"}, {Key: "description", Label: "For"},
		{Key: "payment_method", Label: "Method"}, {Key: "amount", Label: "Amount", Format: "money"}, {Key: "status", Label: "Status"}}, rows)
}

func (a *API) subscribe(c fiber.Ctx) error {
	var in struct {
		Plan          string `json:"plan"`
		PaymentMethod string `json:"payment_method"`
		PromoCode     string `json:"promo_code"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	pl, ok := plans[in.Plan]
	v := &helper.Validation{}
	if !ok {
		v.Add("plan", "Choose a plan.")
	}
	if !validPayMethod(in.PaymentMethod, true) {
		v.Add("payment_method", "Choose a payment method.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	inv, err := a.createInvoice(c, "subscription", helper.T(c, "Pro plan, {n} month(s)", "n", pl.Months), in.PaymentMethod, pl.Price, in.PromoCode, map[string]any{"plan": pl.Code, "months": pl.Months})
	if err != nil {
		return err
	}
	return helper.OK(c, inv)
}

func (a *API) coinTopUp(c fiber.Ctx) error {
	var in struct {
		Amount        float64 `json:"amount"`
		PaymentMethod string  `json:"payment_method"`
		PromoCode     string  `json:"promo_code"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	v := &helper.Validation{}
	if in.Amount < 10000 {
		v.Add("amount", "The minimum top-up is Rp 10,000.")
	}
	if !validPayMethod(in.PaymentMethod, false) {
		v.Add("payment_method", "Choose a payment method.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	inv, err := a.createInvoice(c, "coin_topup", helper.T(c, "Premium coin top-up"), in.PaymentMethod, in.Amount, in.PromoCode, map[string]any{})
	if err != nil {
		return err
	}
	return helper.OK(c, inv)
}

func (a *API) redeemVoucher(c fiber.Ctx) error {
	var in struct {
		Code string `json:"code"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	p := helper.P(c)
	var out map[string]any
	err := a.tx(c.Context(), func(tx pgx.Tx) error {
		ctx := c.Context()
		var code, typ string
		var amount float64
		err := tx.QueryRow(ctx, `UPDATE vouchers SET used_count = used_count + 1 WHERE upper(code)=upper($1) AND reward_type <> 'discount_percent' AND used_count < max_uses AND (expires_at IS NULL OR expires_at > now())
			RETURNING code, reward_type, reward_amount`, strings.TrimSpace(in.Code)).Scan(&code, &typ, &amount)
		if err != nil {
			return helper.Invalid("code", "This voucher is not valid or has been used up.")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO voucher_redemptions (code, store_id) VALUES ($1,$2)`, code, p.StoreID); err != nil {
			if repositories.IsUniqueViolation(err) {
				return helper.Invalid("code", "You have already used this voucher.")
			}
			return err
		}
		switch typ {
		case "coin":
			_, err = tx.Exec(ctx, `UPDATE stores SET coin_balance = coin_balance + $2 WHERE id=$1`, p.StoreID, amount)
		case "subscription_days":
			_, err = tx.Exec(ctx, `UPDATE stores SET plan='pro', plan_expires_at = GREATEST(COALESCE(plan_expires_at, now()), now()) + make_interval(days => $2::int) WHERE id=$1`, p.StoreID, int(amount))
		case "plugin":
			_, err = tx.Exec(ctx, `INSERT INTO store_plugins (store_id, code, status, expires_at) VALUES ($1,'business','active', now() + make_interval(days => $2::int))
				ON CONFLICT (store_id, code) DO UPDATE SET status='active', expires_at = GREATEST(COALESCE(store_plugins.expires_at, now()), now()) + make_interval(days => $2::int)`, p.StoreID, int(amount))
		}
		if err != nil {
			return err
		}
		var bal float64
		if err := tx.QueryRow(ctx, `SELECT coin_balance FROM stores WHERE id=$1`, p.StoreID).Scan(&bal); err != nil {
			return err
		}
		out = map[string]any{"code": code, "reward_type": typ, "reward_amount": amount, "coin_balance": bal}
		return nil
	})
	if err != nil {
		return err
	}
	return helper.OK(c, out)
}

func (a *API) invoice(c fiber.Ctx, id string) (map[string]any, error) {
	row, err := repositories.JSONRow(c.Context(), a.DB, `SELECT `+invoiceCols+` FROM invoices WHERE id=$1 AND store_id=$2`, id, helper.P(c).StoreID)
	if err == nil && row == nil {
		err = helper.NotFound("Invoice")
	}
	return row, err
}

func (a *API) invoiceProof(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	inv, err := a.invoice(c, id)
	if err != nil {
		return err
	}
	if inv["status"] != "pending" {
		return helper.Conflict("Only a pending invoice accepts a payment proof.")
	}
	up, err := a.saveUpload(c, "file", "payment_proof")
	if err != nil {
		return err
	}
	if _, err := a.DB.Exec(c.Context(), `UPDATE invoices SET payment_proof_url=$2, sender_account_name=NULLIF($3,''), updated_at=now() WHERE id=$1`, id, up["url"], c.FormValue("sender_account_name")); err != nil {
		return err
	}
	a.notify(c.Context(), a.DB, helper.P(c).StoreID, nil, "back_office", "Payment proof received", "We are checking your transfer for {number}.", "billing", "number", fmt.Sprint(inv["invoice_number"]))
	inv, err = a.invoice(c, id)
	if err != nil {
		return err
	}
	return helper.OK(c, inv)
}

func (a *API) cancelInvoice(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	tag, err := a.DB.Exec(c.Context(), `UPDATE invoices SET status='cancelled', updated_at=now() WHERE id=$1 AND store_id=$2 AND status='pending'`, id, helper.P(c).StoreID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return helper.Conflict("Only a pending invoice can be cancelled.")
	}
	inv, err := a.invoice(c, id)
	if err != nil {
		return err
	}
	return helper.OK(c, inv)
}

func (a *API) invoicePDF(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	inv, err := a.invoice(c, id)
	if err != nil {
		return err
	}
	var storeName string
	_ = a.DB.QueryRow(c.Context(), `SELECT name FROM stores WHERE id=$1`, helper.P(c).StoreID).Scan(&storeName)
	d := export.NewPDF(595, 842)
	tr := helper.Lang(c).T
	d.Page()
	d.Text(40, 790, 20, true, tr("INVOICE"))
	d.Text(40, 766, 10, false, fmt.Sprint(inv["invoice_number"]))
	d.Text(360, 790, 10, true, "Lestari")
	d.Text(360, 776, 9, false, tr("Billed to")+": "+storeName)
	d.Text(360, 762, 9, false, tr("Date")+": "+formatTime(inv["created_at"], a.Loc))
	d.Text(360, 748, 9, false, tr("Status")+": "+strings.ToUpper(tr(fmt.Sprint(inv["status"]))))
	y := 700.0
	d.Rect(40, y-4, 515, 16, 0.92)
	d.Text(46, y, 9, true, tr("Description"))
	d.TextRight(549, y, 9, true, tr("Amount (Rp)"))
	y -= 22
	d.Text(46, y, 10, false, fmt.Sprint(inv["description"]))
	d.TextRight(549, y, 10, false, export.Thousands(num(inv["amount"])))
	if fee := num(inv["admin_fee"]); fee > 0 {
		y -= 16
		d.Text(46, y, 10, false, tr("Admin fee"))
		d.TextRight(549, y, 10, false, export.Thousands(fee))
	}
	y -= 12
	d.Line(40, y, 555)
	y -= 18
	d.Text(380, y, 11, true, tr("Total"))
	d.TextRight(549, y, 11, true, export.Thousands(num(inv["amount"])+num(inv["admin_fee"])))
	d.Text(40, 120, 8, false, tr("Paid with {method} on {date}.", "method", i18n.Tr(fmt.Sprint(inv["payment_method"])), "date", formatTime(inv["paid_at"], a.Loc)))
	return export.Send(c, "invoice-"+strings.ReplaceAll(fmt.Sprint(inv["invoice_number"]), "/", "-")+".pdf", "application/pdf", d.Bytes())
}

// ---------- plugins ----------

func (a *API) listPlugins(c fiber.Ctx) error {
	p := helper.P(c)
	rows, err := a.DB.Query(c.Context(), `SELECT code, status, trial_used, expires_at FROM store_plugins WHERE store_id=$1`, p.StoreID)
	if err != nil {
		return err
	}
	type st struct {
		status    string
		trialUsed bool
		expires   *time.Time
	}
	state := map[string]st{}
	for rows.Next() {
		var code string
		var s st
		if err := rows.Scan(&code, &s.status, &s.trialUsed, &s.expires); err != nil {
			rows.Close()
			return err
		}
		state[code] = s
	}
	rows.Close()
	out := make([]fiber.Map, 0, len(pluginCatalog))
	for _, pl := range pluginCatalog {
		s, ok := state[pl.Code]
		status := "inactive"
		var expires any
		if ok {
			status = s.status
			if s.expires != nil {
				expires = s.expires
				if s.expires.Before(time.Now()) {
					status = "expired"
				}
			}
		}
		features := make([]string, len(pl.Features))
		for i, f := range pl.Features {
			features[i] = helper.T(c, f)
		}
		out = append(out, fiber.Map{"code": pl.Code, "name": helper.T(c, pl.Name), "description": helper.T(c, pl.Description), "features": features,
			"price_monthly": pl.Monthly, "price_yearly": pl.Monthly * 10, "status": status, "trial_available": !ok || !s.trialUsed, "expires_at": expires})
	}
	return helper.OK(c, out)
}

func (a *API) subscribePlugin(c fiber.Ctx) error {
	pl := pluginByCode(c.Params("code"))
	if pl == nil {
		return helper.NotFound("Plugin")
	}
	var in struct {
		Period        string `json:"period"`
		PaymentMethod string `json:"payment_method"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	months := map[string]int{"monthly": 1, "yearly": 12}[in.Period]
	v := &helper.Validation{}
	if months == 0 {
		v.Add("period", "Must be one of: monthly, yearly.")
	}
	if !validPayMethod(in.PaymentMethod, true) {
		v.Add("payment_method", "Choose a payment method.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	price := pl.Monthly
	if months == 12 {
		price = pl.Monthly * 10
	}
	inv, err := a.createInvoice(c, "plugin", helper.T(c, "{plugin} plugin, {n} month(s)", "plugin", i18n.Tr(pl.Name), "n", months), in.PaymentMethod, price, "", map[string]any{"code": pl.Code, "months": months})
	if err != nil {
		return err
	}
	return helper.OK(c, inv)
}

func (a *API) trialPlugin(c fiber.Ctx) error {
	pl := pluginByCode(c.Params("code"))
	if pl == nil {
		return helper.NotFound("Plugin")
	}
	p := helper.P(c)
	var out map[string]any
	err := a.tx(c.Context(), func(tx pgx.Tx) error {
		var used bool
		err := tx.QueryRow(c.Context(), `SELECT trial_used OR status = 'active' FROM store_plugins WHERE store_id=$1 AND code=$2`, p.StoreID, pl.Code).Scan(&used)
		if err == nil && used {
			return helper.Conflict("The free trial for this plugin was already used.")
		}
		out, err = repositories.JSONRow(c.Context(), tx, `INSERT INTO store_plugins (store_id, code, status, trial_used, expires_at) VALUES ($1,$2,'trial',true, now() + interval '7 days')
			ON CONFLICT (store_id, code) DO UPDATE SET status='trial', trial_used=true, expires_at = now() + interval '7 days'
			RETURNING code, status, expires_at`, p.StoreID, pl.Code)
		return err
	})
	if err != nil {
		return err
	}
	return helper.OK(c, out)
}
