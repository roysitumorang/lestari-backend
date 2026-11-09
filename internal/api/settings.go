package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/export"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
	"github.com/roysitumorang/lestari-backend/internal/i18n"
)

type settingDef struct {
	Key      string
	Plugin   string // plugin needed to change it
	Fields   []crud.Field
	Defaults map[string]any
	Validate func(ctx context.Context, a *API, p *httpx.Principal, vals map[string]any) error
	After    func(ctx context.Context, tx pgx.Tx, p *httpx.Principal, vals map[string]any) error
}

func bools(names ...string) []crud.Field {
	out := make([]crud.Field, len(names))
	for i, n := range names {
		out[i] = crud.Field{Name: n, Kind: crud.Bool}
	}
	return out
}

var slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])?$`)

var receiptFlags = []string{"show_logo", "auto_print", "show_receipt_number", "show_receipt_code", "show_sequence_number", "show_unit", "show_customer_address",
	"show_total_quantity", "show_signature", "show_price_type", "separate_admin_fee", "show_points", "hide_tax_percentage", "show_feedback_link", "show_qr_code"}

var couriers = en("anteraja", "tiki", "esl", "jnt", "jne", "lion", "ncs", "pahala", "pos", "rpx", "sicepat", "wahana")

func (a *API) settingDefs() map[string]*settingDef {
	defs := []*settingDef{
		{Key: "points", Plugin: "business",
			Fields: append(bools("is_enabled", "earn_on_credit_sales"),
				crud.Field{Name: "earn_spend_amount", Kind: crud.Num, Min: new(float64(0))}, crud.Field{Name: "earn_points", Kind: crud.Num, Min: new(float64(0))},
				crud.Field{Name: "redeem_point_value", Kind: crud.Num, Min: new(float64(0))}, crud.Field{Name: "minimum_redeem_points", Kind: crud.Num, Min: new(float64(0))},
				crud.Field{Name: "expiry_days", Kind: crud.Int, Min: new(float64(0))}),
			Defaults: map[string]any{"is_enabled": false, "earn_spend_amount": 10000.0, "earn_points": 1.0, "redeem_point_value": 100.0, "minimum_redeem_points": 0.0, "expiry_days": 0.0, "earn_on_credit_sales": false}},
		{Key: "store",
			Fields: []crud.Field{{Name: "logo_url"}, {Name: "cover_url"}, {Name: "name", Required: true}, {Name: "owner_name"}, {Name: "phone"}, {Name: "motto"},
				{Name: "business_type"}, {Name: "business_type_other"}, {Name: "address"}, {Name: "country_code", Enum: en("ID", "MY", "SG", "TL")},
				{Name: "province_id", Kind: crud.UUID}, {Name: "city_id", Kind: crud.UUID}, {Name: "postal_code"},
				{Name: "latitude", Kind: crud.Num, Min: new(float64(-90)), Max: new(float64(90))}, {Name: "longitude", Kind: crud.Num, Min: new(float64(-180)), Max: new(float64(180))},
				{Name: "stock_valuation_method", Enum: en("default", "fifo", "lifo", "average")},
				{Name: "currency", Enum: en("IDR", "MYR", "SGD", "USD", "AUD", "EUR")}, {Name: "timezone", Enum: en("Asia/Jakarta", "Asia/Makassar", "Asia/Jayapura")},
				{Name: "allow_negative_stock", Kind: crud.Bool}},
			Defaults: map[string]any{"name": "", "country_code": "ID", "currency": "IDR", "timezone": "Asia/Jakarta", "stock_valuation_method": "average", "allow_negative_stock": false},
			After: func(ctx context.Context, tx pgx.Tx, p *httpx.Principal, vals map[string]any) error {
				name, _ := vals["name"].(string)
				if name == "" {
					return nil
				}
				_, err := tx.Exec(ctx, `UPDATE branches SET name = CASE WHEN is_head_office THEN name ELSE $2 END, phone = COALESCE(NULLIF($3,''), phone), address = COALESCE(NULLIF($4,''), address), updated_at=now() WHERE id=$1`,
					p.BranchID, name, strOr(vals["phone"], ""), strOr(vals["address"], ""))
				if err == nil && p.BranchID == p.HeadOffice {
					_, err = tx.Exec(ctx, `UPDATE stores SET name=$2, updated_at=now() WHERE id=$1`, p.StoreID, name)
				}
				return err
			}},
		{Key: "online-store",
			Fields: append(bools("is_enabled", "seo_enabled", "pwa_enabled", "use_physical_stock", "notify_owner"),
				crud.Field{Name: "subdomain"}, crud.Field{Name: "store_email"}, crud.Field{Name: "description"}, crud.Field{Name: "facebook"}, crud.Field{Name: "twitter"}, crud.Field{Name: "instagram"},
				crud.Field{Name: "notify_staff_ids", Kind: crud.UUIDArray}),
			Defaults: map[string]any{"is_enabled": false, "seo_enabled": true, "pwa_enabled": false, "use_physical_stock": true, "notify_owner": true, "notify_staff_ids": []any{}},
			Validate: func(ctx context.Context, a *API, p *httpx.Principal, vals map[string]any) error {
				return a.uniqueSlug(ctx, p, "online-store", "subdomain", vals)
			}},
		{Key: "link-page",
			Fields: append(bools("is_enabled", "show_watermark"), crud.Field{Name: "username", Required: true}, crud.Field{Name: "slogan"},
				crud.Field{Name: "background_color"}, crud.Field{Name: "links", Kind: crud.JSON}),
			Defaults: map[string]any{"is_enabled": false, "username": "", "show_watermark": true, "background_color": "#0f9d76", "links": []any{}},
			Validate: func(ctx context.Context, a *API, p *httpx.Principal, vals map[string]any) error {
				return a.uniqueSlug(ctx, p, "link-page", "username", vals)
			}},
		{Key: "qr-order",
			Fields: append(bools("is_enabled", "qris_enabled", "cashier_payment_enabled", "addons_enabled", "table_ordering_enabled", "kiosk_enabled", "notify_owner"),
				crud.Field{Name: "order_expiry_hours", Kind: crud.Int, Enum: en("3", "6", "12", "24")}, crud.Field{Name: "payment_timing", Enum: en("pay_later", "pay_now")},
				crud.Field{Name: "menu_layout", Enum: en("tabs", "by_category")}, crud.Field{Name: "category_ids", Kind: crud.UUIDArray}, crud.Field{Name: "notify_staff_ids", Kind: crud.UUIDArray}),
			Defaults: map[string]any{"is_enabled": false, "order_expiry_hours": 3.0, "payment_timing": "pay_later", "qris_enabled": false, "cashier_payment_enabled": true,
				"addons_enabled": false, "menu_layout": "tabs", "category_ids": []any{}, "table_ordering_enabled": false, "kiosk_enabled": false, "notify_owner": true, "notify_staff_ids": []any{}}},
		{Key: "default-tax",
			Fields:   append(bools("is_enabled", "is_inclusive"), crud.Field{Name: "rate", Kind: crud.Num, Min: new(float64(0)), Max: new(float64(100))}),
			Defaults: map[string]any{"is_enabled": false, "rate": 0.0, "is_inclusive": false}},
		{Key: "service-charge", Plugin: "business",
			Fields:   append(bools("is_enabled", "apply_before_tax"), crud.Field{Name: "name"}, crud.Field{Name: "rate", Kind: crud.Num, Min: new(float64(0)), Max: new(float64(100))}),
			Defaults: map[string]any{"is_enabled": false, "name": "Service charge", "rate": 0.0, "apply_before_tax": true}},
		{Key: "rounding", Plugin: "business",
			Fields:   append(bools("is_enabled"), crud.Field{Name: "mode", Enum: en("nearest", "up", "down")}, crud.Field{Name: "multiple", Kind: crud.Int, Enum: en("50", "100", "500", "1000")}),
			Defaults: map[string]any{"is_enabled": false, "mode": "nearest", "multiple": 100.0}},
		{Key: "receipt",
			Fields: append(bools(receiptFlags...), crud.Field{Name: "logo_url"}, crud.Field{Name: "header_note"}, crud.Field{Name: "footer_note"}),
			Defaults: func() map[string]any {
				m := map[string]any{"footer_note": "Thank you for shopping with us!"}
				for _, f := range receiptFlags {
					m[f] = f == "show_logo" || f == "show_receipt_number" || f == "show_total_quantity"
				}
				return m
			}()},
		{Key: "display",
			Fields:   []crud.Field{{Name: "color_theme", Enum: en("emerald", "ivory", "charcoal")}, {Name: "show_breadcrumbs", Kind: crud.Bool}, {Name: "language", Enum: en("en", "id")}},
			Defaults: map[string]any{"color_theme": "emerald", "show_breadcrumbs": true, "language": "en"}},
		{Key: "catalog",
			Fields: append(bools("bank_transfer_enabled", "direct_payment_enabled", "takeaway_enabled", "cod_enabled", "courier_enabled", "own_courier_enabled"),
				crud.Field{Name: "couriers", Kind: crud.StrArray, Enum: couriers}, crud.Field{Name: "own_courier_mode", Enum: en("city", "radius")},
				crud.Field{Name: "own_courier_city_rates", Kind: crud.JSON}, crud.Field{Name: "own_courier_radius_rates", Kind: crud.JSON}),
			Defaults: map[string]any{"bank_transfer_enabled": true, "direct_payment_enabled": true, "takeaway_enabled": true, "cod_enabled": false, "courier_enabled": false,
				"couriers": []any{}, "own_courier_enabled": false, "own_courier_mode": "city", "own_courier_city_rates": []any{}, "own_courier_radius_rates": []any{}}},
	}
	out := map[string]*settingDef{}
	for _, d := range defs {
		out[d.Key] = d
	}
	return out
}

// setting returns a branch setting merged over its defaults.
func (a *API) setting(ctx context.Context, q db.Querier, branchID string, key string) (map[string]any, error) {
	out := map[string]any{}
	if d, ok := a.defs[key]; ok {
		maps.Copy(out, d.Defaults)
	}
	var raw []byte
	err := q.QueryRow(ctx, `SELECT value FROM settings WHERE branch_id=$1 AND key=$2`, branchID, key).Scan(&raw)
	if err != nil {
		if db.IsNoRows(err) {
			return out, nil
		}
		return out, err
	}
	stored := map[string]any{}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return out, err
	}
	maps.Copy(out, stored)
	return out, nil
}

func (a *API) uniqueSlug(ctx context.Context, p *httpx.Principal, key, field string, vals map[string]any) error {
	s, _ := vals[field].(string)
	if s == "" {
		return nil
	}
	s = strings.ToLower(s)
	vals[field] = s
	if !slugRe.MatchString(s) {
		return httpx.Invalid(field, "Use 3–40 lowercase letters, numbers or dashes.")
	}
	var taken bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM settings WHERE key=$1 AND lower(value->>$2)=$3 AND branch_id <> $4)`, key, field, s, p.BranchID).Scan(&taken); err != nil {
		return err
	}
	if taken {
		return httpx.Invalid(field, "This name is already taken.")
	}
	return nil
}

func (a *API) getSetting(key string) fiber.Handler {
	return func(c fiber.Ctx) error {
		m, err := a.setting(c.Context(), a.DB, httpx.P(c).BranchID, key)
		if err != nil {
			return err
		}
		return httpx.OK(c, m)
	}
}

func (a *API) putSetting(key string) fiber.Handler {
	d := a.defs[key]
	res := &crud.Resource{Singular: "Setting", Fields: d.Fields}
	return func(c fiber.Ctx) error {
		p := httpx.P(c)
		if d.Plugin != "" {
			if err := requirePlugin(c, d.Plugin); err != nil {
				return err
			}
		}
		if p.Role == "cashier" {
			return httpx.Forbidden("Cashiers cannot change settings.")
		}
		body, err := httpx.Body(c)
		if err != nil {
			return err
		}
		vals, err := res.Values(body, "settings") // non-empty id = partial update: only attributes present are written
		if err != nil {
			return err
		}
		// required fields must not be cleared
		v := &httpx.Validation{}
		for _, f := range d.Fields {
			if f.Required {
				if x, present := vals[f.Name]; present && x == nil {
					v.Add(f.Name, "Required")
				}
			}
		}
		if err := v.Err(); err != nil {
			return err
		}
		if d.Validate != nil {
			if err := d.Validate(c.Context(), a, p, vals); err != nil {
				return err
			}
		}
		cur, err := a.setting(c.Context(), a.DB, p.BranchID, key)
		if err != nil {
			return err
		}
		maps.Copy(cur, vals)
		b, _ := json.Marshal(cur)
		err = a.tx(c.Context(), func(tx pgx.Tx) error {
			if _, err := tx.Exec(c.Context(), `INSERT INTO settings (branch_id, key, value) VALUES ($1,$2,$3::jsonb)
				ON CONFLICT (branch_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, p.BranchID, key, string(b)); err != nil {
				return err
			}
			if d.After != nil {
				return d.After(c.Context(), tx, p, cur)
			}
			return nil
		})
		if err != nil {
			return err
		}
		a.activity(c, "settings_updated", "Updated {key} settings", "key", key)
		return a.getSetting(key)(c)
	}
}

func (a *API) registerSettings() {
	a.defs = a.settingDefs()
	for key := range a.defs {
		if key == "catalog" {
			continue // registered by the online catalog module
		}
		a.addH("GET", "/settings/"+key, a.getSetting(key))
		a.addH("PUT", "/settings/"+key, a.putSetting(key))
	}
	a.addH("GET", "/settings/store/qr-code", a.storeQRCode)
	a.addH("GET", "/settings/link-page/visitors", a.linkPageVisitors)
	a.addH("GET", "/settings/link-page/username-availability", func(c fiber.Ctx) error {
		u := strings.ToLower(strings.TrimSpace(c.Query("username")))
		if !slugRe.MatchString(u) {
			return httpx.OK(c, fiber.Map{"available": false})
		}
		var taken bool
		err := a.DB.QueryRow(c.Context(), `SELECT EXISTS(SELECT 1 FROM settings WHERE key='link-page' AND lower(value->>'username')=$1 AND branch_id <> $2)`, u, httpx.P(c).BranchID).Scan(&taken)
		if err != nil {
			return err
		}
		return httpx.OK(c, fiber.Map{"available": !taken})
	})
	a.addH("GET", "/settings/qr-order/menu-book", a.menuBook)
	a.addH("GET", "/settings/storage", a.storage)
	a.addH("POST", "/settings/storage/purchases", a.buyStorage)
	a.addH("POST", "/settings/reset-data/otp", a.resetOTP)
	a.addH("POST", "/settings/reset-data", a.resetData)
}

func (a *API) storeQRCode(c fiber.Ctx) error {
	p := httpx.P(c)
	lp, _ := a.setting(c.Context(), a.DB, p.BranchID, "link-page")
	os, _ := a.setting(c.Context(), a.DB, p.BranchID, "online-store")
	url := a.Cfg.PublicURL + "/s/" + p.BranchID
	if u := strOr(lp["username"], ""); u != "" {
		url = a.Cfg.PublicURL + "/l/" + u
	} else if s := strOr(os["subdomain"], ""); s != "" {
		url = a.Cfg.PublicURL + "/shop/" + s
	}
	png, err := qrcode.Encode(url, qrcode.Medium, 512)
	if err != nil {
		return err
	}
	return export.Send(c, "store-qr.png", "image/png", png)
}

func (a *API) linkPageVisitors(c fiber.Ctx) error {
	p := httpx.P(c)
	pg := httpx.Pagination(c, 50)
	var total int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM link_page_visits WHERE branch_id=$1`, p.BranchID).Scan(&total); err != nil {
		return err
	}
	raw, err := db.JSONRaw(c.Context(), a.DB, `SELECT ip_address, visited_at, referrer FROM link_page_visits WHERE branch_id=$1 ORDER BY visited_at DESC LIMIT $2 OFFSET $3`, p.BranchID, pg.PerPage, pg.Offset)
	if err != nil {
		return err
	}
	return httpx.List(c, raw, httpx.NewMeta(pg, total))
}

func (a *API) menuBook(c fiber.Ctx) error {
	p := httpx.P(c)
	qo, _ := a.setting(c.Context(), a.DB, p.BranchID, "qr-order")
	ids := []string{}
	if arr, ok := qo["category_ids"].([]any); ok {
		for _, x := range arr {
			if id, ok := x.(string); ok && httpx.IsUUID(id) {
				ids = append(ids, id)
			}
		}
	}
	rows, err := db.JSONRows(c.Context(), a.DB, `SELECT COALESCE(c.name, 'Other') AS category, p.name, p.selling_price, p.online_description
		FROM products p LEFT JOIN categories c ON c.id = p.category_id
		WHERE p.branch_id=$1 AND p.deleted_at IS NULL AND p.show_in_online_store AND (cardinality($2::uuid[]) = 0 OR p.category_id = ANY($2))
		ORDER BY c.sort_order NULLS LAST, c.name NULLS LAST, p.name`, p.BranchID, ids)
	if err != nil {
		return err
	}
	name, _, _ := a.storeHeader(c.Context(), p.BranchID)
	d := export.NewPDF(595, 842)
	tr := httpx.Lang(c).T
	d.Page()
	y := 790.0
	d.Text(40, y, 20, true, name)
	y -= 22
	d.Text(40, y, 11, false, tr("Menu"))
	y -= 30
	last := ""
	for _, r := range rows {
		if y < 60 {
			d.Page()
			y = 800
		}
		if cat := fmt.Sprint(r["category"]); cat != last {
			y -= 6
			d.Text(40, y, 13, true, cat)
			y -= 20
			last = cat
		}
		d.Text(52, y, 10.5, false, export.Fit(fmt.Sprint(r["name"]), 380, 10.5))
		d.TextRight(555, y, 10.5, true, "Rp "+export.Thousands(num(r["selling_price"])))
		y -= 13
		if desc := strOr(r["online_description"], ""); desc != "" {
			d.Text(52, y, 8, false, export.Fit(desc, 480, 8))
			y -= 12
		}
		y -= 4
	}
	return export.Send(c, "menu.pdf", "application/pdf", d.Bytes())
}

// ---------- storage ----------

var storagePackages = map[string]struct {
	MB    float64
	Price float64
}{"1gb_monthly": {1024, 15000}, "5gb_monthly": {5120, 50000}, "10gb_monthly": {10240, 90000}}

func (a *API) storage(c fiber.Ctx) error {
	p := httpx.P(c)
	row, err := db.JSONRow(c.Context(), a.DB, `SELECT s.plan,
		round(((SELECT COALESCE(sum(size),0) FROM uploads WHERE store_id = s.id) / 1048576.0
		  + (SELECT count(*) FROM sales sa JOIN branches b ON b.id = sa.branch_id WHERE b.store_id = s.id) * 0.004
		  + (SELECT count(*) FROM products pr JOIN branches b ON b.id = pr.branch_id WHERE b.store_id = s.id) * 0.002)::numeric, 2) AS used_mb,
		s.storage_quota_mb AS quota_mb,
		(SELECT count(*) FROM sales sa JOIN branches b ON b.id = sa.branch_id WHERE b.store_id = s.id) AS transaction_count,
		s.storage_extra_expires_at AS expires_at
		FROM stores s WHERE s.id = $1`, p.StoreID)
	if err != nil {
		return err
	}
	return httpx.OK(c, row)
}

func (a *API) buyStorage(c fiber.Ctx) error {
	var in struct {
		Package       string `json:"package"`
		PaymentMethod string `json:"payment_method"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	pkg, ok := storagePackages[in.Package]
	if !ok {
		return httpx.Invalid("package", "Unknown storage package.")
	}
	if in.PaymentMethod != "" && in.PaymentMethod != "coin" {
		return httpx.Invalid("payment_method", "Storage can only be paid with premium coins.")
	}
	p := httpx.P(c)
	var out map[string]any
	err := a.tx(c.Context(), func(tx pgx.Tx) error {
		if err := spendCoins(c.Context(), tx, p.StoreID, pkg.Price); err != nil {
			return err
		}
		var err error
		out, err = db.JSONRow(c.Context(), tx, `UPDATE stores SET storage_quota_mb = 500 + $2,
			storage_extra_expires_at = GREATEST(COALESCE(storage_extra_expires_at, current_date), current_date) + 30, updated_at = now()
			WHERE id=$1 RETURNING storage_quota_mb AS quota_mb, storage_extra_expires_at AS expires_at, coin_balance`, p.StoreID, pkg.MB)
		if err != nil {
			return err
		}
		num, err := a.nextNumber(c.Context(), tx, "INV", "invoices", "invoice_number", "store_id", p.StoreID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(c.Context(), `INSERT INTO invoices (store_id, invoice_number, kind, description, payment_method, amount, status, paid_at) VALUES ($1,$2,'storage',$3,'coin',$4,'paid',now())`,
			p.StoreID, num, "Extra storage "+in.Package, pkg.Price)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(c, out)
}

// spendCoins deducts premium coins or fails with 402.
func spendCoins(ctx context.Context, tx pgx.Tx, storeID string, amount float64) error {
	tag, err := tx.Exec(ctx, `UPDATE stores SET coin_balance = coin_balance - $2 WHERE id=$1 AND coin_balance >= $2`, storeID, amount)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.PaymentRequired("insufficient_balance", "Your premium coin balance is too low. Top up first.")
	}
	return nil
}

// ---------- reset data ----------

func (a *API) resetOTP(c fiber.Ctx) error {
	p := httpx.P(c)
	if !p.IsOwner() {
		return httpx.Forbidden("Only the owner can reset data.")
	}
	var in struct {
		Channel string `json:"channel"`
	}
	_ = httpx.Decode(c, &in)
	var email string
	var phone *string
	if err := a.DB.QueryRow(c.Context(), `SELECT email, phone FROM users WHERE id=$1`, p.UserID).Scan(&email, &phone); err != nil {
		return err
	}
	code, err := a.createOTP(c.Context(), p.UserID, "reset_data", "")
	if err != nil {
		return err
	}
	to, channel := email, "email"
	if in.Channel == "whatsapp" && phone != nil && *phone != "" {
		to, channel = *phone, "whatsapp"
	}
	if err := a.Sender.Send(c.Context(), channel, to, httpx.T(c, "Your Lestari data-reset code is {code}. Do not share it.", "code", code)); err != nil {
		return err
	}
	return httpx.OK(c, fiber.Map{"expires_in": 300, "sent_to": mask(to)})
}

func mask(s string) string {
	if at := strings.Index(s, "@"); at > 1 {
		return s[:1] + strings.Repeat("*", at-1) + s[at:]
	}
	if len(s) > 4 {
		return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
	}
	return s
}

var resetTypes = en("images", "products", "sales", "purchases", "customers", "suppliers", "staff", "taxes", "discounts", "categories", "fees", "promotions", "shifts", "points", "held_transactions", "online_orders")

func (a *API) resetData(c fiber.Ctx) error {
	p := httpx.P(c)
	if !p.IsOwner() {
		return httpx.Forbidden("Only the owner can reset data.")
	}
	var in struct {
		DataTypes []string `json:"data_types"`
		Scope     string   `json:"scope"`
		Year      int      `json:"year"`
		Month     int      `json:"month"`
		OTP       string   `json:"otp"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	v := &httpx.Validation{}
	if len(in.DataTypes) == 0 {
		v.Add("data_types", "Choose what to delete.")
	}
	for _, t := range in.DataTypes {
		if !contains(resetTypes, t) {
			v.Add("data_types", "Unknown data type {type}.", "type", t)
		}
	}
	if in.Scope == "" {
		in.Scope = "all"
	}
	switch in.Scope {
	case "all":
	case "year", "month":
		if in.Year < 2000 || in.Year > 2100 {
			v.Add("year", "Choose a year.")
		}
		if in.Scope == "month" && (in.Month < 1 || in.Month > 12) {
			v.Add("month", "Choose a month.")
		}
	default:
		v.Add("scope", "Must be one of: all, year, month.")
	}
	if strings.TrimSpace(in.OTP) == "" {
		v.Add("otp", "Required")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if err := a.checkOTP(c.Context(), p.UserID, "reset_data", "", in.OTP); err != nil {
		he := &httpx.Error{}
		if errors.As(err, &he) {
			he.Fields = map[string][]i18n.Text{"otp": he.Fields["code"]}
		}
		return err
	}
	branch, store := p.BranchID, p.StoreID
	start, end := "1900-01-01", "2999-12-31"
	switch in.Scope {
	case "year":
		start, end = fmt.Sprintf("%d-01-01", in.Year), fmt.Sprintf("%d-12-31", in.Year)
	case "month":
		start = fmt.Sprintf("%d-%02d-01", in.Year, in.Month)
		end = fmt.Sprintf("(DATE '%s' + interval '1 month' - interval '1 day')::date", start)
	}
	endExpr := "'" + end + "'::date"
	if in.Scope == "month" {
		endExpr = end
	}
	stmts := map[string][]string{
		"images":            {`UPDATE products SET image_url = NULL WHERE branch_id = $1`},
		"products":          {`UPDATE products SET deleted_at = now() WHERE branch_id = $1 AND deleted_at IS NULL`},
		"sales":             {`DELETE FROM returns WHERE branch_id = $1 AND kind = 'sale' AND created_at::date BETWEEN $2::date AND ` + endExpr, `DELETE FROM sales WHERE branch_id = $1 AND created_at::date BETWEEN $2::date AND ` + endExpr},
		"purchases":         {`DELETE FROM returns WHERE branch_id = $1 AND kind = 'purchase' AND created_at::date BETWEEN $2::date AND ` + endExpr, `DELETE FROM purchases WHERE branch_id = $1 AND purchase_date BETWEEN $2::date AND ` + endExpr},
		"customers":         {`DELETE FROM customers WHERE branch_id = $1`},
		"suppliers":         {`DELETE FROM suppliers WHERE branch_id = $1`},
		"staff":             {`UPDATE users SET deleted_at = now() WHERE branch_id = $1 AND role <> 'owner' AND deleted_at IS NULL`},
		"taxes":             {`DELETE FROM taxes WHERE branch_id = $1`},
		"discounts":         {`DELETE FROM discounts WHERE branch_id = $1`},
		"categories":        {`DELETE FROM categories WHERE branch_id = $1`},
		"fees":              {`DELETE FROM fees WHERE branch_id = $1`},
		"promotions":        {`DELETE FROM promotions WHERE branch_id = $1`},
		"shifts":            {`DELETE FROM shifts WHERE branch_id = $1 AND opened_at::date BETWEEN $2::date AND ` + endExpr},
		"points":            {`DELETE FROM point_ledger WHERE customer_id IN (SELECT id FROM customers WHERE branch_id = $1)`, `UPDATE customers SET points = 0 WHERE branch_id = $1`},
		"online_orders":     {`DELETE FROM catalog_orders WHERE branch_id = $1 AND created_at::date BETWEEN $2::date AND ` + endExpr},
		"held_transactions": {}, // held bills live in the cashier app
	}
	types := in.DataTypes
	id, err := a.startJob(store, "reset_data", in, func(ctx context.Context) (any, error) {
		deleted := map[string]int64{}
		err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
			for _, t := range types {
				for _, s := range stmts[t] {
					args := []any{branch}
					if strings.Contains(s, "$2") {
						args = append(args, start)
					}
					tag, err := tx.Exec(ctx, s, args...)
					if err != nil {
						return fmt.Errorf("%s: %w", t, err)
					}
					deleted[t] += tag.RowsAffected()
				}
			}
			return nil
		})
		if err == nil {
			a.notify(ctx, a.DB, store, &branch, "back_office", "Data reset finished", "The data you selected has been deleted.", "")
		}
		return deleted, err
	})
	if err != nil {
		return err
	}
	a.activity(c, "data_reset", "Reset {types}", "types", strings.Join(types, ", "))
	return httpx.OK(c, fiber.Map{"job_id": id, "status": "queued"})
}
