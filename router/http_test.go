package router

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/config"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/seeds"
)

var paramRe = regexp.MustCompile(`\{[a-z_]+\}|:[a-z_]+`)

// endpoints that answer with a file instead of JSON
var fileRe = regexp.MustCompile(`(/(export|pdf|qr-code|qr-codes|barcodes|labels|import-template|print|invoice|menu-book|delivery-note)|^/sales/[0-9a-f-]+/receipt)$`)

func normalize(route string) string { return paramRe.ReplaceAllString(route, "{}") }

// TestRoutesCoverContract checks that every endpoint the client documents in ENDPOINTS.md is routed.
func TestRoutesCoverContract(t *testing.T) {
	q := NewService(config.Load(), nil, nil)
	have := map[string]bool{}
	for _, r := range q.Routes(q.NewApp(slog.New(slog.NewTextHandler(io.Discard, nil)))) {
		have[normalize(r)] = true
	}
	f, err := os.Open("testdata/endpoints.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		n++
		if !have[normalize(line)] {
			t.Errorf("missing endpoint: %s", line)
		}
	}
	if n < 300 {
		t.Fatalf("contract looks truncated: %d endpoints", n)
	}
}

// ---------- integration (needs TEST_DATABASE_URL) ----------

type harness struct {
	t      *testing.T
	app    *fiber.App
	a      seeds.Deps
	routes []string // every "METHOD /path" of the app
	token  string
	branch string
	lang   string // Accept-Language
}

func (h *harness) call(method, path string, body any, ctype ...string) (int, []byte, http.Header) {
	h.t.Helper()
	var r io.Reader
	ct := "application/json"
	if len(ctype) > 0 {
		ct = ctype[0]
		r = body.(io.Reader)
	} else if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequestWithContext(h.t.Context(), method, "/api/v1"+path, r)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+h.token)
	if h.branch != "" {
		req.Header.Set("X-Branch-Id", h.branch)
	}
	if h.lang != "" {
		req.Header.Set("Accept-Language", h.lang)
	}
	res, err := h.app.Test(req, fiber.TestConfig{Timeout: 60 * time.Second})
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b, res.Header
}

func (h *harness) json(method, path string, body any, want int) map[string]any {
	h.t.Helper()
	code, b, _ := h.call(method, path, body)
	if code != want {
		h.t.Errorf("%s %s: status %d, want %d: %s", method, path, code, want, trim(b))
		return nil
	}
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

func trim(b []byte) string {
	if len(b) > 400 {
		return string(b[:400]) + "…"
	}
	return string(b)
}

// upperKeys lists the JSON attribute names (as paths) that contain an upper-case letter.
func upperKeys(v any, path string) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if k != strings.ToLower(k) {
				out = append(out, path+k)
			}
			out = append(out, upperKeys(child, path+k+".")...)
		}
	case []any:
		if len(x) > 0 {
			out = append(out, upperKeys(x[0], path)...)
		}
	}
	return out
}

// rows returns data.rows of a list response.
func rows(m map[string]any) []any {
	d, _ := m["data"].(map[string]any)
	arr, _ := d["rows"].([]any)
	return arr
}

func first(m map[string]any) map[string]any {
	if arr := rows(m); len(arr) > 0 {
		r, _ := arr[0].(map[string]any)
		return r
	}
	return nil
}

func setup(t *testing.T) *harness {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run integration tests")
	}
	cfg := config.Load()
	cfg.DatabaseURL = url
	cfg.UploadDir = t.TempDir()
	ctx := context.Background()
	pool, err := config.CreateDbConnection(ctx, url, 0, cfg.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	q := NewService(cfg, pool, pool)
	if err := q.Migration.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	app, a := q.NewApp(slog.New(slog.NewTextHandler(io.Discard, nil))), q.SeedDeps()
	info, err := seeds.Demo(ctx, app, a)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	var uid string
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, info.Email).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	tok, err := a.Auth.Issue(ctx, uid, info.StoreID, true)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, app: app, a: a, routes: q.Routes(app), token: tok.AccessToken}
}

// resolveParams fills path parameters using the matching list endpoint.
func (h *harness) resolveParams(path string, query string) (string, bool) {
	if !strings.Contains(path, ":") {
		return path, true
	}
	before, after, _ := strings.Cut(path, "/:")
	base := before
	param, _, _ := strings.Cut(after, "/")
	if param == "date" {
		return strings.Replace(path, ":date", time.Now().Format("2006-01-02"), 1), true
	}
	if param == "code" {
		return "", false
	}
	listPath := base
	if base == "/billing/invoices" || strings.HasPrefix(base, "/reports/") || base == "/staff-attendances" {
		listPath += "?" + query
	}
	if base == "/notifications" {
		listPath = "/notifications"
	}
	code, b, _ := h.call("GET", listPath, nil)
	if code != 200 {
		return "", false
	}
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	row := first(m)
	if row == nil {
		return "", false
	}
	v, ok := row[param]
	if !ok || v == nil {
		v = row["id"]
	}
	if v == nil {
		return "", false
	}
	return strings.Replace(path, ":"+param, fmt.Sprintf("%v", v), 1), true
}

func TestIntegrationEveryGET(t *testing.T) {
	h := setup(t)
	start := time.Now().AddDate(0, 0, -60).Format("2006-01-02")
	end := time.Now().Format("2006-01-02")
	q := "start_date=" + start + "&end_date=" + end
	routes := h.routes
	sort.Strings(routes)
	tested, skipped := 0, []string{}
	for _, r := range routes {
		method, path, _ := strings.Cut(r, " ")
		if method != "GET" {
			continue
		}
		p, ok := h.resolveParams(path, q)
		if !ok {
			skipped = append(skipped, path)
			continue
		}
		sep := "?"
		if strings.Contains(p, "?") {
			sep = "&"
		}
		extra := ""
		if path == "/products/check-sku" {
			extra = "&sku=ABC"
		}
		code, body, hdr := h.call("GET", p+sep+q+extra, nil)
		tested++
		if code != 200 {
			t.Errorf("GET %s → %d: %s", p, code, trim(body))
			continue
		}
		ct := hdr.Get("Content-Type")
		if fileRe.MatchString(p) {
			if strings.HasPrefix(ct, "application/json") || len(body) < 20 {
				t.Errorf("GET %s: expected a file, got %s (%d bytes)", p, ct, len(body))
			}
			if strings.Contains(ct, "pdf") && !bytes.HasPrefix(body, []byte("%PDF")) {
				t.Errorf("GET %s: not a PDF", p)
			}
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil || m["data"] == nil {
			t.Errorf("GET %s: missing data envelope: %s", p, trim(body))
		}
		for _, k := range upperKeys(m, "") {
			t.Errorf("GET %s: JSON attribute %q is not lower case", p, k)
		}
	}
	t.Logf("tested %d GET endpoints, skipped (no row to address): %v", tested, skipped)
	if len(skipped) > 0 {
		t.Errorf("endpoints not exercised (seed has no matching row): %v", skipped)
	}
	if tested < 190 {
		t.Errorf("only %d GET endpoints tested", tested)
	}
}

func TestIntegrationRoundTrips(t *testing.T) {
	h := setup(t)
	// every settings page: read, write back unchanged
	for _, key := range []string{"points", "store", "online-store", "link-page", "qr-order", "default-tax", "service-charge", "rounding", "receipt", "display", "catalog"} {
		got := h.json("GET", "/settings/"+key, nil, 200)
		if got == nil {
			continue
		}
		h.json("PUT", "/settings/"+key, got["data"], 200)
	}
	h.json("PUT", "/account/profile", h.json("GET", "/account/profile", nil, 200)["data"], 200)

	// every editable resource: GET one row and PUT it back unchanged
	for _, base := range []string{"/products", "/categories", "/customers", "/customer-types", "/suppliers", "/discounts", "/taxes", "/fees", "/cash-drawers",
		"/promotions", "/commissions", "/purchases", "/finance-entries", "/branches", "/staff", "/payment-methods", "/catalog/payment-accounts", "/ppob/prices/prepaid", "/ppob/prices/postpaid"} {
		row := first(h.json("GET", base, nil, 200))
		if row == nil {
			t.Errorf("%s: no rows to round-trip", base)
			continue
		}
		id := fmt.Sprint(row["id"])
		full := map[string]any{"data": row}
		if !strings.HasPrefix(base, "/ppob/prices") { // PPOB prices have no single-item GET
			full = h.json("GET", base+"/"+id, nil, 200)
		}
		h.json("PUT", base+"/"+id, full["data"], 200)
	}
}

func TestIntegrationWorkflows(t *testing.T) {
	h := setup(t)
	today := time.Now().Format("2006-01-02")

	// product with wholesale tiers prices the 12th unit at the tier
	p := h.json("POST", "/products", map[string]any{"name": "Test tea", "selling_price": 5000, "purchase_price": 3000, "stock": 30,
		"price_type": "wholesale", "wholesale_prices": []any{map[string]any{"min_quantity": 12, "price": 4500}}}, 201)
	pid := p["data"].(map[string]any)["id"]
	sale := h.json("POST", "/sales", map[string]any{"items": []any{map[string]any{"product_id": pid, "quantity": 12}}, "paid_amount": 60000}, 201)
	if s, ok := sale["data"].(map[string]any); ok {
		if s["subtotal"].(float64) != 54000 || s["change_amount"].(float64) != 6000 {
			t.Errorf("wholesale sale: subtotal %v change %v", s["subtotal"], s["change_amount"])
		}
	}
	// overselling is refused
	h.json("POST", "/sales", map[string]any{"items": []any{map[string]any{"product_id": pid, "quantity": 100}}}, 422)
	// stock adjustment + movements
	h.json("POST", fmt.Sprintf("/inventory/stocks/%v/adjustments", pid), map[string]any{"mode": "set", "quantity": 40, "reason": "correction"}, 200)
	mv := h.json("GET", fmt.Sprintf("/inventory/stocks/%v/movements", pid), nil, 200)
	if arr := rows(mv); len(arr) != 3 {
		t.Errorf("expected 3 movements (opening, sale, adjustment), got %d", len(arr))
	}
	// opname approve sets stock exactly
	op := h.json("POST", "/stock-opnames", map[string]any{"opname_date": today, "performed_by_name": "Test", "performed_by_role": "owner",
		"items": []any{map[string]any{"product_id": pid, "physical_stock": 33}}}, 201)
	h.json("POST", fmt.Sprintf("/stock-opnames/%v/approve", op["data"].(map[string]any)["id"]), nil, 200)
	st := h.json("GET", "/inventory/stocks?q="+"Test%20tea", nil, 200)
	if r := first(st); r == nil || r["stock"].(float64) != 33 {
		t.Errorf("stock after opname: %v", r)
	}

	// credit purchase creates a payable; paying it in full closes it
	sup := first(h.json("GET", "/suppliers", nil, 200))
	pu := h.json("POST", "/purchases", map[string]any{"supplier_id": sup["id"], "purchase_date": today, "payment_type": "credit",
		"items": []any{map[string]any{"product_id": pid, "quantity": 10, "unit_cost": 2800}}}, 201)
	pay := h.json("GET", "/payables?status=unpaid&q="+strings.ReplaceAll(fmt.Sprint(pu["data"].(map[string]any)["receipt_number"]), "/", "%2F"), nil, 200)
	if r := first(pay); r != nil {
		h.json("POST", fmt.Sprintf("/payables/%v/payments", r["id"]), map[string]any{"payment_date": today, "amount": 28000, "payment_method": "cash"}, 200)
		if got := h.json("GET", fmt.Sprintf("/payables/%v", r["id"]), nil, 200); got["data"].(map[string]any)["status"] != "paid" {
			t.Errorf("payable not paid: %v", got["data"])
		}
	} else {
		t.Error("credit purchase created no payable")
	}

	// transfer with confirmation: accept as the receiving branch
	br := h.json("GET", "/branches", nil, 200)
	var target string
	for _, x := range rows(br) {
		if m := x.(map[string]any); m["is_head_office"] == false {
			target = m["id"].(string)
		}
	}
	tr := h.json("POST", "/stock-transfers", map[string]any{"to_branch_id": target, "transfer_date": today, "requires_confirmation": true,
		"items": []any{map[string]any{"product_id": pid, "quantity": 5}}}, 201)
	h.branch = target
	h.json("POST", fmt.Sprintf("/stock-transfers/%v/accept", tr["data"].(map[string]any)["id"]), map[string]any{}, 200)
	recv := h.json("GET", "/inventory/stocks?q=Test%20tea", nil, 200)
	if r := first(recv); r == nil || r["stock"].(float64) != 5 {
		t.Errorf("receiving branch stock: %v", r)
	}
	h.branch = ""

	// billing: voucher, coin purchase of storage, plugin trial
	h.json("POST", "/billing/vouchers/redeem", map[string]any{"code": "WELCOME50K"}, 200)
	h.json("POST", "/billing/vouchers/redeem", map[string]any{"code": "WELCOME50K"}, 422)
	h.json("POST", "/settings/storage/purchases", map[string]any{"package": "1gb_monthly", "payment_method": "coin"}, 200)
	h.json("POST", "/plugins/food_menu/trial", nil, 200)
	inv := h.json("POST", "/billing/coin-top-ups", map[string]any{"amount": 50000, "payment_method": "qris"}, 200)
	if inv["data"].(map[string]any)["status"] != "pending" {
		t.Errorf("top-up should be pending: %v", inv)
	}

	// online order status flow and an invalid jump
	o := first(h.json("GET", "/catalog/orders?status=unpaid", nil, 200))
	if o != nil {
		h.json("PATCH", fmt.Sprintf("/catalog/orders/%v/status", o["id"]), map[string]any{"status": "completed"}, 409)
		h.json("PATCH", fmt.Sprintf("/catalog/orders/%v/status", o["id"]), map[string]any{"status": "processing"}, 200)
	}

	// uploads
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "logo.png")
	fw.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82"))
	mw.WriteField("purpose", "logo_url")
	mw.Close()
	code, b, _ := h.call("POST", "/uploads", &buf, mw.FormDataContentType())
	if code != 201 || !strings.Contains(string(b), "/uploads/") {
		t.Errorf("upload: %d %s", code, b)
	}

	// ids are PostgreSQL-generated UUID v7 values
	if s, _ := pid.(string); !uuidV7.MatchString(s) {
		t.Errorf("product id is not a UUID v7: %v", pid)
	}
	h.json("GET", "/products/123", nil, 404)
	h.json("GET", "/products/0190a1b2-0000-7000-8000-000000000000", nil, 404)
	h.json("POST", "/sales", map[string]any{"items": []any{map[string]any{"product_id": 5, "quantity": 1}}}, 400)
	h.json("POST", "/products", map[string]any{"name": "Bad ref", "selling_price": 1, "category_id": "nope"}, 422)

	// passwords travel base64-encoded
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	login := h.json("POST", "/auth/login", map[string]any{"email": seeds.DemoEmail, "password": b64(seeds.DemoPassword)}, 200)
	if login["data"].(map[string]any)["access_token"] == "" {
		t.Errorf("login: %v", login)
	}
	h.json("POST", "/auth/login", map[string]any{"email": seeds.DemoEmail, "password": seeds.DemoPassword}, 422)
	h.json("POST", "/auth/login", map[string]any{"email": seeds.DemoEmail, "password": b64("wrong-password")}, 401)
	h.json("POST", "/auth/login", map[string]any{"email": "kasir@lestari.id", "password": b64("kasir12345")}, 200) // created through POST /staff
	h.json("POST", "/staff", map[string]any{"name": "Plain", "email": "plain@lestari.id", "password": "not base64!", "role": "cashier"}, 422)
	h.json("PUT", "/auth/password", map[string]any{"current_password": b64(seeds.DemoPassword), "new_password": b64("s3cret pässwörd"), "new_password_confirmation": b64("s3cret pässwörd")}, 200)
	h.json("POST", "/auth/login", map[string]any{"email": seeds.DemoEmail, "password": b64("s3cret pässwörd")}, 200)

	// validation and auth errors use the error envelope
	bad := h.json("POST", "/products", map[string]any{"selling_price": "x"}, 422)
	if e, ok := bad["data"].(map[string]any); !ok || e["fields"] == nil || e["code"] != "validation_failed" || bad["message"] == "" ||
		bad["status_code"] != float64(422) || bad["request_id"] == "" || bad["app"] != "lestari" {
		t.Errorf("validation envelope: %v", bad)
	}
	h.token = "nope"
	h.json("GET", "/products", nil, 401)
}

var uuidV7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestIntegrationTaxes exercises the tax endpoints end to end (model, sanitizer, repository, service, controller).
func TestIntegrationTaxes(t *testing.T) {
	h := setup(t)
	fields := func(m map[string]any) map[string]any {
		d, _ := m["data"].(map[string]any)
		f, _ := d["fields"].(map[string]any)
		return f
	}
	// validation: required, types and range, all reported together
	bad := h.json("POST", "/taxes", map[string]any{"rate": "x", "is_active": []any{}}, 422)
	if f := fields(bad); f["name"] == nil || f["rate"] == nil || f["is_active"] == nil {
		t.Fatalf("validation fields: %v", bad)
	}
	h.json("POST", "/taxes", map[string]any{"name": "Luxury", "rate": 150}, 422)
	// create with defaults
	created := h.json("POST", "/taxes", map[string]any{"name": "  Zz Service tax ", "rate": 7.5}, 201)["data"].(map[string]any)
	id := created["id"].(string)
	if !uuidV7.MatchString(id) || created["name"] != "Zz Service tax" || created["rate"] != 7.5 || created["is_active"] != true || created["is_inclusive"] != false {
		t.Fatalf("created: %v", created)
	}
	// partial update keeps the other attributes
	updated := h.json("PUT", "/taxes/"+id, map[string]any{"is_inclusive": true}, 200)["data"].(map[string]any)
	if updated["is_inclusive"] != true || updated["name"] != "Zz Service tax" || updated["rate"] != 7.5 {
		t.Fatalf("updated: %v", updated)
	}
	if updated["created_at"] != created["created_at"] || updated["updated_at"] == created["updated_at"] {
		t.Fatalf("a change must move updated_at only: created %v, updated %v", created, updated)
	}
	// the same values again: nothing is written, updated_at stays
	same := h.json("PUT", "/taxes/"+id, map[string]any{"name": "Zz Service tax", "rate": 7.5, "is_inclusive": true, "is_active": true}, 200)["data"].(map[string]any)
	if same["updated_at"] != updated["updated_at"] {
		t.Fatalf("unchanged update moved updated_at: %v → %v", updated["updated_at"], same["updated_at"])
	}
	if one := h.json("PUT", "/taxes/"+id, map[string]any{"name": "Zz Service tax", "rate": 8}, 200)["data"].(map[string]any); one["rate"] != float64(8) || one["updated_at"] == same["updated_at"] || one["is_inclusive"] != true {
		t.Fatalf("single change: %v", one)
	}
	h.json("PUT", "/taxes/"+id, map[string]any{"name": ""}, 422)
	// list: search, filter, sort, paging
	list := h.json("GET", "/taxes?q=zz+service&is_active=true&sort=rate&order=desc&per_page=1", nil, 200)
	if r := rows(list); len(r) != 1 || r[0].(map[string]any)["id"] != id {
		t.Fatalf("list: %v", list)
	}
	info := list["data"].(map[string]any)["info"].(map[string]any)
	if info["limit"] != float64(1) || info["page"] != float64(1) || info["total"] != float64(1) {
		t.Fatalf("info: %v", info)
	}
	if r := rows(h.json("GET", "/taxes?sort=name&order=desc", nil, 200)); len(r) < 2 || r[0].(map[string]any)["id"] != id {
		t.Fatalf("sorted list should start with the new tax: %v", r)
	}
	h.json("GET", "/taxes/"+id, nil, 200)
	h.json("GET", "/taxes/not-a-uuid", nil, 404)
	// deleting a tax a product uses clears it on the product
	used := h.json("POST", "/taxes", map[string]any{"name": "Zz Used", "rate": 2}, 201)["data"].(map[string]any)["id"].(string)
	prod := h.json("POST", "/products", map[string]any{"name": "Taxed item", "selling_price": 1000, "tax_id": used}, 201)["data"].(map[string]any)
	h.json("DELETE", "/taxes/"+used, nil, 204)
	if got := h.json("GET", "/products/"+prod["id"].(string), nil, 200)["data"].(map[string]any); got["tax_id"] != nil {
		t.Fatalf("product still points at the deleted tax: %v", got["tax_id"])
	}
	h.json("DELETE", "/products/"+prod["id"].(string), nil, 204)
	// bulk delete is all or nothing
	other := h.json("POST", "/taxes", map[string]any{"name": "Zz Temp", "rate": 1}, 201)["data"].(map[string]any)["id"].(string)
	h.json("POST", "/taxes/bulk-delete", map[string]any{"ids": []any{id, "00000000-0000-7000-8000-000000000000"}}, 404)
	h.json("GET", "/taxes/"+id, nil, 200)
	h.json("POST", "/taxes/bulk-delete", map[string]any{"ids": []any{}}, 422)
	if d := h.json("POST", "/taxes/bulk-delete", map[string]any{"ids": []any{id, other}}, 200)["data"].(map[string]any); d["deleted"] != float64(2) {
		t.Fatalf("bulk delete: %v", d)
	}
	h.json("GET", "/taxes/"+id, nil, 404)
	h.json("DELETE", "/taxes/"+other, nil, 404)
}

// TestIntegrationLanguage checks that messages, notifications and files follow Accept-Language, then the
// branch's display language.
func TestIntegrationLanguage(t *testing.T) {
	h := setup(t)
	errorBody := func(b []byte) (string, map[string][]string) {
		var e struct {
			Message string `json:"message"`
			Data    struct {
				Fields map[string][]string `json:"fields"`
			} `json:"data"`
		}
		_ = json.Unmarshal(b, &e)
		return e.Message, e.Data.Fields
	}

	// English by default
	code, b, _ := h.call("POST", "/taxes", map[string]any{})
	if msg, fields := errorBody(b); code != 422 || msg != "Some fields are invalid." || fields["name"][0] != "Required" {
		t.Fatalf("en: %d %s", code, b)
	}
	// Bahasa Indonesia via Accept-Language
	h.lang = "id-ID,id;q=0.9,en;q=0.8"
	code, b, hdr := h.call("POST", "/taxes", map[string]any{})
	if msg, fields := errorBody(b); code != 422 || msg != "Beberapa isian tidak valid." || fields["name"][0] != "Wajib diisi" {
		t.Fatalf("id: %d %s", code, b)
	}
	if hdr.Get("Content-Language") != "id" {
		t.Errorf("Content-Language = %q", hdr.Get("Content-Language"))
	}
	code, b, _ = h.call("GET", "/products/00000000-0000-7000-8000-000000000000", nil)
	if msg, _ := errorBody(b); code != 404 || msg != "Produk tidak ditemukan" {
		t.Fatalf("id 404: %d %s", code, b)
	}
	code, b, _ = h.call("POST", "/sales", map[string]any{"items": []any{map[string]any{"quantity": 0}}})
	if _, fields := errorBody(b); code != 422 || !strings.Contains(strings.Join(fields["items"], " "), "Baris 1") {
		t.Fatalf("id row message: %d %s", code, b)
	}
	// files: column headers and title in the request language
	code, b, _ = h.call("GET", "/reports/daily-sales/export?format=csv&start_date=2020-01-01&end_date=2030-01-01", nil)
	if code != 200 || !strings.Contains(string(b), "Tanggal") {
		t.Fatalf("id csv: %d %.200s", code, b)
	}
	// notifications are stored untranslated and shown in the reader's language
	var store string
	if err := h.a.DB.QueryRow(t.Context(), `SELECT store_id FROM users WHERE email='demo@lestari.id'`).Scan(&store); err != nil {
		t.Fatal(err)
	}
	if _, err := h.a.DB.Exec(t.Context(), `INSERT INTO notifications (store_id, source, title, message, params) VALUES ($1,'back_office','Transfer accepted','Transfer {number} was received.','{"number":"TR-1"}')`, store); err != nil {
		t.Fatal(err)
	}
	code, b, _ = h.call("GET", "/notifications?per_page=50", nil)
	if code != 200 || !strings.Contains(string(b), "Transfer TR-1 telah diterima.") || strings.Contains(string(b), `"params"`) {
		t.Fatalf("id notifications: %d %.300s", code, b)
	}
	// without Accept-Language the branch's display language decides
	if code, b, _ := h.call("PUT", "/settings/display", map[string]any{"language": "id"}); code != 200 {
		t.Fatalf("display: %d %s", code, b)
	}
	h.lang = ""
	code, b, _ = h.call("POST", "/taxes", map[string]any{})
	if msg, _ := errorBody(b); code != 422 || msg != "Beberapa isian tidak valid." {
		t.Fatalf("display language: %d %s", code, b)
	}
	h.lang = "en"
	code, b, _ = h.call("POST", "/taxes", map[string]any{})
	if msg, _ := errorBody(b); code != 422 || msg != "Some fields are invalid." {
		t.Fatalf("header wins over display language: %d %s", code, b)
	}
	_, _, _ = h.call("PUT", "/settings/display", map[string]any{"language": "en"})
}

func TestTOTP(t *testing.T) {
	s := middlewares.NewTOTPSecret()
	if !middlewares.VerifyTOTP(s, middlewares.TOTPCode(s)) {
		t.Fatal("current code rejected")
	}
	if middlewares.VerifyTOTP(s, "000000") && middlewares.TOTPCode(s) != "000000" {
		t.Fatal("wrong code accepted")
	}
}

// TestIntegrationProductsAndStock covers the product extras, stock adjustments and the stock opname rules.
func TestIntegrationProductsAndStock(t *testing.T) {
	h := setup(t)
	today := time.Now().Format("2006-01-02")
	data := func(m map[string]any) map[string]any { d, _ := m["data"].(map[string]any); return d }

	// a product without a SKU gets the suggested one; a service never tracks stock
	next := data(h.json("GET", "/products/generate-sku", nil, 200))["sku"]
	p := data(h.json("POST", "/products", map[string]any{"name": "Stock probe", "selling_price": 7000, "purchase_price": 4000, "stock": 10}, 201))
	if p["sku"] != next || p["stock"].(float64) != 10 {
		t.Fatalf("created product: sku %v (want %v) stock %v", p["sku"], next, p["stock"])
	}
	pid := p["id"].(string)
	svc := data(h.json("POST", "/products", map[string]any{"name": "Probe service", "selling_price": 1000, "item_type": "service"}, 201))
	if svc["track_stock"] != false {
		t.Errorf("service tracks stock: %v", svc["track_stock"])
	}
	// SKU checks ignore case and can leave out the product being edited
	sku := p["sku"].(string)
	if data(h.json("GET", "/products/check-sku?sku="+strings.ToLower(sku), nil, 200))["available"] != false {
		t.Error("taken SKU reported as available")
	}
	if data(h.json("GET", "/products/check-sku?sku="+sku+"&exclude_id="+pid, nil, 200))["available"] != true {
		t.Error("own SKU reported as taken")
	}
	h.json("GET", "/products/check-sku", nil, 422)
	h.json("POST", "/products", map[string]any{"name": "Same SKU", "selling_price": 1, "sku": sku}, 409)
	// stock cannot be changed through the product, and an unchanged update keeps updated_at
	up := data(h.json("PUT", "/products/"+pid, map[string]any{"stock": 99, "name": "Stock probe"}, 200))
	if up["stock"].(float64) != 10 || up["updated_at"] != p["updated_at"] {
		t.Errorf("update: stock %v updated_at %v (was %v)", up["stock"], up["updated_at"], p["updated_at"])
	}
	// a duplicate has a new SKU, no barcode and no stock
	dup := data(h.json("POST", "/products/"+pid+"/duplicate", nil, 201))
	if dup["sku"] == sku || dup["stock"].(float64) != 0 || dup["name"] != "Stock probe (copy)" {
		t.Errorf("duplicate: %v %v %v", dup["sku"], dup["stock"], dup["name"])
	}
	for _, path := range []string{"/products/barcodes?product_ids[]=" + pid, "/products/barcodes?symbology=qr&paper_size=label_50x25", "/products/labels"} {
		if code, b, _ := h.call("GET", path, nil); code != 200 || len(b) < 200 {
			t.Errorf("%s: status %d, %d bytes", path, code, len(b))
		}
	}

	// adjustments: add with a cost averages the purchase price; an unchanged set records nothing
	adj := data(h.json("POST", "/inventory/stocks/"+pid+"/adjustments", map[string]any{"mode": "add", "quantity": 10, "unit_cost": 6000, "reason": "received"}, 200))
	if adj["stock"].(float64) != 20 || adj["unit_cost"].(float64) != 5000 {
		t.Errorf("add 10 @ 6000 to 10 @ 4000: stock %v cost %v", adj["stock"], adj["unit_cost"])
	}
	h.json("POST", "/inventory/stocks/"+pid+"/adjustments", map[string]any{"mode": "set", "quantity": 20}, 200)
	h.json("POST", "/inventory/stocks/"+pid+"/adjustments", map[string]any{"mode": "subtract", "quantity": 5, "note": "broken"}, 200)
	h.json("POST", "/inventory/stocks/"+pid+"/adjustments", map[string]any{"mode": "double", "quantity": -1, "reason": "whim"}, 422)
	h.json("POST", "/inventory/stocks/0190a1b2-0000-7000-8000-000000000000/adjustments", map[string]any{"mode": "add", "quantity": 1}, 404)
	mv := rows(h.json("GET", "/inventory/stocks/"+pid+"/movements", nil, 200))
	if len(mv) != 3 {
		t.Fatalf("expected 3 movements (opening, add, subtract), got %d", len(mv))
	}
	if last := mv[0].(map[string]any); last["quantity_change"].(float64) != -5 || last["stock_after"].(float64) != 15 || last["note"] != "broken" {
		t.Errorf("latest movement: %v", last)
	}
	if len(rows(h.json("GET", "/inventory/stocks?stock_status=out&q=Stock%20probe", nil, 200))) != 1 {
		t.Error("the duplicate (stock 0) should be the only probe that is out of stock")
	}
	if code, b, _ := h.call("GET", "/inventory/stocks/export?format=csv&q=Stock%20probe", nil); code != 200 || !strings.Contains(string(b), sku) {
		t.Errorf("stock export: status %d", code)
	}

	// stock opname: needs items of this branch, can be rejected once, and cannot be deleted after approval
	opname := map[string]any{"opname_date": today, "performed_by_name": "Test", "performed_by_role": "owner"}
	h.json("POST", "/stock-opnames", opname, 422)
	opname["items"] = []any{map[string]any{"product_id": "0190a1b2-0000-7000-8000-000000000000", "physical_stock": 1}}
	h.json("POST", "/stock-opnames", opname, 422)
	opname["items"] = []any{map[string]any{"product_id": pid, "physical_stock": 12, "note": "recount"}}
	rejected := data(h.json("POST", "/stock-opnames", opname, 201))
	if items, _ := rejected["items"].([]any); len(items) != 1 || items[0].(map[string]any)["difference"].(float64) != -3 || rejected["status"] != "pending" {
		t.Fatalf("created opname: %v", rejected)
	}
	rid := rejected["id"].(string)
	if got := data(h.json("POST", "/stock-opnames/"+rid+"/reject", map[string]any{"reason": "wrong shelf"}, 200)); got["status"] != "rejected" || got["reject_reason"] != "wrong shelf" {
		t.Errorf("rejected opname: %v %v", got["status"], got["reject_reason"])
	}
	h.json("POST", "/stock-opnames/"+rid+"/reject", nil, 409)
	h.json("POST", "/stock-opnames/"+rid+"/approve", nil, 409)
	approved := data(h.json("POST", "/stock-opnames", opname, 201))
	aid := approved["id"].(string)
	if approved["code"] == rejected["code"] {
		t.Errorf("two opnames share the code %v", approved["code"])
	}
	h.json("POST", "/stock-opnames/"+aid+"/approve", nil, 200)
	if r := first(h.json("GET", "/inventory/stocks?q="+sku, nil, 200)); r == nil || r["stock"].(float64) != 12 {
		t.Errorf("stock after approval: %v", r)
	}
	if code, b, _ := h.call("GET", "/stock-opnames/"+aid+"/export?format=csv", nil); code != 200 || !strings.Contains(string(b), sku) {
		t.Errorf("opname export: status %d", code)
	}
	h.json("DELETE", "/stock-opnames/"+aid, nil, 409)
	h.json("POST", "/stock-opnames/bulk-delete", map[string]any{"ids": []any{rid, aid}}, 409)
	h.json("DELETE", "/stock-opnames/"+rid, nil, 204)
	h.json("GET", "/stock-opnames/"+rid, nil, 404)

	// a deleted product disappears from the lists and frees its SKU
	h.json("DELETE", "/products/"+pid, nil, 204)
	h.json("GET", "/products/"+pid, nil, 404)
	if data(h.json("GET", "/products/check-sku?sku="+sku, nil, 200))["available"] != true {
		t.Error("the SKU of a deleted product should be free")
	}
	job := data(h.json("POST", "/inventory/stocks/recalculate", nil, 200))
	if job["status"] != "queued" || job["job_id"] == nil {
		t.Errorf("recalculate: %v", job)
	}
}

// TestIntegrationStockTransfers covers transfers between branches and the per-branch prices.
func TestIntegrationStockTransfers(t *testing.T) {
	h := setup(t)
	today := time.Now().Format("2006-01-02")
	data := func(m map[string]any) map[string]any { d, _ := m["data"].(map[string]any); return d }
	stockOf := func(q string) float64 {
		t.Helper()
		r := first(h.json("GET", "/inventory/stocks?q="+q, nil, 200))
		if r == nil {
			return -1
		}
		return r["stock"].(float64)
	}
	var head, target string
	for _, x := range rows(h.json("GET", "/branches", nil, 200)) {
		if m := x.(map[string]any); m["is_head_office"] == false {
			target = m["id"].(string)
		} else {
			head = m["id"].(string)
		}
	}
	p := data(h.json("POST", "/products", map[string]any{"name": "Transfer probe", "selling_price": 9000, "purchase_price": 5000, "stock": 20}, 201))
	pid, sku := p["id"].(string), p["sku"].(string)
	item := func(qty float64) []any { return []any{map[string]any{"product_id": pid, "quantity": qty}} }

	// validation: another branch, known products, enough stock
	h.json("POST", "/stock-transfers", map[string]any{"to_branch_id": head, "transfer_date": today, "items": item(1)}, 422)
	h.json("POST", "/stock-transfers", map[string]any{"to_branch_id": "0190a1b2-0000-7000-8000-000000000000", "transfer_date": today, "items": item(1)}, 422)
	h.json("POST", "/stock-transfers", map[string]any{"to_branch_id": target, "transfer_date": today}, 422)
	h.json("POST", "/stock-transfers", map[string]any{"to_branch_id": target, "transfer_date": today, "items": item(21)}, 422)
	if got := stockOf(sku); got != 20 {
		t.Fatalf("a refused transfer changed stock: %v", got)
	}

	// without confirmation the stock arrives at once and the product is created in the receiving branch
	direct := data(h.json("POST", "/stock-transfers", map[string]any{"to_branch_id": target, "transfer_date": today, "items": item(4)}, 201))
	if direct["status"] != "completed" || direct["direction"] != "out" || direct["total_quantity"].(float64) != 4 {
		t.Errorf("direct transfer: %v %v %v", direct["status"], direct["direction"], direct["total_quantity"])
	}
	if items, _ := direct["items"].([]any); len(items) != 1 || items[0].(map[string]any)["received_quantity"].(float64) != 4 {
		t.Errorf("direct transfer items: %v", direct["items"])
	}
	h.json("DELETE", "/stock-transfers/"+direct["id"].(string), nil, 409)

	// with confirmation: 6 sent, 4 received, 2 go back
	pending := data(h.json("POST", "/stock-transfers", map[string]any{"to_branch_id": target, "transfer_date": today, "requires_confirmation": true, "note": "restock", "items": item(6)}, 201))
	tid := pending["id"].(string)
	if pending["status"] != "pending" || stockOf(sku) != 10 {
		t.Errorf("pending transfer: status %v, sender stock %v", pending["status"], stockOf(sku))
	}
	h.json("POST", "/stock-transfers/"+tid+"/accept", map[string]any{}, 403) // the sender cannot accept
	cancelled := data(h.json("POST", "/stock-transfers", map[string]any{"to_branch_id": target, "transfer_date": today, "requires_confirmation": true, "items": item(3)}, 201))
	rejected := data(h.json("POST", "/stock-transfers", map[string]any{"to_branch_id": target, "transfer_date": today, "requires_confirmation": true, "items": item(2)}, 201))
	if len(rows(h.json("GET", "/stock-transfers?direction=out&status=pending&q=TRF", nil, 200))) < 3 || len(rows(h.json("GET", "/stock-transfers?direction=in&status=pending", nil, 200))) != 0 {
		t.Error("direction filter of the sender")
	}

	h.branch = target
	if got := stockOf(sku); got != 4 {
		t.Errorf("receiver stock after the direct transfer: %v", got)
	}
	if seen := data(h.json("GET", "/stock-transfers/"+tid, nil, 200)); seen["direction"] != "in" || seen["from_branch_name"] == "" {
		t.Errorf("transfer seen by the receiver: %v %v", seen["direction"], seen["from_branch_name"])
	}
	h.json("DELETE", "/stock-transfers/"+tid, nil, 403) // the receiver cannot delete
	h.json("POST", "/stock-transfers/"+tid+"/accept", map[string]any{"items": []any{map[string]any{"product_id": pid, "received_quantity": 7}}}, 422)
	accepted := data(h.json("POST", "/stock-transfers/"+tid+"/accept", map[string]any{"items": []any{map[string]any{"product_id": pid, "received_quantity": 4}}, "note": "two missing"}, 200))
	if accepted["status"] != "accepted" || accepted["note"] != "two missing" || accepted["items"].([]any)[0].(map[string]any)["received_quantity"].(float64) != 4 {
		t.Errorf("accepted transfer: %v", accepted)
	}
	h.json("POST", "/stock-transfers/"+tid+"/accept", map[string]any{}, 409)
	h.json("POST", "/stock-transfers/"+rejected["id"].(string)+"/reject", map[string]any{}, 422)
	if got := data(h.json("POST", "/stock-transfers/"+rejected["id"].(string)+"/reject", map[string]any{"reason": "not ordered"}, 200)); got["status"] != "rejected" || got["reject_reason"] != "not ordered" {
		t.Errorf("rejected transfer: %v %v", got["status"], got["reject_reason"])
	}
	if got := stockOf(sku); got != 8 {
		t.Errorf("receiver stock after accepting 4 of 6: %v", got)
	}
	if code, b, _ := h.call("GET", "/stock-transfers/"+tid+"/print", nil); code != 200 || !strings.HasPrefix(string(b), "%PDF") {
		t.Errorf("print: status %d", code)
	}
	if len(rows(h.json("GET", "/notifications", nil, 200))) == 0 {
		t.Error("the receiving branch got no notification")
	}

	h.branch = ""
	h.json("DELETE", "/stock-transfers/"+cancelled["id"].(string), nil, 204)
	h.json("GET", "/stock-transfers/"+cancelled["id"].(string), nil, 404)
	// 20 - 4 (direct) - 6 + 2 (not received) - 3 + 3 (cancelled) - 2 + 2 (rejected)
	if got := stockOf(sku); got != 12 {
		t.Errorf("sender stock at the end: %v", got)
	}

	// branch products: the copy in the receiving branch keeps its own price until prices are synced
	list := rows(h.json("GET", "/branch-products?branch_id="+target+"&q="+sku, nil, 200))
	if len(list) != 1 {
		t.Fatalf("expected the copy in the receiving branch, got %d rows", len(list))
	}
	bp := list[0].(map[string]any)
	bpid := bp["id"].(string)
	if bp["stock"].(float64) != 8 || bp["selling_price"].(float64) != 9000 {
		t.Errorf("branch product: %v", bp)
	}
	same := data(h.json("PUT", "/branch-products/"+bpid, map[string]any{"selling_price": 9000}, 200))
	if same["updated_at"] != bp["updated_at"] {
		t.Error("an unchanged branch product got a new updated_at")
	}
	changed := data(h.json("PUT", "/branch-products/"+bpid, map[string]any{"selling_price": 9500, "is_available": false}, 200))
	if changed["selling_price"].(float64) != 9500 || changed["is_available"] != false || changed["updated_at"] == bp["updated_at"] {
		t.Errorf("changed branch product: %v", changed)
	}
	h.json("PUT", "/branch-products/"+bpid, map[string]any{"selling_price": nil}, 422)
	h.json("PUT", "/branch-products/0190a1b2-0000-7000-8000-000000000000", map[string]any{"selling_price": 1}, 404)
	h.json("POST", "/branch-products/sync-prices", map[string]any{}, 422)
	if got := data(h.json("POST", "/branch-products/sync-prices", map[string]any{"branch_ids": []any{target}, "product_ids": []any{pid}}, 200)); got["updated"].(float64) != 1 {
		t.Errorf("sync prices: %v", got)
	}
	if got := data(h.json("POST", "/branch-products/sync-prices", map[string]any{"branch_ids": []any{target}, "product_ids": []any{pid}}, 200)); got["updated"].(float64) != 0 {
		t.Errorf("second sync should change nothing: %v", got)
	}
	if code, b, _ := h.call("GET", "/branch-products/export?format=csv&q="+sku, nil); code != 200 || !strings.Contains(string(b), sku) {
		t.Errorf("branch product export: status %d", code)
	}
}

func TestIntegrationSalesAndPurchases(t *testing.T) {
	h := setup(t)
	today := time.Now().Format("2006-01-02")
	data := func(m map[string]any) map[string]any { d, _ := m["data"].(map[string]any); return d }
	num := func(m map[string]any, key string) float64 { f, _ := m[key].(float64); return f }
	stock := func(id string) float64 { return num(data(h.json("GET", "/products/"+id, nil, 200)), "stock") }

	p := data(h.json("POST", "/products", map[string]any{"name": "Trade probe", "selling_price": 10000, "purchase_price": 4000, "stock": 10, "show_in_online_store": true}, 201))
	pid := p["id"].(string)
	supplier := data(h.json("POST", "/suppliers", map[string]any{"name": "Trade probe supplier"}, 201))["id"]
	customer := data(h.json("POST", "/customers", map[string]any{"name": "Trade probe customer"}, 201))["id"]

	// a credit purchase receives stock, averages the cost and becomes a payable
	h.json("POST", "/purchases", map[string]any{"purchase_date": today, "payment_type": "credit", "items": []any{map[string]any{"product_id": pid, "quantity": 5, "unit_cost": 5000}}}, 422)
	h.json("POST", "/purchases", map[string]any{"purchase_date": today, "items": []any{}}, 422)
	pu := data(h.json("POST", "/purchases", map[string]any{"purchase_date": today, "payment_type": "credit", "supplier_id": supplier,
		"items": []any{map[string]any{"product_id": pid, "quantity": 5, "unit_cost": 5000}}}, 201))
	puID := pu["id"].(string)
	if pu["status"] != "unpaid" || num(pu, "grand_total") != 25000 || !strings.HasPrefix(pu["receipt_number"].(string), "PO/") || len(pu["items"].([]any)) != 1 {
		t.Fatalf("purchase: %v", pu)
	}
	prod := data(h.json("GET", "/products/"+pid, nil, 200))
	if num(prod, "stock") != 15 || num(prod, "purchase_price") != 4333.33 {
		t.Errorf("after purchase: stock %v cost %v", prod["stock"], prod["purchase_price"])
	}
	// an update that changes nothing keeps updated_at; new items replace the stock of the old ones
	same := data(h.json("PUT", "/purchases/"+puID, map[string]any{"note": nil, "payment_type": "credit"}, 200))
	if same["updated_at"] != pu["updated_at"] {
		t.Errorf("unchanged purchase was written: %v → %v", pu["updated_at"], same["updated_at"])
	}
	changed := data(h.json("PUT", "/purchases/"+puID, map[string]any{"note": "probe", "items": []any{map[string]any{"product_id": pid, "quantity": 2, "unit_cost": 5000}}}, 200))
	if num(changed, "grand_total") != 10000 || changed["note"] != "probe" || stock(pid) != 12 {
		t.Errorf("edited purchase: total %v note %v stock %v", changed["grand_total"], changed["note"], stock(pid))
	}
	pay := first(h.json("GET", "/payables?q="+strings.ReplaceAll(pu["receipt_number"].(string), "/", "%2F"), nil, 200))
	if pay == nil || num(pay, "remaining_amount") != 10000 {
		t.Fatalf("payable: %v", pay)
	}
	payID := pay["id"].(string)
	h.json("POST", "/payables/"+payID+"/payments", map[string]any{"amount": 99999, "payment_date": today, "payment_method": "cash"}, 422)
	h.json("POST", "/payables/"+payID+"/payments", map[string]any{"amount": 4000, "payment_date": "soon", "payment_method": "shells"}, 422)
	part := data(h.json("POST", "/payables/"+payID+"/payments", map[string]any{"amount": 4000, "payment_date": today, "payment_method": "cash"}, 200))
	if part["status"] != "unpaid" || num(part, "remaining_amount") != 6000 {
		t.Errorf("part payment: %v", part)
	}
	// a return to the supplier takes stock out and may reduce what is owed
	h.json("POST", "/purchases/"+puID+"/returns", map[string]any{"items": []any{map[string]any{"product_id": pid, "quantity": 3}}}, 422)
	ret := data(h.json("POST", "/purchases/"+puID+"/returns", map[string]any{"refund_method": "reduce_payable", "items": []any{map[string]any{"product_id": pid, "quantity": 1}}}, 201))
	if num(ret, "refund_amount") != 5000 || !strings.HasPrefix(ret["return_number"].(string), "RB/") || stock(pid) != 11 {
		t.Errorf("purchase return: %v, stock %v", ret, stock(pid))
	}
	full := data(h.json("POST", "/payables/"+payID+"/payments", map[string]any{"amount": 1000, "payment_date": today, "payment_method": "bank_transfer"}, 200))
	if full["status"] != "paid" {
		t.Errorf("payable after the last payment: %v", full)
	}
	h.json("POST", "/payables/"+payID+"/payments", map[string]any{"amount": 1, "payment_date": today, "payment_method": "cash"}, 409)
	if got := data(h.json("GET", "/purchases/"+puID, nil, 200))["status"]; got != "completed" {
		t.Errorf("purchase after its payable was paid: %v", got)
	}
	if len(data(h.json("GET", "/payables/"+payID, nil, 200))["payments"].([]any)) != 2 {
		t.Error("the payable should list two payments")
	}

	// a cash sale takes stock and gives change; it cannot sell more than there is
	item := func(q float64) []any { return []any{map[string]any{"product_id": pid, "quantity": q}} }
	h.json("POST", "/sales", map[string]any{"items": item(99)}, 422)
	h.json("POST", "/sales", map[string]any{"items": item(1), "paid_amount": 5000}, 422)
	h.json("POST", "/sales", map[string]any{"items": item(1), "payment_type": "credit"}, 422)
	sale := data(h.json("POST", "/sales", map[string]any{"items": item(2), "paid_amount": 50000, "label": "Probe"}, 201))
	saleID := sale["id"].(string)
	if num(sale, "grand_total") != 20000 || num(sale, "change_amount") != 30000 || num(sale, "paid_amount") != 20000 || sale["status"] != "completed" ||
		!strings.HasPrefix(sale["receipt_number"].(string), "S/") || len(sale["items"].([]any)) != 1 || stock(pid) != 9 {
		t.Fatalf("sale: %v, stock %v", sale, stock(pid))
	}
	h.json("POST", "/sales", map[string]any{"items": item(1), "receipt_number": sale["receipt_number"]}, 409)
	// a credit sale becomes a receivable; paying it completes the sale
	credit := data(h.json("POST", "/sales", map[string]any{"items": item(1), "payment_type": "credit", "customer_id": customer, "paid_amount": 0}, 201))
	if credit["status"] != "unpaid" {
		t.Errorf("credit sale: %v", credit["status"])
	}
	rec := first(h.json("GET", fmt.Sprintf("/receivables?customer_id=%v", customer), nil, 200))
	if rec == nil || num(rec, "remaining_amount") != 10000 || rec["customer_name"] != "Trade probe customer" {
		t.Fatalf("receivable: %v", rec)
	}
	h.json("POST", "/receivables/"+rec["id"].(string)+"/payments", map[string]any{"amount": 10000, "payment_date": today, "payment_method": "qris"}, 200)
	if got := data(h.json("GET", "/sales/"+credit["id"].(string), nil, 200))["status"]; got != "completed" {
		t.Errorf("credit sale after payment: %v", got)
	}
	// a customer return puts stock back; deleting a sale does too
	h.json("POST", "/sales/"+saleID+"/returns", map[string]any{"items": item(5)}, 422)
	back := data(h.json("POST", "/sales/"+saleID+"/returns", map[string]any{"reason": "probe", "items": item(1)}, 201))
	if num(back, "refund_amount") != 10000 || !strings.HasPrefix(back["return_number"].(string), "RJ/") || stock(pid) != 9 {
		t.Errorf("sales return: %v, stock %v", back, stock(pid))
	}
	h.json("GET", "/sales-returns/"+back["id"].(string), nil, 200)
	h.json("DELETE", "/sales/"+saleID, nil, 204)
	h.json("GET", "/sales/"+saleID, nil, 404)
	if stock(pid) != 11 {
		t.Errorf("stock after deleting the sale: %v", stock(pid))
	}

	// shifts: one open shift per cash drawer
	drawer := data(h.json("POST", "/cash-drawers", map[string]any{"name": "Probe drawer"}, 201))["id"]
	shift := data(h.json("POST", "/shifts", map[string]any{"cash_drawer_id": drawer, "opening_cash": 100000}, 201))
	h.json("POST", "/shifts", map[string]any{"cash_drawer_id": drawer}, 409)
	in := data(h.json("POST", "/sales", map[string]any{"items": item(1), "shift_id": shift["id"]}, 201))
	closed := data(h.json("POST", "/shifts/"+shift["id"].(string)+"/close", map[string]any{"actual_cash": 109000}, 200))
	if num(closed, "expected_cash") != 110000 || num(closed, "difference") != -1000 || len(closed["payments"].([]any)) != 1 {
		t.Errorf("closed shift: %v", closed)
	}
	h.json("POST", "/shifts/"+shift["id"].(string)+"/close", map[string]any{"actual_cash": 1}, 409)
	h.json("DELETE", "/sales/"+in["id"].(string), nil, 204)

	// online orders take stock while they are being processed
	order := data(h.json("POST", "/catalog/orders", map[string]any{"customer_name": "Probe buyer", "shipping_fee": 5000, "items": item(2)}, 201))
	orderID := order["id"].(string)
	if order["status"] != "unpaid" || num(order, "grand_total") != 25000 || !strings.HasPrefix(order["order_number"].(string), "ORD/") {
		t.Fatalf("order: %v", order)
	}
	h.json("POST", "/catalog/orders", map[string]any{"items": item(1)}, 422)
	h.json("PATCH", "/catalog/orders/"+orderID+"/status", map[string]any{"status": "delivered"}, 409)
	h.json("PATCH", "/catalog/orders/"+orderID+"/status", map[string]any{"status": "processing"}, 200)
	if stock(pid) != 9 {
		t.Errorf("stock while the order is processed: %v", stock(pid))
	}
	h.json("PATCH", "/catalog/orders/"+orderID+"/status", map[string]any{"status": "cancelled"}, 422)
	done := data(h.json("PATCH", "/catalog/orders/"+orderID+"/status", map[string]any{"status": "cancelled", "reason": "probe"}, 200))
	if done["status"] != "cancelled" || done["status_reason"] != "probe" || stock(pid) != 11 {
		t.Errorf("cancelled order: %v, stock %v", done["status"], stock(pid))
	}
	account := data(h.json("POST", "/catalog/payment-accounts", map[string]any{"type": "bank", "provider": "BCA", "account_name": "Probe", "account_number": "123"}, 201))
	h.json("POST", "/catalog/payment-accounts", map[string]any{"type": "qris", "provider": "QRIS", "account_name": "Probe"}, 422)
	h.json("DELETE", "/catalog/payment-accounts/"+account["id"].(string), nil, 204)

	// reports: summaries, totals rows and details
	daily := data(h.json("GET", "/reports/daily-sales?start_date="+today+"&end_date="+today, nil, 200))
	if s, _ := daily["summary"].(map[string]any); s == nil || num(s, "transaction_count") < 1 || daily["totals"] == nil {
		t.Errorf("daily sales: %v", daily)
	}
	pl := data(h.json("GET", "/reports/profit-loss?start_date="+today+"&end_date="+today, nil, 200))
	if s, _ := pl["summary"].(map[string]any); s == nil || num(s, "purchase_total") < 10000 || len(pl["rows"].([]any)) != 1 {
		t.Errorf("profit and loss: %v", pl)
	}
	flow := data(h.json("GET", "/reports/cash-flow?start_date="+today+"&end_date="+today+"&type=expense&per_page=5", nil, 200))
	if s, _ := flow["summary"].(map[string]any); s == nil || num(s, "total_income") != 0 || num(s, "total_expense") <= 0 {
		t.Errorf("cash flow (expenses): %v", flow["summary"])
	}
	week := rows(h.json("GET", "/reports/visitors?group_by=weekday&start_date="+today+"&end_date="+today, nil, 200))
	if len(week) != 1 || week[0].(map[string]any)["period"] != time.Now().Weekday().String() {
		t.Errorf("visitors by weekday: %v", week)
	}
	if got := data(h.json("GET", "/reports/sales-by-product/"+pid, nil, 200))["sales"]; len(got.([]any)) < 1 {
		t.Errorf("sales of the product: %v", got)
	}
	dash := data(h.json("GET", "/dashboard", nil, 200))
	if s, _ := dash["summary"].(map[string]any); s == nil || s["compared_to_previous"] == nil || len(dash["hourly"].([]any)) != 24 {
		t.Errorf("dashboard: %v", dash["summary"])
	}
	h.json("GET", "/dashboard?date=yesterday", nil, 422)
}
