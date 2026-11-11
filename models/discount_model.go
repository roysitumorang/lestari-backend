package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrDiscountNotFound = errors.New("discount not found")
	// ErrDiscountConflict is a unique-constraint violation: another discount has the same code or name.
	ErrDiscountConflict = errors.New("discount with the same code or name already exists")
	// ErrDiscountInUse is returned when other records still reference the discount.
	ErrDiscountInUse = errors.New("discount is still used by other records")

	// MapDiscountSortings lists the fields a client may sort by (?sort=).
	MapDiscountSortings = map[string]struct{}{
		"id":         {},
		"created_at": {},
		"updated_at": {},
		"name":       {},
		"type":       {},
		"value":      {},
		"is_active":  {},
	}
)

// Discount is one row of discounts.
type Discount struct {
	ID        uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID  uuid.UUID `json:"-"`
	Name      string    `json:"name" example:"Member day"`
	Type      string    `json:"type" example:"percent" enums:"percent,amount"`
	Value     float64   `json:"value" example:"10"`
	IsActive  bool      `json:"is_active" example:"true"`
	CreatedAt time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt time.Time `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// DiscountRequest is the body of POST /discounts and PUT /discounts/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type DiscountRequest struct {
	Nulls    `json:"-"`
	Name     *string  `json:"name" example:"Member day"`
	Type     *string  `json:"type" example:"percent" enums:"percent,amount"`
	Value    *float64 `json:"value" example:"10" minimum:"0"`
	IsActive *bool    `json:"is_active" example:"true"`
}

// Apply copies the attributes present in the request onto the discount.
func (q *DiscountRequest) Apply(discount *Discount) *Discount {
	if q.Name != nil {
		discount.Name = *q.Name
	}
	if q.Type != nil {
		discount.Type = *q.Type
	}
	if q.Value != nil {
		discount.Value = *q.Value
	}
	if q.IsActive != nil {
		discount.IsActive = *q.IsActive
	}
	return discount
}

// DiscountFilter selects discounts; build it with NewDiscountFilter().With…().
type DiscountFilter struct {
	DiscountIDs,
	BranchIDs []uuid.UUID
	IsActive *bool
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewDiscountFilter() *DiscountFilter {
	return &DiscountFilter{}
}

func (q *DiscountFilter) WithDiscountIDs(discountIDs ...uuid.UUID) *DiscountFilter {
	q.DiscountIDs = discountIDs
	return q
}

func (q *DiscountFilter) WithBranchIDs(branchIDs ...uuid.UUID) *DiscountFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *DiscountFilter) WithIsActive(isActive bool) *DiscountFilter {
	q.IsActive = &isActive
	return q
}

func (q *DiscountFilter) WithKeyword(keyword string) *DiscountFilter {
	q.Keyword = keyword
	return q
}

func (q *DiscountFilter) WithSortings(sortings ...Sorting) *DiscountFilter {
	q.Sortings = sortings
	return q
}

func (q *DiscountFilter) WithLimit(limit int64) *DiscountFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *DiscountFilter) WithPage(page int64) *DiscountFilter {
	q.Page = page
	return q
}
