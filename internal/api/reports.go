package api

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// rq carries the common report parameters.
type rq struct {
	a          *API
	c          fiber.Ctx
	p          *helper.Principal
	start, end string
	q          string
	page       helper.Page
	all        bool // export: no pagination
}

func (r *rq) str(k string) string { return strings.TrimSpace(r.c.Query(k)) }

// id returns a UUID query parameter, or nil (SQL NULL) when it is absent or malformed.
func (r *rq) id(k string) any { return helper.QueryUUID(r.c, k) }

func (r *rq) int(k string) int64 {
	n, _ := strconv.ParseInt(r.c.Query(k), 10, 64)
	return n
}

type rres struct {
	rows    []map[string]any
	summary map[string]any
	totals  map[string]any
	chart   []map[string]any
	total   int
	paged   bool
}

type report struct {
	path, title string
	dated       bool
	cols        []export.Column
	run         func(r *rq) (*rres, error)
}

// paged runs base (a full SELECT) with count + LIMIT/OFFSET, or without limits when exporting.
func (r *rq) paged(base, order string, args ...any) (*rres, error) {
	ctx := r.c.Context()
	if r.all {
		rows, err := repositories.JSONRows(ctx, r.a.DB, base+" ORDER BY "+order+" LIMIT 100000", args...)
		return &rres{rows: rows, total: len(rows)}, err
	}
	var total int
	if err := r.a.DB.QueryRow(ctx, `SELECT count(*) FROM (`+base+`) _c`, args...).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := repositories.JSONRows(ctx, r.a.DB, base+" ORDER BY "+order+fmt.Sprintf(" LIMIT %d OFFSET %d", r.page.PerPage, r.page.Offset), args...)
	return &rres{rows: rows, total: total, paged: true}, err
}

func (r *rq) rows(sql string, args ...any) ([]map[string]any, error) {
	return repositories.JSONRows(r.c.Context(), r.a.DB, sql, args...)
}

func (r *rq) row(sql string, args ...any) (map[string]any, error) {
	m, err := repositories.JSONRow(r.c.Context(), r.a.DB, sql, args...)
	if m == nil {
		m = map[string]any{}
	}
	return m, err
}

func (a *API) newRQ(c fiber.Ctx, dated bool) (*rq, error) {
	r := &rq{a: a, c: c, p: helper.P(c), q: strings.TrimSpace(c.Query("q")), page: helper.Paging(c, 50)}
	if dated {
		var err error
		if r.start, r.end, err = helper.DateRange(c, a.Loc); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (a *API) reportHandler(rp report) fiber.Handler {
	return func(c fiber.Ctx) error {
		r, err := a.newRQ(c, rp.dated)
		if err != nil {
			return err
		}
		res, err := rp.run(r)
		if err != nil {
			return err
		}
		meta := helper.Unpaged(len(res.rows))
		if res.paged {
			meta = helper.NewMeta(r.page, res.total)
		}
		x := helper.Extras{}
		if res.summary != nil {
			x.Summary = res.summary
		}
		if res.totals != nil {
			x.Totals = res.totals
		}
		if res.chart != nil {
			x.Chart = res.chart
		}
		return helper.ListWith(c, res.rows, meta, x)
	}
}

func (a *API) reportExport(rp report) fiber.Handler {
	return func(c fiber.Ctx) error {
		r, err := a.newRQ(c, rp.dated)
		if err != nil {
			return err
		}
		r.all = true
		res, err := rp.run(r)
		if err != nil {
			return err
		}
		name := strings.Trim(strings.ReplaceAll(strings.TrimPrefix(rp.path, "/reports/"), "/", "-"), "-")
		rows := res.rows
		if res.totals != nil {
			t := map[string]any{}
			maps.Copy(t, res.totals)
			t[rp.cols[0].Key] = "Total"
			rows = append(rows, t)
		}
		return export.Write(c, name, rp.title, rp.cols, rows)
	}
}

func col(key, label, format string) export.Column {
	return export.Column{Key: key, Label: label, Format: format}
}

// sums adds up numeric keys across rows.
func sums(rows []map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		var t float64
		for _, r := range rows {
			t += num(r[k])
		}
		out[k] = round2(t)
	}
	return out
}

const saleItemSQL = `FROM sale_items i JOIN sales s ON s.id = i.sale_id`

func (a *API) registerReports() {
	reports := []report{
		{path: "/reports/profit-loss", title: "Profit & Loss", dated: true, run: a.profitLoss, cols: []export.Column{
			col("date", "Date", "date"), col("gross_sales", "Sales", "money"), col("discounts", "Discounts", "money"), col("cost_of_goods_sold", "COGS", "money"),
			col("gross_profit", "Gross profit", "money"), col("other_income", "Other income", "money"), col("mdr_fees", "MDR fees", "money"),
			col("commissions", "Commissions", "money"), col("other_expenses", "Expenses", "money"), col("net_profit", "Net profit", "money")}},
		{path: "/reports/cash-flow", title: "Cash Flow", dated: true, run: a.cashFlow, cols: []export.Column{
			col("date", "Date", "datetime"), col("type", "Type", ""), col("source", "Source", ""), col("category", "Category", ""), col("description", "Description", ""), col("amount", "Amount", "money")}},
		{path: "/reports/daily-sales", title: "Daily Sales", dated: true, run: a.dailySales, cols: []export.Column{
			col("date", "Date", "date"), col("transaction_count", "Transactions", "number"), col("gross_sales", "Gross", "money"), col("discounts", "Discounts", "money"),
			col("taxes", "Tax", "money"), col("net_sales", "Net", "money"), col("profit", "Profit", "money")}},
		{path: "/reports/sales-transactions", title: "Sales Transactions", dated: true, run: a.salesTransactions, cols: []export.Column{
			col("created_at", "Date", "datetime"), col("receipt_number", "Receipt", ""), col("cashier_name", "Cashier", ""), col("payment_method", "Method", ""), col("payment_type", "Type", ""),
			col("profit", "Profit", "money"), col("grand_total", "Total", "money"), col("paid_amount", "Paid", "money"), col("service_charge", "Service", "money"),
			col("rounding", "Rounding", "money"), col("label", "Label", ""), col("customer_type", "Customer type", "")}},
		{path: "/reports/sales-by-product", title: "Sales by Product", dated: true, run: a.salesByProduct, cols: []export.Column{
			col("product_name", "Product", ""), col("sku", "SKU", ""), col("category_name", "Category", ""), col("quantity_sold", "Qty", "number"), col("gross_sales", "Gross", "money"),
			col("discounts", "Discounts", "money"), col("net_sales", "Net", "money"), col("cost_of_goods_sold", "COGS", "money"), col("profit", "Profit", "money")}},
		{path: "/reports/sales-by-category", title: "Sales by Category", dated: true, run: a.salesByCategory, cols: []export.Column{
			col("category_name", "Category", ""), col("product_count", "Products", "number"), col("quantity_sold", "Qty", "number"), col("gross_sales", "Gross", "money"),
			col("net_sales", "Net", "money"), col("profit", "Profit", "money")}},
		{path: "/reports/daily-purchases", title: "Daily Purchases", dated: true, run: a.dailyPurchases, cols: []export.Column{
			col("date", "Date", "date"), col("purchase_count", "Purchases", "number"), col("item_quantity", "Items", "number"), col("grand_total", "Total", "money"), col("paid_amount", "Paid", "money")}},
		{path: "/reports/purchase-transactions", title: "Purchase Transactions", dated: true, run: a.purchaseTransactions, cols: []export.Column{
			col("purchase_date", "Date", "date"), col("receipt_number", "Number", ""), col("supplier_name", "Supplier", ""), col("staff_name", "Recorded by", ""),
			col("payment_type", "Payment", ""), col("grand_total", "Total", "money"), col("paid_amount", "Paid", "money")}},
		{path: "/reports/customer-points", title: "Points by Customer", dated: true, run: a.customerPoints, cols: []export.Column{
			col("customer_name", "Customer", ""), col("customer_code", "Code", ""), col("points_earned", "Earned", "number"), col("points_redeemed", "Redeemed", "number"),
			col("transaction_count", "Transactions", "number"), col("points_balance", "Balance", "number")}},
		{path: "/reports/point-redemptions", title: "Point Redemptions", dated: true, run: a.pointProducts("redeemed"), cols: []export.Column{
			col("product_name", "Product", ""), col("quantity", "Qty", "number"), col("transaction_count", "Transactions", "number"), col("points_redeemed", "Points", "number"), col("profit", "Profit", "money")}},
		{path: "/reports/point-earnings", title: "Point-Earning Sales", dated: true, run: a.pointProducts("earned"), cols: []export.Column{
			col("product_name", "Product", ""), col("quantity", "Qty", "number"), col("transaction_count", "Transactions", "number"), col("points_earned", "Points", "number"), col("profit", "Profit", "money")}},
		{path: "/reports/capital", title: "Inventory Capital", dated: false, run: a.capital, cols: []export.Column{
			col("product_name", "Product", ""), col("sku", "SKU", ""), col("category_name", "Category", ""), col("stock", "Stock", "number"), col("unit_cost", "Unit cost", "money"), col("capital_value", "Capital", "money")}},
		{path: "/reports/shifts", title: "Shifts", dated: true, run: a.shifts, cols: []export.Column{
			col("opened_at", "Opened", "datetime"), col("closed_at", "Closed", "datetime"), col("cash_drawer_name", "Drawer", ""), col("cashier_name", "Cashier", ""),
			col("opening_cash", "Opening", "money"), col("expected_cash", "Expected", "money"), col("actual_cash", "Counted", "money"), col("difference", "Difference", "money")}},
		{path: "/reports/fees/sales", title: "Fees on Sales", dated: true, run: a.feesSales, cols: feeCols},
		{path: "/reports/fees/purchases", title: "Fees on Purchases", dated: true, run: a.feesPurchases, cols: feeCols},
		{path: "/reports/fees/mdr", title: "MDR Fees", dated: true, run: a.mdr, cols: []export.Column{
			col("created_at", "Date", "datetime"), col("receipt_number", "Receipt", ""), col("cashier_name", "Cashier", ""), col("payment_method", "Method", ""),
			col("transaction_amount", "Sale", "money"), col("mdr_rate", "Rate %", "number"), col("amount", "MDR", "money")}},
		{path: "/reports/taxes", title: "Taxes", dated: true, run: a.taxReport, cols: []export.Column{
			col("date", "Date", "date"), col("tax_name", "Tax", ""), col("rate", "Rate %", "number"), col("transaction_count", "Transactions", "number"),
			col("taxable_amount", "Taxable", "money"), col("tax_amount", "Tax", "money")}},
		{path: "/reports/ppob", title: "PPOB Sales", dated: true, run: a.ppobReport, cols: []export.Column{
			col("created_at", "Date", "datetime"), col("reference_id", "Reference", ""), col("product_name", "Product", ""), col("customer_number", "Customer no.", ""),
			col("base_price", "Cost", "money"), col("selling_price", "Price", "money"), col("profit", "Profit", "money"), col("cashier_name", "Cashier", ""), col("status", "Status", "")}},
		{path: "/reports/visitors", title: "Visitors", dated: true, run: a.visitors, cols: []export.Column{
			col("period", "Period", ""), col("visitor_count", "Visitors", "number"), col("net_sales", "Sales", "money")}},
		{path: "/reports/promotions/transactions", title: "Promotion Transactions", dated: true, run: a.promoTransactions, cols: []export.Column{
			col("promotion_name", "Promotion", ""), col("transaction_count", "Transactions", "number"), col("discount_total", "Discount", "money"), col("net_sales", "Revenue", "money"), col("profit", "Profit", "money")}},
		{path: "/reports/promotions/products", title: "Promotion Product Sales", dated: true, run: a.promoProducts, cols: []export.Column{
			col("product_name", "Product", ""), col("promotion_name", "Promotion", ""), col("quantity", "Qty", "number"), col("discount_total", "Discount", "money"),
			col("net_sales", "Revenue", "money"), col("profit", "Profit", "money")}},
		{path: "/reports/store-ratings", title: "Store Ratings", dated: true, run: a.ratings, cols: []export.Column{
			col("created_at", "Date", "datetime"), col("customer_name", "Customer", ""), col("rating", "Rating", "number"), col("comment", "Comment", ""), col("receipt_number", "Receipt", "")}},
		{path: "/reports/branches", title: "Branch Performance", dated: true, run: a.branchReport, cols: []export.Column{
			col("branch_name", "Branch", ""), col("transaction_count", "Transactions", "number"), col("revenue", "Revenue", "money"), col("profit", "Profit", "money"), col("average_basket", "Avg. basket", "money")}},
		{path: "/reports/stock-transfers", title: "Stock Transfers", dated: true, run: a.transferReport(false), cols: transferCols},
		{path: "/reports/stock-transfers/confirmations", title: "Transfers Awaiting Confirmation", dated: true, run: a.transferReport(true), cols: transferCols},
	}
	for _, rp := range reports {
		h, e := a.reportHandler(rp), a.reportExport(rp)
		if rp.path == "/reports/stock-transfers/confirmations" {
			a.add("GET", rp.path, requirePluginH("business"), h)
			a.add("GET", rp.path+"/export", requirePluginH("business"), e)
			continue
		}
		a.addH("GET", rp.path, h)
		a.addH("GET", rp.path+"/export", e)
	}
	a.addH("POST", "/reports/daily-sales/rebuild", a.rebuildDaily)
	a.addH("GET", "/reports/daily-sales/:date", a.salesOfDay)
	a.addH("GET", "/reports/sales-by-product/:product_id", a.salesWhere("EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.product_id = $4)", "product_id"))
	a.addH("GET", "/reports/customer-points/:customer_id", a.salesWhere("s.customer_id = $4 AND (s.points_earned > 0 OR s.points_redeemed > 0)", "customer_id"))
	a.addH("GET", "/reports/point-redemptions/:product_id", a.salesWhere("EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.product_id = $4 AND i.points_redeemed > 0)", "product_id"))
	a.addH("GET", "/reports/point-earnings/:product_id", a.salesWhere("EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.product_id = $4 AND i.points_earned > 0)", "product_id"))
	a.addH("GET", "/reports/promotions/transactions/:promotion_id", a.salesWhere("(s.promotion_id = $4 OR EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.promotion_id = $4))", "promotion_id"))
	a.addH("GET", "/reports/promotions/products/:product_id", a.salesWhere("EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.product_id = $4 AND i.promotion_id IS NOT NULL)", "product_id"))
	a.addH("GET", "/reports/shifts/:id", a.shiftDetail)
	a.addH("GET", "/reports/shifts/:id/export", a.shiftPDF)
	a.addH("GET", "/reports/store-ratings/:id", func(c fiber.Ctx) error {
		id, err := helper.ParamID(c, "id")
		if err != nil {
			return err
		}
		row, err := repositories.JSONRow(c.Context(), a.DB, `SELECT r.id, r.created_at, r.customer_name, r.rating, r.comment, r.tags, s.receipt_number FROM store_ratings r LEFT JOIN sales s ON s.id = r.sale_id WHERE r.id=$1 AND r.branch_id=$2`, id, helper.P(c).BranchID)
		if err != nil {
			return err
		}
		if row == nil {
			return helper.NotFound("Review")
		}
		return helper.OK(c, row)
	})
}

func requirePluginH(code string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := requirePlugin(c, code); err != nil {
			return err
		}
		return c.Next()
	}
}

var feeCols = []export.Column{col("created_at", "Date", "datetime"), col("receipt_number", "Receipt", ""), col("cashier_name", "Cashier", ""), col("fee_name", "Fee", ""), col("amount", "Amount", "money")}
var transferCols = []export.Column{col("transfer_date", "Date", "date"), col("transfer_number", "Number", ""), col("direction", "Type", ""), col("from_branch_name", "From", ""),
	col("to_branch_name", "To", ""), col("item_count", "Items", "number"), col("total_quantity", "Qty", "number"), col("status", "Status", "")}

// salesFilter limits sales to the branch, period and (optionally) unpaid credit sales.
const salesBase = `s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date`

// ---------- profit & loss ----------

func (a *API) profitLoss(r *rq) (*rres, error) {
	unpaid := r.str("include_unpaid") == "true"
	rows, err := r.rows(`
		WITH d AS (SELECT generate_series($2::date, $3::date, interval '1 day')::date AS date),
		sa AS (SELECT s.created_at::date AS date, sum(s.subtotal) AS gross_sales, sum(s.discount_total) AS discounts, sum(s.cost_total) AS cogs,
		          sum(s.mdr_amount) AS mdr, sum(s.commission_total) AS commissions
		       FROM sales s WHERE `+salesBase+` AND ($4 OR s.status <> 'unpaid') GROUP BY 1),
		fe AS (SELECT entry_date AS date, sum(amount) FILTER (WHERE type='income') AS income, sum(amount) FILTER (WHERE type='expense') AS expense
		       FROM finance_entries WHERE branch_id = $1 AND entry_date BETWEEN $2::date AND $3::date GROUP BY 1)
		SELECT d.date, COALESCE(sa.gross_sales,0) AS gross_sales, COALESCE(sa.discounts,0) AS discounts, COALESCE(sa.cogs,0) AS cost_of_goods_sold,
		  COALESCE(sa.gross_sales,0) - COALESCE(sa.discounts,0) - COALESCE(sa.cogs,0) AS gross_profit,
		  COALESCE(fe.income,0) AS other_income, COALESCE(sa.mdr,0) AS mdr_fees, COALESCE(sa.commissions,0) AS commissions, COALESCE(fe.expense,0) AS other_expenses,
		  COALESCE(sa.gross_sales,0) - COALESCE(sa.discounts,0) - COALESCE(sa.cogs,0) + COALESCE(fe.income,0) - COALESCE(sa.mdr,0) - COALESCE(sa.commissions,0) - COALESCE(fe.expense,0) AS net_profit
		FROM d LEFT JOIN sa ON sa.date = d.date LEFT JOIN fe ON fe.date = d.date ORDER BY d.date`, r.p.BranchID, r.start, r.end, unpaid)
	if err != nil {
		return nil, err
	}
	keys := []string{"gross_sales", "discounts", "cost_of_goods_sold", "gross_profit", "other_income", "mdr_fees", "commissions", "other_expenses", "net_profit"}
	totals := sums(rows, keys...)
	summary := map[string]any{}
	maps.Copy(summary, totals)
	var purchases float64
	_ = a.DB.QueryRow(r.c.Context(), `SELECT COALESCE(sum(grand_total),0) FROM purchases WHERE branch_id=$1 AND purchase_date BETWEEN $2::date AND $3::date`, r.p.BranchID, r.start, r.end).Scan(&purchases)
	summary["purchase_total"] = purchases
	return &rres{rows: rows, summary: summary, totals: totals}, nil
}

// ---------- cash flow ----------

func (a *API) cashFlow(r *rq) (*rres, error) {
	base := `SELECT * FROM (
		SELECT s.created_at AS date, 'income' AS type, 'sale' AS source, s.payment_method AS category, 'Sale ' || s.receipt_number AS description, s.paid_amount AS amount
		  FROM sales s WHERE ` + salesBase + ` AND s.paid_amount > 0
		UNION ALL
		SELECT p.purchase_date::timestamptz, 'expense', 'purchase', COALESCE(p.payment_method,'cash'), 'Purchase ' || p.receipt_number, p.paid_amount
		  FROM purchases p WHERE p.branch_id = $1 AND p.purchase_date BETWEEN $2::date AND $3::date AND p.paid_amount > 0
		UNION ALL
		SELECT dp.payment_date::timestamptz, CASE d.kind WHEN 'receivable' THEN 'income' ELSE 'expense' END, d.kind || '_payment', dp.payment_method, 'Payment for ' || d.reference_number, dp.amount
		  FROM debt_payments dp JOIN debts d ON d.id = dp.debt_id WHERE d.branch_id = $1 AND dp.payment_date BETWEEN $2::date AND $3::date
		UNION ALL
		SELECT f.entry_date::timestamptz, f.type, 'cash_entry', f.category, COALESCE(f.description, f.category), f.amount
		  FROM finance_entries f WHERE f.branch_id = $1 AND f.entry_date BETWEEN $2::date AND $3::date
		UNION ALL
		SELECT r.created_at, 'expense', 'refund', COALESCE(r.refund_method,'cash'), 'Refund ' || r.return_number, r.refund_amount
		  FROM returns r WHERE r.branch_id = $1 AND r.kind = 'sale' AND r.created_at::date BETWEEN $2::date AND $3::date AND r.refund_amount > 0
		) x WHERE ($4 = '' OR x.type = $4) AND ($5 = '' OR x.description ILIKE '%'||$5||'%' OR x.category ILIKE '%'||$5||'%')`
	args := []any{r.p.BranchID, r.start, r.end, r.str("type"), r.q}
	res, err := r.paged(base, "date DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(sum(amount) FILTER (WHERE type='income'),0) AS total_income, COALESCE(sum(amount) FILTER (WHERE type='expense'),0) AS total_expense,
		COALESCE(sum(CASE WHEN type='income' THEN amount ELSE -amount END),0) AS net_cash_flow FROM (`+base+`) t`, args...)
	return res, err
}

// ---------- sales ----------

func (a *API) dailySales(r *rq) (*rres, error) {
	rows, err := r.rows(`SELECT s.created_at::date AS date, count(*) AS transaction_count, sum(s.subtotal) AS gross_sales, sum(s.discount_total) AS discounts,
		sum(s.tax_total) AS taxes, sum(s.subtotal - s.discount_total) AS net_sales, sum(s.profit) AS profit
		FROM sales s WHERE `+salesBase+` GROUP BY 1 ORDER BY 1`, r.p.BranchID, r.start, r.end)
	if err != nil {
		return nil, err
	}
	t := sums(rows, "transaction_count", "gross_sales", "discounts", "taxes", "net_sales", "profit")
	return &rres{rows: rows, summary: t, totals: t}, nil
}

const saleRowCols = `s.id, s.created_at, s.receipt_number, s.cashier_name, s.payment_method, s.payment_type, s.profit, s.grand_total, s.paid_amount,
	s.service_charge, s.rounding, s.label, s.customer_type, s.status, (SELECT name FROM customers WHERE id = s.customer_id) AS customer_name,
	(SELECT email FROM users WHERE id = s.cashier_id) AS staff_email`

func (a *API) salesTransactions(r *rq) (*rres, error) {
	base := `SELECT ` + saleRowCols + ` FROM sales s WHERE ` + salesBase + `
		AND ($4 = '' OR s.payment_method = $4) AND ($5 = '' OR s.payment_type = $5) AND ($6::uuid IS NULL OR s.cashier_id = $6::uuid)
		AND ($7 = '' OR s.receipt_number ILIKE '%'||$7||'%')`
	args := []any{r.p.BranchID, r.start, r.end, r.str("payment_method"), r.str("payment_type"), r.id("cashier_id"), r.q}
	res, err := r.paged(base, "s.created_at DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT count(*) AS transaction_count, COALESCE(sum(grand_total),0) AS grand_total, COALESCE(sum(profit),0) AS profit FROM (`+base+`) t`, args...)
	return res, err
}

func (a *API) salesByProduct(r *rq) (*rres, error) {
	base := `SELECT i.product_id, max(i.product_name) AS product_name, max(p.sku) AS sku, max(c.name) AS category_name,
		sum(i.quantity) AS quantity_sold, sum(i.quantity * i.unit_price) AS gross_sales, sum(i.discount_amount + i.promotion_discount) AS discounts,
		sum(i.subtotal) AS net_sales, sum(i.quantity * i.unit_cost) AS cost_of_goods_sold, sum(i.subtotal - i.quantity * i.unit_cost) AS profit
		` + saleItemSQL + ` LEFT JOIN products p ON p.id = i.product_id LEFT JOIN categories c ON c.id = COALESCE(i.category_id, p.category_id)
		WHERE ` + salesBase + ` AND ($4::uuid IS NULL OR COALESCE(i.category_id, p.category_id) = $4::uuid) AND ($5 = '' OR i.product_name ILIKE '%'||$5||'%' OR p.sku ILIKE '%'||$5||'%')
		GROUP BY i.product_id`
	args := []any{r.p.BranchID, r.start, r.end, r.id("category_id"), r.q}
	res, err := r.paged(base, "net_sales DESC NULLS LAST", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(sum(quantity_sold),0) AS quantity_sold, COALESCE(sum(net_sales),0) AS net_sales, COALESCE(sum(profit),0) AS profit FROM (`+base+`) t`, args...)
	return res, err
}

func (a *API) salesByCategory(r *rq) (*rres, error) {
	rows, err := r.rows(`SELECT COALESCE(c.name, 'Uncategorised') AS category_name, count(DISTINCT i.product_id) AS product_count, sum(i.quantity) AS quantity_sold,
		sum(i.quantity * i.unit_price) AS gross_sales, sum(i.subtotal) AS net_sales, sum(i.subtotal - i.quantity * i.unit_cost) AS profit
		`+saleItemSQL+` LEFT JOIN products p ON p.id = i.product_id LEFT JOIN categories c ON c.id = COALESCE(i.category_id, p.category_id)
		WHERE `+salesBase+` AND ($4 = '' OR c.name ILIKE '%'||$4||'%') GROUP BY 1 ORDER BY net_sales DESC`, r.p.BranchID, r.start, r.end, r.q)
	if err != nil {
		return nil, err
	}
	t := sums(rows, "quantity_sold", "gross_sales", "net_sales", "profit")
	return &rres{rows: rows, summary: t, totals: t}, nil
}

func (a *API) salesOfDay(c fiber.Ctx) error {
	day := c.Params("date")
	if _, ok := isoDate(day); !ok {
		return helper.NotFound("Day")
	}
	rows, err := repositories.JSONRows(c.Context(), a.DB, `SELECT `+saleRowCols+` FROM sales s WHERE s.branch_id=$1 AND s.created_at::date = $2::date ORDER BY s.created_at`, helper.P(c).BranchID, day)
	if err != nil {
		return err
	}
	return helper.OK(c, fiber.Map{"date": day, "sales": rows})
}

// salesWhere serves the "which receipts" detail endpoints of several reports.
func (a *API) salesWhere(cond, param string) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := helper.ParamID(c, param)
		if err != nil {
			return err
		}
		start, end, err := helper.DateRange(c, a.Loc)
		if err != nil {
			return err
		}
		// without an explicit period, show the last year
		if c.Query("start_date") == "" {
			start = a.now().AddDate(-1, 0, 0).Format("2006-01-02")
		}
		rows, err := repositories.JSONRows(c.Context(), a.DB, `SELECT `+saleRowCols+`, s.points_earned, s.points_redeemed FROM sales s WHERE `+salesBase+` AND `+cond+` ORDER BY s.created_at DESC LIMIT 500`,
			helper.P(c).BranchID, start, end, id)
		if err != nil {
			return err
		}
		return helper.OK(c, fiber.Map{"sales": rows})
	}
}

func (a *API) rebuildDaily(c fiber.Ctx) error {
	p := helper.P(c)
	// totals are computed live from transactions, so the job only validates the period
	id, err := a.startJob(p.StoreID, "rebuild_daily_sales", fiber.Map{"start_date": c.Query("start_date"), "branch_id": p.BranchID}, func(context.Context) (any, error) {
		return map[string]any{"rebuilt": true}, nil
	})
	if err != nil {
		return err
	}
	return helper.OK(c, fiber.Map{"job_id": id, "status": "queued"})
}

// ---------- purchases ----------

func (a *API) dailyPurchases(r *rq) (*rres, error) {
	rows, err := r.rows(`SELECT p.purchase_date AS date, count(*) AS purchase_count,
		COALESCE(sum((SELECT sum(quantity) FROM purchase_items i WHERE i.purchase_id = p.id)),0) AS item_quantity, sum(p.grand_total) AS grand_total, sum(p.paid_amount) AS paid_amount
		FROM purchases p WHERE p.branch_id=$1 AND p.purchase_date BETWEEN $2::date AND $3::date AND ($4 = '' OR p.receipt_number ILIKE '%'||$4||'%') GROUP BY 1 ORDER BY 1`,
		r.p.BranchID, r.start, r.end, r.q)
	if err != nil {
		return nil, err
	}
	t := sums(rows, "purchase_count", "item_quantity", "grand_total", "paid_amount")
	return &rres{rows: rows, summary: t, totals: t}, nil
}

func (a *API) purchaseTransactions(r *rq) (*rres, error) {
	base := `SELECT p.id, p.purchase_date, p.receipt_number, (SELECT name FROM suppliers WHERE id = p.supplier_id) AS supplier_name, p.staff_name, p.payment_type, p.grand_total, p.paid_amount
		FROM purchases p WHERE p.branch_id=$1 AND p.purchase_date BETWEEN $2::date AND $3::date AND ($4 = '' OR p.payment_type = $4)
		AND ($5::uuid IS NULL OR p.staff_id = $5::uuid) AND ($6::uuid IS NULL OR p.supplier_id = $6::uuid) AND ($7 = '' OR p.receipt_number ILIKE '%'||$7||'%')`
	args := []any{r.p.BranchID, r.start, r.end, r.str("payment_type"), r.id("staff_id"), r.id("supplier_id"), r.q}
	res, err := r.paged(base, "p.purchase_date DESC, p.id DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT count(*) AS purchase_count, COALESCE(sum(grand_total),0) AS grand_total FROM (`+base+`) t`, args...)
	return res, err
}

// ---------- points ----------

func (a *API) customerPoints(r *rq) (*rres, error) {
	base := `SELECT cu.id AS customer_id, cu.name AS customer_name, cu.code AS customer_code, COALESCE(sum(s.points_earned),0) AS points_earned,
		COALESCE(sum(s.points_redeemed),0) AS points_redeemed, count(s.id) AS transaction_count, cu.points AS points_balance
		FROM customers cu JOIN sales s ON s.customer_id = cu.id AND ` + salesBase + ` AND (s.points_earned > 0 OR s.points_redeemed > 0)
		WHERE cu.branch_id = $1 AND ($4::uuid IS NULL OR cu.id = $4::uuid) AND ($5 = '' OR cu.name ILIKE '%'||$5||'%' OR cu.code ILIKE '%'||$5||'%')
		GROUP BY cu.id`
	args := []any{r.p.BranchID, r.start, r.end, r.id("customer_id"), r.q}
	res, err := r.paged(base, "points_earned DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(sum(points_redeemed),0) AS points_redeemed, COALESCE(sum(points_earned),0) AS points_earned, COALESCE(sum(transaction_count),0) AS transaction_count FROM (`+base+`) t`, args...)
	return res, err
}

func (a *API) pointProducts(kind string) func(r *rq) (*rres, error) {
	pointsCol := "points_" + kind
	return func(r *rq) (*rres, error) {
		base := `SELECT i.product_id, max(i.product_name) AS product_name, sum(i.quantity) AS quantity, count(DISTINCT s.id) AS transaction_count,
			sum(i.` + pointsCol + `) AS ` + pointsCol + `, sum(i.subtotal - i.quantity * i.unit_cost) AS profit
			` + saleItemSQL + ` WHERE ` + salesBase + ` AND i.` + pointsCol + ` > 0 AND ($4 = '' OR i.product_name ILIKE '%'||$4||'%') GROUP BY i.product_id`
		args := []any{r.p.BranchID, r.start, r.end, r.q}
		res, err := r.paged(base, pointsCol+" DESC", args...)
		if err != nil {
			return nil, err
		}
		res.summary, err = r.row(`SELECT COALESCE(sum(`+pointsCol+`),0) AS `+pointsCol+`, COALESCE(sum(quantity),0) AS quantity, COALESCE(sum(profit),0) AS profit,
			COALESCE(sum(transaction_count),0) AS transaction_count FROM (`+base+`) t`, args...)
		return res, err
	}
}

// ---------- inventory capital ----------

func (a *API) capital(r *rq) (*rres, error) {
	base := `SELECT p.id AS product_id, p.name AS product_name, p.sku, c.name AS category_name, p.stock, p.purchase_price AS unit_cost,
		round(GREATEST(p.stock,0) * p.purchase_price, 2) AS capital_value
		FROM products p LEFT JOIN categories c ON c.id = p.category_id WHERE p.branch_id=$1 AND p.deleted_at IS NULL AND p.track_stock
		AND ($2::uuid IS NULL OR p.category_id = $2::uuid) AND ($3 = '' OR p.name ILIKE '%'||$3||'%' OR p.sku ILIKE '%'||$3||'%')`
	args := []any{r.p.BranchID, r.id("category_id"), r.q}
	res, err := r.paged(base, "capital_value DESC, product_name", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(sum(capital_value),0) AS total_capital, COALESCE(sum(GREATEST(stock,0)),0) AS total_stock FROM (`+base+`) t`, args...)
	return res, err
}

// ---------- shifts ----------

const shiftCols = `sh.id, sh.opened_at, sh.closed_at, d.name AS cash_drawer_name, u.name AS cashier_name, sh.opening_cash, sh.cash_in, sh.cash_out,
	COALESCE((SELECT sum(s.grand_total) FROM sales s WHERE s.shift_id = sh.id AND s.payment_method = 'cash'), 0) AS cash_sales,
	sh.opening_cash + sh.cash_in - sh.cash_out + COALESCE((SELECT sum(s.grand_total) FROM sales s WHERE s.shift_id = sh.id AND s.payment_method = 'cash'), 0) AS expected_cash,
	sh.actual_cash,
	CASE WHEN sh.actual_cash IS NULL THEN NULL ELSE sh.actual_cash - (sh.opening_cash + sh.cash_in - sh.cash_out + COALESCE((SELECT sum(s.grand_total) FROM sales s WHERE s.shift_id = sh.id AND s.payment_method = 'cash'), 0)) END AS difference`

const shiftFrom = ` FROM shifts sh LEFT JOIN cash_drawers d ON d.id = sh.cash_drawer_id LEFT JOIN users u ON u.id = sh.cashier_id`

func (a *API) shifts(r *rq) (*rres, error) {
	base := `SELECT ` + shiftCols + shiftFrom + ` WHERE sh.branch_id=$1 AND sh.opened_at::date BETWEEN $2::date AND $3::date AND ($4::uuid IS NULL OR sh.cash_drawer_id = $4::uuid) AND ($5::uuid IS NULL OR sh.cashier_id = $5::uuid)`
	return r.paged(base, "sh.opened_at DESC", r.p.BranchID, r.start, r.end, r.id("cash_drawer_id"), r.id("cashier_id"))
}

func (a *API) shiftRow(c fiber.Ctx) (map[string]any, error) {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return nil, err
	}
	row, err := repositories.JSONRow(c.Context(), a.DB, `SELECT `+shiftCols+`,
		COALESCE((SELECT json_agg(x) FROM (SELECT payment_method, count(*) AS transaction_count, sum(grand_total) AS amount FROM sales WHERE shift_id = sh.id GROUP BY 1 ORDER BY 1) x), '[]') AS payments`+
		shiftFrom+` WHERE sh.id=$1 AND sh.branch_id=$2`, id, helper.P(c).BranchID)
	if err == nil && row == nil {
		err = helper.NotFound("Shift")
	}
	return row, err
}

func (a *API) shiftDetail(c fiber.Ctx) error {
	row, err := a.shiftRow(c)
	if err != nil {
		return err
	}
	return helper.OK(c, row)
}

func (a *API) shiftPDF(c fiber.Ctx) error {
	s, err := a.shiftRow(c)
	if err != nil {
		return err
	}
	d := export.NewPDF(226, 420)
	tr := helper.Lang(c).T
	d.Page()
	y := 396.0
	line := func(label string, v any, bold bool) {
		d.Text(10, y, 8, bold, label)
		var val string
		switch x := v.(type) {
		case float64:
			val = export.Thousands(x)
		case nil:
			val = "-"
		default:
			val = fmt.Sprint(x)
		}
		d.TextRight(216, y, 8, bold, val)
		y -= 13
	}
	d.Text(10, y, 11, true, tr("SHIFT REPORT"))
	y -= 18
	line(tr("Cash drawer"), s["cash_drawer_name"], false)
	line(tr("Cashier"), s["cashier_name"], false)
	line(tr("Opened"), formatTime(s["opened_at"], a.Loc), false)
	line(tr("Closed"), formatTime(s["closed_at"], a.Loc), false)
	d.Line(10, y+6, 216)
	y -= 4
	line(tr("Opening cash"), s["opening_cash"], false)
	line(tr("Cash sales"), s["cash_sales"], false)
	line(tr("Cash in"), s["cash_in"], false)
	line(tr("Cash out"), s["cash_out"], false)
	line(tr("Expected cash"), s["expected_cash"], true)
	line(tr("Counted cash"), s["actual_cash"], true)
	line(tr("Difference"), s["difference"], true)
	d.Line(10, y+6, 216)
	y -= 4
	if pays, ok := s["payments"].([]any); ok {
		for _, x := range pays {
			if m, ok := x.(map[string]any); ok {
				line(fmt.Sprint(m["payment_method"])+" ("+export.Thousands(num(m["transaction_count"]))+")", m["amount"], false)
			}
		}
	}
	return export.Send(c, "shift.pdf", "application/pdf", d.Bytes())
}

// ---------- fees, MDR, taxes ----------

func (a *API) feesSales(r *rq) (*rres, error) {
	base := `SELECT s.created_at, s.receipt_number, s.cashier_name, f.fee_name, f.amount FROM sale_fees f JOIN sales s ON s.id = f.sale_id
		WHERE ` + salesBase + ` AND ($4::uuid IS NULL OR s.cashier_id = $4::uuid) AND ($5 = '' OR s.payment_method = $5) AND ($6 = '' OR s.receipt_number ILIKE '%'||$6||'%')`
	args := []any{r.p.BranchID, r.start, r.end, r.id("cashier_id"), r.str("payment_method"), r.q}
	res, err := r.paged(base, "s.created_at DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(sum(amount),0) AS total_amount, count(DISTINCT receipt_number) AS transaction_count FROM (`+base+`) t`, args...)
	return res, err
}

func (a *API) feesPurchases(r *rq) (*rres, error) {
	base := `SELECT p.created_at, p.receipt_number, p.staff_name AS cashier_name, 'Purchase fees' AS fee_name, p.fee_total AS amount FROM purchases p
		WHERE p.branch_id=$1 AND p.purchase_date BETWEEN $2::date AND $3::date AND p.fee_total > 0 AND ($4::uuid IS NULL OR p.staff_id = $4::uuid) AND ($5 = '' OR p.receipt_number ILIKE '%'||$5||'%')`
	args := []any{r.p.BranchID, r.start, r.end, r.id("staff_id"), r.q}
	res, err := r.paged(base, "p.created_at DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(sum(amount),0) AS total_amount, count(*) AS transaction_count FROM (`+base+`) t`, args...)
	return res, err
}

func (a *API) mdr(r *rq) (*rres, error) {
	base := `SELECT s.created_at, s.receipt_number, s.cashier_name, s.payment_method, s.grand_total AS transaction_amount, s.mdr_rate, s.mdr_amount AS amount FROM sales s
		WHERE ` + salesBase + ` AND s.mdr_amount > 0 AND ($4::uuid IS NULL OR s.cashier_id = $4::uuid) AND ($5 = '' OR s.payment_method = $5) AND ($6 = '' OR s.receipt_number ILIKE '%'||$6||'%')`
	args := []any{r.p.BranchID, r.start, r.end, r.id("cashier_id"), r.str("payment_method"), r.q}
	res, err := r.paged(base, "s.created_at DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(sum(amount),0) AS total_amount, count(*) AS transaction_count FROM (`+base+`) t`, args...)
	return res, err
}

func (a *API) taxReport(r *rq) (*rres, error) {
	base := `SELECT s.created_at::date AS date, COALESCE(s.tax_name, 'Tax') AS tax_name, s.tax_rate AS rate, count(*) AS transaction_count,
		sum(s.subtotal - s.discount_total + s.service_charge) AS taxable_amount, sum(s.tax_total) AS tax_amount
		FROM sales s WHERE ` + salesBase + ` AND s.tax_total > 0 GROUP BY 1, 2, 3`
	args := []any{r.p.BranchID, r.start, r.end}
	res, err := r.paged(base, "date DESC, tax_name", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(sum(taxable_amount),0) AS taxable_amount, COALESCE(sum(tax_amount),0) AS tax_amount FROM (`+base+`) t`, args...)
	return res, err
}

// ---------- PPOB, visitors, promotions, ratings ----------

func (a *API) ppobReport(r *rq) (*rres, error) {
	base := `SELECT t.created_at, t.reference_id, t.product_name, t.customer_number, t.base_price, t.selling_price, t.selling_price - t.base_price AS profit, t.cashier_name, t.status
		FROM ppob_transactions t WHERE t.branch_id=$1 AND t.created_at::date BETWEEN $2::date AND $3::date AND ($4::uuid IS NULL OR t.cashier_id = $4::uuid) AND ($5 = '' OR t.status = $5)
		AND ($6 = '' OR t.reference_id ILIKE '%'||$6||'%')`
	args := []any{r.p.BranchID, r.start, r.end, r.id("cashier_id"), r.str("status"), strings.TrimPrefix(r.q, "#")}
	res, err := r.paged(base, "t.created_at DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT count(*) AS transaction_count, COALESCE(sum(selling_price),0) AS selling_total, COALESCE(sum(profit),0) AS profit FROM (`+base+`) x WHERE status = 'success'`, args...)
	return res, err
}

func (a *API) visitors(r *rq) (*rres, error) {
	expr, order := `to_char(s.created_at, 'HH24":00"')`, "1"
	switch r.str("group_by") {
	case "weekday":
		expr, order = `to_char(s.created_at, 'ID Day')`, "1"
	case "date":
		expr = `to_char(s.created_at, 'YYYY-MM-DD')`
	case "month":
		expr = `to_char(s.created_at, 'YYYY-MM')`
	}
	rows, err := r.rows(`SELECT `+expr+` AS period, count(*) AS visitor_count, sum(s.subtotal - s.discount_total) AS net_sales FROM sales s WHERE `+salesBase+` GROUP BY 1 ORDER BY `+order,
		r.p.BranchID, r.start, r.end)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if p, ok := row["period"].(string); ok && len(p) > 2 && p[1] == ' ' {
			row["period"] = strings.TrimSpace(p[2:]) // "1 Monday" → "Monday"
		}
	}
	t := sums(rows, "visitor_count", "net_sales")
	avg := 0.0
	if len(rows) > 0 {
		avg = round2(num(t["visitor_count"]) / float64(len(rows)))
	}
	return &rres{rows: rows, summary: map[string]any{"visitor_count": t["visitor_count"], "average_per_period": avg}, totals: t}, nil
}

func (a *API) promoTransactions(r *rq) (*rres, error) {
	// one row per (sale, promotion) pair, then aggregate per promotion
	base := `SELECT pr.id AS promotion_id, pr.name AS promotion_name, count(*) AS transaction_count,
		sum(x.item_discount + CASE WHEN s.promotion_id = pr.id THEN s.discount_total ELSE 0 END) AS discount_total,
		sum(s.subtotal - s.discount_total) AS net_sales, sum(s.profit) AS profit
		FROM (SELECT s.id AS sale_id, pr.id AS promo_id,
		        COALESCE((SELECT sum(i.promotion_discount) FROM sale_items i WHERE i.sale_id = s.id AND i.promotion_id = pr.id), 0) AS item_discount
		      FROM sales s JOIN promotions pr ON pr.branch_id = s.branch_id
		      WHERE ` + salesBase + ` AND (s.promotion_id = pr.id OR EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.promotion_id = pr.id))) x
		JOIN sales s ON s.id = x.sale_id JOIN promotions pr ON pr.id = x.promo_id
		WHERE ($4 = '' OR pr.name ILIKE '%'||$4||'%')
		GROUP BY pr.id`
	args := []any{r.p.BranchID, r.start, r.end, r.q}
	res, err := r.paged(base, "transaction_count DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(sum(transaction_count),0) AS transaction_count, COALESCE(sum(discount_total),0) AS discount_total, COALESCE(sum(net_sales),0) AS net_sales,
		COALESCE(sum(profit),0) AS profit FROM (`+base+`) t`, args...)
	return res, err
}

func (a *API) promoProducts(r *rq) (*rres, error) {
	base := `SELECT i.product_id, max(i.product_name) AS product_name, pr.name AS promotion_name, sum(i.quantity) AS quantity, sum(i.promotion_discount) AS discount_total,
		sum(i.subtotal) AS net_sales, sum(i.subtotal - i.quantity * i.unit_cost) AS profit, count(DISTINCT s.id) AS transaction_count
		` + saleItemSQL + ` JOIN promotions pr ON pr.id = i.promotion_id
		WHERE ` + salesBase + ` AND ($4::uuid IS NULL OR i.promotion_id = $4::uuid) AND ($5 = '' OR i.product_name ILIKE '%'||$5||'%') GROUP BY i.product_id, pr.name`
	args := []any{r.p.BranchID, r.start, r.end, r.id("promotion_id"), r.q}
	res, err := r.paged(base, "net_sales DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(sum(transaction_count),0) AS transaction_count, COALESCE(sum(profit),0) AS profit, COALESCE(sum(net_sales),0) AS net_sales FROM (`+base+`) t`, args...)
	return res, err
}

func (a *API) ratings(r *rq) (*rres, error) {
	base := `SELECT rt.id, rt.created_at, rt.customer_name, rt.rating, rt.comment, rt.tags, s.receipt_number FROM store_ratings rt LEFT JOIN sales s ON s.id = rt.sale_id
		WHERE rt.branch_id=$1 AND rt.created_at::date BETWEEN $2::date AND $3::date AND ($4 = 0 OR rt.rating = $4)`
	args := []any{r.p.BranchID, r.start, r.end, r.int("rating")}
	res, err := r.paged(base, "rt.created_at DESC", args...)
	if err != nil {
		return nil, err
	}
	res.summary, err = r.row(`SELECT COALESCE(round(avg(rating)::numeric, 2), 0) AS average_rating, count(*) AS review_count,
		json_build_object('1', count(*) FILTER (WHERE rating=1), '2', count(*) FILTER (WHERE rating=2), '3', count(*) FILTER (WHERE rating=3),
		  '4', count(*) FILTER (WHERE rating=4), '5', count(*) FILTER (WHERE rating=5)) AS rating_breakdown
		FROM store_ratings WHERE branch_id=$1 AND created_at::date BETWEEN $2::date AND $3::date`, r.p.BranchID, r.start, r.end)
	return res, err
}

// ---------- branches & transfers ----------

func (a *API) branchReport(r *rq) (*rres, error) {
	rows, err := r.rows(`SELECT b.id AS branch_id, b.name AS branch_name, count(s.id) AS transaction_count, COALESCE(sum(s.grand_total),0) AS revenue, COALESCE(sum(s.profit),0) AS profit,
		COALESCE(round(avg(s.grand_total)::numeric, 2),0) AS average_basket
		FROM branches b LEFT JOIN sales s ON s.branch_id = b.id AND s.created_at::date BETWEEN $2::date AND $3::date
		WHERE b.store_id = $1 AND b.deleted_at IS NULL GROUP BY b.id ORDER BY b.is_head_office DESC, b.name`, r.p.StoreID, r.start, r.end)
	if err != nil {
		return nil, err
	}
	t := sums(rows, "transaction_count", "revenue", "profit")
	return &rres{rows: rows, summary: t, totals: t}, nil
}

func (a *API) transferReport(confirmations bool) func(r *rq) (*rres, error) {
	return func(r *rq) (*rres, error) {
		cond := ""
		if confirmations {
			cond = " AND t.requires_confirmation AND ($8 = '' OR t.status = $8)"
		}
		base := `SELECT t.id, t.transfer_date, t.transfer_number, CASE WHEN t.from_branch_id = $2 THEN 'out' ELSE 'in' END AS direction,
			fb.name AS from_branch_name, tb.name AS to_branch_name, (SELECT count(*) FROM stock_transfer_items i WHERE i.transfer_id = t.id) AS item_count,
			(SELECT COALESCE(sum(quantity),0) FROM stock_transfer_items i WHERE i.transfer_id = t.id) AS total_quantity, t.status
			FROM stock_transfers t JOIN branches fb ON fb.id = t.from_branch_id JOIN branches tb ON tb.id = t.to_branch_id
			WHERE t.store_id = $1 AND (t.from_branch_id = $2 OR t.to_branch_id = $2) AND t.transfer_date BETWEEN $3::date AND $4::date
			AND ($5 = '' OR ($5 = 'out' AND t.from_branch_id = $2) OR ($5 = 'in' AND t.to_branch_id = $2))
			AND ($6 = '' OR ($6 = 'head_office' AND (fb.is_head_office OR tb.is_head_office)) OR ($6 = 'branch' AND NOT fb.is_head_office AND NOT tb.is_head_office))
			AND ($7 = '' OR t.transfer_number ILIKE '%'||$7||'%')` + cond
		args := []any{r.p.StoreID, r.p.BranchID, r.start, r.end, r.str("direction"), r.str("role"), r.q}
		if confirmations {
			args = append(args, r.str("status"))
		}
		return r.paged(base, "t.transfer_date DESC, t.id DESC", args...)
	}
}
