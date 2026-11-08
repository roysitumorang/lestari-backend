package api

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/export"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

// startJob records a background job and runs fn in a goroutine. It returns the job id.
func (a *API) startJob(storeID string, kind string, payload any, fn func(ctx context.Context) (any, error)) (string, error) {
	var id string
	pb, _ := json.Marshal(payload)
	if err := a.DB.QueryRow(context.Background(), `INSERT INTO jobs (store_id, kind, status, payload) VALUES ($1,$2,'queued',$3::jsonb) RETURNING id`, storeID, kind, string(pb)).Scan(&id); err != nil {
		return "", err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_, _ = a.DB.Exec(ctx, `UPDATE jobs SET status='running' WHERE id=$1`, id)
		res, err := fn(ctx)
		if err != nil {
			a.Log.Error("job failed", "job", id, "kind", kind, "err", err)
			_, _ = a.DB.Exec(ctx, `UPDATE jobs SET status='failed', error=$2, finished_at=now() WHERE id=$1`, id, err.Error())
			return
		}
		rb, _ := json.Marshal(res)
		_, _ = a.DB.Exec(ctx, `UPDATE jobs SET status='completed', result=$2::jsonb, finished_at=now() WHERE id=$1`, id, string(rb))
	}()
	return id, nil
}

func (a *API) stockResource() *crud.Resource {
	return &crud.Resource{
		Name: "Stock", Singular: "Stock", Table: "products", IDName: "product_id", SoftDelete: true,
		Where: "t.track_stock", DefaultSort: "t.name",
		Fields: []crud.Field{
			{Name: "image_url", ReadOnly: true},
			{Name: "name", ReadOnly: true, Search: true},
			{Name: "sku", ReadOnly: true, Search: true},
			{Name: "category_id", Kind: crud.UUID, ReadOnly: true, Filter: true},
			{Name: "category_name", Expr: "(SELECT name FROM categories WHERE id = t.category_id)"},
			{Name: "stock", Kind: crud.Num, ReadOnly: true},
			{Name: "unit", ReadOnly: true},
			{Name: "minimum_stock", Kind: crud.Num, ReadOnly: true},
			{Name: "unit_cost", Kind: crud.Num, Expr: "t.purchase_price"},
			{Name: "stock_value", Kind: crud.Num, Expr: "round(GREATEST(t.stock, 0) * t.purchase_price, 2)"},
		},
		Filters: []crud.Filter{stockStatusFilter},
	}
}

func (a *API) registerInventory() {
	stock := a.stockResource()
	a.CRUD.Register(a.addH, "/inventory/stocks", stock, "list", "export")
	a.addH("GET", "/inventory/stocks/import-template", func(c fiber.Ctx) error {
		b, err := export.XLSX("Stock", []export.Column{{Key: "sku", Label: "sku*"}, {Key: "stock", Label: "stock*"}, {Key: "unit_cost", Label: "unit_cost"}, {Key: "note", Label: "note"}},
			[]map[string]any{{"sku": "P000001", "stock": 10.0, "unit_cost": 5000.0, "note": "Initial count"}})
		if err != nil {
			return err
		}
		return export.Send(c, "stock-template.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", b)
	})
	a.addH("POST", "/inventory/stocks/import", a.importStock)
	a.addH("POST", "/inventory/stocks/recalculate", a.recalculateStock)
	a.addH("POST", "/inventory/stocks/:product_id/adjustments", a.adjustStock)
	a.addH("GET", "/inventory/stocks/:product_id/movements", a.stockMovements)
	a.registerOpnames()
}

func (a *API) adjustStock(c fiber.Ctx) error {
	pid, err := httpx.ParamID(c, "product_id")
	if err != nil {
		return err
	}
	body, err := httpx.Body(c)
	if err != nil {
		return err
	}
	mode := httpx.Str(body, "mode")
	qty, ok := httpx.Num(body, "quantity")
	v := &httpx.Validation{}
	if mode != "set" && mode != "add" && mode != "subtract" {
		v.Add("mode", "Must be one of: set, add, subtract.")
	}
	if !ok || qty < 0 {
		v.Add("quantity", "Enter a quantity of 0 or more.")
	}
	reason := httpx.Str(body, "reason")
	if reason != "" && !contains([]string{"correction", "damaged", "expired", "lost", "received", "other"}, reason) {
		v.Add("reason", "Unknown reason.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	p := httpx.P(c)
	var cost *float64
	if uc, ok := httpx.Num(body, "unit_cost"); ok && mode == "add" {
		cost = &uc
	}
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		var cur float64
		if err := tx.QueryRow(c.Context(), `SELECT stock FROM products WHERE id=$1 AND branch_id=$2 AND deleted_at IS NULL FOR UPDATE`, pid, p.BranchID).Scan(&cur); err != nil {
			return httpx.NotFound("Product")
		}
		change := qty
		switch mode {
		case "set":
			change = qty - cur
		case "subtract":
			change = -qty
		}
		if change == 0 {
			return nil
		}
		note := strings.TrimSpace(httpx.Str(body, "note"))
		if reason != "" {
			note = strings.TrimSpace(reason + ": " + note)
		}
		if _, err := productStock(c.Context(), tx, p.BranchID, pid, change, "adjustment", "Manual adjustment", cost, p.UserID, p.Name, note); err != nil {
			return err
		}
		if cost != nil && change > 0 {
			// weighted-average cost after receiving stock
			_, err := tx.Exec(c.Context(), `UPDATE products SET purchase_price = CASE WHEN stock > 0 THEN round(((stock - $2::numeric) * purchase_price + $2::numeric * $3::numeric) / stock, 2) ELSE $3::numeric END WHERE id=$1`, pid, change, *cost)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	row, err := a.CRUD.Fetch(c.Context(), a.DB, a.stockResource(), p, pid)
	if err != nil {
		return err
	}
	return httpx.OK(c, row)
}

func (a *API) stockMovements(c fiber.Ctx) error {
	pid, err := httpx.ParamID(c, "product_id")
	if err != nil {
		return err
	}
	p := httpx.P(c)
	pg := httpx.Pagination(c, 50)
	where := `m.product_id=$1 AND m.branch_id=$2 AND (NULLIF($3,'') IS NULL OR m.created_at::date >= NULLIF($3,'')::date) AND (NULLIF($4,'') IS NULL OR m.created_at::date <= NULLIF($4,'')::date)`
	args := []any{pid, p.BranchID, c.Query("start_date"), c.Query("end_date")}
	var total int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM stock_movements m WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	raw, err := db.JSONRaw(c.Context(), a.DB, `SELECT m.id, m.created_at, m.type, m.reference, m.quantity_change, m.stock_after, m.unit_cost, m.actor_name, m.note
		FROM stock_movements m WHERE `+where+fmt.Sprintf(` ORDER BY m.id DESC LIMIT %d OFFSET %d`, pg.PerPage, pg.Offset), args...)
	if err != nil {
		return err
	}
	return httpx.List(c, raw, httpx.NewMeta(pg, total))
}

func (a *API) importStock(c fiber.Ctx) error {
	rows, err := crud.ImportRows(c)
	if err != nil {
		return err
	}
	p := httpx.P(c)
	res := crud.ImportResult{Errors: []crud.ImportError{}}
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		for i, r := range rows {
			sku := httpx.Str(r, "sku")
			qty, ok := httpx.Num(r, "stock")
			if sku == "" || !ok {
				res.Failed++
				res.Errors = append(res.Errors, crud.ImportError{Row: i + 2, Message: "sku and stock are required"})
				continue
			}
			var id string
			var cur float64
			if err := tx.QueryRow(c.Context(), `SELECT id, stock FROM products WHERE branch_id=$1 AND lower(sku)=lower($2) AND deleted_at IS NULL`, p.BranchID, sku).Scan(&id, &cur); err != nil {
				res.Failed++
				res.Errors = append(res.Errors, crud.ImportError{Row: i + 2, Message: "No product with SKU " + sku})
				continue
			}
			var cost *float64
			if uc, ok := httpx.Num(r, "unit_cost"); ok {
				cost = &uc
				if _, err := tx.Exec(c.Context(), `UPDATE products SET purchase_price=$2 WHERE id=$1`, id, uc); err != nil {
					return err
				}
			}
			if qty != cur {
				if _, err := productStock(c.Context(), tx, p.BranchID, id, qty-cur, "adjustment", "Stock import", cost, p.UserID, p.Name, httpx.Str(r, "note")); err != nil {
					return err
				}
			}
			res.Updated++
		}
		return nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(c, res)
}

func (a *API) recalculateStock(c fiber.Ctx) error {
	p := httpx.P(c)
	branch := p.BranchID
	id, err := a.startJob(p.StoreID, "stock_recalculate", fiber.Map{"branch_id": branch}, func(ctx context.Context) (any, error) {
		tag, err := a.DB.Exec(ctx, `UPDATE products pr SET stock = s.total, updated_at = now()
			FROM (SELECT product_id, sum(quantity_change) AS total FROM stock_movements WHERE branch_id = $1 GROUP BY product_id) s
			WHERE pr.id = s.product_id AND pr.branch_id = $1 AND pr.track_stock AND pr.stock <> s.total`, branch)
		if err != nil {
			return nil, err
		}
		return map[string]any{"updated": tag.RowsAffected()}, nil
	})
	if err != nil {
		return err
	}
	return httpx.OK(c, fiber.Map{"job_id": id, "status": "queued"})
}

// ---------- stock opname ----------

func (a *API) opnameResource() *crud.Resource {
	return &crud.Resource{
		Name: "Stock opnames", Singular: "Stock opname", Table: "stock_opnames", DefaultSort: "t.opname_date DESC, t.id DESC",
		Fields: []crud.Field{
			{Name: "code", ReadOnly: true, Search: true},
			{Name: "opname_date", Kind: crud.Date, Required: true},
			{Name: "performed_by_name", Required: true, Search: true},
			{Name: "performed_by_role", Required: true, Enum: en("owner", "administrator", "manager", "cashier")},
			{Name: "status", ReadOnly: true, Filter: true},
			{Name: "note"},
			{Name: "reject_reason", ReadOnly: true},
			{Name: "item_count", Kind: crud.Int, Expr: "(SELECT count(*) FROM stock_opname_items i WHERE i.opname_id = t.id)"},
			{Name: "difference_value", Kind: crud.Num, Expr: "(SELECT COALESCE(sum((i.physical_stock - i.system_stock) * i.unit_cost), 0) FROM stock_opname_items i WHERE i.opname_id = t.id)"},
			{Name: "items", Kind: crud.JSON, Virtual: true, Required: true},
		},
		BeforeWrite: func(ctx context.Context, h *crud.Ctx, vals map[string]any, id string) error {
			if id != "" {
				return httpx.Conflict("A stock opname cannot be edited; delete it and create a new one.")
			}
			code, err := a.nextNumber(ctx, h.Tx, "SO", "stock_opnames", "code", "branch_id", h.P.BranchID)
			vals["code"] = code
			return err
		},
		AfterWrite: func(ctx context.Context, h *crud.Ctx, id string, created bool) error {
			return a.saveOpnameItems(ctx, h.Tx, h.P.BranchID, id, httpx.Items(h.Body, "items"))
		},
		BeforeDelete: func(ctx context.Context, h *crud.Ctx, id string) error {
			var status string
			if err := h.Tx.QueryRow(ctx, `SELECT status FROM stock_opnames WHERE id=$1`, id).Scan(&status); err != nil {
				return err
			}
			if status == "approved" {
				return httpx.Conflict("An approved stock opname has already changed stock and cannot be deleted.")
			}
			return nil
		},
		Detail: func(ctx context.Context, q db.Querier, p *httpx.Principal, row map[string]any) error {
			items, err := db.JSONRows(ctx, q, `SELECT i.product_id, i.product_name, p.sku, i.system_stock, i.physical_stock, i.physical_stock - i.system_stock AS difference, i.unit_cost, i.note
				FROM stock_opname_items i LEFT JOIN products p ON p.id = i.product_id WHERE i.opname_id = $1 ORDER BY i.id`, row["id"])
			row["items"] = items
			return err
		},
	}
}

func (a *API) saveOpnameItems(ctx context.Context, tx pgx.Tx, branchID, opnameID string, items []map[string]any) error {
	v := &httpx.Validation{}
	n := 0
	for i, it := range items {
		pid, ok := httpx.UUID(it, "product_id")
		qty, qok := httpx.Num(it, "physical_stock")
		if !ok && !qok {
			continue // empty row
		}
		if !ok || !qok || qty < 0 {
			v.Add("items", fmt.Sprintf("Row %d needs a product and a counted quantity of 0 or more.", i+1))
			continue
		}
		var name string
		var stock, cost float64
		if err := tx.QueryRow(ctx, `SELECT name, stock, purchase_price FROM products WHERE id=$1 AND branch_id=$2 AND deleted_at IS NULL`, pid, branchID).Scan(&name, &stock, &cost); err != nil {
			v.Add("items", fmt.Sprintf("Row %d: product not found.", i+1))
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO stock_opname_items (opname_id, product_id, product_name, system_stock, physical_stock, unit_cost, note) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''))`,
			opnameID, pid, name, stock, qty, cost, httpx.Str(it, "note")); err != nil {
			return err
		}
		n++
	}
	if n == 0 && len(v.Fields) == 0 {
		v.Add("items", "Add at least one counted product.")
	}
	return v.Err()
}

func (a *API) registerOpnames() {
	res := a.opnameResource()
	a.CRUD.Register(a.addH, "/stock-opnames", res, "list", "create", "read", "delete", "bulk_delete")
	a.addH("POST", "/stock-opnames/:id/approve", func(c fiber.Ctx) error {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			return err
		}
		p := httpx.P(c)
		err = a.tx(c.Context(), func(tx pgx.Tx) error {
			var code, status string
			if err := tx.QueryRow(c.Context(), `SELECT code, status FROM stock_opnames WHERE id=$1 AND branch_id=$2 FOR UPDATE`, id, p.BranchID).Scan(&code, &status); err != nil {
				return httpx.NotFound("Stock opname")
			}
			if status != "pending" {
				return httpx.Conflict("This stock opname was already " + status + ".")
			}
			rows, err := tx.Query(c.Context(), `SELECT i.product_id, i.physical_stock, p.stock FROM stock_opname_items i JOIN products p ON p.id = i.product_id WHERE i.opname_id=$1`, id)
			if err != nil {
				return err
			}
			type line struct {
				pid           string
				physical, cur float64
			}
			var lines []line
			for rows.Next() {
				var l line
				if err := rows.Scan(&l.pid, &l.physical, &l.cur); err != nil {
					rows.Close()
					return err
				}
				lines = append(lines, l)
			}
			rows.Close()
			for _, l := range lines {
				if d := l.physical - l.cur; d != 0 {
					if _, err := productStock(c.Context(), tx, p.BranchID, l.pid, d, "opname", code, nil, p.UserID, p.Name, "Stock opname"); err != nil {
						return err
					}
				}
			}
			_, err = tx.Exec(c.Context(), `UPDATE stock_opnames SET status='approved', decided_at=now(), updated_at=now() WHERE id=$1`, id)
			return err
		})
		if err != nil {
			return err
		}
		row, err := a.CRUD.Fetch(c.Context(), a.DB, res, p, id)
		if err != nil {
			return err
		}
		return httpx.OK(c, row)
	})
	a.addH("POST", "/stock-opnames/:id/reject", func(c fiber.Ctx) error {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			return err
		}
		body, err := httpx.Body(c)
		if err != nil {
			return err
		}
		p := httpx.P(c)
		tag, err := a.DB.Exec(c.Context(), `UPDATE stock_opnames SET status='rejected', reject_reason=NULLIF($3,''), decided_at=now(), updated_at=now() WHERE id=$1 AND branch_id=$2 AND status='pending'`,
			id, p.BranchID, httpx.Str(body, "reason"))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.Conflict("Only a pending stock opname can be rejected.")
		}
		row, err := a.CRUD.Fetch(c.Context(), a.DB, res, p, id)
		if err != nil {
			return err
		}
		return httpx.OK(c, row)
	})
	a.addH("GET", "/stock-opnames/:id/export", func(c fiber.Ctx) error {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			return err
		}
		row, err := a.CRUD.Fetch(c.Context(), a.DB, res, httpx.P(c), id)
		if err != nil {
			return err
		}
		items, _ := row["items"].([]map[string]any)
		cols := []export.Column{{Key: "product_name", Label: "Product"}, {Key: "sku", Label: "SKU"}, {Key: "system_stock", Label: "System", Format: "number"}, {Key: "physical_stock", Label: "Counted", Format: "number"}, {Key: "difference", Label: "Difference", Format: "number"}, {Key: "note", Label: "Note"}}
		return export.Write(c, "stock-opname-"+strings.ReplaceAll(fmt.Sprint(row["code"]), "/", "-"), "Stock opname "+fmt.Sprint(row["code"]), cols, items)
	})
	a.addH("GET", "/stock-opnames/import-template", func(c fiber.Ctx) error {
		b, err := export.XLSX("Stock opname", []export.Column{{Key: "sku", Label: "sku*"}, {Key: "physical_stock", Label: "physical_stock*"}, {Key: "note", Label: "note"}},
			[]map[string]any{{"sku": "P000001", "physical_stock": 12.0}})
		if err != nil {
			return err
		}
		return export.Send(c, "stock-opname-template.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", b)
	})
	a.addH("POST", "/stock-opnames/import", func(c fiber.Ctx) error {
		rows, err := crud.ImportRows(c)
		if err != nil {
			return err
		}
		p := httpx.P(c)
		items := []any{}
		result := crud.ImportResult{Errors: []crud.ImportError{}}
		for i, r := range rows {
			var pid string
			if err := a.DB.QueryRow(c.Context(), `SELECT id FROM products WHERE branch_id=$1 AND lower(sku)=lower($2) AND deleted_at IS NULL`, p.BranchID, httpx.Str(r, "sku")).Scan(&pid); err != nil {
				result.Failed++
				result.Errors = append(result.Errors, crud.ImportError{Row: i + 2, Message: "No product with SKU " + httpx.Str(r, "sku")})
				continue
			}
			qty, _ := httpx.Num(r, "physical_stock")
			items = append(items, map[string]any{"product_id": pid, "physical_stock": qty, "note": httpx.Str(r, "note")})
		}
		if len(items) > 0 {
			by := c.FormValue("performed_by_name", p.Name)
			body := map[string]any{"opname_date": a.today(), "performed_by_name": by, "performed_by_role": p.Role, "note": "Imported from " + c.FormValue("filename", "spreadsheet"), "items": items}
			if _, err := a.CRUD.Save(c, a.opnameResource(), body, ""); err != nil {
				return err
			}
			result.Created = 1
		}
		return httpx.OK(c, result)
	})
}

func contains(list []string, s string) bool {
	return slices.Contains(list, s)
}
