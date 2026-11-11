package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrTaxNotFound = errors.New("tax not found")
	// ErrTaxConflict is a unique-constraint violation: another tax has the same code or name.
	ErrTaxConflict = errors.New("tax with the same code or name already exists")
	// ErrTaxInUse is returned when other records still reference the tax.
	ErrTaxInUse = errors.New("tax is still used by other records")

	// MapTaxSortings lists the fields a client may sort by (?sort=).
	MapTaxSortings = map[string]struct{}{
		"id":           {},
		"created_at":   {},
		"updated_at":   {},
		"name":         {},
		"rate":         {},
		"is_inclusive": {},
		"is_active":    {},
	}
)

// Tax is one row of taxes.
type Tax struct {
	ID          uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID    uuid.UUID `json:"-"`
	Name        string    `json:"name" example:"PPN"`
	Rate        float64   `json:"rate" example:"11"`
	IsInclusive bool      `json:"is_inclusive" example:"true"`
	IsActive    bool      `json:"is_active" example:"true"`
	CreatedAt   time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt   time.Time `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// TaxRequest is the body of POST /taxes and PUT /taxes/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type TaxRequest struct {
	Nulls       `json:"-"`
	Name        *string  `json:"name" example:"PPN"`
	Rate        *float64 `json:"rate" example:"11" minimum:"0" maximum:"100"`
	IsInclusive *bool    `json:"is_inclusive" example:"true"`
	IsActive    *bool    `json:"is_active" example:"true"`
}

// Apply copies the attributes present in the request onto the tax.
func (q *TaxRequest) Apply(tax *Tax) *Tax {
	if q.Name != nil {
		tax.Name = *q.Name
	}
	if q.Rate != nil {
		tax.Rate = *q.Rate
	}
	if q.IsInclusive != nil {
		tax.IsInclusive = *q.IsInclusive
	}
	if q.IsActive != nil {
		tax.IsActive = *q.IsActive
	}
	return tax
}

// TaxFilter selects taxes; build it with NewTaxFilter().With…().
type TaxFilter struct {
	TaxIDs,
	BranchIDs []uuid.UUID
	IsActive *bool
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewTaxFilter() *TaxFilter {
	return &TaxFilter{}
}

func (q *TaxFilter) WithTaxIDs(taxIDs ...uuid.UUID) *TaxFilter {
	q.TaxIDs = taxIDs
	return q
}

func (q *TaxFilter) WithBranchIDs(branchIDs ...uuid.UUID) *TaxFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *TaxFilter) WithIsActive(isActive bool) *TaxFilter {
	q.IsActive = &isActive
	return q
}

func (q *TaxFilter) WithKeyword(keyword string) *TaxFilter {
	q.Keyword = keyword
	return q
}

func (q *TaxFilter) WithSortings(sortings ...Sorting) *TaxFilter {
	q.Sortings = sortings
	return q
}

func (q *TaxFilter) WithLimit(limit int64) *TaxFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *TaxFilter) WithPage(page int64) *TaxFilter {
	q.Page = page
	return q
}
