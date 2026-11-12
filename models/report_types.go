package models

import (
	"errors"
	"time"
	"uuid"
)

// Kinds of "which receipts" detail lists of the reports.
const (
	ReportSalesOfProduct        = "product"
	ReportSalesOfCustomerPoints = "customer_points"
	ReportSalesOfRedemption     = "point_redemption"
	ReportSalesOfEarning        = "point_earning"
	ReportSalesOfPromotion      = "promotion"
	ReportSalesOfPromotionItem  = "promotion_product"
)

var ErrStoreRatingNotFound = errors.New("store rating not found")

// ReportFilter carries the parameters of every report; each report reads the ones it knows.
type ReportFilter struct {
	StoreID,
	BranchID uuid.UUID
	StartDate,
	EndDate,
	Keyword,
	Type,
	PaymentMethod,
	PaymentType,
	Status,
	GroupBy,
	Direction,
	Role string
	CashierID,
	CategoryID,
	StaffID,
	SupplierID,
	CustomerID,
	PromotionID,
	CashDrawerID *uuid.UUID
	Rating        int64
	IncludeUnpaid bool
	Limit,
	Page int64
	// All reads every row, for an export.
	All bool
}

// ReportNoSummary is the summary of the reports that have none.
type ReportNoSummary struct{}

// ReportSale is a sale as listed by the reports.
type ReportSale struct {
	ID             uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CreatedAt      time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	ReceiptNumber  string    `json:"receipt_number" example:"S/20261108/0007"`
	CashierName    string    `json:"cashier_name" example:"Abigael Lestari"`
	PaymentMethod  string    `json:"payment_method" example:"cash"`
	PaymentType    string    `json:"payment_type" example:"cash"`
	Profit         float64   `json:"profit" example:"42000"`
	GrandTotal     float64   `json:"grand_total" example:"163000"`
	PaidAmount     float64   `json:"paid_amount" example:"163000"`
	ServiceCharge  float64   `json:"service_charge" example:"0"`
	Rounding       float64   `json:"rounding" example:"50"`
	Label          *string   `json:"label" example:"Table 4"`
	CustomerType   *string   `json:"customer_type" example:"Member"`
	Status         string    `json:"status" example:"completed"`
	CustomerName   *string   `json:"customer_name" example:"Ruth Simanjuntak"`
	StaffEmail     *string   `json:"staff_email" example:"abigael@lestari.id"`
	PointsEarned   *int64    `json:"points_earned,omitempty" example:"14"`
	PointsRedeemed *int64    `json:"points_redeemed,omitempty" example:"0"`
}

// ReportSales is the "which receipts" detail of a report row.
type ReportSales struct {
	Date  string        `json:"date,omitempty" example:"2026-11-08"`
	Sales []*ReportSale `json:"sales"`
}

// DashboardTotals are the sales of one day.
type DashboardTotals struct {
	TransactionCount int64   `json:"transaction_count" example:"42"`
	Revenue          float64 `json:"revenue" example:"1850000"`
	Profit           float64 `json:"profit" example:"610000"`
	AverageBasket    float64 `json:"average_basket" example:"44047.62"`
}

// DashboardChange compares a day with the day before, in percent; null when the day before had nothing.
type DashboardChange struct {
	TransactionCount *float64 `json:"transaction_count" example:"12.5"`
	Revenue          *float64 `json:"revenue" example:"8.2"`
	Profit           *float64 `json:"profit" example:"-3.1"`
}

// DashboardSummary is the day of the dashboard next to the day before.
type DashboardSummary struct {
	DashboardTotals
	ComparedToPrevious DashboardChange `json:"compared_to_previous"`
}

// DashboardHour is one hour of the day on the dashboard chart.
type DashboardHour struct {
	Hour             string  `json:"hour" example:"10:00"`
	TransactionCount int64   `json:"transaction_count" example:"6"`
	Revenue          float64 `json:"revenue" example:"264000"`
	Profit           float64 `json:"profit" example:"87000"`
}

// DashboardProduct is one of the best sellers of the day.
type DashboardProduct struct {
	ProductID *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6e"`
	Name      string     `json:"name" example:"Kopi Susu Gula Aren"`
	Quantity  float64    `json:"quantity" example:"24"`
	Revenue   float64    `json:"revenue" example:"432000"`
}

// LowStockProduct is a product at or below its minimum stock.
type LowStockProduct struct {
	ProductID    uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6e"`
	Name         string    `json:"name" example:"Gula Aren 1 kg"`
	Stock        float64   `json:"stock" example:"2"`
	MinimumStock float64   `json:"minimum_stock" example:"5"`
}

// DashboardAlert is something the owner should act on.
type DashboardAlert struct {
	Type        string `json:"type" example:"subscription" enums:"store_location,subscription,storage"`
	Message     string `json:"message" example:"Your subscription ends on 15-11-2026."`
	ActionLabel string `json:"action_label" example:"Renew"`
	ActionRoute string `json:"action_route" example:"billing"`
}

// Dashboard is the home screen of the back office.
type Dashboard struct {
	Date        string              `json:"date" example:"2026-11-08"`
	Summary     *DashboardSummary   `json:"summary"`
	Hourly      []*DashboardHour    `json:"hourly"`
	TopProducts []*DashboardProduct `json:"top_products"`
	LowStock    []*LowStockProduct  `json:"low_stock"`
	Alerts      []*DashboardAlert   `json:"alerts"`
}

// DashboardFilter selects the day of the dashboard.
type DashboardFilter struct {
	BranchID uuid.UUID
	Date,
	PreviousDate string
	IncludeUnpaidRevenue,
	IncludeUnpaidProfit bool
}

// StoreUsage is what the dashboard alerts are worked out from.
type StoreUsage struct {
	PlanExpiresAt  *time.Time
	UsedMB         float64
	StorageQuotaMB float64
}
