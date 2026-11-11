package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// businessOnly returns an add function whose routes require the business plugin.
func (a *API) businessOnly() func(method, path string, h fiber.Handler) {
	return func(method, path string, h fiber.Handler) {
		a.add(method, path, middlewares.RequirePlugin("business"), h)
	}
}

func (a *API) registerContacts() {
	a.addH("POST", "/customers/:id/points", a.adjustPoints)
	a.addH("GET", "/customers/:id/point-history", a.pointHistory)
	a.addH("GET", "/customers/qr-codes", a.customerQRCodes)
	a.addH("POST", "/customers/points/import", a.importPoints)

}

// changePoints updates a customer's balance and ledger inside tx.
func changePoints(ctx context.Context, tx pgx.Tx, branchID, customerID string, delta int, typ, ref, note string) (int, error) {
	var after int
	err := tx.QueryRow(ctx, `UPDATE customers SET points = GREATEST(points + $3, 0), updated_at = now() WHERE id = $1 AND branch_id = $2 RETURNING points`,
		customerID, branchID, delta).Scan(&after)
	if repositories.IsNoRows(err) {
		return 0, helper.NotFound("Customer")
	}
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO point_ledger (customer_id, type, points, balance_after, reference, note) VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''))`,
		customerID, typ, delta, after, ref, note)
	return after, err
}

func (a *API) adjustPoints(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	var in struct {
		Mode   string  `json:"mode"`
		Points float64 `json:"points"`
		Note   string  `json:"note"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	if in.Points < 1 {
		return helper.Invalid("points", "Must be at least 1.")
	}
	delta := int(in.Points)
	if in.Mode == "subtract" {
		delta = -delta
	} else if in.Mode != "add" {
		return helper.Invalid("mode", "Must be one of: add, subtract.")
	}
	var after int
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		after, err = changePoints(c.Context(), tx, helper.P(c).BranchID, id, delta, "adjusted", "", in.Note)
		return err
	})
	if err != nil {
		return err
	}
	return helper.OK(c, fiber.Map{"points": after})
}

func (a *API) pointHistory(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	p := helper.P(c)
	pg := helper.Paging(c, 50)
	var total int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM point_ledger l JOIN customers cu ON cu.id = l.customer_id WHERE l.customer_id=$1 AND cu.branch_id=$2`, id, p.BranchID).Scan(&total); err != nil {
		return err
	}
	raw, err := repositories.JSONRaw(c.Context(), a.DB, `SELECT l.id, l.created_at, l.type, l.points, l.balance_after, l.reference, l.note
		FROM point_ledger l JOIN customers cu ON cu.id = l.customer_id WHERE l.customer_id=$1 AND cu.branch_id=$2
		ORDER BY l.id DESC LIMIT $3 OFFSET $4`, id, p.BranchID, pg.PerPage, pg.Offset)
	if err != nil {
		return err
	}
	return helper.List(c, raw, helper.NewMeta(pg, total))
}

func (a *API) customerQRCodes(c fiber.Ctx) error {
	ids := []string{}
	for _, s := range helper.QueryArray(c, "customer_ids") {
		if s = strings.ToLower(strings.TrimSpace(s)); helper.IsUUID(s) {
			ids = append(ids, s)
		}
	}
	rows, err := repositories.JSONRows(c.Context(), a.DB, `SELECT id, name, COALESCE(NULLIF(code,''), 'C'||upper(right(replace(id::text,'-',''),8))) AS code, phone FROM customers
		WHERE branch_id=$1 AND (cardinality($2::uuid[]) = 0 OR id = ANY($2)) ORDER BY name LIMIT 2000`, helper.P(c).BranchID, ids)
	if err != nil {
		return err
	}
	d := export.NewPDF(595, 842)
	tr := helper.Lang(c).T
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
		d.Text(40, 780, 12, false, tr("No customers selected."))
	}
	return export.Send(c, "customer-qr-codes.pdf", "application/pdf", d.Bytes())
}

func (a *API) importPoints(c fiber.Ctx) error {
	rows, err := crud.ImportRows(c)
	if err != nil {
		return err
	}
	mode := c.FormValue("mode", "add")
	p := helper.P(c)
	res := fiber.Map{}
	updated, failed := 0, 0
	errs := []crud.ImportError{}
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		for i, r := range rows {
			code := helper.Str(r, "code")
			pts, ok := helper.Num(r, "points")
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
	return helper.OK(c, res)
}
