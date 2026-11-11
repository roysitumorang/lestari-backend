package models

import (
	"errors"
	"time"
	"uuid"

	"github.com/goccy/go-json"
)

var (
	ErrPaymentMethodNotFound = errors.New("payment method not found")
	// ErrPaymentMethodConflict is a unique-constraint violation: another payment method has the same code or name.
	ErrPaymentMethodConflict = errors.New("payment method with the same code or name already exists")
	// ErrPaymentMethodInUse is returned when other records still reference the payment method.
	ErrPaymentMethodInUse = errors.New("payment method is still used by other records")

	// MapPaymentMethodSortings lists the fields a client may sort by (?sort=).
	MapPaymentMethodSortings = map[string]struct{}{
		"id":                     {},
		"created_at":             {},
		"updated_at":             {},
		"name":                   {},
		"type":                   {},
		"code":                   {},
		"mdr_type":               {},
		"mdr_value":              {},
		"charge_fee_to_customer": {},
		"edc_card_rates":         {},
		"is_active":              {},
	}
)

// PaymentMethod is one row of payment_methods.
type PaymentMethod struct {
	ID                  uuid.UUID       `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID            uuid.UUID       `json:"-"`
	Name                string          `json:"name" example:"QRIS"`
	Type                string          `json:"type" example:"qris" enums:"cash,debit,credit_card,qris,e_wallet,bank_transfer,other"`
	Code                *string         `json:"code" example:"QR"`
	MdrType             *string         `json:"mdr_type" example:"percent" enums:"percent,amount"`
	MdrValue            *float64        `json:"mdr_value" example:"0.7"`
	ChargeFeeToCustomer bool            `json:"charge_fee_to_customer" example:"true"`
	EdcCardRates        json.RawMessage `json:"edc_card_rates" swaggertype:"array,object"`
	IsActive            bool            `json:"is_active" example:"true"`
	CreatedAt           time.Time       `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt           time.Time       `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// PaymentMethodRequest is the body of POST /payment-methods and PUT /payment-methods/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type PaymentMethodRequest struct {
	Nulls               `json:"-"`
	Name                *string         `json:"name" example:"QRIS"`
	Type                *string         `json:"type" example:"qris" enums:"cash,debit,credit_card,qris,e_wallet,bank_transfer,other"`
	Code                *string         `json:"code" example:"QR"`
	MdrType             *string         `json:"mdr_type" example:"percent" enums:"percent,amount"`
	MdrValue            *float64        `json:"mdr_value" example:"0.7" minimum:"0"`
	ChargeFeeToCustomer *bool           `json:"charge_fee_to_customer" example:"true"`
	EdcCardRates        json.RawMessage `json:"edc_card_rates" swaggertype:"array,object"`
	IsActive            *bool           `json:"is_active" example:"true"`
}

// Apply copies the attributes present in the request onto the payment method.
func (q *PaymentMethodRequest) Apply(paymentMethod *PaymentMethod) *PaymentMethod {
	if q.Name != nil {
		paymentMethod.Name = *q.Name
	}
	if q.Type != nil {
		paymentMethod.Type = *q.Type
	}
	if q.Code != nil || q.IsNull("code") {
		paymentMethod.Code = q.Code
	}
	if q.MdrType != nil || q.IsNull("mdr_type") {
		paymentMethod.MdrType = q.MdrType
	}
	if q.MdrValue != nil || q.IsNull("mdr_value") {
		paymentMethod.MdrValue = q.MdrValue
	}
	if q.ChargeFeeToCustomer != nil {
		paymentMethod.ChargeFeeToCustomer = *q.ChargeFeeToCustomer
	}
	if q.EdcCardRates != nil {
		paymentMethod.EdcCardRates = q.EdcCardRates
	}
	if q.IsActive != nil {
		paymentMethod.IsActive = *q.IsActive
	}
	return paymentMethod
}

// PaymentMethodFilter selects paymentMethods; build it with NewPaymentMethodFilter().With…().
type PaymentMethodFilter struct {
	PaymentMethodIDs,
	BranchIDs []uuid.UUID
	Type     *string
	IsActive *bool
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewPaymentMethodFilter() *PaymentMethodFilter {
	return &PaymentMethodFilter{}
}

func (q *PaymentMethodFilter) WithPaymentMethodIDs(paymentMethodIDs ...uuid.UUID) *PaymentMethodFilter {
	q.PaymentMethodIDs = paymentMethodIDs
	return q
}

func (q *PaymentMethodFilter) WithBranchIDs(branchIDs ...uuid.UUID) *PaymentMethodFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *PaymentMethodFilter) WithType(typeValue string) *PaymentMethodFilter {
	q.Type = &typeValue
	return q
}

func (q *PaymentMethodFilter) WithIsActive(isActive bool) *PaymentMethodFilter {
	q.IsActive = &isActive
	return q
}

func (q *PaymentMethodFilter) WithKeyword(keyword string) *PaymentMethodFilter {
	q.Keyword = keyword
	return q
}

func (q *PaymentMethodFilter) WithSortings(sortings ...Sorting) *PaymentMethodFilter {
	q.Sortings = sortings
	return q
}

func (q *PaymentMethodFilter) WithLimit(limit int64) *PaymentMethodFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *PaymentMethodFilter) WithPage(page int64) *PaymentMethodFilter {
	q.Page = page
	return q
}
