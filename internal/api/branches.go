package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/internal/auth"
	. "github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/export"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

const branchRenewalMonthly = 99000.0

func (a *API) registerBranches() {
	branches := &Resource{
		Name: "Branches", Singular: "Branch", Table: "branches", SoftDelete: true, DefaultSort: "t.is_head_office DESC, t.name",
		Scope: storeScope, InsertScope: storeInsert,
		Fields: []Field{
			{Name: "name", Required: true, Search: true},
			{Name: "code", Search: true},
			{Name: "manager_name"},
			{Name: "phone"},
			{Name: "email", Required: true},
			{Name: "password", Kind: Password, Col: "-"},
			{Name: "address"},
			{Name: "is_head_office", Kind: Bool, ReadOnly: true},
			{Name: "subscription_expires_at", Kind: Timestamp, ReadOnly: true},
		},
		BeforeWrite: func(ctx context.Context, h *Ctx, vals map[string]any, id string) error {
			if h.P.Role != "owner" && h.P.Role != "administrator" {
				return httpx.Forbidden("Only the owner or an administrator can manage branches.")
			}
			delete(vals, "-") // password is handled in AfterWrite
			return nil
		},
		AfterWrite: func(ctx context.Context, h *Ctx, id string, created bool) error {
			pw, _ := httpx.DecodeBase64Password(httpx.Str(h.Body, "password")) // validated in Values
			email := strings.ToLower(httpx.Str(h.Body, "email"))
			if created {
				if len(pw) < 8 {
					return httpx.Invalid("password", "Set a password of at least 8 characters for the branch login.")
				}
				hash, err := auth.HashPassword(pw)
				if err != nil {
					return err
				}
				_, err = h.Tx.Exec(ctx, `INSERT INTO users (store_id, branch_id, name, email, password_hash, role, notifications) VALUES ($1,$2,$3,$4,$5,'administrator',$6)`,
					h.P.StoreID, id, "Branch "+httpx.Str(h.Body, "name"), email, hash, defaultNotifications)
				if db.IsUniqueViolation(err) {
					return httpx.Invalid("email", "This email is already registered.")
				}
				if err != nil {
					return err
				}
				if _, err := h.Tx.Exec(ctx, `UPDATE branches SET subscription_expires_at = (SELECT plan_expires_at FROM stores WHERE id=$2) WHERE id=$1`, id, h.P.StoreID); err != nil {
					return err
				}
				return a.seedBranch(ctx, h.Tx, id)
			}
			if pw != "" {
				if len(pw) < 8 {
					return httpx.Invalid("password", "Use at least 8 characters.")
				}
				hash, err := auth.HashPassword(pw)
				if err != nil {
					return err
				}
				if _, err := h.Tx.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE branch_id=$1 AND role='administrator' AND name LIKE 'Branch %'`, id, hash); err != nil {
					return err
				}
			}
			if email != "" {
				_, err := h.Tx.Exec(ctx, `UPDATE users SET email=$2 WHERE branch_id=$1 AND role='administrator' AND name LIKE 'Branch %'`, id, email)
				if db.IsUniqueViolation(err) {
					return httpx.Invalid("email", "This email is already registered.")
				}
				return err
			}
			return nil
		},
		BeforeDelete: func(ctx context.Context, h *Ctx, id string) error {
			if !h.P.IsOwner() {
				return httpx.Forbidden("Only the owner can delete branches.")
			}
			var head bool
			if err := h.Tx.QueryRow(ctx, `SELECT is_head_office FROM branches WHERE id=$1`, id).Scan(&head); err != nil {
				return err
			}
			if head {
				return httpx.Conflict("The head office cannot be deleted.")
			}
			_, err := h.Tx.Exec(ctx, `UPDATE users SET deleted_at=now() WHERE branch_id=$1 AND deleted_at IS NULL`, id)
			return err
		},
	}
	a.CRUD.Register(a.addH, "/branches", branches, "list", "create", "read", "update", "delete")
	a.addH("POST", "/branches/:id/copy-products", a.copyProducts)

	// per-branch prices of products that came from the head office
	branchProducts := &Resource{
		Name: "Branch products", Singular: "Branch product", Table: "products", SoftDelete: true, DefaultSort: "t.branch_id, t.name",
		Scope: func(b *Builder, p *httpx.Principal) string {
			return "t.branch_id IN (SELECT id FROM branches WHERE store_id = " + b.Add(p.StoreID) + " AND deleted_at IS NULL)"
		},
		Fields: []Field{
			{Name: "branch_id", Kind: UUID, ReadOnly: true, Filter: true},
			{Name: "branch_name", Expr: "(SELECT name FROM branches WHERE id = t.branch_id)"},
			{Name: "product_name", Col: "name", ReadOnly: true, Search: true},
			{Name: "sku", ReadOnly: true, Search: true},
			{Name: "category_id", Kind: UUID, ReadOnly: true, Filter: true},
			{Name: "stock", Kind: Num, ReadOnly: true},
			{Name: "selling_price", Kind: Num, Required: true, Min: F(0)},
			{Name: "purchase_price", Kind: Num, Min: F(0)},
			{Name: "is_available", Kind: Bool},
		},
	}
	a.CRUD.Register(a.gated("business"), "/branch-products", branchProducts, "list", "update", "export")
	a.add("POST", "/branch-products/sync-prices", auth.RequirePlugin("business"), fiber.Handler(a.syncBranchPrices))

	a.addH("GET", "/billing/branches", a.branchBilling)
	a.addH("POST", "/billing/branch-renewals", a.branchRenewal)
}

func (a *API) copyProducts(c fiber.Ctx) error {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		return err
	}
	var in struct {
		CategoryIDs  []string `json:"category_ids"`
		IncludeStock bool     `json:"include_stock"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	p := httpx.P(c)
	var head bool
	if err := a.DB.QueryRow(c.Context(), `SELECT is_head_office FROM branches WHERE id=$1 AND store_id=$2 AND deleted_at IS NULL`, id, p.StoreID).Scan(&head); err != nil {
		return httpx.NotFound("Branch")
	}
	if head {
		return httpx.Conflict("Products are copied from the head office to other branches.")
	}
	hq, target := p.HeadOffice, id
	cats := in.CategoryIDs
	if cats == nil {
		cats = []string{}
	}
	jobID, err := a.startJob(p.StoreID, "copy_products", in, func(ctx context.Context) (any, error) {
		var copied int64
		err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
			// categories by name
			if _, err := tx.Exec(ctx, `INSERT INTO categories (branch_id, name, color, sort_order, description)
				SELECT $2, c.name, c.color, c.sort_order, c.description FROM categories c WHERE c.branch_id = $1
				AND (cardinality($3::uuid[]) = 0 OR c.id = ANY($3))
				AND NOT EXISTS (SELECT 1 FROM categories x WHERE x.branch_id = $2 AND lower(x.name) = lower(c.name))`, hq, target, cats); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `INSERT INTO products (branch_id, source_product_id, name, item_type, sku, barcode, category_id, unit, selling_price, purchase_price, price_type,
				discount_type, discount_value, wholesale_prices, customer_type_prices, track_stock, stock, minimum_stock, shelf_location, weight_grams, track_imei, units, variants,
				ingredients, points_earned, points_to_redeem, show_in_online_store, description, online_description, image_url)
				SELECT $2, p.id, p.name, p.item_type, p.sku, p.barcode,
				  (SELECT x.id FROM categories x JOIN categories c ON lower(c.name) = lower(x.name) AND c.id = p.category_id WHERE x.branch_id = $2 LIMIT 1),
				  p.unit, p.selling_price, p.purchase_price, p.price_type, p.discount_type, p.discount_value, p.wholesale_prices, '[]', p.track_stock,
				  CASE WHEN $4 THEN p.stock ELSE 0 END, p.minimum_stock, p.shelf_location, p.weight_grams, p.track_imei, p.units, p.variants, '[]',
				  p.points_earned, p.points_to_redeem, p.show_in_online_store, p.description, p.online_description, p.image_url
				FROM products p WHERE p.branch_id = $1 AND p.deleted_at IS NULL AND (cardinality($3::uuid[]) = 0 OR p.category_id = ANY($3))
				AND NOT EXISTS (SELECT 1 FROM products x WHERE x.branch_id = $2 AND lower(x.sku) = lower(p.sku) AND x.deleted_at IS NULL)`, hq, target, cats, in.IncludeStock)
			if err != nil {
				return err
			}
			copied = tag.RowsAffected()
			if in.IncludeStock {
				_, err = tx.Exec(ctx, `INSERT INTO stock_movements (branch_id, product_id, type, reference, quantity_change, stock_after, actor_name, note)
					SELECT branch_id, id, 'adjustment', 'Copied from head office', stock, stock, 'system', 'Opening stock' FROM products
					WHERE branch_id = $1 AND stock <> 0 AND track_stock AND NOT EXISTS (SELECT 1 FROM stock_movements m WHERE m.product_id = products.id)`, target)
			}
			return err
		})
		if err == nil {
			a.notify(ctx, a.DB, p.StoreID, &target, "back_office", "Products copied", fmt.Sprintf("%d products were copied from the head office.", copied), "branch-products")
		}
		return map[string]any{"copied": copied}, err
	})
	if err != nil {
		return err
	}
	return httpx.OK(c, fiber.Map{"job_id": jobID, "status": "queued"})
}

func (a *API) syncBranchPrices(c fiber.Ctx) error {
	var in struct {
		BranchIDs  []string `json:"branch_ids"`
		ProductIDs []string `json:"product_ids"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	if len(in.BranchIDs) == 0 {
		return httpx.Invalid("branch_ids", "Choose at least one branch.")
	}
	if in.ProductIDs == nil {
		in.ProductIDs = []string{}
	}
	p := httpx.P(c)
	tag, err := a.DB.Exec(c.Context(), `UPDATE products bp SET selling_price = hq.selling_price, updated_at = now()
		FROM products hq WHERE hq.branch_id = $1 AND hq.deleted_at IS NULL
		  AND (bp.source_product_id = hq.id OR lower(bp.sku) = lower(hq.sku))
		  AND bp.branch_id = ANY($2) AND bp.branch_id IN (SELECT id FROM branches WHERE store_id = $3) AND bp.branch_id <> $1
		  AND (cardinality($4::uuid[]) = 0 OR hq.id = ANY($4) OR bp.id = ANY($4))`, p.HeadOffice, in.BranchIDs, p.StoreID, in.ProductIDs)
	if err != nil {
		return err
	}
	return httpx.OK(c, fiber.Map{"updated": tag.RowsAffected()})
}

func (a *API) branchBilling(c fiber.Ctx) error {
	p := httpx.P(c)
	pg := httpx.Pagination(c, 25)
	var total int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM branches WHERE store_id=$1 AND deleted_at IS NULL`, p.StoreID).Scan(&total); err != nil {
		return err
	}
	raw, err := db.JSONRaw(c.Context(), a.DB, `SELECT b.id AS branch_id, b.id, b.name AS branch_name, s.plan,
		b.subscription_expires_at AS expires_at,
		CASE WHEN b.subscription_expires_at IS NULL OR b.subscription_expires_at < now() THEN 'expired'
		     WHEN b.subscription_expires_at < now() + interval '7 days' THEN 'expiring' ELSE 'active' END AS status,
		$2::numeric AS renewal_price
		FROM branches b JOIN stores s ON s.id = b.store_id WHERE b.store_id = $1 AND b.deleted_at IS NULL
		ORDER BY b.is_head_office DESC, b.name LIMIT $3 OFFSET $4`, p.StoreID, branchRenewalMonthly, pg.PerPage, pg.Offset)
	if err != nil {
		return err
	}
	return httpx.List(c, raw, httpx.NewMeta(pg, total))
}

var periodMonths = map[string]int{"monthly": 1, "quarterly": 3, "yearly": 12}

func (a *API) branchRenewal(c fiber.Ctx) error {
	var in struct {
		BranchIDs     []string `json:"branch_ids"`
		Period        string   `json:"period"`
		PaymentMethod string   `json:"payment_method"`
		PromoCode     string   `json:"promo_code"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	v := &httpx.Validation{}
	months, ok := periodMonths[in.Period]
	if !ok {
		v.Add("period", "Must be one of: monthly, quarterly, yearly.")
	}
	if len(in.BranchIDs) == 0 {
		v.Add("branch_ids", "Choose at least one branch.")
	}
	if !validPayMethod(in.PaymentMethod, true) {
		v.Add("payment_method", "Choose a payment method.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	p := httpx.P(c)
	var n int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM branches WHERE id = ANY($1) AND store_id=$2 AND deleted_at IS NULL`, in.BranchIDs, p.StoreID).Scan(&n); err != nil {
		return err
	}
	if n != len(in.BranchIDs) {
		return httpx.Invalid("branch_ids", "Some branches were not found.")
	}
	amount := branchRenewalMonthly * float64(months*n)
	if months == 12 {
		amount *= 0.85 // yearly discount
	}
	inv, err := a.createInvoice(c, "branch_renewal", fmt.Sprintf("Renewal of %d branch(es) for %d month(s)", n, months), in.PaymentMethod, amount, in.PromoCode,
		map[string]any{"branch_ids": in.BranchIDs, "months": months})
	if err != nil {
		return err
	}
	return httpx.OK(c, inv)
}

// ---------- stock transfers ----------

// transferDirection is "out" for the sending branch and "in" for the receiver.
func transferDirection(p *httpx.Principal, row map[string]any) {
	row["direction"] = "in"
	if from, _ := row["from_branch_id"].(string); from == p.BranchID {
		row["direction"] = "out"
	}
	if fb, ok := row["from_branch"].(map[string]any); ok {
		row["from_branch_name"] = fb["name"]
	}
	if tb, ok := row["to_branch"].(map[string]any); ok {
		row["to_branch_name"] = tb["name"]
	}
}

var transferDirectionFilter = Filter{Param: "direction", Build: func(b *Builder, p *httpx.Principal, v string) string {
	switch v {
	case "out":
		return "t.from_branch_id = " + b.Add(p.BranchID)
	case "in":
		return "t.to_branch_id = " + b.Add(p.BranchID)
	}
	return ""
}}

// transferRoleFilter: "head_office" = transfers where the head office takes part, "branch" = between other branches.
var transferRoleFilter = Filter{Param: "role", Build: func(b *Builder, p *httpx.Principal, v string) string {
	hq := b.Add(p.HeadOffice)
	switch v {
	case "head_office":
		return "(t.from_branch_id = " + hq + " OR t.to_branch_id = " + hq + ")"
	case "branch":
		return "(t.from_branch_id <> " + hq + " AND t.to_branch_id <> " + hq + ")"
	}
	return ""
}}

func (a *API) transferResource() *Resource {
	return &Resource{
		Name: "Stock transfers", Singular: "Stock transfer", Table: "stock_transfers", DefaultSort: "t.transfer_date DESC, t.id DESC",
		Scope: func(b *Builder, p *httpx.Principal) string {
			ph := b.Add(p.BranchID)
			return "t.store_id = " + b.Add(p.StoreID) + " AND (t.from_branch_id = " + ph + " OR t.to_branch_id = " + ph + ")"
		},
		InsertScope: func(p *httpx.Principal) map[string]any {
			return map[string]any{"store_id": p.StoreID, "from_branch_id": p.BranchID}
		},
		Fields: []Field{
			{Name: "transfer_number", ReadOnly: true, Search: true},
			{Name: "from_branch_id", Kind: UUID, ReadOnly: true, Ref: &Ref{Key: "from_branch", Table: "branches", ScopeCol: "store_id"}},
			{Name: "to_branch_id", Kind: UUID, Required: true, Ref: &Ref{Key: "to_branch", Table: "branches", ScopeCol: "store_id"}},
			{Name: "transfer_date", Kind: Date, Required: true},
			{Name: "requires_confirmation", Kind: Bool, Default: false},
			{Name: "status", ReadOnly: true, Filter: true},
			{Name: "note"},
			{Name: "reject_reason", ReadOnly: true},
			{Name: "total_quantity", Kind: Num, Expr: "(SELECT COALESCE(sum(quantity),0) FROM stock_transfer_items i WHERE i.transfer_id = t.id)"},
			{Name: "item_count", Kind: Int, Expr: "(SELECT count(*) FROM stock_transfer_items i WHERE i.transfer_id = t.id)"},
			{Name: "items", Kind: JSON, Virtual: true, Required: true},
		},
		Filters: []Filter{transferDirectionFilter, transferRoleFilter, dateFrom("t.transfer_date"), dateTo("t.transfer_date")},
		Post:    transferDirection,
		BeforeWrite: func(ctx context.Context, h *Ctx, vals map[string]any, id string) error {
			if id != "" {
				return httpx.Conflict("A transfer cannot be edited; delete it and create a new one.")
			}
			if to, _ := vals["to_branch_id"].(string); to == h.P.BranchID {
				return httpx.Invalid("to_branch_id", "Choose a different branch.")
			}
			num, err := a.nextNumber(ctx, h.Tx, "TRF", "stock_transfers", "transfer_number", "store_id", h.P.StoreID)
			vals["transfer_number"] = num
			vals["status"] = "completed"
			if vals["requires_confirmation"] == true {
				vals["status"] = "pending"
			}
			return err
		},
		AfterWrite: func(ctx context.Context, h *Ctx, id string, created bool) error {
			return a.saveTransferItems(ctx, h.Tx, h.P, id, httpx.Items(h.Body, "items"))
		},
		BeforeDelete: func(ctx context.Context, h *Ctx, id string) error {
			var status string
			var from string
			if err := h.Tx.QueryRow(ctx, `SELECT status, from_branch_id FROM stock_transfers WHERE id=$1`, id).Scan(&status, &from); err != nil {
				return err
			}
			if from != h.P.BranchID {
				return httpx.Forbidden("Only the sending branch can delete a transfer.")
			}
			if status != "pending" {
				return httpx.Conflict("Only a transfer that is still pending can be deleted.")
			}
			items, err := transferLines(ctx, h.Tx, id)
			if err != nil {
				return err
			}
			for _, x := range items {
				if x.pid == "" {
					continue
				}
				if _, err := productStock(ctx, h.Tx, h.P.BranchID, x.pid, x.qty, "transfer_in", "Transfer cancelled", nil, h.P.UserID, h.P.Name, ""); err != nil {
					return err
				}
			}
			return nil
		},
		Detail: func(ctx context.Context, q db.Querier, p *httpx.Principal, row map[string]any) error {
			items, err := db.JSONRows(ctx, q, `SELECT product_id, product_name, sku, quantity, received_quantity, note FROM stock_transfer_items WHERE transfer_id=$1 ORDER BY id`, row["id"])
			row["items"] = items
			return err
		},
	}
}

type transferLine struct {
	pid      string
	sku      string
	name     string
	qty      float64
	received *float64
}

func transferLines(ctx context.Context, tx pgx.Tx, id string) ([]transferLine, error) {
	rows, err := tx.Query(ctx, `SELECT COALESCE(product_id::text,''), COALESCE(sku,''), product_name, quantity, received_quantity FROM stock_transfer_items WHERE transfer_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []transferLine
	for rows.Next() {
		var l transferLine
		if err := rows.Scan(&l.pid, &l.sku, &l.name, &l.qty, &l.received); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// saveTransferItems takes stock out of the sender and, without confirmation, puts it into the receiver.
func (a *API) saveTransferItems(ctx context.Context, tx pgx.Tx, p *httpx.Principal, id string, items []map[string]any) error {
	var number string
	var to string
	var confirm bool
	if err := tx.QueryRow(ctx, `SELECT transfer_number, to_branch_id, requires_confirmation FROM stock_transfers WHERE id=$1`, id).Scan(&number, &to, &confirm); err != nil {
		return err
	}
	v := &httpx.Validation{}
	n := 0
	for i, it := range items {
		pid, ok := httpx.UUID(it, "product_id")
		qty, qok := httpx.Num(it, "quantity")
		if !ok && !qok {
			continue
		}
		if !ok || !qok || qty <= 0 {
			v.Add("items", fmt.Sprintf("Row %d needs a product and a quantity above 0.", i+1))
			continue
		}
		var name, sku string
		var stock float64
		if err := tx.QueryRow(ctx, `SELECT name, sku, stock FROM products WHERE id=$1 AND branch_id=$2 AND deleted_at IS NULL FOR UPDATE`, pid, p.BranchID).Scan(&name, &sku, &stock); err != nil {
			v.Add("items", fmt.Sprintf("Row %d: product not found in this branch.", i+1))
			continue
		}
		if stock < qty {
			v.Add("items", fmt.Sprintf("Row %d: only %s of %s in stock.", i+1, trimNum(stock), name))
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO stock_transfer_items (transfer_id, product_id, product_name, sku, quantity, received_quantity, note) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''))`,
			id, pid, name, sku, qty, map[bool]any{true: nil, false: qty}[confirm], httpx.Str(it, "note")); err != nil {
			return err
		}
		if _, err := productStock(ctx, tx, p.BranchID, pid, -qty, "transfer_out", number, nil, p.UserID, p.Name, ""); err != nil {
			return err
		}
		if !confirm {
			if err := receiveIntoBranch(ctx, tx, p, to, pid, sku, qty, number); err != nil {
				return err
			}
		}
		n++
	}
	if n == 0 && len(v.Fields) == 0 {
		v.Add("items", "Add at least one item.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	if confirm {
		a.notify(ctx, tx, p.StoreID, &to, "back_office", "Incoming stock transfer", "Transfer "+number+" is waiting for your confirmation.", "report-transfer-confirmations")
	}
	return nil
}

// receiveIntoBranch adds stock to the matching product (by source or SKU) in the receiving branch,
// creating the product there when it does not exist yet.
func receiveIntoBranch(ctx context.Context, tx pgx.Tx, p *httpx.Principal, branchID, sourceID string, sku string, qty float64, ref string) error {
	var target string
	err := tx.QueryRow(ctx, `SELECT id FROM products WHERE branch_id=$1 AND deleted_at IS NULL AND (source_product_id=$2 OR lower(sku)=lower($3)) ORDER BY (source_product_id=$2) DESC LIMIT 1`,
		branchID, sourceID, sku).Scan(&target)
	if db.IsNoRows(err) {
		err = tx.QueryRow(ctx, `INSERT INTO products (branch_id, source_product_id, name, item_type, sku, barcode, unit, selling_price, purchase_price, price_type, track_stock, units, variants, show_in_online_store, image_url)
			SELECT $1, id, name, item_type, sku, barcode, unit, selling_price, purchase_price, price_type, true, units, variants, show_in_online_store, image_url FROM products WHERE id=$2 RETURNING id`,
			branchID, sourceID).Scan(&target)
	}
	if err != nil {
		return err
	}
	_, err = productStock(ctx, tx, branchID, target, qty, "transfer_in", ref, nil, p.UserID, p.Name, "")
	return err
}

func (a *API) registerTransfers() {
	res := a.transferResource()
	a.CRUD.Register(a.addH, "/stock-transfers", res, "list", "create", "read", "delete")
	a.addH("POST", "/stock-transfers/:id/accept", a.acceptTransfer)
	a.addH("POST", "/stock-transfers/:id/reject", a.rejectTransfer)
	a.addH("GET", "/stock-transfers/:id/print", a.printTransfer)
}

func (a *API) acceptTransfer(c fiber.Ctx) error {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Body(c)
	if err != nil {
		return err
	}
	p := httpx.P(c)
	received := map[string]float64{}
	for _, it := range httpx.Items(body, "items") {
		pid, ok := httpx.UUID(it, "product_id")
		q, qok := httpx.Num(it, "received_quantity")
		if ok && qok {
			received[pid] = q
		}
	}
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		ctx := c.Context()
		var number, status string
		var from, to string
		if err := tx.QueryRow(ctx, `SELECT transfer_number, status, from_branch_id, to_branch_id FROM stock_transfers WHERE id=$1 AND store_id=$2 FOR UPDATE`, id, p.StoreID).Scan(&number, &status, &from, &to); err != nil {
			return httpx.NotFound("Stock transfer")
		}
		if to != p.BranchID {
			return httpx.Forbidden("Only the receiving branch can accept this transfer.")
		}
		if status != "pending" {
			return httpx.Conflict("This transfer was already " + status + ".")
		}
		lines, err := transferLines(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, l := range lines {
			q := l.qty
			if r, ok := received[l.pid]; ok {
				if r < 0 || r > l.qty {
					return httpx.Invalid("items", "Received quantity for "+l.name+" must be between 0 and "+trimNum(l.qty)+".")
				}
				q = r
			}
			if _, err := tx.Exec(ctx, `UPDATE stock_transfer_items SET received_quantity=$3 WHERE transfer_id=$1 AND product_id=$2`, id, l.pid, q); err != nil {
				return err
			}
			if q > 0 {
				if err := receiveIntoBranch(ctx, tx, p, to, l.pid, l.sku, q, number); err != nil {
					return err
				}
			}
			if short := l.qty - q; short > 0 && l.pid != "" {
				// the shortfall goes back to the sender
				if _, err := productStock(ctx, tx, from, l.pid, short, "transfer_in", number+" (not received)", nil, p.UserID, p.Name, ""); err != nil {
					return err
				}
			}
		}
		_, err = tx.Exec(ctx, `UPDATE stock_transfers SET status='accepted', note = COALESCE(NULLIF($2,''), note), updated_at=now() WHERE id=$1`, id, httpx.Str(body, "note"))
		if err == nil {
			a.notify(ctx, tx, p.StoreID, &from, "back_office", "Transfer accepted", "Transfer "+number+" was received.", "report-transfers")
		}
		return err
	})
	if err != nil {
		return err
	}
	row, err := a.CRUD.Fetch(c.Context(), a.DB, a.transferResource(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(c, row)
}

func (a *API) rejectTransfer(c fiber.Ctx) error {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Body(c)
	if err != nil {
		return err
	}
	reason := httpx.Str(body, "reason")
	if reason == "" {
		return httpx.Invalid("reason", "Required")
	}
	p := httpx.P(c)
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		ctx := c.Context()
		var number, status string
		var from, to string
		if err := tx.QueryRow(ctx, `SELECT transfer_number, status, from_branch_id, to_branch_id FROM stock_transfers WHERE id=$1 AND store_id=$2 FOR UPDATE`, id, p.StoreID).Scan(&number, &status, &from, &to); err != nil {
			return httpx.NotFound("Stock transfer")
		}
		if to != p.BranchID {
			return httpx.Forbidden("Only the receiving branch can reject this transfer.")
		}
		if status != "pending" {
			return httpx.Conflict("This transfer was already " + status + ".")
		}
		lines, err := transferLines(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, l := range lines {
			if l.pid != "" {
				if _, err := productStock(ctx, tx, from, l.pid, l.qty, "transfer_in", number+" (rejected)", nil, p.UserID, p.Name, ""); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE stock_transfers SET status='rejected', reject_reason=$2, updated_at=now() WHERE id=$1`, id, reason); err != nil {
			return err
		}
		a.notify(ctx, tx, p.StoreID, &from, "back_office", "Transfer rejected", "Transfer "+number+" was rejected: "+reason, "report-transfers")
		return nil
	})
	if err != nil {
		return err
	}
	row, err := a.CRUD.Fetch(c.Context(), a.DB, a.transferResource(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(c, row)
}

func (a *API) printTransfer(c fiber.Ctx) error {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		return err
	}
	t, err := a.CRUD.Fetch(c.Context(), a.DB, a.transferResource(), httpx.P(c), id)
	if err != nil {
		return err
	}
	d := export.NewPDF(595, 842)
	d.Page()
	d.Text(40, 790, 16, true, "STOCK TRANSFER")
	d.Text(40, 770, 10, false, "No: "+fmt.Sprint(t["transfer_number"]))
	d.Text(40, 756, 10, false, "Date: "+fmt.Sprint(t["transfer_date"]))
	d.Text(40, 742, 10, false, "From: "+fmt.Sprint(t["from_branch_name"])+"   To: "+fmt.Sprint(t["to_branch_name"]))
	d.Text(40, 728, 10, false, "Status: "+fmt.Sprint(t["status"]))
	y := 700.0
	d.Rect(40, y-4, 515, 16, 0.92)
	d.Text(46, y, 9, true, "Product")
	d.Text(300, y, 9, true, "SKU")
	d.Text(420, y, 9, true, "Qty")
	d.Text(480, y, 9, true, "Received")
	y -= 20
	items, _ := t["items"].([]map[string]any)
	for _, it := range items {
		if y < 100 {
			d.Page()
			y = 800
		}
		d.Text(46, y, 9, false, export.Fit(fmt.Sprint(it["product_name"]), 240, 9))
		d.Text(300, y, 9, false, strOr(it["sku"], ""))
		d.Text(420, y, 9, false, export.Thousands(num(it["quantity"])))
		if it["received_quantity"] != nil {
			d.Text(480, y, 9, false, export.Thousands(num(it["received_quantity"])))
		}
		y -= 15
	}
	if note := strOr(t["note"], ""); note != "" {
		d.Text(40, y-10, 9, false, "Note: "+export.Fit(note, 500, 9))
	}
	y -= 70
	d.Line(40, y, 200)
	d.Text(40, y-12, 9, false, "Sent by")
	d.Line(355, y, 515)
	d.Text(355, y-12, 9, false, "Received by")
	return export.Send(c, "transfer-"+strings.ReplaceAll(fmt.Sprint(t["transfer_number"]), "/", "-")+".pdf", "application/pdf", d.Bytes())
}
