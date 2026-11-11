package api

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/repositories"
)

const branchRenewalMonthly = 99000.0

func (a *API) registerBranches() {
	branches := &crud.Resource{
		Name: "Branches", Singular: "Branch", Table: "branches", SoftDelete: true, DefaultSort: "t.is_head_office DESC, t.name",
		Scope: storeScope, InsertScope: storeInsert,
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "code", Search: true},
			{Name: "manager_name"},
			{Name: "phone"},
			{Name: "email", Required: true},
			{Name: "password", Kind: crud.Password, Col: "-"},
			{Name: "address"},
			{Name: "is_head_office", Kind: crud.Bool, ReadOnly: true},
			{Name: "subscription_expires_at", Kind: crud.Timestamp, ReadOnly: true},
		},
		BeforeWrite: func(ctx context.Context, h *crud.Ctx, vals map[string]any, id string) error {
			if h.P.Role != "owner" && h.P.Role != "administrator" {
				return helper.Forbidden("Only the owner or an administrator can manage branches.")
			}
			delete(vals, "-") // password is handled in AfterWrite
			return nil
		},
		AfterWrite: func(ctx context.Context, h *crud.Ctx, id string, created bool) error {
			pw, _ := helper.DecodeBase64Password(helper.Str(h.Body, "password")) // validated in Values
			email := strings.ToLower(helper.Str(h.Body, "email"))
			if created {
				if len(pw) < 8 {
					return helper.Invalid("password", "Set a password of at least 8 characters for the branch login.")
				}
				hash, err := middlewares.HashPassword(pw)
				if err != nil {
					return err
				}
				_, err = h.Tx.Exec(ctx, `INSERT INTO users (store_id, branch_id, name, email, password_hash, role, notifications) VALUES ($1,$2,$3,$4,$5,'administrator',$6)`,
					h.P.StoreID, id, "Branch "+helper.Str(h.Body, "name"), email, hash, defaultNotifications)
				if repositories.IsUniqueViolation(err) {
					return helper.Invalid("email", "This email is already registered.")
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
					return helper.Invalid("password", "Use at least 8 characters.")
				}
				hash, err := middlewares.HashPassword(pw)
				if err != nil {
					return err
				}
				if _, err := h.Tx.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE branch_id=$1 AND role='administrator' AND name LIKE 'Branch %'`, id, hash); err != nil {
					return err
				}
			}
			if email != "" {
				_, err := h.Tx.Exec(ctx, `UPDATE users SET email=$2 WHERE branch_id=$1 AND role='administrator' AND name LIKE 'Branch %'`, id, email)
				if repositories.IsUniqueViolation(err) {
					return helper.Invalid("email", "This email is already registered.")
				}
				return err
			}
			return nil
		},
		BeforeDelete: func(ctx context.Context, h *crud.Ctx, id string) error {
			if !h.P.IsOwner() {
				return helper.Forbidden("Only the owner can delete branches.")
			}
			var head bool
			if err := h.Tx.QueryRow(ctx, `SELECT is_head_office FROM branches WHERE id=$1`, id).Scan(&head); err != nil {
				return err
			}
			if head {
				return helper.Conflict("The head office cannot be deleted.")
			}
			_, err := h.Tx.Exec(ctx, `UPDATE users SET deleted_at=now() WHERE branch_id=$1 AND deleted_at IS NULL`, id)
			return err
		},
	}
	a.CRUD.Register(a.addH, "/branches", branches, "list", "create", "read", "update", "delete")
	a.addH("POST", "/branches/:id/copy-products", a.copyProducts)

	a.addH("GET", "/billing/branches", a.branchBilling)
	a.addH("POST", "/billing/branch-renewals", a.branchRenewal)
}

func (a *API) copyProducts(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	var in struct {
		CategoryIDs  []string `json:"category_ids"`
		IncludeStock bool     `json:"include_stock"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	p := helper.P(c)
	var head bool
	if err := a.DB.QueryRow(c.Context(), `SELECT is_head_office FROM branches WHERE id=$1 AND store_id=$2 AND deleted_at IS NULL`, id, p.StoreID).Scan(&head); err != nil {
		return helper.NotFound("Branch")
	}
	if head {
		return helper.Conflict("Products are copied from the head office to other branches.")
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
			a.notify(ctx, a.DB, p.StoreID, &target, "back_office", "Products copied", "{n} products were copied from the head office.", "branch-products", "n", copied)
		}
		return map[string]any{"copied": copied}, err
	})
	if err != nil {
		return err
	}
	return helper.OK(c, fiber.Map{"job_id": jobID, "status": "queued"})
}

func (a *API) branchBilling(c fiber.Ctx) error {
	p := helper.P(c)
	pg := helper.Paging(c, 25)
	var total int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM branches WHERE store_id=$1 AND deleted_at IS NULL`, p.StoreID).Scan(&total); err != nil {
		return err
	}
	raw, err := repositories.JSONRaw(c.Context(), a.DB, `SELECT b.id AS branch_id, b.id, b.name AS branch_name, s.plan,
		b.subscription_expires_at AS expires_at,
		CASE WHEN b.subscription_expires_at IS NULL OR b.subscription_expires_at < now() THEN 'expired'
		     WHEN b.subscription_expires_at < now() + interval '7 days' THEN 'expiring' ELSE 'active' END AS status,
		$2::numeric AS renewal_price
		FROM branches b JOIN stores s ON s.id = b.store_id WHERE b.store_id = $1 AND b.deleted_at IS NULL
		ORDER BY b.is_head_office DESC, b.name LIMIT $3 OFFSET $4`, p.StoreID, branchRenewalMonthly, pg.PerPage, pg.Offset)
	if err != nil {
		return err
	}
	return helper.List(c, raw, helper.NewMeta(pg, total))
}

var periodMonths = map[string]int{"monthly": 1, "quarterly": 3, "yearly": 12}

func (a *API) branchRenewal(c fiber.Ctx) error {
	var in struct {
		BranchIDs     []string `json:"branch_ids"`
		Period        string   `json:"period"`
		PaymentMethod string   `json:"payment_method"`
		PromoCode     string   `json:"promo_code"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	v := &helper.Validation{}
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
	p := helper.P(c)
	var n int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM branches WHERE id = ANY($1) AND store_id=$2 AND deleted_at IS NULL`, in.BranchIDs, p.StoreID).Scan(&n); err != nil {
		return err
	}
	if n != len(in.BranchIDs) {
		return helper.Invalid("branch_ids", "Some branches were not found.")
	}
	amount := branchRenewalMonthly * float64(months*n)
	if months == 12 {
		amount *= 0.85 // yearly discount
	}
	inv, err := a.createInvoice(c, "branch_renewal", helper.T(c, "Renewal of {n} branch(es) for {months} month(s)", "n", n, "months", months), in.PaymentMethod, amount, in.PromoCode,
		map[string]any{"branch_ids": in.BranchIDs, "months": months})
	if err != nil {
		return err
	}
	return helper.OK(c, inv)
}
