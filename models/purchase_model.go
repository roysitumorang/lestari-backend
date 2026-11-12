package models

import (
	"errors"
	"time"
	"uuid"
)

const (
	DebtKindPayable    = "payable"
	DebtKindReceivable = "receivable"
	DebtStatusUnpaid   = "unpaid"
	DebtStatusPaid     = "paid"

	ReturnKindSale     = "sale"
	ReturnKindPurchase = "purchase"

	RefundMethodCash          = "cash"
	RefundMethodReducePayable = "reduce_payable"
)

var (
	ErrPurchaseNotFound = errors.New("purchase not found")
	ErrDebtNotFound     = errors.New("debt not found")
	ErrReturnNotFound   = errors.New("return not found")
	// ErrDebtPaid is returned when a payment is recorded against a debt that is already paid in full.
	ErrDebtPaid = errors.New("debt already paid")

	// MapPurchaseSortings lists the fields a client may sort purchases by (?sort=).
	MapPurchaseSortings = map[string]struct{}{
		"purchase_date": {}, "receipt_number": {}, "supplier_name": {}, "invoice_number": {}, "payment_type": {}, "due_date": {},
		"subtotal": {}, "grand_total": {}, "paid_amount": {}, "staff_name": {}, "status": {}, "created_at": {},
	}
	// MapDebtSortings lists the fields a client may sort payables and receivables by (?sort=).
	MapDebtSortings = map[string]struct{}{
		"reference_number": {}, "party_name": {}, "supplier_name": {}, "customer_name": {}, "transaction_date": {}, "due_date": {},
		"total_amount": {}, "paid_amount": {}, "remaining_amount": {}, "status": {},
	}
	// MapReturnSortings lists the fields a client may sort returns by (?sort=).
	MapReturnSortings = map[string]struct{}{
		"created_at": {}, "return_number": {}, "refund_method": {}, "refund_amount": {}, "cashier_name": {}, "item_count": {},
	}
)

// Purchase is goods bought from a supplier; items are filled when a single purchase is read.
type Purchase struct {
	ID            uuid.UUID       `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d80"`
	CreatedAt     time.Time       `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt     time.Time       `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
	ReceiptNumber string          `json:"receipt_number" example:"PO/20261108/0003"`
	SupplierID    *uuid.UUID      `json:"supplier_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d81"`
	Supplier      *Reference      `json:"supplier"`
	SupplierName  *string         `json:"supplier_name" example:"PT Sumber Makmur"`
	PurchaseDate  string          `json:"purchase_date" example:"2026-11-08"`
	InvoiceNumber *string         `json:"invoice_number" example:"INV-88213"`
	PaymentType   string          `json:"payment_type" example:"credit" enums:"cash,credit"`
	PaymentMethod *string         `json:"payment_method" example:"bank_transfer" enums:"cash,debit,credit_card,qris,e_wallet,bank_transfer,other"`
	DueDate       *string         `json:"due_date" example:"2026-12-08"`
	PaidAmount    float64         `json:"paid_amount" example:"500000"`
	Subtotal      float64         `json:"subtotal" example:"1900000"`
	DiscountTotal float64         `json:"discount_total" example:"0"`
	TaxTotal      float64         `json:"tax_total" example:"0"`
	FeeTotal      float64         `json:"fee_total" example:"25000"`
	GrandTotal    float64         `json:"grand_total" example:"1925000"`
	StaffName     string          `json:"staff_name" example:"Abigael Lestari"`
	Status        string          `json:"status" example:"unpaid" enums:"completed,unpaid"`
	Note          *string         `json:"note" example:"monthly restock"`
	Items         []*PurchaseItem `json:"items,omitempty"`
}

// PurchaseItem is one line of a purchase.
type PurchaseItem struct {
	ProductID      *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6e"`
	ProductName    string     `json:"product_name" example:"Gula Aren 1 kg"`
	Quantity       float64    `json:"quantity" example:"20"`
	Unit           *string    `json:"unit" example:"kg"`
	UnitCost       float64    `json:"unit_cost" example:"95000"`
	DiscountAmount float64    `json:"discount_amount" example:"0"`
	Subtotal       float64    `json:"subtotal" example:"1900000"`
}

// PurchaseItemRequest is one line of the body of a purchase.
type PurchaseItemRequest struct {
	ProductID      uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6e"`
	Quantity       float64   `json:"quantity" example:"20" minimum:"0"`
	Unit           string    `json:"unit" example:"kg"`
	UnitCost       float64   `json:"unit_cost" example:"95000" minimum:"0"`
	DiscountAmount float64   `json:"discount_amount" example:"0" minimum:"0"`
}

// PurchaseRequest is the body of POST /purchases and PUT /purchases/{id}. On an update only the attributes sent
// change; sending items replaces every line (stock is taken back and received again).
type PurchaseRequest struct {
	SupplierID    *uuid.UUID `json:"supplier_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d81"`
	PurchaseDate  *string    `json:"purchase_date" example:"2026-11-08"`
	InvoiceNumber *string    `json:"invoice_number" example:"INV-88213"`
	PaymentType   *string    `json:"payment_type" example:"credit" enums:"cash,credit"`
	PaymentMethod *string    `json:"payment_method" example:"bank_transfer" enums:"cash,debit,credit_card,qris,e_wallet,bank_transfer,other"`
	DueDate       *string    `json:"due_date" example:"2026-12-08"`
	// with a credit purchase: the down payment
	PaidAmount    *float64               `json:"paid_amount" example:"500000" minimum:"0"`
	DiscountTotal *float64               `json:"discount_total" example:"0" minimum:"0"`
	TaxTotal      *float64               `json:"tax_total" example:"0" minimum:"0"`
	FeeTotal      *float64               `json:"fee_total" example:"25000" minimum:"0"`
	Note          *string                `json:"note" example:"monthly restock"`
	Items         []*PurchaseItemRequest `json:"items"`
}

// PurchaseWrite is a validated purchase with who records it and when. HasItems tells whether the body carried
// items; Nulls the attributes sent as null.
type PurchaseWrite struct {
	PurchaseRequest
	Nulls
	ID       uuid.UUID
	BranchID uuid.UUID
	Actor    Actor
	HasItems bool
	At       time.Time
}

// PurchaseFilter selects purchases.
type PurchaseFilter struct {
	BranchID   uuid.UUID
	PurchaseID uuid.UUID
	Keyword    string
	SupplierID *uuid.UUID
	PaymentType,
	StartDate,
	EndDate string
	Sortings []Sorting
	Limit,
	Page int64
}

// PurchaseTotals is what the totals of a purchase are worked out from.
type PurchaseTotals struct {
	PaymentType   string
	ReceiptNumber string
	SupplierID    *uuid.UUID
	PaidAmount    *float64
	DueDate       *string
	DiscountTotal float64
	TaxTotal      float64
	FeeTotal      float64
	PurchaseDate  string
	Subtotal      float64
	// DebtPaid is what was already paid against the payable of the purchase.
	DebtPaid float64
}

// Debt is a payable (to a supplier, from a credit purchase) or a receivable (from a customer, from a credit
// sale); payments are filled when a single debt is read.
type Debt struct {
	ID              uuid.UUID      `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d90"`
	CreatedAt       time.Time      `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt       time.Time      `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
	ReferenceNumber string         `json:"reference_number" example:"PO/20261108/0003"`
	SupplierID      *uuid.UUID     `json:"supplier_id,omitempty" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d81"`
	Supplier        *Reference     `json:"supplier,omitempty"`
	SupplierName    *string        `json:"supplier_name,omitempty" example:"PT Sumber Makmur"`
	CustomerID      *uuid.UUID     `json:"customer_id,omitempty" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6c"`
	Customer        *Reference     `json:"customer,omitempty"`
	CustomerName    *string        `json:"customer_name,omitempty" example:"Ruth Simanjuntak"`
	TransactionDate string         `json:"transaction_date" example:"2026-11-08"`
	DueDate         *string        `json:"due_date" example:"2026-12-08"`
	TotalAmount     float64        `json:"total_amount" example:"1925000"`
	PaidAmount      float64        `json:"paid_amount" example:"500000"`
	RemainingAmount float64        `json:"remaining_amount" example:"1425000"`
	Status          string         `json:"status" example:"unpaid" enums:"unpaid,paid"`
	Payments        []*DebtPayment `json:"payments,omitempty"`
	SaleID          *uuid.UUID     `json:"-"`
	PurchaseID      *uuid.UUID     `json:"-"`
}

// DebtPayment is one payment against a debt.
type DebtPayment struct {
	ID            uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d91"`
	PaymentDate   string    `json:"payment_date" example:"2026-11-20"`
	Amount        float64   `json:"amount" example:"500000"`
	PaymentMethod string    `json:"payment_method" example:"bank_transfer"`
	Note          *string   `json:"note" example:"first instalment"`
}

// DebtPaymentRequest is the body of POST /payables/{id}/payments and POST /receivables/{id}/payments.
type DebtPaymentRequest struct {
	Amount        float64 `json:"amount" example:"500000"`
	PaymentDate   string  `json:"payment_date" example:"2026-11-20"`
	PaymentMethod string  `json:"payment_method" example:"bank_transfer" enums:"cash,debit,credit_card,qris,e_wallet,bank_transfer,other"`
	Note          string  `json:"note" example:"first instalment"`
}

// DebtPaymentCreate is a validated payment against a debt.
type DebtPaymentCreate struct {
	*DebtPaymentRequest
	BranchID uuid.UUID
	DebtID   uuid.UUID
	Kind     string
	At       time.Time
}

// DebtPaymentResponse is the debt after a payment.
type DebtPaymentResponse struct {
	ID              uuid.UUID    `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d90"`
	PaidAmount      float64      `json:"paid_amount" example:"1000000"`
	RemainingAmount float64      `json:"remaining_amount" example:"925000"`
	Status          string       `json:"status" example:"unpaid" enums:"unpaid,paid"`
	Payment         *DebtPayment `json:"payment"`
}

// DebtFilter selects payables or receivables.
type DebtFilter struct {
	BranchID uuid.UUID
	DebtID   uuid.UUID
	Kind     string
	Keyword  string
	PartyID  *uuid.UUID
	Status   string
	Overdue  bool
	// Today (YYYY-MM-DD, from the caller's clock) is what overdue is measured against.
	Today    string
	Sortings []Sorting
	Limit,
	Page int64
}

// DebtRecord is a debt as written by the services.
type DebtRecord struct {
	BranchID uuid.UUID
	Kind     string
	SupplierID,
	CustomerID,
	PurchaseID,
	SaleID *uuid.UUID
	ReferenceNumber,
	TransactionDate string
	DueDate *string
	TotalAmount,
	PaidAmount float64
	Status string
	At     time.Time
}

// Return is goods returned by a customer (kind sale) or to a supplier (kind purchase); items are filled when a
// single return is read.
type Return struct {
	ID             uuid.UUID     `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4da0"`
	CreatedAt      time.Time     `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	ReturnNumber   string        `json:"return_number" example:"RJ/20261108/0001"`
	RefundMethod   *string       `json:"refund_method" example:"cash"`
	RefundAmount   float64       `json:"refund_amount" example:"36000"`
	Reason         *string       `json:"reason" example:"wrong size"`
	CashierName    string        `json:"cashier_name" example:"Abigael Lestari"`
	ItemCount      int64         `json:"item_count" example:"1"`
	SaleID         *uuid.UUID    `json:"sale_id,omitempty" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	ReceiptNumber  *string       `json:"receipt_number,omitempty" example:"S/20261108/0007"`
	Customer       *Reference    `json:"customer,omitempty"`
	PurchaseID     *uuid.UUID    `json:"purchase_id,omitempty" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d80"`
	PurchaseNumber *string       `json:"purchase_number,omitempty" example:"PO/20261108/0003"`
	Supplier       *Reference    `json:"supplier,omitempty"`
	Items          []*ReturnItem `json:"items,omitempty"`
}

// ReturnItem is one line of a return; unit_cost repeats unit_price.
type ReturnItem struct {
	ProductID   *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6e"`
	ProductName string     `json:"product_name" example:"Kopi Susu Gula Aren"`
	Quantity    float64    `json:"quantity" example:"2"`
	UnitPrice   float64    `json:"unit_price" example:"18000"`
	UnitCost    float64    `json:"unit_cost" example:"18000"`
	Subtotal    float64    `json:"subtotal" example:"36000"`
}

// ReturnItemRequest is one line of the body of a return.
type ReturnItemRequest struct {
	ProductID uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6e"`
	Quantity  float64   `json:"quantity" example:"2"`
}

// ReturnRequest is the body of POST /sales/{id}/returns and POST /purchases/{id}/returns.
type ReturnRequest struct {
	// cash by default; a purchase return may also reduce the payable (reduce_payable)
	RefundMethod string               `json:"refund_method" example:"cash"`
	Reason       string               `json:"reason" example:"wrong size"`
	Items        []*ReturnItemRequest `json:"items"`
}

// ReturnCreate is a validated return with who records it and when.
type ReturnCreate struct {
	*ReturnRequest
	BranchID   uuid.UUID
	DocumentID uuid.UUID
	Kind       string
	Actor      Actor
	At         time.Time
}

// ReturnFilter selects returns of a kind.
type ReturnFilter struct {
	BranchID uuid.UUID
	ReturnID uuid.UUID
	Kind     string
	Keyword  string
	StartDate,
	EndDate string
	Sortings []Sorting
	Limit,
	Page int64
}
