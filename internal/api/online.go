package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/export"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
	"github.com/roysitumorang/lestari-backend/internal/i18n"
)

var orderStatuses = en("unpaid", "awaiting_confirmation", "payment_rejected", "processing", "shipped", "delivered", "completed", "returned", "cancelled")

// allowed status changes from the back office
var orderTransitions = map[string][]string{
	"unpaid":                {"processing", "cancelled"},
	"awaiting_confirmation": {"processing", "payment_rejected", "cancelled"},
	"payment_rejected":      {"awaiting_confirmation", "cancelled"},
	"processing":            {"shipped", "completed", "cancelled"},
	"shipped":               {"delivered", "completed", "returned"},
	"delivered":             {"completed", "returned"},
}

// stockHeld reports whether an order in this status has taken stock.
func stockHeld(status string) bool {
	return status == "processing" || status == "shipped" || status == "delivered" || status == "completed"
}

func (a *API) orderResource() *crud.Resource {
	return &crud.Resource{
		Name: "Online orders", Singular: "Order", Table: "catalog_orders", DefaultSort: "t.created_at DESC, t.id DESC",
		Fields: []crud.Field{
			{Name: "order_number", ReadOnly: true, Search: true},
			{Name: "customer_name", Required: true, Search: true},
			{Name: "customer_phone"},
			{Name: "shipping_address"},
			{Name: "shipping_method", Enum: en("takeaway", "cod", "courier", "own_courier")},
			{Name: "courier"},
			{Name: "tracking_number", ReadOnly: true},
			{Name: "payment_method", Enum: en("bank_transfer", "direct"), Default: "bank_transfer", Filter: true},
			{Name: "payment_proof_url"},
			{Name: "subtotal", Kind: crud.Num, ReadOnly: true},
			{Name: "shipping_fee", Kind: crud.Num, Min: new(float64(0)), Default: 0.0},
			{Name: "grand_total", Kind: crud.Num, ReadOnly: true},
			{Name: "customer_note"},
			{Name: "status", ReadOnly: true, Filter: true},
			{Name: "status_reason", ReadOnly: true},
			{Name: "items", Kind: crud.JSON, Virtual: true, Required: true},
		},
		Filters: []crud.Filter{dateFrom("t.created_at::date"), dateTo("t.created_at::date")},
		BeforeWrite: func(ctx context.Context, h *crud.Ctx, vals map[string]any, id string) error {
			if id != "" {
				return httpx.Conflict("Change an order with its status actions.")
			}
			num, err := a.nextNumber(ctx, h.Tx, "ORD", "catalog_orders", "order_number", "branch_id", h.P.BranchID)
			vals["order_number"] = num
			vals["status"] = "unpaid"
			if vals["payment_proof_url"] != nil {
				vals["status"] = "awaiting_confirmation"
			}
			return err
		},
		AfterWrite: func(ctx context.Context, h *crud.Ctx, id string, created bool) error {
			var sub float64
			n := 0
			for i, it := range httpx.Items(h.Body, "items") {
				pid, ok := httpx.UUID(it, "product_id")
				qty, qok := httpx.Num(it, "quantity")
				if !ok || !qok || qty <= 0 {
					return httpx.Invalid("items", "Row {row} needs a product and a quantity above 0.", "row", i+1)
				}
				var name string
				var price float64
				if err := h.Tx.QueryRow(ctx, `SELECT name, selling_price FROM products WHERE id=$1 AND branch_id=$2 AND deleted_at IS NULL AND show_in_online_store`, pid, h.P.BranchID).Scan(&name, &price); err != nil {
					return httpx.Invalid("items", "Row {row}: product is not sold online.", "row", i+1)
				}
				s := round2(price * qty)
				if _, err := h.Tx.Exec(ctx, `INSERT INTO catalog_order_items (order_id, product_id, product_name, quantity, unit_price, subtotal) VALUES ($1,$2,$3,$4,$5,$6)`, id, pid, name, qty, price, s); err != nil {
					return err
				}
				sub += s
				n++
			}
			if n == 0 {
				return httpx.Invalid("items", "Add at least one item.")
			}
			_, err := h.Tx.Exec(ctx, `UPDATE catalog_orders SET subtotal=$2, grand_total=$2 + shipping_fee WHERE id=$1`, id, sub)
			if err == nil {
				a.notify(ctx, h.Tx, h.P.StoreID, &h.P.BranchID, "online_store", "New online order", "You have a new order.", "catalog-orders")
			}
			return err
		},
		Detail: func(ctx context.Context, q db.Querier, p *httpx.Principal, row map[string]any) error {
			items, err := db.JSONRows(ctx, q, `SELECT product_id, product_name, quantity, unit_price, subtotal FROM catalog_order_items WHERE order_id=$1 ORDER BY id`, row["id"])
			row["items"] = items
			return err
		},
	}
}

func (a *API) registerOnlineCatalog() {
	orders := a.orderResource()
	list := a.CRUD.List(orders)
	a.addH("GET", "/catalog/orders", func(c fiber.Ctx) error {
		if c.Query("sort_by") == "grand_total" {
			args := c.RequestCtx().QueryArgs()
			args.Set("sort", "grand_total")
			args.Set("order", "desc")
		}
		return list(c)
	})
	a.CRUD.Register(a.addH, "/catalog/orders", orders, "create", "read", "export")
	a.addH("PATCH", "/catalog/orders/:id/status", a.orderStatus)
	a.addH("GET", "/catalog/orders/:id/invoice", a.orderInvoice)

	a.CRUD.Register(a.addH, "/catalog/payment-accounts", &crud.Resource{
		Name: "Payout accounts", Singular: "Account", Table: "catalog_payment_accounts", DefaultSort: "t.type, t.provider",
		Fields: []crud.Field{
			{Name: "type", Required: true, Enum: en("bank", "e_wallet", "qris"), Default: "bank", Filter: true},
			{Name: "provider", Required: true, Search: true},
			{Name: "account_name", Required: true, Search: true},
			{Name: "account_number"},
			{Name: "qris_image_url"},
			{Name: "is_active", Kind: crud.Bool, Default: true},
		},
		BeforeWrite: func(ctx context.Context, h *crud.Ctx, vals map[string]any, id string) error {
			typ, _ := vals["type"].(string)
			if typ == "qris" && vals["qris_image_url"] == nil && id == "" {
				return httpx.Invalid("qris_image_url", "Upload the QRIS image.")
			}
			if (typ == "bank" || typ == "e_wallet") && vals["account_number"] == nil && id == "" {
				return httpx.Invalid("account_number", "Required")
			}
			return nil
		},
	}, "list", "create", "read", "update", "delete")

	a.addH("GET", "/settings/catalog", a.getSetting("catalog"))
	a.addH("PUT", "/settings/catalog", a.putSetting("catalog"))
}

func (a *API) orderStatus(c fiber.Ctx) error {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		return err
	}
	body, err := httpx.Body(c)
	if err != nil {
		return err
	}
	to := httpx.Str(body, "status")
	if !contains(orderStatuses, to) {
		return httpx.Invalid("status", "Must be one of: {values}.", "values", strings.Join(orderStatuses, ", "))
	}
	reason := httpx.Str(body, "reason")
	if (to == "cancelled" || to == "payment_rejected") && reason == "" {
		return httpx.Invalid("reason", "Tell the customer why.")
	}
	p := httpx.P(c)
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		ctx := c.Context()
		var from, number string
		if err := tx.QueryRow(ctx, `SELECT status, order_number FROM catalog_orders WHERE id=$1 AND branch_id=$2 FOR UPDATE`, id, p.BranchID).Scan(&from, &number); err != nil {
			return httpx.NotFound("Order")
		}
		if !contains(orderTransitions[from], to) {
			return httpx.Conflict("An order that is {from} cannot become {to}.", "from", i18n.Tr(from), "to", i18n.Tr(to))
		}
		os, _ := a.setting(ctx, tx, p.BranchID, "online-store")
		if os["use_physical_stock"] != false && stockHeld(to) != stockHeld(from) {
			dir := -1.0
			if !stockHeld(to) {
				dir = 1
			}
			rows, err := tx.Query(ctx, `SELECT product_id, quantity FROM catalog_order_items WHERE order_id=$1 AND product_id IS NOT NULL`, id)
			if err != nil {
				return err
			}
			type it struct {
				pid string
				q   float64
			}
			var items []it
			for rows.Next() {
				var x it
				_ = rows.Scan(&x.pid, &x.q)
				items = append(items, x)
			}
			rows.Close()
			for _, x := range items {
				prod, err := a.loadProduct(ctx, tx, p.BranchID, x.pid)
				if err != nil {
					continue
				}
				if err := a.consumeStock(ctx, tx, p, prod, x.q, dir, "online_order", number); err != nil {
					return err
				}
			}
		}
		_, err := tx.Exec(ctx, `UPDATE catalog_orders SET status=$2, status_reason=NULLIF($3,''), courier=COALESCE(NULLIF($4,''), courier),
			tracking_number=COALESCE(NULLIF($5,''), tracking_number), updated_at=now() WHERE id=$1`, id, to, reason, httpx.Str(body, "courier"), httpx.Str(body, "tracking_number"))
		return err
	})
	if err != nil {
		return err
	}
	row, err := a.CRUD.Fetch(c.Context(), a.DB, a.orderResource(), p, id)
	if err != nil {
		return err
	}
	return httpx.OK(c, row)
}

func (a *API) orderInvoice(c fiber.Ctx) error {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		return err
	}
	p := httpx.P(c)
	o, err := a.CRUD.Fetch(c.Context(), a.DB, a.orderResource(), p, id)
	if err != nil {
		return err
	}
	name, addr, phone := a.storeHeader(c.Context(), p.BranchID)
	d := export.NewPDF(595, 842)
	tr := httpx.Lang(c).T
	d.Page()
	d.Text(40, 790, 18, true, tr("INVOICE"))
	d.Text(40, 770, 10, true, name)
	d.Text(40, 756, 9, false, addr)
	d.Text(40, 744, 9, false, phone)
	d.Text(360, 790, 9, false, tr("Order")+": "+fmt.Sprint(o["order_number"]))
	d.Text(360, 776, 9, false, tr("Date")+": "+formatTime(o["created_at"], a.Loc))
	d.Text(360, 762, 9, false, tr("Status")+": "+tr(fmt.Sprint(o["status"])))
	d.Text(40, 712, 9, true, tr("Bill to")+":")
	d.Text(40, 698, 9, false, fmt.Sprint(o["customer_name"]))
	d.Text(40, 686, 9, false, strOr(o["customer_phone"], ""))
	d.Text(40, 674, 9, false, export.Fit(strOr(o["shipping_address"], ""), 300, 9))
	y := 640.0
	d.Rect(40, y-4, 515, 16, 0.92)
	d.Text(46, y, 9, true, tr("Item"))
	d.Text(330, y, 9, true, tr("Qty"))
	d.Text(390, y, 9, true, tr("Price"))
	d.TextRight(549, y, 9, true, tr("Subtotal"))
	y -= 20
	items, _ := o["items"].([]map[string]any)
	for _, it := range items {
		d.Text(46, y, 9, false, export.Fit(fmt.Sprint(it["product_name"]), 270, 9))
		d.Text(330, y, 9, false, export.Thousands(num(it["quantity"])))
		d.Text(390, y, 9, false, export.Thousands(num(it["unit_price"])))
		d.TextRight(549, y, 9, false, export.Thousands(num(it["subtotal"])))
		y -= 15
	}
	y -= 6
	d.Line(330, y, 555)
	for _, r := range [][2]string{{"Subtotal", "subtotal"}, {"Shipping", "shipping_fee"}, {"Total", "grand_total"}} {
		y -= 16
		d.Text(390, y, 9.5, r[0] == "Total", tr(r[0]))
		d.TextRight(549, y, 9.5, r[0] == "Total", export.Thousands(num(o[r[1]])))
	}
	return export.Send(c, "order-"+strings.ReplaceAll(fmt.Sprint(o["order_number"]), "/", "-")+".pdf", "application/pdf", d.Bytes())
}
