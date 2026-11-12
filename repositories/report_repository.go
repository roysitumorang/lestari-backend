package repositories

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
)

// ReportRepository reads the reports: the rows of a page (or all of them for an export), their summary and how
// many rows there are.
type ReportRepository interface {
	FindProfitLoss(ctx context.Context, f *models.ReportFilter) ([]*models.ProfitLossRow, *models.ProfitLossSummary, int64, error)
	FindCashFlow(ctx context.Context, f *models.ReportFilter) ([]*models.CashFlowRow, *models.CashFlowSummary, int64, error)
	FindDailySales(ctx context.Context, f *models.ReportFilter) ([]*models.DailySalesRow, *models.DailySalesSummary, int64, error)
	FindSalesTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.ReportSale, *models.SalesTransactionsSummary, int64, error)
	FindSalesByProduct(ctx context.Context, f *models.ReportFilter) ([]*models.SalesByProductRow, *models.SalesByProductSummary, int64, error)
	FindSalesByCategory(ctx context.Context, f *models.ReportFilter) ([]*models.SalesByCategoryRow, *models.SalesByCategorySummary, int64, error)
	FindDailyPurchases(ctx context.Context, f *models.ReportFilter) ([]*models.DailyPurchasesRow, *models.DailyPurchasesSummary, int64, error)
	FindPurchaseTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.PurchaseTransactionsRow, *models.PurchaseTransactionsSummary, int64, error)
	FindCustomerPoints(ctx context.Context, f *models.ReportFilter) ([]*models.CustomerPointsRow, *models.CustomerPointsSummary, int64, error)
	FindPointRedemptions(ctx context.Context, f *models.ReportFilter) ([]*models.PointRedemptionsRow, *models.PointRedemptionsSummary, int64, error)
	FindPointEarnings(ctx context.Context, f *models.ReportFilter) ([]*models.PointEarningsRow, *models.PointEarningsSummary, int64, error)
	FindCapital(ctx context.Context, f *models.ReportFilter) ([]*models.CapitalRow, *models.CapitalSummary, int64, error)
	FindSaleFees(ctx context.Context, f *models.ReportFilter) ([]*models.FeeRow, *models.FeeSummary, int64, error)
	FindPurchaseFees(ctx context.Context, f *models.ReportFilter) ([]*models.FeeRow, *models.FeeSummary, int64, error)
	FindMDRFees(ctx context.Context, f *models.ReportFilter) ([]*models.MDRFeeRow, *models.FeeSummary, int64, error)
	FindTaxes(ctx context.Context, f *models.ReportFilter) ([]*models.TaxReportRow, *models.TaxReportSummary, int64, error)
	FindPPOB(ctx context.Context, f *models.ReportFilter) ([]*models.PPOBReportRow, *models.PPOBReportSummary, int64, error)
	FindVisitors(ctx context.Context, f *models.ReportFilter) ([]*models.VisitorsRow, *models.VisitorsSummary, int64, error)
	FindPromotionTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.PromotionTransactionsRow, *models.PromotionTransactionsSummary, int64, error)
	FindPromotionProducts(ctx context.Context, f *models.ReportFilter) ([]*models.PromotionProductsRow, *models.PromotionProductsSummary, int64, error)
	FindStoreRatings(ctx context.Context, f *models.ReportFilter) ([]*models.StoreRatingRow, *models.StoreRatingSummary, int64, error)
	FindBranches(ctx context.Context, f *models.ReportFilter) ([]*models.BranchReportRow, *models.BranchReportSummary, int64, error)
	FindStockTransfers(ctx context.Context, f *models.ReportFilter) ([]*models.StockTransferReportRow, *models.ReportNoSummary, int64, error)
	FindStockTransferConfirmations(ctx context.Context, f *models.ReportFilter) ([]*models.StockTransferReportRow, *models.ReportNoSummary, int64, error)
	reportDetails
}

type reportRepository struct {
	base
}

func NewReportRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) ReportRepository {
	return &reportRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

func (q *reportRepository) FindProfitLoss(ctx context.Context, f *models.ReportFilter) ([]*models.ProfitLossRow, *models.ProfitLossSummary, int64, error) {
	ctxt := "ReportRepository-FindProfitLoss"
	base := `WITH d AS (SELECT GENERATE_SERIES($2::date, $3::date, INTERVAL '1 day')::date AS date),
		sa AS (
			SELECT s.created_at::date AS date
				, SUM(s.subtotal) AS gross_sales
				, SUM(s.discount_total) AS discounts
				, SUM(s.cost_total) AS cogs
				, SUM(s.mdr_amount) AS mdr
				, SUM(s.commission_total) AS commissions
			FROM sales s
			WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
				AND ($4 OR s.status <> 'unpaid')
			GROUP BY 1
		),
		fe AS (
			SELECT entry_date AS date
				, SUM(amount) FILTER (WHERE type = 'income') AS income
				, SUM(amount) FILTER (WHERE type = 'expense') AS expense
			FROM finance_entries
			WHERE branch_id = $1
				AND entry_date BETWEEN $2::date AND $3::date
			GROUP BY 1
		)
		SELECT d.date::text AS date
			, COALESCE(sa.gross_sales, 0) AS gross_sales
			, COALESCE(sa.discounts, 0) AS discounts
			, COALESCE(sa.cogs, 0) AS cost_of_goods_sold
			, COALESCE(sa.gross_sales, 0) - COALESCE(sa.discounts, 0) - COALESCE(sa.cogs, 0) AS gross_profit
			, COALESCE(fe.income, 0) AS other_income
			, COALESCE(sa.mdr, 0) AS mdr_fees
			, COALESCE(sa.commissions, 0) AS commissions
			, COALESCE(fe.expense, 0) AS other_expenses
			, COALESCE(sa.gross_sales, 0) - COALESCE(sa.discounts, 0) - COALESCE(sa.cogs, 0) + COALESCE(fe.income, 0) - COALESCE(sa.mdr, 0)
				- COALESCE(sa.commissions, 0) - COALESCE(fe.expense, 0) AS net_profit
		FROM d
		LEFT JOIN sa ON sa.date = d.date
		LEFT JOIN fe ON fe.date = d.date`
	return findReport[models.ProfitLossRow, models.ProfitLossSummary](ctx, q.dbRead, ctxt, f, false, base, "date", sprintf(`SELECT COALESCE(SUM(gross_sales), 0) AS gross_sales
		, COALESCE(SUM(discounts), 0) AS discounts
		, COALESCE(SUM(cost_of_goods_sold), 0) AS cost_of_goods_sold
		, COALESCE(SUM(gross_profit), 0) AS gross_profit
		, COALESCE(SUM(other_income), 0) AS other_income
		, COALESCE(SUM(mdr_fees), 0) AS mdr_fees
		, COALESCE(SUM(commissions), 0) AS commissions
		, COALESCE(SUM(other_expenses), 0) AS other_expenses
		, COALESCE(SUM(net_profit), 0) AS net_profit
		, (SELECT COALESCE(SUM(p.grand_total), 0) FROM purchases p WHERE p.branch_id = $1 AND p.purchase_date BETWEEN $2::date AND $3::date AND $4::boolean IS NOT NULL) AS purchase_total
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.IncludeUnpaid)
}

func (q *reportRepository) FindCashFlow(ctx context.Context, f *models.ReportFilter) ([]*models.CashFlowRow, *models.CashFlowSummary, int64, error) {
	ctxt := "ReportRepository-FindCashFlow"
	base := `SELECT x.date
			, x.type
			, x.source
			, x.category
			, x.description
			, x.amount
		FROM (
			SELECT s.created_at AS date, 'income' AS type, 'sale' AS source, s.payment_method AS category, 'Sale ' || s.receipt_number AS description, s.paid_amount AS amount
			FROM sales s
			WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date AND s.paid_amount > 0
			UNION ALL
			SELECT p.purchase_date::timestamptz, 'expense', 'purchase', COALESCE(p.payment_method, 'cash'), 'Purchase ' || p.receipt_number, p.paid_amount
			FROM purchases p
			WHERE p.branch_id = $1 AND p.purchase_date BETWEEN $2::date AND $3::date AND p.paid_amount > 0
			UNION ALL
			SELECT dp.payment_date::timestamptz, CASE d.kind WHEN 'receivable' THEN 'income' ELSE 'expense' END, d.kind || '_payment', dp.payment_method, 'Payment for ' || d.reference_number, dp.amount
			FROM debt_payments dp
			JOIN debts d ON d.id = dp.debt_id
			WHERE d.branch_id = $1 AND dp.payment_date BETWEEN $2::date AND $3::date
			UNION ALL
			SELECT f.entry_date::timestamptz, f.type, 'cash_entry', f.category, COALESCE(f.description, f.category), f.amount
			FROM finance_entries f
			WHERE f.branch_id = $1 AND f.entry_date BETWEEN $2::date AND $3::date
			UNION ALL
			SELECT r.created_at, 'expense', 'refund', COALESCE(r.refund_method, 'cash'), 'Refund ' || r.return_number, r.refund_amount
			FROM returns r
			WHERE r.branch_id = $1 AND r.kind = 'sale' AND r.created_at::date BETWEEN $2::date AND $3::date AND r.refund_amount > 0
		) x
		WHERE ($4 = '' OR x.type = $4)
			AND ($5 = '' OR x.description ILIKE '%' || $5 || '%' OR x.category ILIKE '%' || $5 || '%')`
	return findReport[models.CashFlowRow, models.CashFlowSummary](ctx, q.dbRead, ctxt, f, true, base, "date DESC", sprintf(`SELECT COALESCE(SUM(amount) FILTER (WHERE type = 'income'), 0) AS total_income
		, COALESCE(SUM(amount) FILTER (WHERE type = 'expense'), 0) AS total_expense
		, COALESCE(SUM(CASE WHEN type = 'income' THEN amount ELSE -amount END), 0) AS net_cash_flow
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.Type, f.Keyword)
}

func (q *reportRepository) FindDailySales(ctx context.Context, f *models.ReportFilter) ([]*models.DailySalesRow, *models.DailySalesSummary, int64, error) {
	ctxt := "ReportRepository-FindDailySales"
	base := `SELECT s.created_at::date::text AS date
			, COUNT(1) AS transaction_count
			, SUM(s.subtotal) AS gross_sales
			, SUM(s.discount_total) AS discounts
			, SUM(s.tax_total) AS taxes
			, SUM(s.subtotal - s.discount_total) AS net_sales
			, SUM(s.profit) AS profit
		FROM sales s
		WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
		GROUP BY 1`
	return findReport[models.DailySalesRow, models.DailySalesSummary](ctx, q.dbRead, ctxt, f, false, base, "1", sprintf(`SELECT COALESCE(SUM(transaction_count), 0)::bigint AS transaction_count
		, COALESCE(SUM(gross_sales), 0) AS gross_sales
		, COALESCE(SUM(discounts), 0) AS discounts
		, COALESCE(SUM(taxes), 0) AS taxes
		, COALESCE(SUM(net_sales), 0) AS net_sales
		, COALESCE(SUM(profit), 0) AS profit
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate)
}

func (q *reportRepository) FindSalesTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.ReportSale, *models.SalesTransactionsSummary, int64, error) {
	ctxt := "ReportRepository-FindSalesTransactions"
	base := `SELECT ` + reportSaleColumns + `
		FROM sales s
		WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			AND ($4 = '' OR s.payment_method = $4)
			AND ($5 = '' OR s.payment_type = $5)
			AND ($6::uuid IS NULL OR s.cashier_id = $6::uuid)
			AND ($7 = '' OR s.receipt_number ILIKE '%' || $7 || '%')`
	return findReport[models.ReportSale, models.SalesTransactionsSummary](ctx, q.dbRead, ctxt, f, true, base, "s.created_at DESC", sprintf(`SELECT COUNT(1) AS transaction_count
		, COALESCE(SUM(grand_total), 0) AS grand_total
		, COALESCE(SUM(profit), 0) AS profit
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.PaymentMethod, f.PaymentType, f.CashierID, f.Keyword)
}

func (q *reportRepository) FindSalesByProduct(ctx context.Context, f *models.ReportFilter) ([]*models.SalesByProductRow, *models.SalesByProductSummary, int64, error) {
	ctxt := "ReportRepository-FindSalesByProduct"
	base := `SELECT i.product_id
			, MAX(i.product_name) AS product_name
			, MAX(p.sku) AS sku
			, MAX(c.name) AS category_name
			, SUM(i.quantity) AS quantity_sold
			, SUM(i.quantity * i.unit_price) AS gross_sales
			, SUM(i.discount_amount + i.promotion_discount) AS discounts
			, SUM(i.subtotal) AS net_sales
			, SUM(i.quantity * i.unit_cost) AS cost_of_goods_sold
			, SUM(i.subtotal - i.quantity * i.unit_cost) AS profit
		FROM sale_items i JOIN sales s ON s.id = i.sale_id
		LEFT JOIN products p ON p.id = i.product_id
		LEFT JOIN categories c ON c.id = COALESCE(i.category_id, p.category_id)
		WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			AND ($4::uuid IS NULL OR COALESCE(i.category_id, p.category_id) = $4::uuid)
			AND ($5 = '' OR i.product_name ILIKE '%' || $5 || '%' OR p.sku ILIKE '%' || $5 || '%')
		GROUP BY i.product_id`
	return findReport[models.SalesByProductRow, models.SalesByProductSummary](ctx, q.dbRead, ctxt, f, true, base, "net_sales DESC NULLS LAST", sprintf(`SELECT COALESCE(SUM(quantity_sold), 0) AS quantity_sold
		, COALESCE(SUM(net_sales), 0) AS net_sales
		, COALESCE(SUM(profit), 0) AS profit
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.CategoryID, f.Keyword)
}

func (q *reportRepository) FindSalesByCategory(ctx context.Context, f *models.ReportFilter) ([]*models.SalesByCategoryRow, *models.SalesByCategorySummary, int64, error) {
	ctxt := "ReportRepository-FindSalesByCategory"
	base := `SELECT COALESCE(c.name, 'Uncategorised') AS category_name
			, COUNT(DISTINCT i.product_id) AS product_count
			, SUM(i.quantity) AS quantity_sold
			, SUM(i.quantity * i.unit_price) AS gross_sales
			, SUM(i.subtotal) AS net_sales
			, SUM(i.subtotal - i.quantity * i.unit_cost) AS profit
		FROM sale_items i JOIN sales s ON s.id = i.sale_id
		LEFT JOIN products p ON p.id = i.product_id
		LEFT JOIN categories c ON c.id = COALESCE(i.category_id, p.category_id)
		WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			AND ($4 = '' OR c.name ILIKE '%' || $4 || '%')
		GROUP BY 1`
	return findReport[models.SalesByCategoryRow, models.SalesByCategorySummary](ctx, q.dbRead, ctxt, f, false, base, "net_sales DESC", sprintf(`SELECT COALESCE(SUM(quantity_sold), 0) AS quantity_sold
		, COALESCE(SUM(gross_sales), 0) AS gross_sales
		, COALESCE(SUM(net_sales), 0) AS net_sales
		, COALESCE(SUM(profit), 0) AS profit
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.Keyword)
}

func (q *reportRepository) FindDailyPurchases(ctx context.Context, f *models.ReportFilter) ([]*models.DailyPurchasesRow, *models.DailyPurchasesSummary, int64, error) {
	ctxt := "ReportRepository-FindDailyPurchases"
	base := `SELECT p.purchase_date::text AS date
			, COUNT(1) AS purchase_count
			, COALESCE(SUM((SELECT SUM(i.quantity) FROM purchase_items i WHERE i.purchase_id = p.id)), 0) AS item_quantity
			, SUM(p.grand_total) AS grand_total
			, SUM(p.paid_amount) AS paid_amount
		FROM purchases p
		WHERE p.branch_id = $1
			AND p.purchase_date BETWEEN $2::date AND $3::date
			AND ($4 = '' OR p.receipt_number ILIKE '%' || $4 || '%')
		GROUP BY 1`
	return findReport[models.DailyPurchasesRow, models.DailyPurchasesSummary](ctx, q.dbRead, ctxt, f, false, base, "1", sprintf(`SELECT COALESCE(SUM(purchase_count), 0)::bigint AS purchase_count
		, COALESCE(SUM(item_quantity), 0) AS item_quantity
		, COALESCE(SUM(grand_total), 0) AS grand_total
		, COALESCE(SUM(paid_amount), 0) AS paid_amount
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.Keyword)
}

func (q *reportRepository) FindPurchaseTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.PurchaseTransactionsRow, *models.PurchaseTransactionsSummary, int64, error) {
	ctxt := "ReportRepository-FindPurchaseTransactions"
	base := `SELECT p.id
			, p.purchase_date::text AS purchase_date
			, p.receipt_number
			, (SELECT name FROM suppliers WHERE id = p.supplier_id) AS supplier_name
			, p.staff_name
			, p.payment_type
			, p.grand_total
			, p.paid_amount
		FROM purchases p
		WHERE p.branch_id = $1
			AND p.purchase_date BETWEEN $2::date AND $3::date
			AND ($4 = '' OR p.payment_type = $4)
			AND ($5::uuid IS NULL OR p.staff_id = $5::uuid)
			AND ($6::uuid IS NULL OR p.supplier_id = $6::uuid)
			AND ($7 = '' OR p.receipt_number ILIKE '%' || $7 || '%')`
	return findReport[models.PurchaseTransactionsRow, models.PurchaseTransactionsSummary](ctx, q.dbRead, ctxt, f, true, base, "p.purchase_date DESC, p.id DESC", sprintf(`SELECT COUNT(1) AS purchase_count
		, COALESCE(SUM(grand_total), 0) AS grand_total
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.PaymentType, f.StaffID, f.SupplierID, f.Keyword)
}

func (q *reportRepository) FindCustomerPoints(ctx context.Context, f *models.ReportFilter) ([]*models.CustomerPointsRow, *models.CustomerPointsSummary, int64, error) {
	ctxt := "ReportRepository-FindCustomerPoints"
	base := `SELECT cu.id AS customer_id
			, cu.name AS customer_name
			, cu.code AS customer_code
			, COALESCE(SUM(s.points_earned), 0) AS points_earned
			, COALESCE(SUM(s.points_redeemed), 0) AS points_redeemed
			, COUNT(s.id) AS transaction_count
			, cu.points AS points_balance
		FROM customers cu
		JOIN sales s ON s.customer_id = cu.id
			AND s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			AND (s.points_earned > 0 OR s.points_redeemed > 0)
		WHERE cu.branch_id = $1
			AND ($4::uuid IS NULL OR cu.id = $4::uuid)
			AND ($5 = '' OR cu.name ILIKE '%' || $5 || '%' OR cu.code ILIKE '%' || $5 || '%')
		GROUP BY cu.id`
	return findReport[models.CustomerPointsRow, models.CustomerPointsSummary](ctx, q.dbRead, ctxt, f, true, base, "points_earned DESC", sprintf(`SELECT COALESCE(SUM(points_redeemed), 0)::bigint AS points_redeemed
		, COALESCE(SUM(points_earned), 0)::bigint AS points_earned
		, COALESCE(SUM(transaction_count), 0)::bigint AS transaction_count
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.CustomerID, f.Keyword)
}

func (q *reportRepository) FindPointRedemptions(ctx context.Context, f *models.ReportFilter) ([]*models.PointRedemptionsRow, *models.PointRedemptionsSummary, int64, error) {
	ctxt := "ReportRepository-FindPointRedemptions"
	base := `SELECT i.product_id
			, MAX(i.product_name) AS product_name
			, SUM(i.quantity) AS quantity
			, COUNT(DISTINCT s.id) AS transaction_count
			, SUM(i.points_redeemed) AS points_redeemed
			, SUM(i.subtotal - i.quantity * i.unit_cost) AS profit
		FROM sale_items i JOIN sales s ON s.id = i.sale_id
		WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			AND i.points_redeemed > 0
			AND ($4 = '' OR i.product_name ILIKE '%' || $4 || '%')
		GROUP BY i.product_id`
	return findReport[models.PointRedemptionsRow, models.PointRedemptionsSummary](ctx, q.dbRead, ctxt, f, true, base, "points_redeemed DESC", sprintf(`SELECT COALESCE(SUM(points_redeemed), 0)::bigint AS points_redeemed
		, COALESCE(SUM(quantity), 0) AS quantity
		, COALESCE(SUM(profit), 0) AS profit
		, COALESCE(SUM(transaction_count), 0)::bigint AS transaction_count
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.Keyword)
}

func (q *reportRepository) FindPointEarnings(ctx context.Context, f *models.ReportFilter) ([]*models.PointEarningsRow, *models.PointEarningsSummary, int64, error) {
	ctxt := "ReportRepository-FindPointEarnings"
	base := `SELECT i.product_id
			, MAX(i.product_name) AS product_name
			, SUM(i.quantity) AS quantity
			, COUNT(DISTINCT s.id) AS transaction_count
			, SUM(i.points_earned) AS points_earned
			, SUM(i.subtotal - i.quantity * i.unit_cost) AS profit
		FROM sale_items i JOIN sales s ON s.id = i.sale_id
		WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			AND i.points_earned > 0
			AND ($4 = '' OR i.product_name ILIKE '%' || $4 || '%')
		GROUP BY i.product_id`
	return findReport[models.PointEarningsRow, models.PointEarningsSummary](ctx, q.dbRead, ctxt, f, true, base, "points_earned DESC", sprintf(`SELECT COALESCE(SUM(points_earned), 0)::bigint AS points_earned
		, COALESCE(SUM(quantity), 0) AS quantity
		, COALESCE(SUM(profit), 0) AS profit
		, COALESCE(SUM(transaction_count), 0)::bigint AS transaction_count
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.Keyword)
}

func (q *reportRepository) FindCapital(ctx context.Context, f *models.ReportFilter) ([]*models.CapitalRow, *models.CapitalSummary, int64, error) {
	ctxt := "ReportRepository-FindCapital"
	base := `SELECT p.id AS product_id
			, p.name AS product_name
			, p.sku
			, c.name AS category_name
			, p.stock
			, p.purchase_price AS unit_cost
			, ROUND(GREATEST(p.stock, 0) * p.purchase_price, 2) AS capital_value
		FROM products p
		LEFT JOIN categories c ON c.id = p.category_id
		WHERE p.branch_id = $1
			AND p.deleted_at IS NULL
			AND p.track_stock
			AND ($2::uuid IS NULL OR p.category_id = $2::uuid)
			AND ($3 = '' OR p.name ILIKE '%' || $3 || '%' OR p.sku ILIKE '%' || $3 || '%')`
	return findReport[models.CapitalRow, models.CapitalSummary](ctx, q.dbRead, ctxt, f, true, base, "capital_value DESC, product_name", sprintf(`SELECT COALESCE(SUM(capital_value), 0) AS total_capital
		, COALESCE(SUM(GREATEST(stock, 0)), 0) AS total_stock
	FROM (%s) t`, base), f.BranchID, f.CategoryID, f.Keyword)
}

func (q *reportRepository) FindSaleFees(ctx context.Context, f *models.ReportFilter) ([]*models.FeeRow, *models.FeeSummary, int64, error) {
	ctxt := "ReportRepository-FindSaleFees"
	base := `SELECT s.created_at
			, s.receipt_number
			, s.cashier_name
			, f.fee_name
			, f.amount
		FROM sale_fees f
		JOIN sales s ON s.id = f.sale_id
		WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			AND ($4::uuid IS NULL OR s.cashier_id = $4::uuid)
			AND ($5 = '' OR s.payment_method = $5)
			AND ($6 = '' OR s.receipt_number ILIKE '%' || $6 || '%')`
	return findReport[models.FeeRow, models.FeeSummary](ctx, q.dbRead, ctxt, f, true, base, "s.created_at DESC", sprintf(`SELECT COALESCE(SUM(amount), 0) AS total_amount
		, COUNT(DISTINCT receipt_number) AS transaction_count
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.CashierID, f.PaymentMethod, f.Keyword)
}

func (q *reportRepository) FindPurchaseFees(ctx context.Context, f *models.ReportFilter) ([]*models.FeeRow, *models.FeeSummary, int64, error) {
	ctxt := "ReportRepository-FindPurchaseFees"
	base := `SELECT p.created_at
			, p.receipt_number
			, p.staff_name AS cashier_name
			, 'Purchase fees' AS fee_name
			, p.fee_total AS amount
		FROM purchases p
		WHERE p.branch_id = $1
			AND p.purchase_date BETWEEN $2::date AND $3::date
			AND p.fee_total > 0
			AND ($4::uuid IS NULL OR p.staff_id = $4::uuid)
			AND ($5 = '' OR p.receipt_number ILIKE '%' || $5 || '%')`
	return findReport[models.FeeRow, models.FeeSummary](ctx, q.dbRead, ctxt, f, true, base, "p.created_at DESC", sprintf(`SELECT COALESCE(SUM(amount), 0) AS total_amount
		, COUNT(1) AS transaction_count
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.StaffID, f.Keyword)
}

func (q *reportRepository) FindMDRFees(ctx context.Context, f *models.ReportFilter) ([]*models.MDRFeeRow, *models.FeeSummary, int64, error) {
	ctxt := "ReportRepository-FindMDRFees"
	base := `SELECT s.created_at
			, s.receipt_number
			, s.cashier_name
			, s.payment_method
			, s.grand_total AS transaction_amount
			, s.mdr_rate
			, s.mdr_amount AS amount
		FROM sales s
		WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			AND s.mdr_amount > 0
			AND ($4::uuid IS NULL OR s.cashier_id = $4::uuid)
			AND ($5 = '' OR s.payment_method = $5)
			AND ($6 = '' OR s.receipt_number ILIKE '%' || $6 || '%')`
	return findReport[models.MDRFeeRow, models.FeeSummary](ctx, q.dbRead, ctxt, f, true, base, "s.created_at DESC", sprintf(`SELECT COALESCE(SUM(amount), 0) AS total_amount
		, COUNT(1) AS transaction_count
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.CashierID, f.PaymentMethod, f.Keyword)
}

func (q *reportRepository) FindTaxes(ctx context.Context, f *models.ReportFilter) ([]*models.TaxReportRow, *models.TaxReportSummary, int64, error) {
	ctxt := "ReportRepository-FindTaxes"
	base := `SELECT s.created_at::date::text AS date
			, COALESCE(s.tax_name, 'Tax') AS tax_name
			, s.tax_rate AS rate
			, COUNT(1) AS transaction_count
			, SUM(s.subtotal - s.discount_total + s.service_charge) AS taxable_amount
			, SUM(s.tax_total) AS tax_amount
		FROM sales s
		WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			AND s.tax_total > 0
		GROUP BY 1, 2, 3`
	return findReport[models.TaxReportRow, models.TaxReportSummary](ctx, q.dbRead, ctxt, f, true, base, "date DESC, tax_name", sprintf(`SELECT COALESCE(SUM(taxable_amount), 0) AS taxable_amount
		, COALESCE(SUM(tax_amount), 0) AS tax_amount
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate)
}

func (q *reportRepository) FindPPOB(ctx context.Context, f *models.ReportFilter) ([]*models.PPOBReportRow, *models.PPOBReportSummary, int64, error) {
	ctxt := "ReportRepository-FindPPOB"
	base := `SELECT t.created_at
			, t.reference_id
			, t.product_name
			, t.customer_number
			, t.base_price
			, t.selling_price
			, t.selling_price - t.base_price AS profit
			, t.cashier_name
			, t.status
		FROM ppob_transactions t
		WHERE t.branch_id = $1
			AND t.created_at::date BETWEEN $2::date AND $3::date
			AND ($4::uuid IS NULL OR t.cashier_id = $4::uuid)
			AND ($5 = '' OR t.status = $5)
			AND ($6 = '' OR t.reference_id ILIKE '%' || $6 || '%')`
	return findReport[models.PPOBReportRow, models.PPOBReportSummary](ctx, q.dbRead, ctxt, f, true, base, "t.created_at DESC", sprintf(`SELECT COUNT(1) AS transaction_count
		, COALESCE(SUM(selling_price), 0) AS selling_total
		, COALESCE(SUM(profit), 0) AS profit
	FROM (%s) x
	WHERE status = 'success'`, base), f.BranchID, f.StartDate, f.EndDate, f.CashierID, f.Status, strings.TrimPrefix(f.Keyword, "#"))
}

func (q *reportRepository) FindVisitors(ctx context.Context, f *models.ReportFilter) ([]*models.VisitorsRow, *models.VisitorsSummary, int64, error) {
	ctxt := "ReportRepository-FindVisitors"
	// the weekday is sorted by its ISO number, which is then cut off ("1 Monday" → "Monday")
	period, label := `TO_CHAR(s.created_at, 'HH24":00"')`, "x.period"
	switch f.GroupBy {
	case "weekday":
		period, label = "TO_CHAR(s.created_at, 'ID Day')", "TRIM(SUBSTR(x.period, 3))"
	case "date":
		period = "TO_CHAR(s.created_at, 'YYYY-MM-DD')"
	case "month":
		period = "TO_CHAR(s.created_at, 'YYYY-MM')"
	}
	base := `SELECT ` + label + ` AS period
			, x.visitor_count
			, x.net_sales
		FROM (
			SELECT ` + period + ` AS period
				, COUNT(1) AS visitor_count
				, SUM(s.subtotal - s.discount_total) AS net_sales
			FROM sales s
			WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			GROUP BY 1
		) x`
	return findReport[models.VisitorsRow, models.VisitorsSummary](ctx, q.dbRead, ctxt, f, false, base, "x.period", sprintf(`SELECT COALESCE(SUM(visitor_count), 0)::bigint AS visitor_count
		, COALESCE(ROUND(AVG(visitor_count), 2), 0) AS average_per_period
		, COALESCE(SUM(net_sales), 0) AS net_sales
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate)
}

func (q *reportRepository) FindPromotionTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.PromotionTransactionsRow, *models.PromotionTransactionsSummary, int64, error) {
	ctxt := "ReportRepository-FindPromotionTransactions"
	base := `SELECT pr.id AS promotion_id
			, pr.name AS promotion_name
			, COUNT(1) AS transaction_count
			, SUM(x.item_discount + CASE WHEN s.promotion_id = pr.id THEN s.discount_total ELSE 0 END) AS discount_total
			, SUM(s.subtotal - s.discount_total) AS net_sales
			, SUM(s.profit) AS profit
		FROM (
			SELECT s.id AS sale_id
				, pr.id AS promo_id
				, COALESCE((SELECT SUM(i.promotion_discount) FROM sale_items i WHERE i.sale_id = s.id AND i.promotion_id = pr.id), 0) AS item_discount
			FROM sales s
			JOIN promotions pr ON pr.branch_id = s.branch_id
			WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
				AND (s.promotion_id = pr.id OR EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.promotion_id = pr.id))
		) x
		JOIN sales s ON s.id = x.sale_id
		JOIN promotions pr ON pr.id = x.promo_id
		WHERE ($4 = '' OR pr.name ILIKE '%' || $4 || '%')
		GROUP BY pr.id`
	return findReport[models.PromotionTransactionsRow, models.PromotionTransactionsSummary](ctx, q.dbRead, ctxt, f, true, base, "transaction_count DESC", sprintf(`SELECT COALESCE(SUM(transaction_count), 0)::bigint AS transaction_count
		, COALESCE(SUM(discount_total), 0) AS discount_total
		, COALESCE(SUM(net_sales), 0) AS net_sales
		, COALESCE(SUM(profit), 0) AS profit
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.Keyword)
}

func (q *reportRepository) FindPromotionProducts(ctx context.Context, f *models.ReportFilter) ([]*models.PromotionProductsRow, *models.PromotionProductsSummary, int64, error) {
	ctxt := "ReportRepository-FindPromotionProducts"
	base := `SELECT i.product_id
			, MAX(i.product_name) AS product_name
			, pr.name AS promotion_name
			, SUM(i.quantity) AS quantity
			, SUM(i.promotion_discount) AS discount_total
			, SUM(i.subtotal) AS net_sales
			, SUM(i.subtotal - i.quantity * i.unit_cost) AS profit
			, COUNT(DISTINCT s.id) AS transaction_count
		FROM sale_items i JOIN sales s ON s.id = i.sale_id
		JOIN promotions pr ON pr.id = i.promotion_id
		WHERE s.branch_id = $1 AND s.created_at::date BETWEEN $2::date AND $3::date
			AND ($4::uuid IS NULL OR i.promotion_id = $4::uuid)
			AND ($5 = '' OR i.product_name ILIKE '%' || $5 || '%')
		GROUP BY i.product_id, pr.name`
	return findReport[models.PromotionProductsRow, models.PromotionProductsSummary](ctx, q.dbRead, ctxt, f, true, base, "net_sales DESC", sprintf(`SELECT COALESCE(SUM(transaction_count), 0)::bigint AS transaction_count
		, COALESCE(SUM(profit), 0) AS profit
		, COALESCE(SUM(net_sales), 0) AS net_sales
	FROM (%s) t`, base), f.BranchID, f.StartDate, f.EndDate, f.PromotionID, f.Keyword)
}

func (q *reportRepository) FindStoreRatings(ctx context.Context, f *models.ReportFilter) ([]*models.StoreRatingRow, *models.StoreRatingSummary, int64, error) {
	ctxt := "ReportRepository-FindStoreRatings"
	base := `SELECT rt.id
			, rt.created_at
			, rt.customer_name
			, rt.rating
			, rt.comment
			, rt.tags
			, s.receipt_number
		FROM store_ratings rt
		LEFT JOIN sales s ON s.id = rt.sale_id
		WHERE rt.branch_id = $1
			AND rt.created_at::date BETWEEN $2::date AND $3::date
			AND ($4 = 0 OR rt.rating = $4)`
	return findReport[models.StoreRatingRow, models.StoreRatingSummary](ctx, q.dbRead, ctxt, f, true, base, "rt.created_at DESC", `SELECT COALESCE(ROUND(AVG(rating), 2), 0) AS average_rating
		, COUNT(1) AS review_count
		, JSON_BUILD_OBJECT(
			'1', COUNT(1) FILTER (WHERE rating = 1)
			, '2', COUNT(1) FILTER (WHERE rating = 2)
			, '3', COUNT(1) FILTER (WHERE rating = 3)
			, '4', COUNT(1) FILTER (WHERE rating = 4)
			, '5', COUNT(1) FILTER (WHERE rating = 5)
		) AS rating_breakdown
	FROM store_ratings
	WHERE branch_id = $1
		AND created_at::date BETWEEN $2::date AND $3::date
		AND $4::int >= 0`, f.BranchID, f.StartDate, f.EndDate, f.Rating)
}

func (q *reportRepository) FindBranches(ctx context.Context, f *models.ReportFilter) ([]*models.BranchReportRow, *models.BranchReportSummary, int64, error) {
	ctxt := "ReportRepository-FindBranches"
	base := `SELECT b.id AS branch_id
			, b.name AS branch_name
			, COUNT(s.id) AS transaction_count
			, COALESCE(SUM(s.grand_total), 0) AS revenue
			, COALESCE(SUM(s.profit), 0) AS profit
			, COALESCE(ROUND(AVG(s.grand_total), 2), 0) AS average_basket
		FROM branches b
		LEFT JOIN sales s ON s.branch_id = b.id
			AND s.created_at::date BETWEEN $2::date AND $3::date
		WHERE b.store_id = $1
			AND b.deleted_at IS NULL
		GROUP BY b.id`
	return findReport[models.BranchReportRow, models.BranchReportSummary](ctx, q.dbRead, ctxt, f, false, base, "b.is_head_office DESC, b.name", sprintf(`SELECT COALESCE(SUM(transaction_count), 0)::bigint AS transaction_count
		, COALESCE(SUM(revenue), 0) AS revenue
		, COALESCE(SUM(profit), 0) AS profit
	FROM (%s) t`, base), f.StoreID, f.StartDate, f.EndDate)
}

func (q *reportRepository) FindStockTransfers(ctx context.Context, f *models.ReportFilter) ([]*models.StockTransferReportRow, *models.ReportNoSummary, int64, error) {
	ctxt := "ReportRepository-FindStockTransfers"
	base := `SELECT t.id
			, t.transfer_date::text AS transfer_date
			, t.transfer_number
			, CASE WHEN t.from_branch_id = $2 THEN 'out' ELSE 'in' END AS direction
			, fb.name AS from_branch_name
			, tb.name AS to_branch_name
			, (SELECT COUNT(1) FROM stock_transfer_items i WHERE i.transfer_id = t.id) AS item_count
			, (SELECT COALESCE(SUM(i.quantity), 0) FROM stock_transfer_items i WHERE i.transfer_id = t.id) AS total_quantity
			, t.status
		FROM stock_transfers t
		JOIN branches fb ON fb.id = t.from_branch_id
		JOIN branches tb ON tb.id = t.to_branch_id
		WHERE t.store_id = $1
			AND (t.from_branch_id = $2 OR t.to_branch_id = $2)
			AND t.transfer_date BETWEEN $3::date AND $4::date
			AND ($5 = '' OR ($5 = 'out' AND t.from_branch_id = $2) OR ($5 = 'in' AND t.to_branch_id = $2))
			AND ($6 = '' OR ($6 = 'head_office' AND (fb.is_head_office OR tb.is_head_office)) OR ($6 = 'branch' AND NOT fb.is_head_office AND NOT tb.is_head_office))
			AND ($7 = '' OR t.transfer_number ILIKE '%' || $7 || '%')`
	return findReport[models.StockTransferReportRow, models.ReportNoSummary](ctx, q.dbRead, ctxt, f, true, base, "t.transfer_date DESC, t.id DESC", "", f.StoreID, f.BranchID, f.StartDate, f.EndDate, f.Direction, f.Role, f.Keyword)
}

func (q *reportRepository) FindStockTransferConfirmations(ctx context.Context, f *models.ReportFilter) ([]*models.StockTransferReportRow, *models.ReportNoSummary, int64, error) {
	ctxt := "ReportRepository-FindStockTransferConfirmations"
	base := `SELECT t.id
			, t.transfer_date::text AS transfer_date
			, t.transfer_number
			, CASE WHEN t.from_branch_id = $2 THEN 'out' ELSE 'in' END AS direction
			, fb.name AS from_branch_name
			, tb.name AS to_branch_name
			, (SELECT COUNT(1) FROM stock_transfer_items i WHERE i.transfer_id = t.id) AS item_count
			, (SELECT COALESCE(SUM(i.quantity), 0) FROM stock_transfer_items i WHERE i.transfer_id = t.id) AS total_quantity
			, t.status
		FROM stock_transfers t
		JOIN branches fb ON fb.id = t.from_branch_id
		JOIN branches tb ON tb.id = t.to_branch_id
		WHERE t.store_id = $1
			AND (t.from_branch_id = $2 OR t.to_branch_id = $2)
			AND t.transfer_date BETWEEN $3::date AND $4::date
			AND ($5 = '' OR ($5 = 'out' AND t.from_branch_id = $2) OR ($5 = 'in' AND t.to_branch_id = $2))
			AND ($6 = '' OR ($6 = 'head_office' AND (fb.is_head_office OR tb.is_head_office)) OR ($6 = 'branch' AND NOT fb.is_head_office AND NOT tb.is_head_office))
			AND ($7 = '' OR t.transfer_number ILIKE '%' || $7 || '%')
			AND t.requires_confirmation
			AND ($8 = '' OR t.status = $8)`
	return findReport[models.StockTransferReportRow, models.ReportNoSummary](ctx, q.dbRead, ctxt, f, true, base, "t.transfer_date DESC, t.id DESC", "", f.StoreID, f.BranchID, f.StartDate, f.EndDate, f.Direction, f.Role, f.Keyword, f.Status)
}
