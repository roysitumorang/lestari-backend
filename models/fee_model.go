package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrFeeNotFound = errors.New("fee not found")
	// ErrFeeConflict is a unique-constraint violation: another fee has the same code or name.
	ErrFeeConflict = errors.New("fee with the same code or name already exists")
	// ErrFeeInUse is returned when other records still reference the fee.
	ErrFeeInUse = errors.New("fee is still used by other records")

	// MapFeeSortings lists the fields a client may sort by (?sort=).
	MapFeeSortings = map[string]struct{}{
		"id":          {},
		"created_at":  {},
		"updated_at":  {},
		"name":        {},
		"amount_type": {},
		"amount":      {},
		"applies_to":  {},
	}
)

// Fee is one row of fees.
type Fee struct {
	ID         uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID   uuid.UUID `json:"-"`
	Name       string    `json:"name" example:"Packaging"`
	AmountType string    `json:"amount_type" example:"amount" enums:"percent,amount"`
	Amount     float64   `json:"amount" example:"2000"`
	AppliesTo  string    `json:"applies_to" example:"sale" enums:"sale,purchase,both"`
	CreatedAt  time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt  time.Time `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// FeeRequest is the body of POST /fees and PUT /fees/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type FeeRequest struct {
	Nulls      `json:"-"`
	Name       *string  `json:"name" example:"Packaging"`
	AmountType *string  `json:"amount_type" example:"amount" enums:"percent,amount"`
	Amount     *float64 `json:"amount" example:"2000" minimum:"0"`
	AppliesTo  *string  `json:"applies_to" example:"sale" enums:"sale,purchase,both"`
}

// Apply copies the attributes present in the request onto the fee.
func (q *FeeRequest) Apply(fee *Fee) *Fee {
	if q.Name != nil {
		fee.Name = *q.Name
	}
	if q.AmountType != nil {
		fee.AmountType = *q.AmountType
	}
	if q.Amount != nil {
		fee.Amount = *q.Amount
	}
	if q.AppliesTo != nil {
		fee.AppliesTo = *q.AppliesTo
	}
	return fee
}

// FeeFilter selects fees; build it with NewFeeFilter().With…().
type FeeFilter struct {
	FeeIDs,
	BranchIDs []uuid.UUID
	AppliesTo *string
	Keyword   string
	Sortings  []Sorting
	Limit,
	Page int64
}

func NewFeeFilter() *FeeFilter {
	return &FeeFilter{}
}

func (q *FeeFilter) WithFeeIDs(feeIDs ...uuid.UUID) *FeeFilter {
	q.FeeIDs = feeIDs
	return q
}

func (q *FeeFilter) WithBranchIDs(branchIDs ...uuid.UUID) *FeeFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *FeeFilter) WithAppliesTo(appliesTo string) *FeeFilter {
	q.AppliesTo = &appliesTo
	return q
}

func (q *FeeFilter) WithKeyword(keyword string) *FeeFilter {
	q.Keyword = keyword
	return q
}

func (q *FeeFilter) WithSortings(sortings ...Sorting) *FeeFilter {
	q.Sortings = sortings
	return q
}

func (q *FeeFilter) WithLimit(limit int64) *FeeFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *FeeFilter) WithPage(page int64) *FeeFilter {
	q.Page = page
	return q
}
