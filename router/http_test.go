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
	"github.com/roysitumorang/lestari-backend/internal/api"
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
	a      *api.API
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
	app, a := q.NewApp(slog.New(slog.NewTextHandler(io.Discard, nil))), q.API
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
	return &harness{t: t, app: app, a: a, token: tok.AccessToken}
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
	routes := h.a.Routes()
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
