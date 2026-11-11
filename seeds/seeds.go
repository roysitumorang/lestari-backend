// Package seeds creates a demo store with realistic data by calling the HTTP API,
// so the demo also exercises the real business rules (pricing, stock, debts, points).
package seeds

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math/rand"
	"net/http/httptest"
	"strconv"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/internal/api"
)

type Info struct {
	Email, Password string
	StoreID         string
}

const (
	DemoEmail    = "demo@lestari.id"
	DemoPassword = "demo12345"
)

type client struct {
	ctx    context.Context
	app    *fiber.App
	prefix string
	token  string
	branch string
	err    error
}

func (c *client) do(method, path string, body any) map[string]any {
	if c.err != nil {
		return nil
	}
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequestWithContext(c.ctx, method, c.prefix+path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.branch != "" {
		req.Header.Set("X-Branch-Id", c.branch)
	}
	res, err := c.app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	if err != nil {
		c.err = fmt.Errorf("%s %s: %w", method, path, err)
		return nil
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		c.err = fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, raw)
		return nil
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	if d, ok := out["data"].(map[string]any); ok {
		return d
	}
	return out
}

// b64 encodes a password the way clients send it.
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func id(m map[string]any) string {
	if m == nil {
		return ""
	}
	v, _ := m["id"].(string)
	return v
}

// Demo creates (or recreates) the demo store.
func Demo(ctx context.Context, app *fiber.App, a *api.API) (Info, error) {
	info := Info{Email: DemoEmail, Password: DemoPassword}
	// remove a previous demo store
	if _, err := a.DB.Exec(ctx, `DELETE FROM stores WHERE id IN (SELECT store_id FROM users WHERE email = $1)`, DemoEmail); err != nil {
		return info, err
	}
	var userID string
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		var err error
		userID, info.StoreID, err = a.Provision(ctx, tx, api.Provision{StoreName: "Apotik Lestari", OwnerName: "Abigael Lestari", Email: DemoEmail, Password: DemoPassword, Phone: "+6281234567890"})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE stores SET plan='pro', plan_expires_at = now() + interval '1 year', coin_balance = 250000, reward_balance = 50000, referral_code = 'LESTARI' WHERE id=$1`, info.StoreID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO store_plugins (store_id, code, status, trial_used, expires_at) VALUES ($1,'business','active',true, now() + interval '1 year'), ($1,'attendance','trial',true, now() + interval '7 days')`, info.StoreID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO vouchers (code, reward_type, reward_amount, max_uses) VALUES ('WELCOME50K','coin',50000,1000), ('HEMAT10','discount_percent',10,1000), ('PPOBGRATIS','ppob_free_fee',0,1000) ON CONFLICT DO NOTHING`)
		return err
	})
	if err != nil {
		return info, err
	}
	tok, err := a.Auth.Issue(ctx, userID, info.StoreID, true)
	if err != nil {
		return info, err
	}
	c := &client{ctx: ctx, app: app, prefix: a.Cfg.APIPrefix, token: tok.AccessToken}
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic sample data, not security-sensitive
	now := time.Now().In(a.Loc)

	// ---------- settings ----------
	var province, city string
	if err := a.DB.QueryRow(ctx, `SELECT p.id, c.id FROM provinces p JOIN cities c ON c.province_id = p.id WHERE p.code = '12' AND c.name = 'KABUPATEN TOBA'`).Scan(&province, &city); err != nil {
		return info, err
	}
	c.do("PUT", "/settings/store", map[string]any{"name": "Apotik Lestari", "owner_name": "Abigael Lestari", "phone": "+6281234567890", "address": "Jl. Sisingamangaraja No. 12, Balige",
		"business_type": "Pharmacy & beauty", "province_id": province, "city_id": city, "latitude": 2.3333, "longitude": 99.0667, "motto": "Sehat bersama Lestari"})
	c.do("PUT", "/settings/points", map[string]any{"is_enabled": true, "earn_spend_amount": 10000, "earn_points": 1, "redeem_point_value": 100})
	c.do("PUT", "/settings/link-page", map[string]any{"is_enabled": true, "username": "apotiklestari", "slogan": "Apotek keluarga di Balige",
		"links": []any{map[string]any{"type": "whatsapp", "label": "Chat with us", "url": "https://wa.me/6281234567890", "is_active": true}}})

	// ---------- master data ----------
	cats := map[string]string{}
	for i, n := range []string{"Medicine", "Vitamins", "Personal care", "Drinks", "Medical devices"} {
		cats[n] = id(c.do("POST", "/categories", map[string]any{"name": n, "sort_order": i + 1}))
	}
	tax := id(c.do("POST", "/taxes", map[string]any{"name": "VAT 11%", "rate": 11}))
	_ = tax
	c.do("POST", "/discounts", map[string]any{"name": "Member 5%", "type": "percent", "value": 5})
	c.do("POST", "/discounts", map[string]any{"name": "Rp 2.000 off", "type": "amount", "value": 2000})
	packing := id(c.do("POST", "/fees", map[string]any{"name": "Packaging", "amount_type": "amount", "amount": 1000}))
	member := id(c.do("POST", "/customer-types", map[string]any{"name": "Member", "discount_type": "percent", "discount_value": 2, "point_multiplier": 1.5}))
	reseller := id(c.do("POST", "/customer-types", map[string]any{"name": "Reseller", "point_multiplier": 1}))

	type prod struct {
		name, cat, unit string
		price, cost     float64
		stock, min      float64
	}
	prods := []prod{
		{"Paracetamol 500mg strip", "Medicine", "strip", 6000, 3500, 120, 20}, {"Amoxicillin 500mg strip", "Medicine", "strip", 15000, 9800, 60, 10},
		{"Ibuprofen 400mg strip", "Medicine", "strip", 9000, 5600, 80, 15}, {"Antacid syrup 100ml", "Medicine", "bottle", 18500, 12000, 35, 8},
		{"Cough syrup 60ml", "Medicine", "bottle", 22000, 14500, 40, 8}, {"Vitamin C 1000mg (10)", "Vitamins", "tube", 45000, 31000, 50, 10},
		{"Multivitamin kids syrup", "Vitamins", "bottle", 38000, 26000, 25, 5}, {"Vitamin D3 1000IU (30)", "Vitamins", "bottle", 85000, 60000, 18, 5},
		{"Hand sanitizer 100ml", "Personal care", "bottle", 15000, 8000, 70, 10}, {"Face mask (50 pcs)", "Personal care", "box", 35000, 21000, 45, 10},
		{"Baby oil 100ml", "Personal care", "bottle", 24000, 16000, 30, 6}, {"Mineral water 600ml", "Drinks", "bottle", 4000, 2300, 200, 48},
		{"Isotonic drink 500ml", "Drinks", "bottle", 8000, 5200, 96, 24}, {"Digital thermometer", "Medical devices", "pcs", 45000, 28000, 12, 3},
		{"Blood pressure monitor", "Medical devices", "pcs", 420000, 310000, 4, 2},
	}
	var productIDs []string
	left := map[string]float64{} // stock the seed may still sell
	for i, p := range prods {
		body := map[string]any{"name": p.name, "category_id": cats[p.cat], "unit": p.unit, "selling_price": p.price, "purchase_price": p.cost, "stock": p.stock,
			"minimum_stock": p.min, "barcode": fmt.Sprintf("899%010d", 1000+i), "show_in_online_store": true}
		if p.cat == "Drinks" {
			body["price_type"] = "wholesale"
			body["wholesale_prices"] = []any{map[string]any{"min_quantity": 12, "price": p.price * 0.9}, map[string]any{"min_quantity": 24, "price": p.price * 0.85}}
		}
		if p.cat == "Vitamins" {
			body["points_earned"] = 2
		}
		pid := id(c.do("POST", "/products", body))
		productIDs = append(productIDs, pid)
		left[pid] = p.stock - p.min // keep some stock on the shelf
	}
	c.do("POST", "/products", map[string]any{"name": "Blood pressure check", "item_type": "service", "selling_price": 10000, "category_id": cats["Medical devices"]})

	var customers []string
	for i, n := range []string{"Rina Simanjuntak", "Budi Hutapea", "Sari Panjaitan", "Joko Sitorus", "Maria Nainggolan", "Toko Obat Sehat (reseller)"} {
		t := member
		if i == 5 {
			t = reseller
		}
		customers = append(customers, id(c.do("POST", "/customers", map[string]any{"name": n, "phone": fmt.Sprintf("+62812000%04d", i+1), "code": fmt.Sprintf("C%03d", i+1),
			"customer_type_id": t, "address": "Balige, Toba"})))
	}
	var suppliers []string
	for i, n := range []string{"PT Kimia Farma Trading", "CV Medika Sumatera", "PT Enseval Putera"} {
		suppliers = append(suppliers, id(c.do("POST", "/suppliers", map[string]any{"name": n, "contact_person": "Sales " + strconv.Itoa(i+1), "phone": fmt.Sprintf("+6261400%04d", i+1), "code": fmt.Sprintf("S%02d", i+1), "address": "Medan"})))
	}
	cashier := id(c.do("POST", "/staff", map[string]any{"name": "Vina Pazary", "email": "kasir@lestari.id", "password": b64("kasir12345"), "role": "cashier", "phone": "+6281299990001"}))
	c.do("POST", "/staff", map[string]any{"name": "Roy Situmorang", "email": "manajer@lestari.id", "password": b64("manajer12345"), "role": "manager"})
	c.do("POST", "/promotions", map[string]any{"name": "Vitamin week", "type": "product_discount", "start_date": now.AddDate(0, 0, -10).Format("2006-01-02"), "end_date": now.AddDate(0, 0, 20).Format("2006-01-02"),
		"discount_type": "percent", "discount_value": 10, "products": []any{map[string]any{"product_id": productIDs[5]}, map[string]any{"product_id": productIDs[6]}}})
	c.do("POST", "/promotions", map[string]any{"name": "Spend 200K save 5%", "type": "minimum_purchase", "start_date": now.AddDate(0, 0, -30).Format("2006-01-02"), "end_date": now.AddDate(0, 1, 0).Format("2006-01-02"),
		"discount_type": "percent", "discount_value": 5, "minimum_purchase": 200000})
	c.do("POST", "/commissions", map[string]any{"name": "Vitamin bonus", "basis": "product", "type": "percent", "value": 2, "product_ids": []any{productIDs[5], productIDs[6], productIDs[7]}})

	// ---------- purchases ----------
	c.do("POST", "/purchases", map[string]any{"supplier_id": suppliers[0], "purchase_date": now.AddDate(0, 0, -20).Format("2006-01-02"), "invoice_number": "KF-88231", "payment_type": "cash", "payment_method": "bank_transfer",
		"items": []any{map[string]any{"product_id": productIDs[0], "quantity": 50, "unit_cost": 3400}, map[string]any{"product_id": productIDs[2], "quantity": 30, "unit_cost": 5500}}})
	c.do("POST", "/purchases", map[string]any{"supplier_id": suppliers[1], "purchase_date": now.AddDate(0, 0, -8).Format("2006-01-02"), "invoice_number": "MS-1029", "payment_type": "credit",
		"due_date": now.AddDate(0, 0, 22).Format("2006-01-02"), "paid_amount": 500000,
		"items": []any{map[string]any{"product_id": productIDs[5], "quantity": 40, "unit_cost": 30500}, map[string]any{"product_id": productIDs[9], "quantity": 20, "unit_cost": 20500}}})

	// ---------- sales over the last 30 days ----------
	var drawer string
	if d := c.do("GET", "/cash-drawers", nil); d != nil {
		if arr, ok := d["rows"].([]any); ok && len(arr) > 0 {
			drawer, _ = arr[0].(map[string]any)["id"].(string)
		}
	}
	methods := []string{"cash", "cash", "cash", "qris", "debit", "e_wallet"}
	for day := 29; day >= 0 && c.err == nil; day-- {
		date := now.AddDate(0, 0, -day)
		var shift string
		if day <= 6 {
			shift = id(c.do("POST", "/shifts", map[string]any{"cash_drawer_id": drawer, "opening_cash": 200000}))
		}
		n := 2 + rng.Intn(5)
		for range n {
			at := time.Date(date.Year(), date.Month(), date.Day(), 8+rng.Intn(13), rng.Intn(60), rng.Intn(60), 0, a.Loc)
			if at.After(now) {
				at = now.Add(-time.Duration(rng.Intn(3600)) * time.Second)
			}
			var items []any
			for range 1 + rng.Intn(3) {
				pid := productIDs[rng.Intn(len(productIDs))]
				q := float64(1 + rng.Intn(3))
				if left[pid] < q {
					continue
				}
				left[pid] -= q
				items = append(items, map[string]any{"product_id": pid, "quantity": q})
			}
			if len(items) == 0 {
				continue
			}
			body := map[string]any{"created_at": at.Format(time.RFC3339), "payment_method": methods[rng.Intn(len(methods))], "items": items}
			if rng.Intn(3) == 0 {
				body["customer_id"] = customers[rng.Intn(len(customers))]
			}
			if rng.Intn(8) == 0 {
				body["fee_ids"] = []any{packing}
			}
			if shift != "" {
				body["shift_id"] = shift
			}
			c.do("POST", "/sales", body)
		}
		if shift != "" && day > 0 {
			c.do("POST", fmt.Sprintf("/shifts/%s/close", shift), map[string]any{"actual_cash": 200000 + float64(rng.Intn(5))*100000})
		}
	}
	// a credit sale with partial payment, then an instalment
	credit := c.do("POST", "/sales", map[string]any{"customer_id": customers[5], "payment_type": "credit", "payment_method": "cash", "paid_amount": 100000,
		"due_date": now.AddDate(0, 0, 14).Format("2006-01-02"), "items": []any{map[string]any{"product_id": productIDs[11], "quantity": 48}, map[string]any{"product_id": productIDs[9], "quantity": 5}}})
	if credit != nil {
		if recv := c.do("GET", "/receivables?status=unpaid", nil); recv != nil {
			if arr, ok := recv["rows"].([]any); ok && len(arr) > 0 {
				rid, _ := arr[0].(map[string]any)["id"].(string)
				c.do("POST", fmt.Sprintf("/receivables/%s/payments", rid), map[string]any{"payment_date": now.Format("2006-01-02"), "amount": 50000, "payment_method": "cash"})
			}
		}
		c.do("POST", fmt.Sprintf("/sales/%s/returns", id(credit)), map[string]any{"items": []any{map[string]any{"product_id": productIDs[9], "quantity": 1}}, "reason": "Damaged box"})
	}

	// a point redemption (the customer has points from earlier purchases)
	c.do("POST", "/customers/"+customers[0]+"/points", map[string]any{"mode": "add", "points": 200, "note": "Welcome bonus"})
	c.do("POST", "/sales", map[string]any{"customer_id": customers[0], "payment_method": "cash",
		"items": []any{map[string]any{"product_id": productIDs[11], "quantity": 2, "points_redeemed": 50}}})
	// a purchase return
	if pu := c.do("GET", "/purchases?per_page=1", nil); pu != nil {
		if arr, ok := pu["rows"].([]any); ok && len(arr) > 0 {
			first := arr[0].(map[string]any)
			if full := c.do("GET", fmt.Sprintf("/purchases/%v", first["id"]), nil); full != nil {
				if items, ok := full["items"].([]any); ok && len(items) > 0 {
					it := items[0].(map[string]any)
					c.do("POST", fmt.Sprintf("/purchases/%v/returns", first["id"]), map[string]any{"refund_method": "reduce_payable", "reason": "Expired batch",
						"items": []any{map[string]any{"product_id": it["product_id"], "quantity": 2}}})
				}
			}
		}
	}
	// a paid invoice (storage bought with coins) and a support-access grant
	c.do("POST", "/settings/storage/purchases", map[string]any{"package": "1gb_monthly", "payment_method": "coin"})
	c.do("POST", "/help-access", map[string]any{"platform": "web", "account_id": userID, "duration_hours": 24, "description": "Help setting up receipt printer"})

	// ---------- cash in / out ----------
	for i, e := range []struct {
		typ, cat string
		amt      float64
	}{{"expense", "Rent", 2500000}, {"expense", "Electricity", 450000}, {"income", "Capital injection", 5000000}, {"expense", "Salary", 3000000}} {
		c.do("POST", "/finance-entries", map[string]any{"entry_date": now.AddDate(0, 0, -25+i*6).Format("2006-01-02"), "type": e.typ, "category": e.cat, "amount": e.amt, "payment_method": "bank_transfer"})
	}

	// ---------- stock opname, branch, transfer ----------
	c.do("POST", "/stock-opnames", map[string]any{"opname_date": now.AddDate(0, 0, -3).Format("2006-01-02"), "performed_by_name": "Vina Pazary", "performed_by_role": "cashier",
		"items": []any{map[string]any{"product_id": productIDs[0], "physical_stock": 140}, map[string]any{"product_id": productIDs[12], "physical_stock": 90, "note": "6 bottles leaking"}}})
	branch := c.do("POST", "/branches", map[string]any{"name": "Lestari Laguboti", "code": "LGB", "manager_name": "Sari", "phone": "+6281277770000", "email": "laguboti@lestari.id", "password": b64("cabang12345"), "address": "Laguboti, Toba"})
	if b := id(branch); b != "" {
		c.do("POST", "/stock-transfers", map[string]any{"to_branch_id": b, "transfer_date": now.AddDate(0, 0, -2).Format("2006-01-02"),
			"items": []any{map[string]any{"product_id": productIDs[0], "quantity": 20}, map[string]any{"product_id": productIDs[11], "quantity": 24}}})
		c.do("POST", "/stock-transfers", map[string]any{"to_branch_id": b, "transfer_date": now.Format("2006-01-02"), "requires_confirmation": true,
			"items": []any{map[string]any{"product_id": productIDs[8], "quantity": 10}}})
	}

	// ---------- online orders, PPOB, ratings ----------
	ord := c.do("POST", "/catalog/orders", map[string]any{"customer_name": "Dewi Sinaga", "customer_phone": "+6281311112222", "shipping_address": "Jl. Pagar Batu, Balige",
		"shipping_method": "own_courier", "shipping_fee": 5000, "payment_method": "bank_transfer", "items": []any{map[string]any{"product_id": productIDs[5], "quantity": 2}}})
	if ord != nil {
		c.do("PATCH", fmt.Sprintf("/catalog/orders/%s/status", id(ord)), map[string]any{"status": "processing"})
	}
	c.do("POST", "/catalog/orders", map[string]any{"customer_name": "Andi Pardede", "customer_phone": "+6281333334444", "shipping_method": "takeaway", "payment_method": "direct",
		"items": []any{map[string]any{"product_id": productIDs[13], "quantity": 1}}})
	c.do("POST", "/catalog/payment-accounts", map[string]any{"type": "bank", "provider": "BCA", "account_name": "Abigael Lestari", "account_number": "1234567890"})
	if c.err == nil {
		// a paid PPOB balance so the cashier can sell
		ref := "SEED-" + info.StoreID
		_, c.err = a.DB.Exec(ctx, `INSERT INTO ppob_deposits (branch_id, reference_id, type, payment_method, amount, status, balance_after) SELECT id, $2, 'top_up', 'virtual_account', 500000, 'success', 500000 FROM branches WHERE store_id=$1 AND is_head_office`, info.StoreID, ref)
	}
	if prices := c.do("GET", "/ppob/prices/prepaid?per_page=5", nil); prices != nil {
		if arr, ok := prices["rows"].([]any); ok {
			for i, x := range arr {
				c.do("POST", "/ppob/transactions", map[string]any{"price_id": x.(map[string]any)["id"], "customer_number": fmt.Sprintf("08121111%04d", i)})
			}
		}
	}
	for i, r := range []int{5, 5, 4, 3, 5, 4} {
		c.do("POST", "/store-ratings", map[string]any{"rating": r, "customer_name": []string{"Rina", "Budi", "Sari", "Joko", "Maria", "Andi"}[i], "comment": []string{"Friendly staff", "Complete stock", "", "Queue was long", "Great service", "Fast"}[i]})
	}
	if c.err == nil && cashier != "" {
		_, c.err = a.DB.Exec(ctx, `INSERT INTO staff_attendances (branch_id, staff_id, date, clock_in_at, clock_out_at, status, location)
			SELECT b.id, $2, d::date, d + interval '7 hours 55 minutes', d + interval '16 hours', CASE WHEN extract(dow FROM d) = 1 THEN 'late' ELSE 'present' END, 'Balige'
			FROM branches b, generate_series(current_date - 6, current_date - 1, interval '1 day') d WHERE b.store_id = $1 AND b.is_head_office`, info.StoreID, cashier)
	}
	if c.err == nil {
		_, c.err = a.DB.Exec(ctx, `INSERT INTO notifications (store_id, source, title, message, link_route) VALUES ($1,'back_office','Welcome to Lestari','Your demo store is ready. Explore the reports!','dashboard')`, info.StoreID)
	}
	return info, c.err
}
