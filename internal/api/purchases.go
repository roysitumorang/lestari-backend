package api

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/repositories"
)

func (a *API) purchaseResource() *crud.Resource {
	return &crud.Resource{
		Name: "Purchases", Singular: "Purchase", Table: "purchases", DefaultSort: "t.purchase_date DESC, t.id DESC",
		Fields: []crud.Field{
			{Name: "receipt_number", ReadOnly: true, Search: true},
			{Name: "supplier_id", Kind: crud.UUID, Filter: true, Ref: &crud.Ref{Key: "supplier", Table: "suppliers"}},
			{Name: "supplier_name", Expr: "(SELECT name FROM suppliers WHERE id = t.supplier_id)", Search: true},
			{Name: "purchase_date", Kind: crud.Date, Required: true},
			{Name: "invoice_number", Search: true},
			{Name: "payment_type", Required: true, Enum: en("cash", "credit"), Default: "cash", Filter: true},
			{Name: "payment_method", Enum: salesPaymentTypes},
			{Name: "due_date", Kind: crud.Date},
			{Name: "paid_amount", Kind: crud.Num, Min: new(float64(0))},
			{Name: "subtotal", Kind: crud.Num, ReadOnly: true},
			{Name: "discount_total", Kind: crud.Num, Min: new(float64(0)), Default: 0.0},
			{Name: "tax_total", Kind: crud.Num, Min: new(float64(0)), Default: 0.0},
			{Name: "fee_total", Kind: crud.Num, Min: new(float64(0)), Default: 0.0},
			{Name: "grand_total", Kind: crud.Num, ReadOnly: true},
			{Name: "staff_name", ReadOnly: true},
			{Name: "status", ReadOnly: true},
			{Name: "note"},
			{Name: "items", Kind: crud.JSON, Virtual: true, Required: true},
		},
		Filters: []crud.Filter{dateFrom("t.purchase_date"), dateTo("t.purchase_date")},
		BeforeWrite: func(ctx context.Context, h *crud.Ctx, vals map[string]any, id string) error {
			if id == "" {
				num, err := a.nextNumber(ctx, h.Tx, "PO", "purchases", "receipt_number", "branch_id", h.P.BranchID)
				if err != nil {
					return err
				}
				vals["receipt_number"], vals["staff_id"], vals["staff_name"] = num, h.P.UserID, h.P.Name
			}
			return nil
		},
		AfterWrite: func(ctx context.Context, h *crud.Ctx, id string, created bool) error {
			if !created {
				if _, ok := h.Body["items"]; !ok {
					return a.recalcPurchase(ctx, h.Tx, h.P, id)
				}
				if err := a.reversePurchaseItems(ctx, h.Tx, h.P, id, "Purchase edited"); err != nil {
					return err
				}
			}
			if err := a.addPurchaseItems(ctx, h.Tx, h.P, id, helper.Items(h.Body, "items")); err != nil {
				return err
			}
			return a.recalcPurchase(ctx, h.Tx, h.P, id)
		},
		BeforeDelete: func(ctx context.Context, h *crud.Ctx, id string) error {
			return a.reversePurchaseItems(ctx, h.Tx, h.P, id, "Purchase deleted")
		},
		Detail: func(ctx context.Context, q repositories.Querier, p *helper.Principal, row map[string]any) error {
			items, err := repositories.JSONRows(ctx, q, `SELECT product_id, product_name, quantity, unit, unit_cost, discount_amount, subtotal FROM purchase_items WHERE purchase_id=$1 ORDER BY id`, row["id"])
			row["items"] = items
			return err
		},
	}
}

func (a *API) addPurchaseItems(ctx context.Context, tx pgx.Tx, p *helper.Principal, purchaseID string, items []map[string]any) error {
	var ref string
	if err := tx.QueryRow(ctx, `SELECT receipt_number FROM purchases WHERE id=$1`, purchaseID).Scan(&ref); err != nil {
		return err
	}
	v := &helper.Validation{}
	n := 0
	for i, it := range items {
		pid, ok := helper.UUID(it, "product_id")
		qty, qok := helper.Num(it, "quantity")
		if !ok && !qok {
			continue
		}
		cost, _ := helper.Num(it, "unit_cost")
		disc, _ := helper.Num(it, "discount_amount")
		if !ok || !qok || qty <= 0 || cost < 0 {
			v.Add("items", "Row {row} needs a product, a quantity above 0 and a unit cost.", "row", i+1)
			continue
		}
		var name string
		if err := tx.QueryRow(ctx, `SELECT name FROM products WHERE id=$1 AND branch_id=$2 AND deleted_at IS NULL`, pid, p.BranchID).Scan(&name); err != nil {
			v.Add("items", "Row {row}: product not found.", "row", i+1)
			continue
		}
		sub := round2(qty*cost - disc)
		if _, err := tx.Exec(ctx, `INSERT INTO purchase_items (purchase_id, product_id, product_name, quantity, unit, unit_cost, discount_amount, subtotal) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8)`,
			purchaseID, pid, name, qty, helper.Str(it, "unit"), cost, disc, sub); err != nil {
			return err
		}
		effCost := round2(sub / qty)
		after, err := productStock(ctx, tx, p.BranchID, pid, qty, "purchase", ref, &effCost, p.UserID, p.Name, "")
		if err != nil {
			return err
		}
		// weighted-average cost
		if _, err := tx.Exec(ctx, `UPDATE products SET purchase_price = CASE WHEN $2::numeric > 0 AND $2::numeric - $3::numeric > 0
				THEN round((($2::numeric - $3::numeric) * purchase_price + $3::numeric * $4::numeric) / $2::numeric, 2) ELSE $4::numeric END WHERE id=$1`,
			pid, after, qty, effCost); err != nil {
			return err
		}
		n++
	}
	if n == 0 && len(v.Fields) == 0 {
		v.Add("items", "Add at least one item.")
	}
	return v.Err()
}

func (a *API) reversePurchaseItems(ctx context.Context, tx pgx.Tx, p *helper.Principal, purchaseID string, note string) error {
	var ref string
	if err := tx.QueryRow(ctx, `SELECT receipt_number FROM purchases WHERE id=$1`, purchaseID).Scan(&ref); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT product_id, quantity FROM purchase_items WHERE purchase_id=$1 AND product_id IS NOT NULL`, purchaseID)
	if err != nil {
		return err
	}
	type it struct {
		pid string
		qty float64
	}
	var items []it
	for rows.Next() {
		var x it
		if err := rows.Scan(&x.pid, &x.qty); err != nil {
			rows.Close()
			return err
		}
		items = append(items, x)
	}
	rows.Close()
	for _, x := range items {
		if _, err := productStock(ctx, tx, p.BranchID, x.pid, -x.qty, "purchase", ref, nil, p.UserID, p.Name, note); err != nil {
			he := &helper.Error{}
			if errors.As(err, &he) {
				continue // product deleted meanwhile
			}
			return err
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM purchase_items WHERE purchase_id=$1`, purchaseID)
	return err
}

// recalcPurchase refreshes totals, status and the payable for a purchase.
func (a *API) recalcPurchase(ctx context.Context, tx pgx.Tx, p *helper.Principal, id string) error {
	var payType, ref string
	var supplier *string
	var paidIn *float64
	var due *string
	var disc, tax, fee float64
	var date string
	if err := tx.QueryRow(ctx, `SELECT payment_type, receipt_number, supplier_id, paid_amount, due_date::text, discount_total, tax_total, fee_total, purchase_date::text FROM purchases WHERE id=$1`, id).
		Scan(&payType, &ref, &supplier, &paidIn, &due, &disc, &tax, &fee, &date); err != nil {
		return err
	}
	var sub float64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(subtotal),0) FROM purchase_items WHERE purchase_id=$1`, id).Scan(&sub); err != nil {
		return err
	}
	total := round2(math.Max(sub-disc, 0) + tax + fee)
	paid := total
	if payType == "credit" {
		paid = 0
		if paidIn != nil {
			paid = math.Min(*paidIn, total)
		}
		if supplier == nil {
			return helper.Invalid("supplier_id", "A credit purchase needs a supplier.")
		}
	}
	// payments already recorded against the payable count as paid
	var debtPaid float64
	_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(dp.amount),0) FROM debt_payments dp JOIN debts d ON d.id = dp.debt_id WHERE d.purchase_id=$1`, id).Scan(&debtPaid)
	status := "completed"
	if payType == "credit" && paid+debtPaid < total {
		status = "unpaid"
	}
	if _, err := tx.Exec(ctx, `UPDATE purchases SET subtotal=$2, grand_total=$3, paid_amount=$4, status=$5, updated_at=now() WHERE id=$1`, id, sub, total, paid, status); err != nil {
		return err
	}
	if payType != "credit" || paid >= total {
		_, err := tx.Exec(ctx, `DELETE FROM debts WHERE purchase_id=$1 AND NOT EXISTS (SELECT 1 FROM debt_payments dp WHERE dp.debt_id = debts.id)`, id)
		return err
	}
	debtStatus := "unpaid"
	if paid+debtPaid >= total {
		debtStatus = "paid"
	}
	tag, err := tx.Exec(ctx, `UPDATE debts SET supplier_id=$2, total_amount=$3, paid_amount=$4::numeric + $5::numeric, due_date=$6::date, status=$7, transaction_date=$8::date, updated_at=now() WHERE purchase_id=$1`,
		id, supplier, total, paid, debtPaid, due, debtStatus, date)
	if err != nil || tag.RowsAffected() > 0 {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO debts (branch_id, kind, supplier_id, purchase_id, reference_number, transaction_date, due_date, total_amount, paid_amount, status)
		VALUES ($1,'payable',$2,$3,$4,$5::date,$6::date,$7,$8,$9)`, p.BranchID, supplier, id, ref, date, due, total, paid, debtStatus)
	return err
}

func (a *API) registerPurchases() {
	res := a.purchaseResource()
	a.CRUD.Register(a.addH, "/purchases", res, "list", "create", "read", "update", "delete", "export")
	a.addH("POST", "/purchases/:id/returns", a.purchaseReturn)
}

func (a *API) purchaseReturn(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	body, err := helper.Body(c)
	if err != nil {
		return err
	}
	p := helper.P(c)
	refund := helper.Str(body, "refund_method")
	if refund == "" {
		refund = "cash"
	}
	if refund != "cash" && refund != "reduce_payable" {
		return helper.Invalid("refund_method", "Must be one of: cash, reduce_payable.")
	}
	var returnID string
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		ctx := c.Context()
		var ref string
		if err := tx.QueryRow(ctx, `SELECT receipt_number FROM purchases WHERE id=$1 AND branch_id=$2`, id, p.BranchID).Scan(&ref); err != nil {
			return helper.NotFound("Purchase")
		}
		num, err := a.nextNumber(ctx, tx, "RB", "returns", "return_number", "branch_id", p.BranchID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO returns (branch_id, kind, return_number, purchase_id, refund_method, reason, cashier_name) VALUES ($1,'purchase',$2,$3,$4,NULLIF($5,''),$6) RETURNING id`,
			p.BranchID, num, id, refund, helper.Str(body, "reason"), p.Name).Scan(&returnID); err != nil {
			return err
		}
		var total float64
		n := 0
		for i, it := range helper.Items(body, "items") {
			pid, ok := helper.UUID(it, "product_id")
			qty, qok := helper.Num(it, "quantity")
			if !ok && !qok {
				continue
			}
			var name string
			var bought, cost, returned float64
			err := tx.QueryRow(ctx, `SELECT product_name, sum(quantity), round(sum(subtotal)/NULLIF(sum(quantity),0), 2) FROM purchase_items WHERE purchase_id=$1 AND product_id=$2 GROUP BY product_name`, id, pid).Scan(&name, &bought, &cost)
			if err != nil || qty <= 0 {
				return helper.Invalid("items", "Row {row}: the product is not on this purchase or the quantity is invalid.", "row", i+1)
			}
			_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(ri.quantity),0) FROM return_items ri JOIN returns r ON r.id = ri.return_id WHERE r.purchase_id=$1 AND ri.product_id=$2 AND r.id <> $3`, id, pid, returnID).Scan(&returned)
			if qty > bought-returned {
				return helper.Invalid("items", "Row {row}: only {qty} can still be returned.", "row", i+1, "qty", trimNum(bought-returned))
			}
			sub := round2(qty * cost)
			if _, err := tx.Exec(ctx, `INSERT INTO return_items (return_id, product_id, product_name, quantity, unit_price, subtotal) VALUES ($1,$2,$3,$4,$5,$6)`, returnID, pid, name, qty, cost, sub); err != nil {
				return err
			}
			if _, err := productStock(ctx, tx, p.BranchID, pid, -qty, "return", num, &cost, p.UserID, p.Name, "Returned to supplier"); err != nil {
				return err
			}
			total += sub
			n++
		}
		if n == 0 {
			return helper.Invalid("items", "Add at least one item to return.")
		}
		if _, err := tx.Exec(ctx, `UPDATE returns SET refund_amount=$2 WHERE id=$1`, returnID, total); err != nil {
			return err
		}
		if refund == "reduce_payable" {
			_, err := tx.Exec(ctx, `UPDATE debts SET total_amount = GREATEST(total_amount - $2, paid_amount), status = CASE WHEN paid_amount >= total_amount - $2 THEN 'paid' ELSE status END, updated_at=now()
				WHERE purchase_id=$1 AND kind='payable'`, id, total)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	row, err := a.CRUD.Fetch(c.Context(), a.DB, a.returnResource("purchase"), p, returnID)
	if err != nil {
		return err
	}
	return helper.Created(c, row)
}

func trimNum(f float64) string { return fmt.Sprintf("%g", f) }

// ---------- debts ----------

func (a *API) debtResource(kind string) *crud.Resource {
	party, table := "supplier", "suppliers"
	if kind == "receivable" {
		party, table = "customer", "customers"
	}
	return &crud.Resource{
		Name: map[string]string{"payable": "Payables", "receivable": "Receivables"}[kind], Singular: map[string]string{"payable": "Payable", "receivable": "Receivable"}[kind],
		Table: "debts", Where: "t.kind = '" + kind + "'", DefaultSort: "t.status DESC, t.due_date NULLS LAST, t.id DESC",
		Fields: []crud.Field{
			{Name: "reference_number", ReadOnly: true, Search: true},
			{Name: party + "_id", Kind: crud.UUID, ReadOnly: true, Filter: true, Ref: &crud.Ref{Key: party, Table: table}},
			{Name: party + "_name", Expr: "(SELECT name FROM " + table + " WHERE id = t." + party + "_id)", Search: true},
			{Name: "transaction_date", Kind: crud.Date, ReadOnly: true},
			{Name: "due_date", Kind: crud.Date, ReadOnly: true},
			{Name: "total_amount", Kind: crud.Num, ReadOnly: true},
			{Name: "paid_amount", Kind: crud.Num, ReadOnly: true},
			{Name: "remaining_amount", Kind: crud.Num, Expr: "GREATEST(t.total_amount - t.paid_amount, 0)"},
			{Name: "status", ReadOnly: true, Filter: true},
		},
		Filters: []crud.Filter{{Param: "overdue", SQL: "(? <> 'true' OR (t.due_date < current_date AND t.status <> 'paid'))"}},
		Detail: func(ctx context.Context, q repositories.Querier, p *helper.Principal, row map[string]any) error {
			pays, err := repositories.JSONRows(ctx, q, `SELECT id, payment_date, amount, payment_method, note FROM debt_payments WHERE debt_id=$1 ORDER BY payment_date, id`, row["id"])
			row["payments"] = pays
			return err
		},
	}
}

func (a *API) registerDebts() {
	for _, kind := range []string{"payable", "receivable"} {
		res := a.debtResource(kind)
		base := "/" + kind + "s"
		a.CRUD.Register(a.addH, base, res, "list", "read", "export")
		a.addH("POST", base+"/:id/payments", a.payDebt(res))
	}
}

func (a *API) payDebt(res *crud.Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := helper.ParamID(c, "id")
		if err != nil {
			return err
		}
		body, err := helper.Body(c)
		if err != nil {
			return err
		}
		p := helper.P(c)
		amount, ok := helper.Num(body, "amount")
		date := helper.Str(body, "payment_date")
		method := helper.Str(body, "payment_method")
		v := &helper.Validation{}
		if !ok || amount <= 0 {
			v.Add("amount", "Enter an amount above 0.")
		}
		if _, ok := isoDate(date); !ok {
			v.Add("payment_date", "Use the format YYYY-MM-DD.")
		}
		if !contains(salesPaymentTypes, method) {
			v.Add("payment_method", "Choose a payment method.")
		}
		if err := v.Err(); err != nil {
			return err
		}
		var out map[string]any
		err = a.tx(c.Context(), func(tx pgx.Tx) error {
			ctx := c.Context()
			var total, paid float64
			var saleID, purchaseID *string
			kind := map[string]string{"Payables": "payable", "Receivables": "receivable"}[res.Name]
			if err := tx.QueryRow(ctx, `SELECT total_amount, paid_amount, sale_id, purchase_id FROM debts WHERE id=$1 AND branch_id=$2 AND kind=$3 FOR UPDATE`, id, p.BranchID, kind).
				Scan(&total, &paid, &saleID, &purchaseID); err != nil {
				return helper.NotFound(res.Singular)
			}
			remaining := round2(total - paid)
			if remaining <= 0 {
				return helper.Conflict("This {kind} is already paid.", "kind", i18n.Tr(res.Singular))
			}
			if amount > remaining+0.004 {
				return helper.Invalid("amount", "The amount is more than the remaining {amount}.", "amount", trimNum(remaining))
			}
			pay, err := repositories.JSONRow(ctx, tx, `INSERT INTO debt_payments (debt_id, payment_date, amount, payment_method, note) VALUES ($1,$2::date,$3,$4,NULLIF($5,'')) RETURNING id, payment_date, amount, payment_method, note`,
				id, date, amount, method, helper.Str(body, "note"))
			if err != nil {
				return err
			}
			paid = round2(paid + amount)
			status := "unpaid"
			if paid >= total-0.004 {
				status = "paid"
			}
			if _, err := tx.Exec(ctx, `UPDATE debts SET paid_amount=$2, status=$3, updated_at=now() WHERE id=$1`, id, paid, status); err != nil {
				return err
			}
			if status == "paid" && saleID != nil {
				_, _ = tx.Exec(ctx, `UPDATE sales SET status='completed', updated_at=now() WHERE id=$1`, *saleID)
			}
			if status == "paid" && purchaseID != nil {
				_, _ = tx.Exec(ctx, `UPDATE purchases SET status='completed', updated_at=now() WHERE id=$1`, *purchaseID)
			}
			out = map[string]any{"id": id, "paid_amount": paid, "remaining_amount": round2(math.Max(total-paid, 0)), "status": status, "payment": pay}
			return nil
		})
		if err != nil {
			return err
		}
		return helper.OK(c, out)
	}
}

// ---------- returns ----------

func (a *API) returnResource(kind string) *crud.Resource {
	r := &crud.Resource{
		Table: "returns", Where: "t.kind = '" + kind + "'", DefaultSort: "t.created_at DESC, t.id DESC", NoTimestamps: true,
		Fields: []crud.Field{
			{Name: "return_number", ReadOnly: true, Search: true},
			{Name: "created_at", Kind: crud.Timestamp, ReadOnly: true},
			{Name: "refund_method", ReadOnly: true},
			{Name: "refund_amount", Kind: crud.Num, ReadOnly: true},
			{Name: "reason", ReadOnly: true},
			{Name: "cashier_name", ReadOnly: true},
			{Name: "item_count", Kind: crud.Int, Expr: "(SELECT count(*) FROM return_items ri WHERE ri.return_id = t.id)"},
		},
		Filters: []crud.Filter{dateFrom("t.created_at::date"), dateTo("t.created_at::date")},
		Detail: func(ctx context.Context, q repositories.Querier, p *helper.Principal, row map[string]any) error {
			items, err := repositories.JSONRows(ctx, q, `SELECT product_id, product_name, quantity, unit_price, unit_price AS unit_cost, subtotal FROM return_items WHERE return_id=$1 ORDER BY id`, row["id"])
			row["items"] = items
			return err
		},
	}
	if kind == "sale" {
		r.Name, r.Singular = "Sales returns", "Sales return"
		r.Fields = append(r.Fields,
			crud.Field{Name: "sale_id", Kind: crud.UUID, ReadOnly: true},
			crud.Field{Name: "receipt_number", Expr: "(SELECT receipt_number FROM sales WHERE id = t.sale_id)", Search: true},
			crud.Field{Name: "customer", Expr: "(SELECT json_build_object('id', c.id, 'name', c.name) FROM sales s JOIN customers c ON c.id = s.customer_id WHERE s.id = t.sale_id)"},
		)
	} else {
		r.Name, r.Singular = "Purchase returns", "Purchase return"
		r.Fields = append(r.Fields,
			crud.Field{Name: "purchase_id", Kind: crud.UUID, ReadOnly: true},
			crud.Field{Name: "purchase_number", Expr: "(SELECT receipt_number FROM purchases WHERE id = t.purchase_id)", Search: true},
			crud.Field{Name: "supplier", Expr: "(SELECT json_build_object('id', s.id, 'name', s.name) FROM purchases pu JOIN suppliers s ON s.id = pu.supplier_id WHERE pu.id = t.purchase_id)"},
		)
	}
	return r
}

func (a *API) registerReturns() {
	a.CRUD.Register(a.addH, "/sales-returns", a.returnResource("sale"), "list", "read", "export")
	a.CRUD.Register(a.addH, "/purchase-returns", a.returnResource("purchase"), "list", "read", "export")
	a.addH("POST", "/sales/:id/returns", a.saleReturn) // cashier app
}

func (a *API) saleReturn(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	body, err := helper.Body(c)
	if err != nil {
		return err
	}
	p := helper.P(c)
	var returnID string
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		ctx := c.Context()
		var receipt string
		if err := tx.QueryRow(ctx, `SELECT receipt_number FROM sales WHERE id=$1 AND branch_id=$2`, id, p.BranchID).Scan(&receipt); err != nil {
			return helper.NotFound("Sale")
		}
		num, err := a.nextNumber(ctx, tx, "RJ", "returns", "return_number", "branch_id", p.BranchID)
		if err != nil {
			return err
		}
		refund := helper.Str(body, "refund_method")
		if refund == "" {
			refund = "cash"
		}
		if err := tx.QueryRow(ctx, `INSERT INTO returns (branch_id, kind, return_number, sale_id, refund_method, reason, cashier_name) VALUES ($1,'sale',$2,$3,$4,NULLIF($5,''),$6) RETURNING id`,
			p.BranchID, num, id, refund, helper.Str(body, "reason"), p.Name).Scan(&returnID); err != nil {
			return err
		}
		var total float64
		n := 0
		for i, it := range helper.Items(body, "items") {
			pid, _ := helper.UUID(it, "product_id")
			qty, _ := helper.Num(it, "quantity")
			var name string
			var sold, price float64
			if err := tx.QueryRow(ctx, `SELECT product_name, sum(quantity), round(sum(subtotal)/NULLIF(sum(quantity),0),2) FROM sale_items WHERE sale_id=$1 AND product_id=$2 GROUP BY product_name`, id, pid).
				Scan(&name, &sold, &price); err != nil || qty <= 0 || qty > sold {
				return helper.Invalid("items", "Row {row}: the product is not on this sale or the quantity is invalid.", "row", i+1)
			}
			sub := round2(qty * price)
			if _, err := tx.Exec(ctx, `INSERT INTO return_items (return_id, product_id, product_name, quantity, unit_price, subtotal) VALUES ($1,$2,$3,$4,$5,$6)`, returnID, pid, name, qty, price, sub); err != nil {
				return err
			}
			prod, err := a.loadProduct(ctx, tx, p.BranchID, pid)
			if err == nil {
				if err := a.consumeStock(ctx, tx, p, prod, qty, 1, "return", num); err != nil {
					return err
				}
			}
			total += sub
			n++
		}
		if n == 0 {
			return helper.Invalid("items", "Add at least one item to return.")
		}
		_, err = tx.Exec(ctx, `UPDATE returns SET refund_amount=$2 WHERE id=$1`, returnID, total)
		return err
	})
	if err != nil {
		return err
	}
	row, err := a.CRUD.Fetch(c.Context(), a.DB, a.returnResource("sale"), p, returnID)
	if err != nil {
		return err
	}
	return helper.Created(c, row)
}
