package controllers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/middlewares"
)

// mountReports registers every report and its export under /reports.
func (q *reportController) mountReports(private fiber.Router) {
	private.Get("/profit-loss", q.V1ProfitLoss)
	private.Get("/profit-loss/export", q.V1ProfitLossExport)
	private.Get("/cash-flow", q.V1CashFlow)
	private.Get("/cash-flow/export", q.V1CashFlowExport)
	private.Get("/daily-sales", q.V1DailySales)
	private.Get("/daily-sales/export", q.V1DailySalesExport)
	private.Get("/sales-transactions", q.V1SalesTransactions)
	private.Get("/sales-transactions/export", q.V1SalesTransactionsExport)
	private.Get("/sales-by-product", q.V1SalesByProduct)
	private.Get("/sales-by-product/export", q.V1SalesByProductExport)
	private.Get("/sales-by-category", q.V1SalesByCategory)
	private.Get("/sales-by-category/export", q.V1SalesByCategoryExport)
	private.Get("/daily-purchases", q.V1DailyPurchases)
	private.Get("/daily-purchases/export", q.V1DailyPurchasesExport)
	private.Get("/purchase-transactions", q.V1PurchaseTransactions)
	private.Get("/purchase-transactions/export", q.V1PurchaseTransactionsExport)
	private.Get("/customer-points", q.V1CustomerPoints)
	private.Get("/customer-points/export", q.V1CustomerPointsExport)
	private.Get("/point-redemptions", q.V1PointRedemptions)
	private.Get("/point-redemptions/export", q.V1PointRedemptionsExport)
	private.Get("/point-earnings", q.V1PointEarnings)
	private.Get("/point-earnings/export", q.V1PointEarningsExport)
	private.Get("/capital", q.V1Capital)
	private.Get("/capital/export", q.V1CapitalExport)
	private.Get("/fees/sales", q.V1SaleFees)
	private.Get("/fees/sales/export", q.V1SaleFeesExport)
	private.Get("/fees/purchases", q.V1PurchaseFees)
	private.Get("/fees/purchases/export", q.V1PurchaseFeesExport)
	private.Get("/fees/mdr", q.V1MDRFees)
	private.Get("/fees/mdr/export", q.V1MDRFeesExport)
	private.Get("/taxes", q.V1Taxes)
	private.Get("/taxes/export", q.V1TaxesExport)
	private.Get("/ppob", q.V1PPOB)
	private.Get("/ppob/export", q.V1PPOBExport)
	private.Get("/visitors", q.V1Visitors)
	private.Get("/visitors/export", q.V1VisitorsExport)
	private.Get("/promotions/transactions", q.V1PromotionTransactions)
	private.Get("/promotions/transactions/export", q.V1PromotionTransactionsExport)
	private.Get("/promotions/products", q.V1PromotionProducts)
	private.Get("/promotions/products/export", q.V1PromotionProductsExport)
	private.Get("/store-ratings", q.V1StoreRatings)
	private.Get("/store-ratings/export", q.V1StoreRatingsExport)
	private.Get("/branches", q.V1Branches)
	private.Get("/branches/export", q.V1BranchesExport)
	private.Get("/stock-transfers", q.V1StockTransfers)
	private.Get("/stock-transfers/export", q.V1StockTransfersExport)
	private.Get("/stock-transfers/confirmations", middlewares.RequirePlugin("business"), q.V1StockTransferConfirmations)
	private.Get("/stock-transfers/confirmations/export", middlewares.RequirePlugin("business"), q.V1StockTransferConfirmationsExport)
}

// The columns of the report exports.
var (
	profitLossColumns = []export.Column{
		{Key: "date", Label: "Date", Format: "date"},
		{Key: "gross_sales", Label: "Sales", Format: "money"},
		{Key: "discounts", Label: "Discounts", Format: "money"},
		{Key: "cost_of_goods_sold", Label: "COGS", Format: "money"},
		{Key: "gross_profit", Label: "Gross profit", Format: "money"},
		{Key: "other_income", Label: "Other income", Format: "money"},
		{Key: "mdr_fees", Label: "MDR fees", Format: "money"},
		{Key: "commissions", Label: "Commissions", Format: "money"},
		{Key: "other_expenses", Label: "Expenses", Format: "money"},
		{Key: "net_profit", Label: "Net profit", Format: "money"},
	}
	cashFlowColumns = []export.Column{
		{Key: "date", Label: "Date", Format: "datetime"},
		{Key: "type", Label: "Type"},
		{Key: "source", Label: "Source"},
		{Key: "category", Label: "Category"},
		{Key: "description", Label: "Description"},
		{Key: "amount", Label: "Amount", Format: "money"},
	}
	dailySalesColumns = []export.Column{
		{Key: "date", Label: "Date", Format: "date"},
		{Key: "transaction_count", Label: "Transactions", Format: "number"},
		{Key: "gross_sales", Label: "Gross", Format: "money"},
		{Key: "discounts", Label: "Discounts", Format: "money"},
		{Key: "taxes", Label: "Tax", Format: "money"},
		{Key: "net_sales", Label: "Net", Format: "money"},
		{Key: "profit", Label: "Profit", Format: "money"},
	}
	salesTransactionsColumns = []export.Column{
		{Key: "created_at", Label: "Date", Format: "datetime"},
		{Key: "receipt_number", Label: "Receipt"},
		{Key: "cashier_name", Label: "Cashier"},
		{Key: "payment_method", Label: "Method"},
		{Key: "payment_type", Label: "Type"},
		{Key: "profit", Label: "Profit", Format: "money"},
		{Key: "grand_total", Label: "Total", Format: "money"},
		{Key: "paid_amount", Label: "Paid", Format: "money"},
		{Key: "service_charge", Label: "Service", Format: "money"},
		{Key: "rounding", Label: "Rounding", Format: "money"},
		{Key: "label", Label: "Label"},
		{Key: "customer_type", Label: "Customer type"},
	}
	salesByProductColumns = []export.Column{
		{Key: "product_name", Label: "Product"},
		{Key: "sku", Label: "SKU"},
		{Key: "category_name", Label: "Category"},
		{Key: "quantity_sold", Label: "Qty", Format: "number"},
		{Key: "gross_sales", Label: "Gross", Format: "money"},
		{Key: "discounts", Label: "Discounts", Format: "money"},
		{Key: "net_sales", Label: "Net", Format: "money"},
		{Key: "cost_of_goods_sold", Label: "COGS", Format: "money"},
		{Key: "profit", Label: "Profit", Format: "money"},
	}
	salesByCategoryColumns = []export.Column{
		{Key: "category_name", Label: "Category"},
		{Key: "product_count", Label: "Products", Format: "number"},
		{Key: "quantity_sold", Label: "Qty", Format: "number"},
		{Key: "gross_sales", Label: "Gross", Format: "money"},
		{Key: "net_sales", Label: "Net", Format: "money"},
		{Key: "profit", Label: "Profit", Format: "money"},
	}
	dailyPurchasesColumns = []export.Column{
		{Key: "date", Label: "Date", Format: "date"},
		{Key: "purchase_count", Label: "Purchases", Format: "number"},
		{Key: "item_quantity", Label: "Items", Format: "number"},
		{Key: "grand_total", Label: "Total", Format: "money"},
		{Key: "paid_amount", Label: "Paid", Format: "money"},
	}
	purchaseTransactionsColumns = []export.Column{
		{Key: "purchase_date", Label: "Date", Format: "date"},
		{Key: "receipt_number", Label: "Number"},
		{Key: "supplier_name", Label: "Supplier"},
		{Key: "staff_name", Label: "Recorded by"},
		{Key: "payment_type", Label: "Payment"},
		{Key: "grand_total", Label: "Total", Format: "money"},
		{Key: "paid_amount", Label: "Paid", Format: "money"},
	}
	customerPointsColumns = []export.Column{
		{Key: "customer_name", Label: "Customer"},
		{Key: "customer_code", Label: "Code"},
		{Key: "points_earned", Label: "Earned", Format: "number"},
		{Key: "points_redeemed", Label: "Redeemed", Format: "number"},
		{Key: "transaction_count", Label: "Transactions", Format: "number"},
		{Key: "points_balance", Label: "Balance", Format: "number"},
	}
	pointRedemptionsColumns = []export.Column{
		{Key: "product_name", Label: "Product"},
		{Key: "quantity", Label: "Qty", Format: "number"},
		{Key: "transaction_count", Label: "Transactions", Format: "number"},
		{Key: "points_redeemed", Label: "Points", Format: "number"},
		{Key: "profit", Label: "Profit", Format: "money"},
	}
	pointEarningsColumns = []export.Column{
		{Key: "product_name", Label: "Product"},
		{Key: "quantity", Label: "Qty", Format: "number"},
		{Key: "transaction_count", Label: "Transactions", Format: "number"},
		{Key: "points_earned", Label: "Points", Format: "number"},
		{Key: "profit", Label: "Profit", Format: "money"},
	}
	capitalColumns = []export.Column{
		{Key: "product_name", Label: "Product"},
		{Key: "sku", Label: "SKU"},
		{Key: "category_name", Label: "Category"},
		{Key: "stock", Label: "Stock", Format: "number"},
		{Key: "unit_cost", Label: "Unit cost", Format: "money"},
		{Key: "capital_value", Label: "Capital", Format: "money"},
	}
	saleFeesColumns = []export.Column{
		{Key: "created_at", Label: "Date", Format: "datetime"},
		{Key: "receipt_number", Label: "Receipt"},
		{Key: "cashier_name", Label: "Cashier"},
		{Key: "fee_name", Label: "Fee"},
		{Key: "amount", Label: "Amount", Format: "money"},
	}
	purchaseFeesColumns = []export.Column{
		{Key: "created_at", Label: "Date", Format: "datetime"},
		{Key: "receipt_number", Label: "Receipt"},
		{Key: "cashier_name", Label: "Cashier"},
		{Key: "fee_name", Label: "Fee"},
		{Key: "amount", Label: "Amount", Format: "money"},
	}
	mDRFeesColumns = []export.Column{
		{Key: "created_at", Label: "Date", Format: "datetime"},
		{Key: "receipt_number", Label: "Receipt"},
		{Key: "cashier_name", Label: "Cashier"},
		{Key: "payment_method", Label: "Method"},
		{Key: "transaction_amount", Label: "Sale", Format: "money"},
		{Key: "mdr_rate", Label: "Rate %", Format: "number"},
		{Key: "amount", Label: "MDR", Format: "money"},
	}
	taxesColumns = []export.Column{
		{Key: "date", Label: "Date", Format: "date"},
		{Key: "tax_name", Label: "Tax"},
		{Key: "rate", Label: "Rate %", Format: "number"},
		{Key: "transaction_count", Label: "Transactions", Format: "number"},
		{Key: "taxable_amount", Label: "Taxable", Format: "money"},
		{Key: "tax_amount", Label: "Tax", Format: "money"},
	}
	pPOBColumns = []export.Column{
		{Key: "created_at", Label: "Date", Format: "datetime"},
		{Key: "reference_id", Label: "Reference"},
		{Key: "product_name", Label: "Product"},
		{Key: "customer_number", Label: "Customer no."},
		{Key: "base_price", Label: "Cost", Format: "money"},
		{Key: "selling_price", Label: "Price", Format: "money"},
		{Key: "profit", Label: "Profit", Format: "money"},
		{Key: "cashier_name", Label: "Cashier"},
		{Key: "status", Label: "Status"},
	}
	visitorsColumns = []export.Column{
		{Key: "period", Label: "Period"},
		{Key: "visitor_count", Label: "Visitors", Format: "number"},
		{Key: "net_sales", Label: "Sales", Format: "money"},
	}
	promotionTransactionsColumns = []export.Column{
		{Key: "promotion_name", Label: "Promotion"},
		{Key: "transaction_count", Label: "Transactions", Format: "number"},
		{Key: "discount_total", Label: "Discount", Format: "money"},
		{Key: "net_sales", Label: "Revenue", Format: "money"},
		{Key: "profit", Label: "Profit", Format: "money"},
	}
	promotionProductsColumns = []export.Column{
		{Key: "product_name", Label: "Product"},
		{Key: "promotion_name", Label: "Promotion"},
		{Key: "quantity", Label: "Qty", Format: "number"},
		{Key: "discount_total", Label: "Discount", Format: "money"},
		{Key: "net_sales", Label: "Revenue", Format: "money"},
		{Key: "profit", Label: "Profit", Format: "money"},
	}
	storeRatingsColumns = []export.Column{
		{Key: "created_at", Label: "Date", Format: "datetime"},
		{Key: "customer_name", Label: "Customer"},
		{Key: "rating", Label: "Rating", Format: "number"},
		{Key: "comment", Label: "Comment"},
		{Key: "receipt_number", Label: "Receipt"},
	}
	branchesColumns = []export.Column{
		{Key: "branch_name", Label: "Branch"},
		{Key: "transaction_count", Label: "Transactions", Format: "number"},
		{Key: "revenue", Label: "Revenue", Format: "money"},
		{Key: "profit", Label: "Profit", Format: "money"},
		{Key: "average_basket", Label: "Avg. basket", Format: "money"},
	}
	stockTransfersColumns = []export.Column{
		{Key: "transfer_date", Label: "Date", Format: "date"},
		{Key: "transfer_number", Label: "Number"},
		{Key: "direction", Label: "Type"},
		{Key: "from_branch_name", Label: "From"},
		{Key: "to_branch_name", Label: "To"},
		{Key: "item_count", Label: "Items", Format: "number"},
		{Key: "total_quantity", Label: "Qty", Format: "number"},
		{Key: "status", Label: "Status"},
	}
	stockTransferConfirmationsColumns = []export.Column{
		{Key: "transfer_date", Label: "Date", Format: "date"},
		{Key: "transfer_number", Label: "Number"},
		{Key: "direction", Label: "Type"},
		{Key: "from_branch_name", Label: "From"},
		{Key: "to_branch_name", Label: "To"},
		{Key: "item_count", Label: "Items", Format: "number"},
		{Key: "total_quantity", Label: "Qty", Format: "number"},
		{Key: "status", Label: "Status"},
	}
)

// V1ProfitLoss godoc
//
//	@Summary		Profit and loss per day
//	@Description	Sales, cost of goods, other income and expenses of every day of the period. Unpaid credit sales count only with include_unpaid.
//	@Tags			Reports
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			include_unpaid	query		bool	false	"count unpaid credit sales too"
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.ProfitLossRow,summary=models.ProfitLossSummary,totals=models.ProfitLossSummary}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/profit-loss [get]
func (q *reportController) V1ProfitLoss(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1ProfitLoss", true, false, true, q.reportService.FindProfitLoss)
}

// V1ProfitLossExport godoc
//
//	@Summary		Export: profit and loss per day
//	@Description	Every row of the report (same filters) as a file; a totals row closes it.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			include_unpaid	query		bool	false	"count unpaid credit sales too"
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/profit-loss/export [get]
func (q *reportController) V1ProfitLossExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1ProfitLossExport", true, true, "profit-loss", "Profit & Loss", profitLossColumns, q.reportService.FindProfitLoss)
}

// V1CashFlow godoc
//
//	@Summary		Cash flow
//	@Description	Every movement of money in the period: sales, purchases, debt payments, cash entries and refunds.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			type		query		string	false	"only income or only expense"	Enums(income, expense)
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.CashFlowRow,summary=models.CashFlowSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/cash-flow [get]
func (q *reportController) V1CashFlow(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1CashFlow", true, true, false, q.reportService.FindCashFlow)
}

// V1CashFlowExport godoc
//
//	@Summary		Export: cash flow
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			type		query		string	false	"only income or only expense"	Enums(income, expense)
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/cash-flow/export [get]
func (q *reportController) V1CashFlowExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1CashFlowExport", true, false, "cash-flow", "Cash Flow", cashFlowColumns, q.reportService.FindCashFlow)
}

// V1DailySales godoc
//
//	@Summary		Sales per day
//	@Description	The sales of every day with a sale in the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.DailySalesRow,summary=models.DailySalesSummary,totals=models.DailySalesSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/daily-sales [get]
func (q *reportController) V1DailySales(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1DailySales", true, false, true, q.reportService.FindDailySales)
}

// V1DailySalesExport godoc
//
//	@Summary		Export: sales per day
//	@Description	Every row of the report (same filters) as a file; a totals row closes it.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/daily-sales/export [get]
func (q *reportController) V1DailySalesExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1DailySalesExport", true, true, "daily-sales", "Daily Sales", dailySalesColumns, q.reportService.FindDailySales)
}

// V1SalesTransactions godoc
//
//	@Summary		Sales transactions
//	@Description	Every sale of the period.
//	@Tags			Reports
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			payment_method	query		string	false	"only sales paid with this method"
//	@Param			payment_type	query		string	false	"only cash or only credit"	Enums(cash, credit)
//	@Param			cashier_id		query		string	false	"only this cashier (UUID)"
//	@Param			q				query		string	false	"search text"
//	@Param			page			query		int		false	"page, from 1"	default(1)
//	@Param			per_page		query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.ReportSale,summary=models.SalesTransactionsSummary}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/sales-transactions [get]
func (q *reportController) V1SalesTransactions(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1SalesTransactions", true, true, false, q.reportService.FindSalesTransactions)
}

// V1SalesTransactionsExport godoc
//
//	@Summary		Export: sales transactions
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			payment_method	query		string	false	"only sales paid with this method"
//	@Param			payment_type	query		string	false	"only cash or only credit"	Enums(cash, credit)
//	@Param			cashier_id		query		string	false	"only this cashier (UUID)"
//	@Param			q				query		string	false	"search text"
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/sales-transactions/export [get]
func (q *reportController) V1SalesTransactionsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1SalesTransactionsExport", true, false, "sales-transactions", "Sales Transactions", salesTransactionsColumns, q.reportService.FindSalesTransactions)
}

// V1SalesByProduct godoc
//
//	@Summary		Sales by product
//	@Description	What every product sold in the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			category_id	query		string	false	"only this category (UUID)"
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.SalesByProductRow,summary=models.SalesByProductSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/sales-by-product [get]
func (q *reportController) V1SalesByProduct(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1SalesByProduct", true, true, false, q.reportService.FindSalesByProduct)
}

// V1SalesByProductExport godoc
//
//	@Summary		Export: sales by product
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			category_id	query		string	false	"only this category (UUID)"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/sales-by-product/export [get]
func (q *reportController) V1SalesByProductExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1SalesByProductExport", true, false, "sales-by-product", "Sales by Product", salesByProductColumns, q.reportService.FindSalesByProduct)
}

// V1SalesByCategory godoc
//
//	@Summary		Sales by category
//	@Description	What every category sold in the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.SalesByCategoryRow,summary=models.SalesByCategorySummary,totals=models.SalesByCategorySummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/sales-by-category [get]
func (q *reportController) V1SalesByCategory(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1SalesByCategory", true, false, true, q.reportService.FindSalesByCategory)
}

// V1SalesByCategoryExport godoc
//
//	@Summary		Export: sales by category
//	@Description	Every row of the report (same filters) as a file; a totals row closes it.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/sales-by-category/export [get]
func (q *reportController) V1SalesByCategoryExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1SalesByCategoryExport", true, true, "sales-by-category", "Sales by Category", salesByCategoryColumns, q.reportService.FindSalesByCategory)
}

// V1DailyPurchases godoc
//
//	@Summary		Purchases per day
//	@Description	The purchases of every day with a purchase in the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.DailyPurchasesRow,summary=models.DailyPurchasesSummary,totals=models.DailyPurchasesSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/daily-purchases [get]
func (q *reportController) V1DailyPurchases(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1DailyPurchases", true, false, true, q.reportService.FindDailyPurchases)
}

// V1DailyPurchasesExport godoc
//
//	@Summary		Export: purchases per day
//	@Description	Every row of the report (same filters) as a file; a totals row closes it.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/daily-purchases/export [get]
func (q *reportController) V1DailyPurchasesExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1DailyPurchasesExport", true, true, "daily-purchases", "Daily Purchases", dailyPurchasesColumns, q.reportService.FindDailyPurchases)
}

// V1PurchaseTransactions godoc
//
//	@Summary		Purchase transactions
//	@Description	Every purchase of the period.
//	@Tags			Reports
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			payment_type	query		string	false	"only cash or only credit"	Enums(cash, credit)
//	@Param			staff_id		query		string	false	"only recorded by this staff member (UUID)"
//	@Param			supplier_id		query		string	false	"only this supplier (UUID)"
//	@Param			q				query		string	false	"search text"
//	@Param			page			query		int		false	"page, from 1"	default(1)
//	@Param			per_page		query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.PurchaseTransactionsRow,summary=models.PurchaseTransactionsSummary}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/purchase-transactions [get]
func (q *reportController) V1PurchaseTransactions(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1PurchaseTransactions", true, true, false, q.reportService.FindPurchaseTransactions)
}

// V1PurchaseTransactionsExport godoc
//
//	@Summary		Export: purchase transactions
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			payment_type	query		string	false	"only cash or only credit"	Enums(cash, credit)
//	@Param			staff_id		query		string	false	"only recorded by this staff member (UUID)"
//	@Param			supplier_id		query		string	false	"only this supplier (UUID)"
//	@Param			q				query		string	false	"search text"
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/purchase-transactions/export [get]
func (q *reportController) V1PurchaseTransactionsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1PurchaseTransactionsExport", true, false, "purchase-transactions", "Purchase Transactions", purchaseTransactionsColumns, q.reportService.FindPurchaseTransactions)
}

// V1CustomerPoints godoc
//
//	@Summary		Points by customer
//	@Description	The points every customer earned and redeemed in the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			customer_id	query		string	false	"only this customer (UUID)"
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.CustomerPointsRow,summary=models.CustomerPointsSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/customer-points [get]
func (q *reportController) V1CustomerPoints(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1CustomerPoints", true, true, false, q.reportService.FindCustomerPoints)
}

// V1CustomerPointsExport godoc
//
//	@Summary		Export: points by customer
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			customer_id	query		string	false	"only this customer (UUID)"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/customer-points/export [get]
func (q *reportController) V1CustomerPointsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1CustomerPointsExport", true, false, "customer-points", "Points by Customer", customerPointsColumns, q.reportService.FindCustomerPoints)
}

// V1PointRedemptions godoc
//
//	@Summary		Point redemptions by product
//	@Description	The products sold with points redeemed in the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.PointRedemptionsRow,summary=models.PointRedemptionsSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/point-redemptions [get]
func (q *reportController) V1PointRedemptions(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1PointRedemptions", true, true, false, q.reportService.FindPointRedemptions)
}

// V1PointRedemptionsExport godoc
//
//	@Summary		Export: point redemptions by product
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/point-redemptions/export [get]
func (q *reportController) V1PointRedemptionsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1PointRedemptionsExport", true, false, "point-redemptions", "Point Redemptions", pointRedemptionsColumns, q.reportService.FindPointRedemptions)
}

// V1PointEarnings godoc
//
//	@Summary		Point-earning sales by product
//	@Description	The products sold with points earned in the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.PointEarningsRow,summary=models.PointEarningsSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/point-earnings [get]
func (q *reportController) V1PointEarnings(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1PointEarnings", true, true, false, q.reportService.FindPointEarnings)
}

// V1PointEarningsExport godoc
//
//	@Summary		Export: point-earning sales by product
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/point-earnings/export [get]
func (q *reportController) V1PointEarningsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1PointEarningsExport", true, false, "point-earnings", "Point-Earning Sales", pointEarningsColumns, q.reportService.FindPointEarnings)
}

// V1Capital godoc
//
//	@Summary		Inventory capital
//	@Description	What the stock on hand is worth at purchase price.
//	@Tags			Reports
//	@Param			category_id	query		string	false	"only this category (UUID)"
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.CapitalRow,summary=models.CapitalSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/capital [get]
func (q *reportController) V1Capital(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1Capital", false, true, false, q.reportService.FindCapital)
}

// V1CapitalExport godoc
//
//	@Summary		Export: inventory capital
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			category_id	query		string	false	"only this category (UUID)"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/capital/export [get]
func (q *reportController) V1CapitalExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1CapitalExport", false, false, "capital", "Inventory Capital", capitalColumns, q.reportService.FindCapital)
}

// V1SaleFees godoc
//
//	@Summary		Fees charged on sales
//	@Description	Every additional fee charged on a sale of the period.
//	@Tags			Reports
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			cashier_id		query		string	false	"only this cashier (UUID)"
//	@Param			payment_method	query		string	false	"only sales paid with this method"
//	@Param			q				query		string	false	"search text"
//	@Param			page			query		int		false	"page, from 1"	default(1)
//	@Param			per_page		query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.FeeRow,summary=models.FeeSummary}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/fees/sales [get]
func (q *reportController) V1SaleFees(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1SaleFees", true, true, false, q.reportService.FindSaleFees)
}

// V1SaleFeesExport godoc
//
//	@Summary		Export: fees charged on sales
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			cashier_id		query		string	false	"only this cashier (UUID)"
//	@Param			payment_method	query		string	false	"only sales paid with this method"
//	@Param			q				query		string	false	"search text"
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/fees/sales/export [get]
func (q *reportController) V1SaleFeesExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1SaleFeesExport", true, false, "fees-sales", "Fees on Sales", saleFeesColumns, q.reportService.FindSaleFees)
}

// V1PurchaseFees godoc
//
//	@Summary		Fees paid on purchases
//	@Description	The fees of every purchase of the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			staff_id	query		string	false	"only recorded by this staff member (UUID)"
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.FeeRow,summary=models.FeeSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/fees/purchases [get]
func (q *reportController) V1PurchaseFees(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1PurchaseFees", true, true, false, q.reportService.FindPurchaseFees)
}

// V1PurchaseFeesExport godoc
//
//	@Summary		Export: fees paid on purchases
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			staff_id	query		string	false	"only recorded by this staff member (UUID)"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/fees/purchases/export [get]
func (q *reportController) V1PurchaseFeesExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1PurchaseFeesExport", true, false, "fees-purchases", "Fees on Purchases", purchaseFeesColumns, q.reportService.FindPurchaseFees)
}

// V1MDRFees godoc
//
//	@Summary		MDR fees
//	@Description	What the payment providers charged on the sales of the period.
//	@Tags			Reports
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			cashier_id		query		string	false	"only this cashier (UUID)"
//	@Param			payment_method	query		string	false	"only sales paid with this method"
//	@Param			q				query		string	false	"search text"
//	@Param			page			query		int		false	"page, from 1"	default(1)
//	@Param			per_page		query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.MDRFeeRow,summary=models.FeeSummary}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/fees/mdr [get]
func (q *reportController) V1MDRFees(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1MDRFees", true, true, false, q.reportService.FindMDRFees)
}

// V1MDRFeesExport godoc
//
//	@Summary		Export: mDR fees
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			cashier_id		query		string	false	"only this cashier (UUID)"
//	@Param			payment_method	query		string	false	"only sales paid with this method"
//	@Param			q				query		string	false	"search text"
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/fees/mdr/export [get]
func (q *reportController) V1MDRFeesExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1MDRFeesExport", true, false, "fees-mdr", "MDR Fees", mDRFeesColumns, q.reportService.FindMDRFees)
}

// V1Taxes godoc
//
//	@Summary		Taxes collected
//	@Description	The tax collected per day and tax.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.TaxReportRow,summary=models.TaxReportSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/taxes [get]
func (q *reportController) V1Taxes(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1Taxes", true, true, false, q.reportService.FindTaxes)
}

// V1TaxesExport godoc
//
//	@Summary		Export: taxes collected
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/taxes/export [get]
func (q *reportController) V1TaxesExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1TaxesExport", true, false, "taxes", "Taxes", taxesColumns, q.reportService.FindTaxes)
}

// V1PPOB godoc
//
//	@Summary		PPOB sales
//	@Description	The top-ups and bill payments of the period; the summary counts successful ones.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			cashier_id	query		string	false	"only this cashier (UUID)"
//	@Param			status		query		string	false	"only rows with this status"
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.PPOBReportRow,summary=models.PPOBReportSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/ppob [get]
func (q *reportController) V1PPOB(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1PPOB", true, true, false, q.reportService.FindPPOB)
}

// V1PPOBExport godoc
//
//	@Summary		Export: pPOB sales
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			cashier_id	query		string	false	"only this cashier (UUID)"
//	@Param			status		query		string	false	"only rows with this status"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/ppob/export [get]
func (q *reportController) V1PPOBExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1PPOBExport", true, false, "ppob", "PPOB Sales", pPOBColumns, q.reportService.FindPPOB)
}

// V1Visitors godoc
//
//	@Summary		Visitors per period
//	@Description	How many sales were made per hour of the day, weekday, date or month.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			group_by	query		string	false	"period of a row"	Enums(hour, weekday, date, month)	default(hour)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.VisitorsRow,summary=models.VisitorsSummary,totals=models.VisitorsSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/visitors [get]
func (q *reportController) V1Visitors(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1Visitors", true, false, true, q.reportService.FindVisitors)
}

// V1VisitorsExport godoc
//
//	@Summary		Export: visitors per period
//	@Description	Every row of the report (same filters) as a file; a totals row closes it.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			group_by	query		string	false	"period of a row"	Enums(hour, weekday, date, month)	default(hour)
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/visitors/export [get]
func (q *reportController) V1VisitorsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1VisitorsExport", true, true, "visitors", "Visitors", visitorsColumns, q.reportService.FindVisitors)
}

// V1PromotionTransactions godoc
//
//	@Summary		Sales per promotion
//	@Description	How often every promotion was used in the period and what it gave away.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.PromotionTransactionsRow,summary=models.PromotionTransactionsSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/promotions/transactions [get]
func (q *reportController) V1PromotionTransactions(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1PromotionTransactions", true, true, false, q.reportService.FindPromotionTransactions)
}

// V1PromotionTransactionsExport godoc
//
//	@Summary		Export: sales per promotion
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/promotions/transactions/export [get]
func (q *reportController) V1PromotionTransactionsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1PromotionTransactionsExport", true, false, "promotions-transactions", "Promotion Transactions", promotionTransactionsColumns, q.reportService.FindPromotionTransactions)
}

// V1PromotionProducts godoc
//
//	@Summary		Products sold on promotion
//	@Description	The products sold with a promotion in the period.
//	@Tags			Reports
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			promotion_id	query		string	false	"only this promotion (UUID)"
//	@Param			q				query		string	false	"search text"
//	@Param			page			query		int		false	"page, from 1"	default(1)
//	@Param			per_page		query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.PromotionProductsRow,summary=models.PromotionProductsSummary}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/promotions/products [get]
func (q *reportController) V1PromotionProducts(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1PromotionProducts", true, true, false, q.reportService.FindPromotionProducts)
}

// V1PromotionProductsExport godoc
//
//	@Summary		Export: products sold on promotion
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			promotion_id	query		string	false	"only this promotion (UUID)"
//	@Param			q				query		string	false	"search text"
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/promotions/products/export [get]
func (q *reportController) V1PromotionProductsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1PromotionProductsExport", true, false, "promotions-products", "Promotion Product Sales", promotionProductsColumns, q.reportService.FindPromotionProducts)
}

// V1StoreRatings godoc
//
//	@Summary		Store ratings
//	@Description	The reviews customers left in the period; the summary covers every rating of the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			rating		query		int		false	"only reviews with this rating"	minimum(1)	maximum(5)
//	@Param			page		query		int		false	"page, from 1"					default(1)
//	@Param			per_page	query		int		false	"rows per page"					default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.StoreRatingRow,summary=models.StoreRatingSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/store-ratings [get]
func (q *reportController) V1StoreRatings(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1StoreRatings", true, true, false, q.reportService.FindStoreRatings)
}

// V1StoreRatingsExport godoc
//
//	@Summary		Export: store ratings
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			rating		query		int		false	"only reviews with this rating"	minimum(1)	maximum(5)
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/store-ratings/export [get]
func (q *reportController) V1StoreRatingsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1StoreRatingsExport", true, false, "store-ratings", "Store Ratings", storeRatingsColumns, q.reportService.FindStoreRatings)
}

// V1Branches godoc
//
//	@Summary		Branch performance
//	@Description	The sales of every branch of the store in the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.BranchReportRow,summary=models.BranchReportSummary,totals=models.BranchReportSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/branches [get]
func (q *reportController) V1Branches(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1Branches", true, false, true, q.reportService.FindBranches)
}

// V1BranchesExport godoc
//
//	@Summary		Export: branch performance
//	@Description	Every row of the report (same filters) as a file; a totals row closes it.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/branches/export [get]
func (q *reportController) V1BranchesExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1BranchesExport", true, true, "branches", "Branch Performance", branchesColumns, q.reportService.FindBranches)
}

// V1StockTransfers godoc
//
//	@Summary		Stock transfers
//	@Description	The transfers into and out of the active branch in the period.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			direction	query		string	false	"only transfers out of or into the active branch"			Enums(out, in)
//	@Param			role		query		string	false	"only transfers with the head office, or between branches"	Enums(head_office, branch)
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.StockTransferReportRow}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/stock-transfers [get]
func (q *reportController) V1StockTransfers(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1StockTransfers", true, true, false, q.reportService.FindStockTransfers)
}

// V1StockTransfersExport godoc
//
//	@Summary		Export: stock transfers
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			direction	query		string	false	"only transfers out of or into the active branch"			Enums(out, in)
//	@Param			role		query		string	false	"only transfers with the head office, or between branches"	Enums(head_office, branch)
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/stock-transfers/export [get]
func (q *reportController) V1StockTransfersExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1StockTransfersExport", true, false, "stock-transfers", "Stock Transfers", stockTransfersColumns, q.reportService.FindStockTransfers)
}

// V1StockTransferConfirmations godoc
//
//	@Summary		Stock transfers that need confirmation
//	@Description	The transfers of the period that the receiving branch has to confirm (business plugin).
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			direction	query		string	false	"only transfers out of or into the active branch"			Enums(out, in)
//	@Param			role		query		string	false	"only transfers with the head office, or between branches"	Enums(head_office, branch)
//	@Param			status		query		string	false	"only rows with this status"
//	@Param			q			query		string	false	"search text"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.StockTransferReportRow}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/stock-transfers/confirmations [get]
func (q *reportController) V1StockTransferConfirmations(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1StockTransferConfirmations", true, true, false, q.reportService.FindStockTransferConfirmations)
}

// V1StockTransferConfirmationsExport godoc
//
//	@Summary		Export: stock transfers that need confirmation
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			direction	query		string	false	"only transfers out of or into the active branch"			Enums(out, in)
//	@Param			role		query		string	false	"only transfers with the head office, or between branches"	Enums(head_office, branch)
//	@Param			status		query		string	false	"only rows with this status"
//	@Param			q			query		string	false	"search text"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/stock-transfers/confirmations/export [get]
func (q *reportController) V1StockTransferConfirmationsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1StockTransferConfirmationsExport", true, false, "stock-transfers-confirmations", "Transfers Awaiting Confirmation", stockTransferConfirmationsColumns, q.reportService.FindStockTransferConfirmations)
}
