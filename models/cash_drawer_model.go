package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrCashDrawerNotFound = errors.New("cash drawer not found")
	// ErrCashDrawerConflict is a unique-constraint violation: another cash drawer has the same code or name.
	ErrCashDrawerConflict = errors.New("cash drawer with the same code or name already exists")
	// ErrCashDrawerInUse is returned when other records still reference the cash drawer.
	ErrCashDrawerInUse = errors.New("cash drawer is still used by other records")

	// MapCashDrawerSortings lists the fields a client may sort by (?sort=).
	MapCashDrawerSortings = map[string]struct{}{
		"id":                   {},
		"created_at":           {},
		"updated_at":           {},
		"name":                 {},
		"is_active":            {},
		"current_shift_status": {},
	}
)

// CashDrawer is one row of cash_drawers.
type CashDrawer struct {
	ID                 uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID           uuid.UUID `json:"-"`
	Name               string    `json:"name" example:"Front counter"`
	IsActive           bool      `json:"is_active" example:"true"`
	CurrentShiftStatus string    `json:"current_shift_status" example:"closed"`
	CreatedAt          time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt          time.Time `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// CashDrawerRequest is the body of POST /cash-drawers and PUT /cash-drawers/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type CashDrawerRequest struct {
	Nulls    `json:"-"`
	Name     *string `json:"name" example:"Front counter"`
	IsActive *bool   `json:"is_active" example:"true"`
}

// Apply copies the attributes present in the request onto the cash drawer.
func (q *CashDrawerRequest) Apply(cashDrawer *CashDrawer) *CashDrawer {
	if q.Name != nil {
		cashDrawer.Name = *q.Name
	}
	if q.IsActive != nil {
		cashDrawer.IsActive = *q.IsActive
	}
	return cashDrawer
}

// CashDrawerFilter selects cashDrawers; build it with NewCashDrawerFilter().With…().
type CashDrawerFilter struct {
	CashDrawerIDs,
	BranchIDs []uuid.UUID
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewCashDrawerFilter() *CashDrawerFilter {
	return &CashDrawerFilter{}
}

func (q *CashDrawerFilter) WithCashDrawerIDs(cashDrawerIDs ...uuid.UUID) *CashDrawerFilter {
	q.CashDrawerIDs = cashDrawerIDs
	return q
}

func (q *CashDrawerFilter) WithBranchIDs(branchIDs ...uuid.UUID) *CashDrawerFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *CashDrawerFilter) WithKeyword(keyword string) *CashDrawerFilter {
	q.Keyword = keyword
	return q
}

func (q *CashDrawerFilter) WithSortings(sortings ...Sorting) *CashDrawerFilter {
	q.Sortings = sortings
	return q
}

func (q *CashDrawerFilter) WithLimit(limit int64) *CashDrawerFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *CashDrawerFilter) WithPage(page int64) *CashDrawerFilter {
	q.Page = page
	return q
}
