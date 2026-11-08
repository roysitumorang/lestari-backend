package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/roysitumorang/lestari-backend/internal/auth"
	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/export"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

// businessOnly returns an add function whose routes require the business plugin.
func (a *API) businessOnly() func(method, path string, h fiber.Handler) {
	return func(method, path string, h fiber.Handler) { a.add(method, path, auth.RequirePlugin("business"), h) }
}

func (a *API) registerContacts() {
	customers := &crud.Resource{
		Name: "Customers", Singular: "Customer", Table: "customers", ImportKey: "code", DefaultSort: "t.name",
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "phone", Search: true},
			{Name: "email", Search: true},
			{Name: "code", Search: true},
			{Name: "customer_type_id", Kind: crud.UUID, Filter: true, Ref: &crud.Ref{Key: "customer_type", Table: "customer_types"}},
			{Name: "birth_date", Kind: crud.Date},
			{Name: "points", Kind: crud.Int, ReadOnly: true},
			{Name: "receivable_balance", Kind: crud.Num, Expr: "(SELECT COALESCE(sum(d.total_amount - d.paid_amount), 0) FROM debts d WHERE d.customer_id = t.id AND d.kind = 'receivable' AND d.status <> 'paid')"},
			{Name: "address"},
			{Name: "note"},
		},
	}
	a.CRUD.Register(a.addH, "/customers", customers, "list", "create", "read", "update", "delete", "bulk_delete", "import", "export")
	a.addH("POST", "/customers/:id/points", a.adjustPoints)
	a.addH("GET", "/customers/:id/point-history", a.pointHistory)
	a.addH("GET", "/customers/qr-codes", a.customerQRCodes)
	a.addH("POST", "/customers/points/import", a.importPoints)

	customerTypes := &crud.Resource{
		Name: "Customer types", Singular: "Customer type", Table: "customer_types", DefaultSort: "t.name",
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "discount_type", Enum: en("percent", "amount")},
			{Name: "discount_value", Kind: crud.Num, Min: new(float64(0))},
			{Name: "point_multiplier", Kind: crud.Num, Min: new(float64(0)), Default: 1.0},
			{Name: "customer_count", Kind: crud.Int, Expr: "(SELECT count(*) FROM customers c WHERE c.customer_type_id = t.id)"},
			{Name: "description"},
		},
	}
	// listing stays open so lookups work everywhere; changes need the business plugin
	a.CRUD.Register(a.addH, "/customer-types", customerTypes, "list", "read")
	a.CRUD.Register(a.businessOnly(), "/customer-types", customerTypes, "create", "update", "delete")

	suppliers := &crud.Resource{
		Name: "Suppliers", Singular: "Supplier", Table: "suppliers", ImportKey: "code", DefaultSort: "t.name",
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "contact_person", Search: true},
			{Name: "phone", Search: true},
			{Name: "email", Search: true},
			{Name: "code", Search: true},
			{Name: "payable_balance", Kind: crud.Num, Expr: "(SELECT COALESCE(sum(d.total_amount - d.paid_amount), 0) FROM debts d WHERE d.supplier_id = t.id AND d.kind = 'payable' AND d.status <> 'paid')"},
			{Name: "address"},
		},
	}
	a.CRUD.Register(a.addH, "/suppliers", suppliers, "list", "create", "read", "update", "delete", "bulk_delete", "import", "export")
}

// changePoints updates a customer's balance and ledger inside tx.
func changePoints(ctx context.Context, tx pgx.Tx, branchID, customerID string, delta int, typ, ref, note string) (int, error) {
	var after int
	err := tx.QueryRow(ctx, `UPDATE customers SET points = GREATEST(points + $3, 0), updated_at = now() WHERE id = $1 AND branch_id = $2 RETURNING points`,
		customerID, branchID, delta).Scan(&after)
	if db.IsNoRows(err) {
		return 0, httpx.NotFound("Customer")
	}
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO point_ledger (customer_id, type, points, balance_after, reference, note) VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''))`,
		customerID, typ, delta, after, ref, note)
	return after, err
}

func (a *API) adjustPoints(c fiber.Ctx) error {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		return err
	}
	var in struct {
		Mode   string  `json:"mode"`
		Points float64 `json:"points"`
		Note   string  `json:"note"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	if in.Points < 1 {
		return httpx.Invalid("points", "Must be at least 1.")
	}
	delta := int(in.Points)
	if in.Mode == "subtract" {
		delta = -delta
	} else if in.Mode != "add" {
		return httpx.Invalid("mode", "Must be one of: add, subtract.")
	}
	var after int
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		after, err = changePoints(c.Context(), tx, httpx.P(c).BranchID, id, delta, "adjusted", "", in.Note)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.OK(c, fiber.Map{"points": after})
}

func (a *API) pointHistory(c fiber.Ctx) error {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		return err
	}
	p := httpx.P(c)
	pg := httpx.Pagination(c, 50)
	var total int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM point_ledger l JOIN customers cu ON cu.id = l.customer_id WHERE l.customer_id=$1 AND cu.branch_id=$2`, id, p.BranchID).Scan(&total); err != nil {
		return err
	}
	raw, err := db.JSONRaw(c.Context(), a.DB, `SELECT l.id, l.created_at, l.type, l.points, l.balance_after, l.reference, l.note
		FROM point_ledger l JOIN customers cu ON cu.id = l.customer_id WHERE l.customer_id=$1 AND cu.branch_id=$2
		ORDER BY l.id DESC LIMIT $3 OFFSET $4`, id, p.BranchID, pg.PerPage, pg.Offset)
	if err != nil {
		return err
	}
	return httpx.List(c, raw, httpx.NewMeta(pg, total))
}

func (a *API) customerQRCodes(c fiber.Ctx) error {
	ids := []string{}
	for _, s := range httpx.QueryArray(c, "customer_ids") {
		if s = strings.ToLower(strings.TrimSpace(s)); httpx.IsUUID(s) {
			ids = append(ids, s)
		}
	}
	rows, err := db.JSONRows(c.Context(), a.DB, `SELECT id, name, COALESCE(NULLIF(code,''), 'C'||upper(right(replace(id::text,'-',''),8))) AS code, phone FROM customers
		WHERE branch_id=$1 AND (cardinality($2::uuid[]) = 0 OR id = ANY($2)) ORDER BY name LIMIT 2000`, httpx.P(c).BranchID, ids)
	if err != nil {
		return err
	}
	d := export.NewPDF(595, 842)
	card := c.Query("layout") == "card"
	cellW, cellH := 180.0, 200.0
	if card {
		cellW, cellH = 260, 150
	}
	cols := int((595 - 36) / cellW)
	perPage := cols * int((842-36)/cellH)
	for i, r := range rows {
		if i%perPage == 0 {
			d.Page()
		}
		slot := i % perPage
		x := 18 + float64(slot%cols)*cellW
		top := 824 - float64(slot/cols)*cellH
		code := fmt.Sprint(r["code"])
		size := cellH - 60
		if card {
			size = cellH - 30
		}
		if q, err := qrcode.New(code, qrcode.Medium); err == nil {
			d.Matrix(x+6, top-size-6, size, q.Bitmap())
		}
		tx := x + 6
		ty := top - size - 22
		if card {
			tx, ty = x+size+16, top-30
		}
		d.Text(tx, ty, 10, true, export.Fit(fmt.Sprint(r["name"]), cellW-size-20+map[bool]float64{true: 0, false: size}[card], 10))
		d.Text(tx, ty-14, 9, false, code)
	}
	if len(rows) == 0 {
		d.Text(40, 780, 12, false, "No customers selected.")
	}
	return export.Send(c, "customer-qr-codes.pdf", "application/pdf", d.Bytes())
}

func (a *API) importPoints(c fiber.Ctx) error {
	rows, err := crud.ImportRows(c)
	if err != nil {
		return err
	}
	mode := c.FormValue("mode", "add")
	p := httpx.P(c)
	res := fiber.Map{}
	updated, failed := 0, 0
	errs := []crud.ImportError{}
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		for i, r := range rows {
			code := httpx.Str(r, "code")
			pts, ok := httpx.Num(r, "points")
			if code == "" || !ok {
				failed++
				errs = append(errs, crud.ImportError{Row: i + 2, Message: "code and points are required"})
				continue
			}
			var id string
			var cur int
			if err := tx.QueryRow(c.Context(), `SELECT id, points FROM customers WHERE branch_id=$1 AND code=$2`, p.BranchID, code).Scan(&id, &cur); err != nil {
				failed++
				errs = append(errs, crud.ImportError{Row: i + 2, Message: "No customer with code " + code})
				continue
			}
			delta := int(pts)
			if mode == "set" {
				delta = int(pts) - cur
			}
			if _, err := changePoints(c.Context(), tx, p.BranchID, id, delta, "adjusted", "import", ""); err != nil {
				return err
			}
			updated++
		}
		return nil
	})
	if err != nil {
		return err
	}
	res["updated"], res["failed"], res["errors"] = updated, failed, errs
	return httpx.OK(c, res)
}
