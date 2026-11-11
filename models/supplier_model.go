package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrSupplierNotFound = errors.New("supplier not found")
	// ErrSupplierConflict is a unique-constraint violation: another supplier has the same code or name.
	ErrSupplierConflict = errors.New("supplier with the same code or name already exists")
	// ErrSupplierInUse is returned when other records still reference the supplier.
	ErrSupplierInUse = errors.New("supplier is still used by other records")

	// MapSupplierSortings lists the fields a client may sort by (?sort=).
	MapSupplierSortings = map[string]struct{}{
		"id":              {},
		"created_at":      {},
		"updated_at":      {},
		"name":            {},
		"contact_person":  {},
		"phone":           {},
		"email":           {},
		"code":            {},
		"payable_balance": {},
		"address":         {},
	}
)

// Supplier is one row of suppliers.
type Supplier struct {
	ID             uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID       uuid.UUID `json:"-"`
	Name           string    `json:"name" example:"PT Sumber Makmur"`
	ContactPerson  *string   `json:"contact_person" example:"Budi"`
	Phone          *string   `json:"phone" example:"+6281234567890"`
	Email          *string   `json:"email" example:"sales@sumbermakmur.co.id"`
	Code           *string   `json:"code" example:"SUP-001"`
	PayableBalance float64   `json:"payable_balance" example:"250000"`
	Address        *string   `json:"address" example:"Jl. Merdeka No. 1, Medan"`
	CreatedAt      time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt      time.Time `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// SupplierRequest is the body of POST /suppliers and PUT /suppliers/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type SupplierRequest struct {
	Nulls         `json:"-"`
	Name          *string `json:"name" example:"PT Sumber Makmur"`
	ContactPerson *string `json:"contact_person" example:"Budi"`
	Phone         *string `json:"phone" example:"+6281234567890"`
	Email         *string `json:"email" example:"sales@sumbermakmur.co.id"`
	Code          *string `json:"code" example:"SUP-001"`
	Address       *string `json:"address" example:"Jl. Merdeka No. 1, Medan"`
}

// Apply copies the attributes present in the request onto the supplier.
func (q *SupplierRequest) Apply(supplier *Supplier) *Supplier {
	if q.Name != nil {
		supplier.Name = *q.Name
	}
	if q.ContactPerson != nil || q.IsNull("contact_person") {
		supplier.ContactPerson = q.ContactPerson
	}
	if q.Phone != nil || q.IsNull("phone") {
		supplier.Phone = q.Phone
	}
	if q.Email != nil || q.IsNull("email") {
		supplier.Email = q.Email
	}
	if q.Code != nil || q.IsNull("code") {
		supplier.Code = q.Code
	}
	if q.Address != nil || q.IsNull("address") {
		supplier.Address = q.Address
	}
	return supplier
}

// SupplierFilter selects suppliers; build it with NewSupplierFilter().With…().
type SupplierFilter struct {
	SupplierIDs,
	BranchIDs []uuid.UUID
	// Code matches the import key exactly, ignoring case.
	Code     string
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewSupplierFilter() *SupplierFilter {
	return &SupplierFilter{}
}

func (q *SupplierFilter) WithSupplierIDs(supplierIDs ...uuid.UUID) *SupplierFilter {
	q.SupplierIDs = supplierIDs
	return q
}

func (q *SupplierFilter) WithBranchIDs(branchIDs ...uuid.UUID) *SupplierFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *SupplierFilter) WithCode(code string) *SupplierFilter {
	q.Code = code
	return q
}

func (q *SupplierFilter) WithKeyword(keyword string) *SupplierFilter {
	q.Keyword = keyword
	return q
}

func (q *SupplierFilter) WithSortings(sortings ...Sorting) *SupplierFilter {
	q.Sortings = sortings
	return q
}

func (q *SupplierFilter) WithLimit(limit int64) *SupplierFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *SupplierFilter) WithPage(page int64) *SupplierFilter {
	q.Page = page
	return q
}
