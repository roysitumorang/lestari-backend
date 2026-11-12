package models

import (
	"time"
	"uuid"
)

// Code generated from the report definitions; the shared types live in report_types.go.

// ProfitLossRow is a row of the profit & loss report.
type ProfitLossRow struct {
	Date            string  `json:"date" example:"2026-11-08"`
	GrossSales      float64 `json:"gross_sales" example:"150000"`
	Discounts       float64 `json:"discounts" example:"5000"`
	CostOfGoodsSold float64 `json:"cost_of_goods_sold" example:"90000"`
	GrossProfit     float64 `json:"gross_profit" example:"55000"`
	OtherIncome     float64 `json:"other_income" example:"0"`
	MDRFees         float64 `json:"mdr_fees" example:"1050"`
	Commissions     float64 `json:"commissions" example:"3000"`
	OtherExpenses   float64 `json:"other_expenses" example:"20000"`
	NetProfit       float64 `json:"net_profit" example:"30950"`
}

// ProfitLossSummary sums the profit & loss report up.
type ProfitLossSummary struct {
	GrossSales      float64 `json:"gross_sales" example:"150000"`
	Discounts       float64 `json:"discounts" example:"5000"`
	CostOfGoodsSold float64 `json:"cost_of_goods_sold" example:"90000"`
	GrossProfit     float64 `json:"gross_profit" example:"55000"`
	OtherIncome     float64 `json:"other_income" example:"0"`
	MDRFees         float64 `json:"mdr_fees" example:"1050"`
	Commissions     float64 `json:"commissions" example:"3000"`
	OtherExpenses   float64 `json:"other_expenses" example:"20000"`
	NetProfit       float64 `json:"net_profit" example:"30950"`
	PurchaseTotal   float64 `json:"purchase_total" example:"1925000"`
}

// CashFlowRow is a row of the cash flow report.
type CashFlowRow struct {
	Date        time.Time `json:"date" example:"2026-11-08"`
	Type        string    `json:"type" example:"income"`
	Source      string    `json:"source" example:"sale"`
	Category    *string   `json:"category" example:"cash"`
	Description *string   `json:"description" example:"Sale S/20261108/0007"`
	Amount      float64   `json:"amount" example:"150000"`
}

// CashFlowSummary sums the cash flow report up.
type CashFlowSummary struct {
	TotalIncome  float64 `json:"total_income" example:"4500000"`
	TotalExpense float64 `json:"total_expense" example:"1200000"`
	NetCashFlow  float64 `json:"net_cash_flow" example:"3300000"`
}

// DailySalesRow is a row of the daily sales report.
type DailySalesRow struct {
	Date             string  `json:"date" example:"2026-11-08"`
	TransactionCount int64   `json:"transaction_count" example:"12"`
	GrossSales       float64 `json:"gross_sales" example:"150000"`
	Discounts        float64 `json:"discounts" example:"5000"`
	Taxes            float64 `json:"taxes" example:"15950"`
	NetSales         float64 `json:"net_sales" example:"145000"`
	Profit           float64 `json:"profit" example:"42000"`
}

// DailySalesSummary sums the daily sales report up.
type DailySalesSummary struct {
	TransactionCount int64   `json:"transaction_count" example:"12"`
	GrossSales       float64 `json:"gross_sales" example:"150000"`
	Discounts        float64 `json:"discounts" example:"5000"`
	Taxes            float64 `json:"taxes" example:"15950"`
	NetSales         float64 `json:"net_sales" example:"145000"`
	Profit           float64 `json:"profit" example:"42000"`
}

// SalesTransactionsSummary sums the sales transactions report up.
type SalesTransactionsSummary struct {
	TransactionCount int64   `json:"transaction_count" example:"12"`
	GrandTotal       float64 `json:"grand_total" example:"150000"`
	Profit           float64 `json:"profit" example:"42000"`
}

// SalesByProductRow is a row of the sales by product report.
type SalesByProductRow struct {
	ProductID       *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	ProductName     string     `json:"product_name" example:"Kopi Susu Gula Aren"`
	SKU             *string    `json:"sku" example:"P000001"`
	CategoryName    *string    `json:"category_name" example:"Beverages"`
	QuantitySold    float64    `json:"quantity_sold" example:"24"`
	GrossSales      float64    `json:"gross_sales" example:"432000"`
	Discounts       float64    `json:"discounts" example:"0"`
	NetSales        float64    `json:"net_sales" example:"432000"`
	CostOfGoodsSold float64    `json:"cost_of_goods_sold" example:"228000"`
	Profit          float64    `json:"profit" example:"204000"`
}

// SalesByProductSummary sums the sales by product report up.
type SalesByProductSummary struct {
	QuantitySold float64 `json:"quantity_sold" example:"24"`
	NetSales     float64 `json:"net_sales" example:"432000"`
	Profit       float64 `json:"profit" example:"204000"`
}

// SalesByCategoryRow is a row of the sales by category report.
type SalesByCategoryRow struct {
	CategoryName string  `json:"category_name" example:"Beverages"`
	ProductCount int64   `json:"product_count" example:"8"`
	QuantitySold float64 `json:"quantity_sold" example:"120"`
	GrossSales   float64 `json:"gross_sales" example:"2160000"`
	NetSales     float64 `json:"net_sales" example:"2100000"`
	Profit       float64 `json:"profit" example:"980000"`
}

// SalesByCategorySummary sums the sales by category report up.
type SalesByCategorySummary struct {
	QuantitySold float64 `json:"quantity_sold" example:"120"`
	GrossSales   float64 `json:"gross_sales" example:"2160000"`
	NetSales     float64 `json:"net_sales" example:"2100000"`
	Profit       float64 `json:"profit" example:"980000"`
}

// DailyPurchasesRow is a row of the daily purchases report.
type DailyPurchasesRow struct {
	Date          string  `json:"date" example:"2026-11-08"`
	PurchaseCount int64   `json:"purchase_count" example:"3"`
	ItemQuantity  float64 `json:"item_quantity" example:"60"`
	GrandTotal    float64 `json:"grand_total" example:"1925000"`
	PaidAmount    float64 `json:"paid_amount" example:"500000"`
}

// DailyPurchasesSummary sums the daily purchases report up.
type DailyPurchasesSummary struct {
	PurchaseCount int64   `json:"purchase_count" example:"3"`
	ItemQuantity  float64 `json:"item_quantity" example:"60"`
	GrandTotal    float64 `json:"grand_total" example:"1925000"`
	PaidAmount    float64 `json:"paid_amount" example:"500000"`
}

// PurchaseTransactionsRow is a row of the purchase transactions report.
type PurchaseTransactionsRow struct {
	ID            uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	PurchaseDate  string    `json:"purchase_date" example:"2026-11-08"`
	ReceiptNumber string    `json:"receipt_number" example:"PO/20261108/0003"`
	SupplierName  *string   `json:"supplier_name" example:"PT Sumber Makmur"`
	StaffName     string    `json:"staff_name" example:"Abigael Lestari"`
	PaymentType   string    `json:"payment_type" example:"credit"`
	GrandTotal    float64   `json:"grand_total" example:"1925000"`
	PaidAmount    float64   `json:"paid_amount" example:"500000"`
}

// PurchaseTransactionsSummary sums the purchase transactions report up.
type PurchaseTransactionsSummary struct {
	PurchaseCount int64   `json:"purchase_count" example:"3"`
	GrandTotal    float64 `json:"grand_total" example:"1925000"`
}

// CustomerPointsRow is a row of the points by customer report.
type CustomerPointsRow struct {
	CustomerID       uuid.UUID `json:"customer_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CustomerName     string    `json:"customer_name" example:"Ruth Simanjuntak"`
	CustomerCode     *string   `json:"customer_code" example:"CUS-001"`
	PointsEarned     int64     `json:"points_earned" example:"14"`
	PointsRedeemed   int64     `json:"points_redeemed" example:"0"`
	TransactionCount int64     `json:"transaction_count" example:"3"`
	PointsBalance    int64     `json:"points_balance" example:"120"`
}

// CustomerPointsSummary sums the points by customer report up.
type CustomerPointsSummary struct {
	PointsRedeemed   int64 `json:"points_redeemed" example:"0"`
	PointsEarned     int64 `json:"points_earned" example:"14"`
	TransactionCount int64 `json:"transaction_count" example:"3"`
}

// PointRedemptionsRow is a row of the point redemptions report.
type PointRedemptionsRow struct {
	ProductID        *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	ProductName      string     `json:"product_name" example:"Kopi Susu Gula Aren"`
	Quantity         float64    `json:"quantity" example:"6"`
	TransactionCount int64      `json:"transaction_count" example:"3"`
	PointsRedeemed   int64      `json:"points_redeemed" example:"14"`
	Profit           float64    `json:"profit" example:"42000"`
}

// PointRedemptionsSummary sums the point redemptions report up.
type PointRedemptionsSummary struct {
	PointsRedeemed   int64   `json:"points_redeemed" example:"14"`
	Quantity         float64 `json:"quantity" example:"6"`
	Profit           float64 `json:"profit" example:"42000"`
	TransactionCount int64   `json:"transaction_count" example:"3"`
}

// PointEarningsRow is a row of the point-earning sales report.
type PointEarningsRow struct {
	ProductID        *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	ProductName      string     `json:"product_name" example:"Kopi Susu Gula Aren"`
	Quantity         float64    `json:"quantity" example:"6"`
	TransactionCount int64      `json:"transaction_count" example:"3"`
	PointsEarned     int64      `json:"points_earned" example:"14"`
	Profit           float64    `json:"profit" example:"42000"`
}

// PointEarningsSummary sums the point-earning sales report up.
type PointEarningsSummary struct {
	PointsEarned     int64   `json:"points_earned" example:"14"`
	Quantity         float64 `json:"quantity" example:"6"`
	Profit           float64 `json:"profit" example:"42000"`
	TransactionCount int64   `json:"transaction_count" example:"3"`
}

// CapitalRow is a row of the inventory capital report.
type CapitalRow struct {
	ProductID    uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	ProductName  string    `json:"product_name" example:"Kopi Susu Gula Aren"`
	SKU          string    `json:"sku" example:"P000001"`
	CategoryName *string   `json:"category_name" example:"Beverages"`
	Stock        float64   `json:"stock" example:"24"`
	UnitCost     float64   `json:"unit_cost" example:"9500"`
	CapitalValue float64   `json:"capital_value" example:"228000"`
}

// CapitalSummary sums the inventory capital report up.
type CapitalSummary struct {
	TotalCapital float64 `json:"total_capital" example:"12500000"`
	TotalStock   float64 `json:"total_stock" example:"840"`
}

// FeeRow is a row of the fees on sales report.
type FeeRow struct {
	CreatedAt     time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	ReceiptNumber string    `json:"receipt_number" example:"S/20261108/0007"`
	CashierName   string    `json:"cashier_name" example:"Abigael Lestari"`
	FeeName       string    `json:"fee_name" example:"Packaging"`
	Amount        float64   `json:"amount" example:"2000"`
}

// FeeSummary sums the fees on sales report up.
type FeeSummary struct {
	TotalAmount      float64 `json:"total_amount" example:"24000"`
	TransactionCount int64   `json:"transaction_count" example:"12"`
}

// MDRFeeRow is a row of the mdr fees report.
type MDRFeeRow struct {
	CreatedAt         time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	ReceiptNumber     string    `json:"receipt_number" example:"S/20261108/0007"`
	CashierName       string    `json:"cashier_name" example:"Abigael Lestari"`
	PaymentMethod     string    `json:"payment_method" example:"qris"`
	TransactionAmount float64   `json:"transaction_amount" example:"150000"`
	MDRRate           float64   `json:"mdr_rate" example:"0.7"`
	Amount            float64   `json:"amount" example:"1050"`
}

// TaxReportRow is a row of the taxes report.
type TaxReportRow struct {
	Date             string   `json:"date" example:"2026-11-08"`
	TaxName          string   `json:"tax_name" example:"PPN"`
	Rate             *float64 `json:"rate" example:"11"`
	TransactionCount int64    `json:"transaction_count" example:"12"`
	TaxableAmount    float64  `json:"taxable_amount" example:"145000"`
	TaxAmount        float64  `json:"tax_amount" example:"15950"`
}

// TaxReportSummary sums the taxes report up.
type TaxReportSummary struct {
	TaxableAmount float64 `json:"taxable_amount" example:"145000"`
	TaxAmount     float64 `json:"tax_amount" example:"15950"`
}

// PPOBReportRow is a row of the ppob sales report.
type PPOBReportRow struct {
	CreatedAt      time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	ReferenceID    string    `json:"reference_id" example:"PP2611080001"`
	ProductName    string    `json:"product_name" example:"Telkomsel_10.000"`
	CustomerNumber *string   `json:"customer_number" example:"081234567890"`
	BasePrice      float64   `json:"base_price" example:"10150"`
	SellingPrice   float64   `json:"selling_price" example:"12000"`
	Profit         float64   `json:"profit" example:"1850"`
	CashierName    *string   `json:"cashier_name" example:"Abigael Lestari"`
	Status         string    `json:"status" example:"success"`
}

// PPOBReportSummary sums the ppob sales report up.
type PPOBReportSummary struct {
	TransactionCount int64   `json:"transaction_count" example:"12"`
	SellingTotal     float64 `json:"selling_total" example:"240000"`
	Profit           float64 `json:"profit" example:"37000"`
}

// VisitorsRow is a row of the visitors report.
type VisitorsRow struct {
	Period       string  `json:"period" example:"10:00"`
	VisitorCount int64   `json:"visitor_count" example:"12"`
	NetSales     float64 `json:"net_sales" example:"145000"`
}

// VisitorsSummary sums the visitors report up.
type VisitorsSummary struct {
	VisitorCount     int64   `json:"visitor_count" example:"12"`
	AveragePerPeriod float64 `json:"average_per_period" example:"8.5"`
	NetSales         float64 `json:"net_sales" example:"145000"`
}

// PromotionTransactionsRow is a row of the promotion transactions report.
type PromotionTransactionsRow struct {
	PromotionID      uuid.UUID `json:"promotion_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	PromotionName    string    `json:"promotion_name" example:"Weekend deal"`
	TransactionCount int64     `json:"transaction_count" example:"12"`
	DiscountTotal    float64   `json:"discount_total" example:"45000"`
	NetSales         float64   `json:"net_sales" example:"900000"`
	Profit           float64   `json:"profit" example:"310000"`
}

// PromotionTransactionsSummary sums the promotion transactions report up.
type PromotionTransactionsSummary struct {
	TransactionCount int64   `json:"transaction_count" example:"12"`
	DiscountTotal    float64 `json:"discount_total" example:"45000"`
	NetSales         float64 `json:"net_sales" example:"900000"`
	Profit           float64 `json:"profit" example:"310000"`
}

// PromotionProductsRow is a row of the promotion product sales report.
type PromotionProductsRow struct {
	ProductID        *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	ProductName      string     `json:"product_name" example:"Kopi Susu Gula Aren"`
	PromotionName    string     `json:"promotion_name" example:"Weekend deal"`
	Quantity         float64    `json:"quantity" example:"6"`
	DiscountTotal    float64    `json:"discount_total" example:"10800"`
	NetSales         float64    `json:"net_sales" example:"97200"`
	Profit           float64    `json:"profit" example:"40200"`
	TransactionCount int64      `json:"transaction_count" example:"3"`
}

// PromotionProductsSummary sums the promotion product sales report up.
type PromotionProductsSummary struct {
	TransactionCount int64   `json:"transaction_count" example:"3"`
	Profit           float64 `json:"profit" example:"40200"`
	NetSales         float64 `json:"net_sales" example:"97200"`
}

// StoreRatingRow is a row of the store ratings report.
type StoreRatingRow struct {
	ID            uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CreatedAt     time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	CustomerName  *string   `json:"customer_name" example:"Ruth Simanjuntak"`
	Rating        int64     `json:"rating" example:"5"`
	Comment       *string   `json:"comment" example:"Friendly service"`
	Tags          []string  `json:"tags" example:"friendly,fast"`
	ReceiptNumber *string   `json:"receipt_number" example:"S/20261108/0007"`
}

// StoreRatingSummary sums the store ratings report up.
type StoreRatingSummary struct {
	AverageRating   float64          `json:"average_rating" example:"4.6"`
	ReviewCount     int64            `json:"review_count" example:"12"`
	RatingBreakdown map[string]int64 `json:"rating_breakdown"`
}

// BranchReportRow is a row of the branch performance report.
type BranchReportRow struct {
	BranchID         uuid.UUID `json:"branch_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchName       string    `json:"branch_name" example:"Head office"`
	TransactionCount int64     `json:"transaction_count" example:"12"`
	Revenue          float64   `json:"revenue" example:"4500000"`
	Profit           float64   `json:"profit" example:"1300000"`
	AverageBasket    float64   `json:"average_basket" example:"54000"`
}

// BranchReportSummary sums the branch performance report up.
type BranchReportSummary struct {
	TransactionCount int64   `json:"transaction_count" example:"12"`
	Revenue          float64 `json:"revenue" example:"4500000"`
	Profit           float64 `json:"profit" example:"1300000"`
}

// StockTransferReportRow is a row of the stock transfers report.
type StockTransferReportRow struct {
	ID             uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	TransferDate   string    `json:"transfer_date" example:"2026-11-08"`
	TransferNumber string    `json:"transfer_number" example:"TRF/20261108/0001"`
	Direction      string    `json:"direction" example:"out"`
	FromBranchName string    `json:"from_branch_name" example:"Head office"`
	ToBranchName   string    `json:"to_branch_name" example:"Balige branch"`
	ItemCount      int64     `json:"item_count" example:"4"`
	TotalQuantity  float64   `json:"total_quantity" example:"40"`
	Status         string    `json:"status" example:"completed"`
}
