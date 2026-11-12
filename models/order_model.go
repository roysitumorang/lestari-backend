package models

import (
	"errors"
	"slices"
	"time"
	"uuid"
)

const (
	OrderStatusUnpaid               = "unpaid"
	OrderStatusAwaitingConfirmation = "awaiting_confirmation"
	OrderStatusPaymentRejected      = "payment_rejected"
	OrderStatusProcessing           = "processing"
	OrderStatusShipped              = "shipped"
	OrderStatusDelivered            = "delivered"
	OrderStatusCompleted            = "completed"
	OrderStatusReturned             = "returned"
	OrderStatusCancelled            = "cancelled"

	NotificationSourceOnlineStore = "online_store"
)

var (
	ErrOrderNotFound = errors.New("order not found")

	OrderStatuses = []string{OrderStatusUnpaid, OrderStatusAwaitingConfirmation, OrderStatusPaymentRejected, OrderStatusProcessing,
		OrderStatusShipped, OrderStatusDelivered, OrderStatusCompleted, OrderStatusReturned, OrderStatusCancelled}
	OrderShippingMethods = []string{"takeaway", "cod", "courier", "own_courier"}
	OrderPaymentMethods  = []string{"bank_transfer", "direct"}

	// OrderTransitions lists the status changes the back office may make.
	OrderTransitions = map[string][]string{
		OrderStatusUnpaid:               {OrderStatusProcessing, OrderStatusCancelled},
		OrderStatusAwaitingConfirmation: {OrderStatusProcessing, OrderStatusPaymentRejected, OrderStatusCancelled},
		OrderStatusPaymentRejected:      {OrderStatusAwaitingConfirmation, OrderStatusCancelled},
		OrderStatusProcessing:           {OrderStatusShipped, OrderStatusCompleted, OrderStatusCancelled},
		OrderStatusShipped:              {OrderStatusDelivered, OrderStatusCompleted, OrderStatusReturned},
		OrderStatusDelivered:            {OrderStatusCompleted, OrderStatusReturned},
	}

	// MapOrderSortings lists the fields a client may sort online orders by (?sort=).
	MapOrderSortings = map[string]struct{}{
		"created_at": {}, "order_number": {}, "customer_name": {}, "payment_method": {}, "subtotal": {}, "shipping_fee": {}, "grand_total": {},
		"status": {},
	}
)

// OrderHoldsStock reports whether an order in this status has taken its stock.
func OrderHoldsStock(status string) bool {
	return slices.Contains([]string{OrderStatusProcessing, OrderStatusShipped, OrderStatusDelivered, OrderStatusCompleted}, status)
}

// Order is an order from the online catalog; items are filled when a single order is read.
type Order struct {
	ID              uuid.UUID    `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4db0"`
	CreatedAt       time.Time    `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt       time.Time    `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
	OrderNumber     string       `json:"order_number" example:"ORD/20261108/0002"`
	CustomerName    string       `json:"customer_name" example:"Ruth Simanjuntak"`
	CustomerPhone   *string      `json:"customer_phone" example:"+6281234567890"`
	ShippingAddress *string      `json:"shipping_address" example:"Jl. Sudirman No. 5, Medan"`
	ShippingMethod  *string      `json:"shipping_method" example:"courier" enums:"takeaway,cod,courier,own_courier"`
	Courier         *string      `json:"courier" example:"jne"`
	TrackingNumber  *string      `json:"tracking_number" example:"JNE0012345678"`
	PaymentMethod   string       `json:"payment_method" example:"bank_transfer" enums:"bank_transfer,direct"`
	PaymentProofURL *string      `json:"payment_proof_url" example:"/uploads/proof.jpg"`
	Subtotal        float64      `json:"subtotal" example:"36000"`
	ShippingFee     float64      `json:"shipping_fee" example:"10000"`
	GrandTotal      float64      `json:"grand_total" example:"46000"`
	CustomerNote    *string      `json:"customer_note" example:"please pack well"`
	Status          string       `json:"status" example:"awaiting_confirmation" enums:"unpaid,awaiting_confirmation,payment_rejected,processing,shipped,delivered,completed,returned,cancelled"`
	StatusReason    *string      `json:"status_reason" example:""`
	Items           []*OrderItem `json:"items,omitempty"`
}

// OrderItem is one line of an online order.
type OrderItem struct {
	ProductID   *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6e"`
	ProductName string     `json:"product_name" example:"Kopi Susu Gula Aren"`
	Quantity    float64    `json:"quantity" example:"2"`
	UnitPrice   float64    `json:"unit_price" example:"18000"`
	Subtotal    float64    `json:"subtotal" example:"36000"`
}

// OrderRequest is the body of POST /catalog/orders. An order with a payment proof starts awaiting confirmation,
// any other unpaid.
type OrderRequest struct {
	CustomerName    string               `json:"customer_name" example:"Ruth Simanjuntak"`
	CustomerPhone   string               `json:"customer_phone" example:"+6281234567890"`
	ShippingAddress string               `json:"shipping_address" example:"Jl. Sudirman No. 5, Medan"`
	ShippingMethod  string               `json:"shipping_method" example:"courier" enums:"takeaway,cod,courier,own_courier"`
	Courier         string               `json:"courier" example:"jne"`
	PaymentMethod   string               `json:"payment_method" example:"bank_transfer" enums:"bank_transfer,direct"`
	PaymentProofURL string               `json:"payment_proof_url" example:"/uploads/proof.jpg"`
	ShippingFee     float64              `json:"shipping_fee" example:"10000" minimum:"0"`
	CustomerNote    string               `json:"customer_note" example:"please pack well"`
	Items           []*ReturnItemRequest `json:"items"`
}

// OrderCreate is a validated order with where and when it is recorded.
type OrderCreate struct {
	*OrderRequest
	StoreID  uuid.UUID
	BranchID uuid.UUID
	At       time.Time
}

// OrderStatusRequest is the body of PATCH /catalog/orders/{id}/status.
type OrderStatusRequest struct {
	Status string `json:"status" example:"shipped" enums:"awaiting_confirmation,payment_rejected,processing,shipped,delivered,completed,returned,cancelled"`
	// required when cancelling an order or rejecting its payment
	Reason         string `json:"reason" example:""`
	Courier        string `json:"courier" example:"jne"`
	TrackingNumber string `json:"tracking_number" example:"JNE0012345678"`
}

// OrderStatusChange is a validated status change with who makes it and when.
type OrderStatusChange struct {
	*OrderStatusRequest
	BranchID uuid.UUID
	OrderID  uuid.UUID
	Actor    Actor
	At       time.Time
}

// OrderFilter selects online orders.
type OrderFilter struct {
	BranchID uuid.UUID
	Keyword  string
	PaymentMethod,
	Status,
	StartDate,
	EndDate string
	Sortings []Sorting
	Limit,
	Page int64
}

// OnlineProduct is a product as sold in the online catalog.
type OnlineProduct struct {
	Name         string
	SellingPrice float64
}
