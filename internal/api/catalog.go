package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/export"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

var stockStatusFilter = crud.Filter{Param: "stock_status", SQL: `(CASE ? WHEN 'in_stock' THEN t.stock > 0 WHEN 'low' THEN t.minimum_stock IS NOT NULL AND t.stock <= t.minimum_stock AND t.stock > 0 WHEN 'out' THEN t.stock <= 0 ELSE true END)`}

func (a *API) productResource() *crud.Resource {
	return &crud.Resource{
		Name: "Products", Singular: "Product", Table: "products", SoftDelete: true, ImportKey: "sku",
		Where: "t.item_type IS NOT NULL",
		Fields: []crud.Field{
			{Name: "image_url"},
			{Name: "name", Required: true, Search: true},
			{Name: "item_type", Enum: en("product", "service"), Default: "product", Filter: true},
			{Name: "sku", Search: true},
			{Name: "barcode", Search: true},
			{Name: "category_id", Kind: crud.UUID, Filter: true, Ref: &crud.Ref{Key: "category", Table: "categories"}},
			{Name: "unit"},
			{Name: "selling_price", Kind: crud.Num, Required: true, Min: new(float64(0))},
			{Name: "purchase_price", Kind: crud.Num, Min: new(float64(0)), Default: 0.0},
			{Name: "price_type", Enum: en("single", "wholesale", "customer_type"), Default: "single"},
			{Name: "tax_id", Kind: crud.UUID, Ref: &crud.Ref{Key: "tax", Table: "taxes"}},
			{Name: "discount_type", Enum: en("percent", "amount")},
			{Name: "discount_value", Kind: crud.Num, Min: new(float64(0))},
			{Name: "wholesale_prices", Kind: crud.JSON, Default: []any{}},
			{Name: "customer_type_prices", Kind: crud.JSON, Default: []any{}},
			{Name: "track_stock", Kind: crud.Bool, Default: true},
			{Name: "stock", Kind: crud.Num, CreateOnly: true, Default: 0.0},
			{Name: "minimum_stock", Kind: crud.Num, Min: new(float64(0))},
			{Name: "shelf_location"},
			{Name: "weight_grams", Kind: crud.Num, Min: new(float64(0))},
			{Name: "track_imei", Kind: crud.Bool, Default: false},
			{Name: "units", Kind: crud.JSON, Default: []any{}},
			{Name: "variants", Kind: crud.JSON, Default: []any{}},
			{Name: "ingredients", Kind: crud.JSON, Default: []any{}},
			{Name: "points_earned", Kind: crud.Num, Min: new(float64(0))},
			{Name: "points_to_redeem", Kind: crud.Num, Min: new(float64(0))},
			{Name: "show_in_online_store", Kind: crud.Bool, Default: true},
			{Name: "description"},
			{Name: "online_description"},
		},
		Filters: []crud.Filter{stockStatusFilter},
		BeforeWrite: func(ctx context.Context, h *crud.Ctx, vals map[string]any, id string) error {
			if id == "" && (vals["sku"] == nil || vals["sku"] == "") {
				sku, err := nextSKU(ctx, h.Tx, h.P.BranchID)
				if err != nil {
					return err
				}
				vals["sku"] = sku
			}
			if vals["item_type"] == "service" {
				vals["track_stock"] = false
			}
			return nil
		},
		AfterWrite: func(ctx context.Context, h *crud.Ctx, id string, created bool) error {
			if !created {
				return nil
			}
			var stock float64
			var track bool
			var cost float64
			if err := h.Tx.QueryRow(ctx, `SELECT stock, track_stock, purchase_price FROM products WHERE id=$1`, id).Scan(&stock, &track, &cost); err != nil {
				return err
			}
			if track && stock != 0 {
				_, err := h.Tx.Exec(ctx, `INSERT INTO stock_movements (branch_id, product_id, type, reference, quantity_change, stock_after, unit_cost, actor_id, actor_name, note)
					VALUES ($1,$2,'adjustment','Opening stock',$3,$3,$4,$5,$6,'Opening stock')`, h.P.BranchID, id, stock, cost, h.P.UserID, h.P.Name)
				return err
			}
			return nil
		},
	}
}

func nextSKU(ctx context.Context, q db.Querier, branchID string) (string, error) {
	var n int64
	err := q.QueryRow(ctx, `SELECT COALESCE(MAX(NULLIF(regexp_replace(sku, '^P', ''), '')::bigint), 0) + 1 FROM products WHERE branch_id = $1 AND sku ~ '^P[0-9]{1,15}$'`, branchID).Scan(&n)
	return fmt.Sprintf("P%06d", n), err
}

func (a *API) registerCatalog() {
	products := a.productResource()
	a.CRUD.Register(a.addH, "/products", products, "list", "create", "read", "update", "delete", "bulk_delete", "import", "export")

	a.addH("GET", "/products/generate-sku", func(c fiber.Ctx) error {
		sku, err := nextSKU(c.Context(), a.DB, httpx.P(c).BranchID)
		if err != nil {
			return err
		}
		return httpx.OK(c, fiber.Map{"sku": sku})
	})
	a.addH("GET", "/products/check-sku", func(c fiber.Ctx) error {
		sku := strings.TrimSpace(c.Query("sku"))
		if sku == "" {
			return httpx.Invalid("sku", "Required")
		}
		exclude := httpx.QueryUUID(c, "exclude_id")
		var taken bool
		err := a.DB.QueryRow(c.Context(), `SELECT EXISTS(SELECT 1 FROM products WHERE branch_id=$1 AND lower(sku)=lower($2) AND ($3::uuid IS NULL OR id <> $3::uuid) AND deleted_at IS NULL)`,
			httpx.P(c).BranchID, sku, exclude).Scan(&taken)
		if err != nil {
			return err
		}
		return httpx.OK(c, fiber.Map{"available": !taken})
	})
	a.addH("POST", "/products/:id/duplicate", func(c fiber.Ctx) error {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			return err
		}
		row, err := a.CRUD.Fetch(c.Context(), a.DB, products, httpx.P(c), id)
		if err != nil {
			return err
		}
		row["name"] = httpx.T(c, "{name} (copy)", "name", fmt.Sprint(row["name"]))
		row["sku"] = nil
		row["barcode"] = nil
		row["stock"] = 0.0
		out, err := a.CRUD.Save(c, products, row, "")
		if err != nil {
			return err
		}
		return httpx.Created(c, out)
	})
	a.addH("GET", "/products/barcodes", a.productBarcodes)
	a.addH("GET", "/products/labels", func(c fiber.Ctx) error {
		rows, err := a.selectedProducts(c)
		if err != nil {
			return err
		}
		cols := []export.Column{{Key: "name", Label: "Name"}, {Key: "sku", Label: "SKU"}, {Key: "barcode", Label: "Barcode"}, {Key: "selling_price", Label: "Price", Format: "money"}, {Key: "unit", Label: "Unit"}, {Key: "category_name", Label: "Category"}}
		b, err := export.XLSX(httpx.Lang(c), "Labels", cols, rows)
		if err != nil {
			return err
		}
		return export.Send(c, "labels.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", b)
	})

	categories := &crud.Resource{
		Name: "Categories", Singular: "Category", Table: "categories", DefaultSort: "t.sort_order NULLS LAST, t.name",
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "color"},
			{Name: "sort_order", Kind: crud.Int},
			{Name: "product_count", Kind: crud.Int, Expr: "(SELECT count(*) FROM products p WHERE p.category_id = t.id AND p.deleted_at IS NULL)"},
			{Name: "description"},
		},
	}
	a.CRUD.Register(a.addH, "/categories", categories, "list", "create", "read", "update", "delete", "bulk_delete")
}

// selectedProducts returns products filtered by ?product_ids[]= (all when empty).
func (a *API) selectedProducts(c fiber.Ctx) ([]map[string]any, error) {
	ids := []string{}
	for _, s := range httpx.QueryArray(c, "product_ids") {
		if s = strings.ToLower(strings.TrimSpace(s)); httpx.IsUUID(s) {
			ids = append(ids, s)
		}
	}
	return db.JSONRows(c.Context(), a.DB, `SELECT p.id, p.name, p.sku, COALESCE(p.barcode, p.sku) AS barcode, p.selling_price, p.unit,
		(SELECT name FROM categories WHERE id = p.category_id) AS category_name
		FROM products p WHERE p.branch_id = $1 AND p.deleted_at IS NULL AND (cardinality($2::uuid[]) = 0 OR p.id = ANY($2)) ORDER BY p.name LIMIT 5000`,
		httpx.P(c).BranchID, ids)
}

// productBarcodes renders a printable PDF sheet of barcodes or QR codes.
func (a *API) productBarcodes(c fiber.Ctx) error {
	rows, err := a.selectedProducts(c)
	if err != nil {
		return err
	}
	copies, _ := strconv.Atoi(c.Query("copies", "1"))
	if copies < 1 || copies > 100 {
		copies = 1
	}
	symbology := c.Query("symbology", "code128")
	showPrice := c.Query("show_price", "true") != "false"
	var w, h, cellW, cellH float64 = 595, 842, 180, 90 // A4 grid
	switch c.Query("paper_size", "a4") {
	case "label_33x15":
		w, h, cellW, cellH = 93.5, 42.5, 93.5, 42.5
	case "label_50x25":
		w, h, cellW, cellH = 141.7, 70.9, 141.7, 70.9
	}
	d := export.NewPDF(w, h)
	tr := httpx.Lang(c).T
	margin := 18.0
	if w < 200 {
		margin = 0
	}
	cols := max(int((w-2*margin)/cellW), 1)
	perPage := max(cols*int((h-2*margin)/cellH), 1)
	i := 0
	for _, r := range rows {
		for range copies {
			if i%perPage == 0 {
				d.Page()
			}
			slot := i % perPage
			x := margin + float64(slot%cols)*cellW
			top := h - margin - float64(slot/cols)*cellH
			code := fmt.Sprint(r["barcode"])
			fs := cellH / 9
			d.Text(x+4, top-fs-2, fs, true, export.Fit(fmt.Sprint(r["name"]), cellW-8, fs))
			if symbology == "qr" {
				if q, err := qrcode.New(code, qrcode.Medium); err == nil {
					size := cellH * 0.55
					d.Matrix(x+4, top-fs-6-size, size, q.Bitmap())
				}
			} else {
				d.Barcode(x+6, top-cellH*0.62, cellW-12, cellH*0.38, code)
			}
			d.Text(x+4, top-cellH+fs*0.4+2, fs*0.85, false, code)
			if showPrice {
				d.TextRight(x+cellW-4, top-cellH+fs*0.4+2, fs*0.85, true, "Rp "+export.Thousands(num(r["selling_price"])))
			}
			i++
		}
	}
	if i == 0 {
		d.Text(40, h-60, 12, false, tr("No products selected."))
	}
	return export.Send(c, "barcodes.pdf", "application/pdf", d.Bytes())
}

// productStock adjusts stock and writes a movement; it is the single place stock changes.
func productStock(ctx context.Context, tx pgx.Tx, branchID, productID string, change float64, typ, ref string, unitCost *float64, actorID string, actor, note string) (float64, error) {
	var after float64
	var track bool
	err := tx.QueryRow(ctx, `UPDATE products SET stock = stock + $3, updated_at = now() WHERE id = $1 AND branch_id = $2 RETURNING stock, track_stock`,
		productID, branchID, change).Scan(&after, &track)
	if err != nil {
		if db.IsNoRows(err) {
			return 0, httpx.Invalid("product_id", "Product {id} not found.", "id", productID)
		}
		return 0, err
	}
	if !track {
		return after, nil
	}
	var actorRef any
	if actorID != "" {
		actorRef = actorID
	}
	_, err = tx.Exec(ctx, `INSERT INTO stock_movements (branch_id, product_id, type, reference, quantity_change, stock_after, unit_cost, actor_id, actor_name, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''))`, branchID, productID, typ, ref, change, after, unitCost, actorRef, actor, note)
	return after, err
}
