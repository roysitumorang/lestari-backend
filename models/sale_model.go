package models

import (
	"errors"
	"time"
	"uuid"
)

const (
	PaymentTypeCash   = "cash"
	PaymentTypeCredit = "credit"

	SaleStatusCompleted = "completed"
	SaleStatusUnpaid    = "unpaid"

	StockMovementTypeSale        = "sale"
	StockMovementTypeVoid        = "void"
	StockMovementTypeReturn      = "return"
	StockMovementTypePurchase    = "purchase"
	StockMovementTypeOnlineOrder = "online_order"

	PromotionTypeProductDiscount = "product_discount"
	PromotionTypeBuyXGetY        = "buy_x_get_y"
	PromotionTypeMinimumPurchase = "minimum_purchase"
)

var (
	ErrSaleNotFound = errors.New("sale not found")
	// ErrSaleConflict is returned when the receipt number is already used in the branch.
	ErrSaleConflict  = errors.New("sale already exists")
	ErrShiftNotFound = errors.New("shift not found")

	PaymentTypes = []string{PaymentTypeCash, PaymentTypeCredit}
	// SalePaymentMethods are the ways a sale, a purchase or a debt is paid.
	SalePaymentMethods = []string{"cash", "debit", "credit_card", "qris", "e_wallet", "bank_transfer", "other"}

	// MapSaleSortings lists the fields a client may sort sales by (?sort=).
	MapSaleSortings = map[string]struct{}{
		"created_at": {}, "receipt_number": {}, "cashier_name": {}, "customer_name": {}, "payment_method": {}, "payment_type": {},
		"subtotal": {}, "discount_total": {}, "tax_total": {}, "grand_total": {}, "paid_amount": {}, "profit": {}, "status": {}, "due_date": {},
	}
)

// Sale is one sales transaction; items and fees are filled when a single sale is read.
type Sale struct {
	ID             uuid.UUID   `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CreatedAt      time.Time   `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt      time.Time   `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
	ReceiptNumber  string      `json:"receipt_number" example:"S/20261108/0007"`
	CashierID      *uuid.UUID  `json:"cashier_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
	CashierName    string      `json:"cashier_name" example:"Abigael Lestari"`
	CustomerID     *uuid.UUID  `json:"customer_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6c"`
	Customer       *Reference  `json:"customer"`
	CustomerName   *string     `json:"customer_name" example:"Ruth Simanjuntak"`
	PaymentMethod  string      `json:"payment_method" example:"cash" enums:"cash,debit,credit_card,qris,e_wallet,bank_transfer,other"`
	PaymentType    string      `json:"payment_type" example:"cash" enums:"cash,credit"`
	Subtotal       float64     `json:"subtotal" example:"150000"`
	DiscountTotal  float64     `json:"discount_total" example:"5000"`
	TaxName        *string     `json:"tax_name" example:"PPN"`
	TaxTotal       float64     `json:"tax_total" example:"15950"`
	ServiceCharge  float64     `json:"service_charge" example:"0"`
	FeeTotal       float64     `json:"fee_total" example:"2000"`
	Rounding       float64     `json:"rounding" example:"50"`
	GrandTotal     float64     `json:"grand_total" example:"163000"`
	PaidAmount     float64     `json:"paid_amount" example:"163000"`
	ChangeAmount   float64     `json:"change_amount" example:"37000"`
	Profit         float64     `json:"profit" example:"42000"`
	PointsEarned   int64       `json:"points_earned" example:"14"`
	PointsRedeemed int64       `json:"points_redeemed" example:"0"`
	Label          *string     `json:"label" example:"Table 4"`
	CustomerType   *string     `json:"customer_type" example:"Member"`
	Note           *string     `json:"note" example:"no sugar"`
	Status         string      `json:"status" example:"completed" enums:"completed,unpaid"`
	DueDate        *string     `json:"due_date" example:"2026-12-08"`
	Items          []*SaleItem `json:"items,omitempty"`
	Fees           []*SaleFee  `json:"fees,omitempty"`
}

// SaleItem is one line of a sale; discount_amount includes the promotion discount of the line.
type SaleItem struct {
	ID             uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6d"`
	ProductID      *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6e"`
	ProductName    string     `json:"product_name" example:"Kopi Susu Gula Aren"`
	VariantName    *string    `json:"variant_name" example:"Large"`
	Quantity       float64    `json:"quantity" example:"2"`
	UnitPrice      float64    `json:"unit_price" example:"18000"`
	DiscountAmount float64    `json:"discount_amount" example:"0"`
	Subtotal       float64    `json:"subtotal" example:"36000"`
	PointsEarned   int64      `json:"points_earned" example:"2"`
	PointsRedeemed int64      `json:"points_redeemed" example:"0"`
}

// SaleFee is one additional fee charged on a sale.
type SaleFee struct {
	FeeName string  `json:"fee_name" example:"Packaging"`
	Amount  float64 `json:"amount" example:"2000"`
}

// SaleItemRequest is one line of the body of POST /sales.
type SaleItemRequest struct {
	ProductID   uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6e"`
	VariantName string    `json:"variant_name" example:"Large"`
	Quantity    float64   `json:"quantity" example:"2"`
	// overrides the price worked out from the product (variant, wholesale and customer-type prices)
	UnitPrice *float64 `json:"unit_price" example:"18000"`
	// overrides the discount of the product
	DiscountAmount *float64 `json:"discount_amount" example:"0"`
	PointsRedeemed int64    `json:"points_redeemed" example:"0"`
}

// SaleRequest is the body of POST /sales, as sent by cashier apps.
type SaleRequest struct {
	// generated (S/YYYYMMDD/NNNN) when empty
	ReceiptNumber string `json:"receipt_number" example:""`
	// when the sale happened (ISO 8601); now when empty, for sales made offline
	CreatedAt     string             `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	CustomerID    *uuid.UUID         `json:"customer_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6c"`
	ShiftID       *uuid.UUID         `json:"shift_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6f"`
	PaymentMethod string             `json:"payment_method" example:"cash" enums:"cash,debit,credit_card,qris,e_wallet,bank_transfer,other"`
	PaymentType   string             `json:"payment_type" example:"cash" enums:"cash,credit"`
	Items         []*SaleItemRequest `json:"items"`
	// manual discount on the whole sale
	DiscountTotal float64 `json:"discount_total" example:"5000"`
	// the default tax of the branch applies when empty
	TaxID  *uuid.UUID  `json:"tax_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d70"`
	FeeIDs []uuid.UUID `json:"fee_ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d71"`
	// points redeemed as a discount on the whole sale
	PointsRedeemed int64 `json:"points_redeemed" example:"0"`
	// the grand total when empty; a credit sale may pay less
	PaidAmount *float64 `json:"paid_amount" example:"200000"`
	DueDate    string   `json:"due_date" example:""`
	Label      string   `json:"label" example:"Table 4"`
	Note       string   `json:"note" example:"no sugar"`
}

// SaleCreate is a validated sale with who records it and when.
type SaleCreate struct {
	*SaleRequest
	BranchID uuid.UUID
	Actor    Actor
	// BusinessPlugin tells whether the store may use service charge and rounding.
	BusinessPlugin bool
	At             time.Time
}

// SaleFilter selects sales.
type SaleFilter struct {
	BranchID   uuid.UUID
	SaleID     uuid.UUID
	Keyword    string
	CashierID  *uuid.UUID
	CustomerID *uuid.UUID
	PaymentMethod,
	PaymentType,
	Status,
	StartDate,
	EndDate string
	Sortings []Sorting
	Limit,
	Page int64
}

// SaleRecord is a sale as written by the service.
type SaleRecord struct {
	Sale
	BranchID uuid.UUID
	ShiftID,
	TaxID,
	PromotionID *uuid.UUID
	TaxRate,
	CostTotal,
	MDRRate,
	MDRAmount,
	CommissionTotal float64
}

// SaleItemRecord is a sale line as written by the service.
type SaleItemRecord struct {
	SaleItem
	SaleID uuid.UUID
	CategoryID,
	PromotionID *uuid.UUID
	UnitCost,
	PromotionDiscount float64
}

// SaleCustomer is what a sale needs to know about its customer.
type SaleCustomer struct {
	CustomerTypeID   *uuid.UUID
	CustomerTypeName *string
	DiscountType     *string
	DiscountValue    *float64
	PointMultiplier  float64
	Points           int64
}

// SaleProduct is a product as sold: its prices, recipe and stock, locked for the transaction.
type SaleProduct struct {
	ID                 uuid.UUID
	Name               string
	CategoryID         *uuid.UUID
	SellingPrice       float64
	PurchasePrice      float64
	PriceType          string
	DiscountType       *string
	DiscountValue      *float64
	WholesalePrices    []*WholesalePrice
	CustomerTypePrices []*CustomerTypePrice
	Ingredients        []*Ingredient
	Variants           []*Variant
	TrackStock         bool
	Stock              float64
	PointsEarned       *float64
}

// WholesalePrice is the price from a minimum quantity on.
type WholesalePrice struct {
	MinQuantity float64 `json:"min_quantity"`
	Price       float64 `json:"price"`
}

// CustomerTypePrice is the price for customers of a type.
type CustomerTypePrice struct {
	CustomerTypeID uuid.UUID `json:"customer_type_id"`
	Price          float64   `json:"price"`
}

// Ingredient is a product consumed when a recipe product is sold.
type Ingredient struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  float64   `json:"quantity"`
}

// Variant is a named variation of a product with prices of its own.
type Variant struct {
	Name          string   `json:"name"`
	SellingPrice  *float64 `json:"selling_price"`
	PurchasePrice *float64 `json:"purchase_price"`
}

// ActivePromotion is a promotion in effect at the time of a sale.
type ActivePromotion struct {
	ID              uuid.UUID
	Type            string
	DiscountType    *string
	DiscountValue   *float64
	MinimumPurchase float64
	BuyQuantity     float64
	GetQuantity     float64
	Products        []*Ingredient
}

// Covers reports whether the promotion applies to a product.
func (q *ActivePromotion) Covers(productID uuid.UUID) bool {
	for _, item := range q.Products {
		if item.ProductID == productID {
			return true
		}
	}
	return false
}

// SaleTax is the tax applied to a sale.
type SaleTax struct {
	Name        string
	Rate        float64
	IsInclusive bool
}

// SaleFeeRule is a fee as configured: a fixed amount or a percentage of the net sale.
type SaleFeeRule struct {
	Name       string
	AmountType string
	Amount     float64
}

// MDR is the merchant discount rate of a payment method.
type MDR struct {
	MDRType  *string
	MDRValue *float64
}

// CommissionRule is a commission the cashier earns on a sale.
type CommissionRule struct {
	Basis      string
	Type       string
	Value      float64
	ProductIDs []uuid.UUID
}

// SoldQuantity is how much of a product a document carries.
type SoldQuantity struct {
	ProductID   uuid.UUID
	ProductName string
	Quantity    float64
	UnitPrice   float64
}

// Shift is a cashier shift on a cash drawer; expected_cash is opening cash plus cash sales and cash in, minus
// cash out.
type Shift struct {
	ID             uuid.UUID       `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6f"`
	OpenedAt       time.Time       `json:"opened_at" example:"2026-11-08T08:00:00+07:00"`
	ClosedAt       *time.Time      `json:"closed_at" example:"2026-11-08T16:00:00+07:00"`
	CashDrawerID   *uuid.UUID      `json:"cash_drawer_id,omitempty" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d72"`
	CashierID      *uuid.UUID      `json:"cashier_id,omitempty" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
	CashDrawerName *string         `json:"cash_drawer_name" example:"Front counter"`
	CashierName    *string         `json:"cashier_name" example:"Abigael Lestari"`
	OpeningCash    float64         `json:"opening_cash" example:"200000"`
	CashIn         float64         `json:"cash_in" example:"0"`
	CashOut        float64         `json:"cash_out" example:"50000"`
	CashSales      float64         `json:"cash_sales" example:"1250000"`
	ExpectedCash   float64         `json:"expected_cash" example:"1400000"`
	ActualCash     *float64        `json:"actual_cash" example:"1400000"`
	Difference     *float64        `json:"difference" example:"0"`
	Payments       []*ShiftPayment `json:"payments,omitempty"`
}

// ShiftPayment totals the sales of a shift per payment method.
type ShiftPayment struct {
	PaymentMethod    string  `json:"payment_method" example:"cash"`
	TransactionCount int64   `json:"transaction_count" example:"42"`
	Amount           float64 `json:"amount" example:"1250000"`
}

// ShiftOpenRequest is the body of POST /shifts.
type ShiftOpenRequest struct {
	CashDrawerID uuid.UUID `json:"cash_drawer_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d72"`
	OpeningCash  float64   `json:"opening_cash" example:"200000"`
}

// ShiftCloseRequest is the body of POST /shifts/{id}/close.
type ShiftCloseRequest struct {
	ActualCash float64 `json:"actual_cash" example:"1400000"`
	CashIn     float64 `json:"cash_in" example:"0"`
	CashOut    float64 `json:"cash_out" example:"50000"`
}

// ShiftFilter selects shifts.
type ShiftFilter struct {
	BranchID     uuid.UUID
	CashDrawerID *uuid.UUID
	CashierID    *uuid.UUID
	StartDate,
	EndDate string
	Limit,
	Page int64
}
