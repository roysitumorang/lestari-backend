package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrCustomerNotFound = errors.New("customer not found")
	// ErrCustomerConflict is a unique-constraint violation: another customer has the same code or name.
	ErrCustomerConflict = errors.New("customer with the same code or name already exists")
	// ErrCustomerInUse is returned when other records still reference the customer.
	ErrCustomerInUse = errors.New("customer is still used by other records")

	// MapCustomerSortings lists the fields a client may sort by (?sort=).
	MapCustomerSortings = map[string]struct{}{
		"id":                 {},
		"created_at":         {},
		"updated_at":         {},
		"name":               {},
		"phone":              {},
		"email":              {},
		"code":               {},
		"customer_type_id":   {},
		"birth_date":         {},
		"points":             {},
		"receivable_balance": {},
		"address":            {},
		"note":               {},
		"customer_type.name": {},
	}
)

// Customer is one row of customers.
type Customer struct {
	ID                uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID          uuid.UUID  `json:"-"`
	Name              string     `json:"name" example:"Siti Aminah"`
	Phone             *string    `json:"phone" example:"+6281234567890"`
	Email             *string    `json:"email" example:"siti@example.com"`
	Code              *string    `json:"code" example:"CUS-001"`
	CustomerTypeID    *uuid.UUID `json:"customer_type_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CustomerType      *Reference `json:"customer_type"`
	BirthDate         *string    `json:"birth_date" example:"1990-05-17"`
	Points            int64      `json:"points" example:"120"`
	ReceivableBalance float64    `json:"receivable_balance" example:"75000"`
	Address           *string    `json:"address" example:"Jl. Sudirman No. 5, Medan"`
	Note              *string    `json:"note" example:"Prefers WhatsApp"`
	CreatedAt         time.Time  `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt         time.Time  `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// CustomerRequest is the body of POST /customers and PUT /customers/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type CustomerRequest struct {
	Nulls          `json:"-"`
	Name           *string    `json:"name" example:"Siti Aminah"`
	Phone          *string    `json:"phone" example:"+6281234567890"`
	Email          *string    `json:"email" example:"siti@example.com"`
	Code           *string    `json:"code" example:"CUS-001"`
	CustomerTypeID *uuid.UUID `json:"customer_type_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BirthDate      *string    `json:"birth_date" example:"1990-05-17"`
	Address        *string    `json:"address" example:"Jl. Sudirman No. 5, Medan"`
	Note           *string    `json:"note" example:"Prefers WhatsApp"`
}

// Apply copies the attributes present in the request onto the customer.
func (q *CustomerRequest) Apply(customer *Customer) *Customer {
	if q.Name != nil {
		customer.Name = *q.Name
	}
	if q.Phone != nil || q.IsNull("phone") {
		customer.Phone = q.Phone
	}
	if q.Email != nil || q.IsNull("email") {
		customer.Email = q.Email
	}
	if q.Code != nil || q.IsNull("code") {
		customer.Code = q.Code
	}
	if q.CustomerTypeID != nil || q.IsNull("customer_type_id") {
		customer.CustomerTypeID = q.CustomerTypeID
	}
	if q.BirthDate != nil || q.IsNull("birth_date") {
		customer.BirthDate = q.BirthDate
	}
	if q.Address != nil || q.IsNull("address") {
		customer.Address = q.Address
	}
	if q.Note != nil || q.IsNull("note") {
		customer.Note = q.Note
	}
	return customer
}

// CustomerFilter selects customers; build it with NewCustomerFilter().With…().
type CustomerFilter struct {
	CustomerIDs,
	BranchIDs []uuid.UUID
	CustomerTypeID *uuid.UUID
	// Code matches the import key exactly, ignoring case.
	Code     string
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewCustomerFilter() *CustomerFilter {
	return &CustomerFilter{}
}

func (q *CustomerFilter) WithCustomerIDs(customerIDs ...uuid.UUID) *CustomerFilter {
	q.CustomerIDs = customerIDs
	return q
}

func (q *CustomerFilter) WithBranchIDs(branchIDs ...uuid.UUID) *CustomerFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *CustomerFilter) WithCustomerTypeID(customerTypeID uuid.UUID) *CustomerFilter {
	q.CustomerTypeID = &customerTypeID
	return q
}

func (q *CustomerFilter) WithCode(code string) *CustomerFilter {
	q.Code = code
	return q
}

func (q *CustomerFilter) WithKeyword(keyword string) *CustomerFilter {
	q.Keyword = keyword
	return q
}

func (q *CustomerFilter) WithSortings(sortings ...Sorting) *CustomerFilter {
	q.Sortings = sortings
	return q
}

func (q *CustomerFilter) WithLimit(limit int64) *CustomerFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *CustomerFilter) WithPage(page int64) *CustomerFilter {
	q.Page = page
	return q
}
