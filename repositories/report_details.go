package repositories

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

// reportExportLimit caps the rows of a report export.
const reportExportLimit = 100000

// findReport runs a report: base is its full SELECT. A paged report is counted and cut to a page (or to the
// export limit when every row is asked for); summarySQL, when there is one, is run with the same arguments.
func findReport[R, S any](
	ctx context.Context,
	db Querier,
	ctxt string,
	f *models.ReportFilter,
	paged bool,
	base,
	order,
	summarySQL string,
	args ...any,
) (response []*R, summary *S, total int64, err error) {
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(base)
	_, _ = sb.WriteString(" ORDER BY ")
	_, _ = sb.WriteString(order)
	switch {
	case paged && f.All:
		_, _ = sb.WriteString(sprintf(" LIMIT %d", reportExportLimit))
	case paged:
		if total, err = queryValue[int64](ctx, db, ctxt, "SELECT COUNT(1) FROM ("+base+") _c", args...); err != nil {
			return
		}
		_, _ = sb.WriteString(paginate(f.Limit, f.Page))
	}
	if response, err = queryRows[R](ctx, db, ctxt, sb.String(), args...); err != nil {
		return
	}
	if !paged || f.All {
		total = int64(len(response))
	}
	if summarySQL != "" {
		summary, err = queryRow[S](ctx, db, ctxt, summarySQL, args...)
	}
	return
}

const reportSaleColumns = `s.id
			, s.created_at
			, s.receipt_number
			, s.cashier_name
			, s.payment_method
			, s.payment_type
			, s.profit
			, s.grand_total
			, s.paid_amount
			, s.service_charge
			, s.rounding
			, s.label
			, s.customer_type
			, s.status
			, (SELECT name FROM customers WHERE id = s.customer_id) AS customer_name
			, (SELECT email FROM users WHERE id = s.cashier_id) AS staff_email`

// mapReportSaleConditions are the "which receipts" filters of the report details; $4 is the id of the row.
var mapReportSaleConditions = map[string]string{
	models.ReportSalesOfProduct:        "EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.product_id = $4)",
	models.ReportSalesOfCustomerPoints: "s.customer_id = $4 AND (s.points_earned > 0 OR s.points_redeemed > 0)",
	models.ReportSalesOfRedemption:     "EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.product_id = $4 AND i.points_redeemed > 0)",
	models.ReportSalesOfEarning:        "EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.product_id = $4 AND i.points_earned > 0)",
	models.ReportSalesOfPromotion:      "(s.promotion_id = $4 OR EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.promotion_id = $4))",
	models.ReportSalesOfPromotionItem:  "EXISTS (SELECT 1 FROM sale_items i WHERE i.sale_id = s.id AND i.product_id = $4 AND i.promotion_id IS NOT NULL)",
}

// reportDetails are the detail lists of the reports and the queries of the dashboard.
type reportDetails interface {
	FindSalesOfDay(ctx context.Context, branchID uuid.UUID, day string) ([]*models.ReportSale, error)
	// FindSalesOf lists up to 500 sales of the period behind a report row of a kind (models.ReportSalesOf…).
	FindSalesOf(ctx context.Context, f *models.ReportFilter, kind string, id uuid.UUID) ([]*models.ReportSale, error)
	FindStoreRating(ctx context.Context, branchID, ratingID uuid.UUID) (*models.StoreRatingRow, error)
	FindDashboardTotals(ctx context.Context, f *models.DashboardFilter, day string) (*models.DashboardTotals, error)
	FindDashboardHours(ctx context.Context, f *models.DashboardFilter) ([]*models.DashboardHour, error)
	FindTopProducts(ctx context.Context, f *models.DashboardFilter) ([]*models.DashboardProduct, error)
	FindLowStockProducts(ctx context.Context, branchID uuid.UUID) ([]*models.LowStockProduct, error)
	FindStoreUsage(ctx context.Context, storeID uuid.UUID) (*models.StoreUsage, error)
}

func (q *reportRepository) FindSalesOfDay(ctx context.Context, branchID uuid.UUID, day string) ([]*models.ReportSale, error) {
	ctxt := "ReportRepository-FindSalesOfDay"
	return queryRows[models.ReportSale](ctx, q.dbRead, ctxt, `SELECT `+reportSaleColumns+`
		FROM sales s
		WHERE s.branch_id = $1
			AND s.created_at::date = $2::date
		ORDER BY s.created_at`, branchID, day)
}

func (q *reportRepository) FindSalesOf(ctx context.Context, f *models.ReportFilter, kind string, id uuid.UUID) ([]*models.ReportSale, error) {
	ctxt := "ReportRepository-FindSalesOf"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString("SELECT ")
	_, _ = sb.WriteString(reportSaleColumns)
	_, _ = sb.WriteString(`
			, s.points_earned
			, s.points_redeemed
		FROM sales s
		WHERE s.branch_id = $1
			AND s.created_at::date BETWEEN $2::date AND $3::date
			AND `)
	_, _ = sb.WriteString(mapReportSaleConditions[kind])
	_, _ = sb.WriteString(`
		ORDER BY s.created_at DESC
		LIMIT 500`)
	return queryRows[models.ReportSale](ctx, q.dbRead, ctxt, sb.String(), f.BranchID, f.StartDate, f.EndDate, id)
}

func (q *reportRepository) FindStoreRating(ctx context.Context, branchID, ratingID uuid.UUID) (*models.StoreRatingRow, error) {
	ctxt := "ReportRepository-FindStoreRating"
	response, err := queryRow[models.StoreRatingRow](ctx, q.dbRead, ctxt, `SELECT t.id
		, t.created_at
		, t.customer_name
		, t.rating
		, t.comment
		, t.tags
		, s.receipt_number
	FROM store_ratings t
	LEFT JOIN sales s ON s.id = t.sale_id
	WHERE t.id = $1
		AND t.branch_id = $2`, ratingID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrStoreRatingNotFound
	}
	return response, err
}

func (q *reportRepository) FindDashboardTotals(ctx context.Context, f *models.DashboardFilter, day string) (*models.DashboardTotals, error) {
	ctxt := "ReportRepository-FindDashboardTotals"
	return queryRow[models.DashboardTotals](ctx, q.dbRead, ctxt, `SELECT COUNT(1) AS transaction_count
		, COALESCE(SUM(CASE WHEN $3 OR t.status <> 'unpaid' THEN t.grand_total ELSE t.paid_amount END), 0) AS revenue
		, COALESCE(SUM(CASE WHEN $4 OR t.status <> 'unpaid' THEN t.profit ELSE 0 END), 0) AS profit
		, COALESCE(ROUND(AVG(t.grand_total), 2), 0) AS average_basket
	FROM sales t
	WHERE t.branch_id = $1
		AND t.created_at::date = $2::date`, f.BranchID, day, f.IncludeUnpaidRevenue, f.IncludeUnpaidProfit)
}

func (q *reportRepository) FindDashboardHours(ctx context.Context, f *models.DashboardFilter) ([]*models.DashboardHour, error) {
	ctxt := "ReportRepository-FindDashboardHours"
	return queryRows[models.DashboardHour](ctx, q.dbRead, ctxt, `SELECT TO_CHAR(h, 'HH24":00"') AS hour
		, COUNT(s.id) AS transaction_count
		, COALESCE(SUM(CASE WHEN $3 OR s.status <> 'unpaid' THEN s.grand_total ELSE s.paid_amount END), 0) AS revenue
		, COALESCE(SUM(CASE WHEN $4 OR s.status <> 'unpaid' THEN s.profit ELSE 0 END), 0) AS profit
	FROM GENERATE_SERIES($2::date::timestamp, $2::date::timestamp + INTERVAL '23 hours', INTERVAL '1 hour') h
	LEFT JOIN sales s ON s.branch_id = $1
		AND DATE_TRUNC('hour', s.created_at::timestamp) = h
	GROUP BY h
	ORDER BY h`, f.BranchID, f.Date, f.IncludeUnpaidRevenue, f.IncludeUnpaidProfit)
}

func (q *reportRepository) FindTopProducts(ctx context.Context, f *models.DashboardFilter) ([]*models.DashboardProduct, error) {
	ctxt := "ReportRepository-FindTopProducts"
	return queryRows[models.DashboardProduct](ctx, q.dbRead, ctxt, `SELECT i.product_id
		, MAX(i.product_name) AS name
		, SUM(i.quantity) AS quantity
		, SUM(i.subtotal) AS revenue
	FROM sale_items i
	JOIN sales s ON s.id = i.sale_id
	WHERE s.branch_id = $1
		AND s.created_at::date = $2::date
	GROUP BY i.product_id
	ORDER BY revenue DESC
	LIMIT 5`, f.BranchID, f.Date)
}

func (q *reportRepository) FindLowStockProducts(ctx context.Context, branchID uuid.UUID) ([]*models.LowStockProduct, error) {
	ctxt := "ReportRepository-FindLowStockProducts"
	return queryRows[models.LowStockProduct](ctx, q.dbRead, ctxt, `SELECT t.id AS product_id
		, t.name
		, t.stock
		, t.minimum_stock
	FROM products t
	WHERE t.branch_id = $1
		AND t.deleted_at IS NULL
		AND t.track_stock
		AND t.minimum_stock IS NOT NULL
		AND t.stock <= t.minimum_stock
	ORDER BY t.stock - t.minimum_stock
	LIMIT 10`, branchID)
}

func (q *reportRepository) FindStoreUsage(ctx context.Context, storeID uuid.UUID) (*models.StoreUsage, error) {
	ctxt := "ReportRepository-FindStoreUsage"
	return queryRow[models.StoreUsage](ctx, q.dbRead, ctxt, `SELECT t.plan_expires_at
		, COALESCE((SELECT SUM(u.size) FROM uploads u WHERE u.store_id = t.id), 0) ::float8 / 1048576.0 AS used_mb
		, t.storage_quota_mb::float8 AS storage_quota_mb
	FROM stores t
	WHERE t.id = $1`, storeID)
}
