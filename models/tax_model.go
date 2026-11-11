package models

import (
	"errors"
	"time"
	"uuid"
)

const (
	TaxSortingFieldName        = "name"
	TaxSortingFieldRate        = "rate"
	TaxSortingFieldIsInclusive = "is_inclusive"
	TaxSortingFieldIsActive    = "is_active"
)

var (
	ErrTaxNotFound = errors.New("tax not found")

	// MapTaxSortings lists the columns a client may sort by.
	MapTaxSortings = map[string]struct{}{
		SortingFieldID:             {},
		SortingFieldCreatedAt:      {},
		SortingFieldUpdatedAt:      {},
		TaxSortingFieldName:        {},
		TaxSortingFieldRate:        {},
		TaxSortingFieldIsInclusive: {},
		TaxSortingFieldIsActive:    {},
	}
	DefaultTaxSortBy = Sorting{Field: TaxSortingFieldName, Direction: SortingDirectionAsc}
)

// Tax is a sales tax of a branch.
type Tax struct {
	ID          uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID    uuid.UUID `json:"-"`
	Name        string    `json:"name" example:"PPN"`
	Rate        float64   `json:"rate" example:"11"`
	IsInclusive bool      `json:"is_inclusive" example:"false"`
	IsActive    bool      `json:"is_active" example:"true"`
	CreatedAt   time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt   time.Time `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// TaxRequest is the body of POST /taxes and PUT /taxes/{id}. On update only the attributes present are changed.
type TaxRequest struct {
	Name        *string  `json:"name" example:"PPN"`
	Rate        *float64 `json:"rate" example:"11" minimum:"0" maximum:"100"`
	IsInclusive *bool    `json:"is_inclusive" example:"false"`
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
