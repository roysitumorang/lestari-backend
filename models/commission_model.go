package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrCommissionNotFound = errors.New("commission rule not found")
	// ErrCommissionConflict is a unique-constraint violation: another commission rule has the same code or name.
	ErrCommissionConflict = errors.New("commission rule with the same code or name already exists")
	// ErrCommissionInUse is returned when other records still reference the commission rule.
	ErrCommissionInUse = errors.New("commission rule is still used by other records")

	// MapCommissionSortings lists the fields a client may sort by (?sort=).
	MapCommissionSortings = map[string]struct{}{
		"id":          {},
		"created_at":  {},
		"updated_at":  {},
		"name":        {},
		"basis":       {},
		"type":        {},
		"value":       {},
		"staff_ids":   {},
		"product_ids": {},
		"is_active":   {},
	}
)

// Commission is one row of commissions.
type Commission struct {
	ID         uuid.UUID   `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID   uuid.UUID   `json:"-"`
	Name       string      `json:"name" example:"Barista bonus"`
	Basis      string      `json:"basis" example:"product" enums:"product,transaction"`
	Type       string      `json:"type" example:"percent" enums:"percent,amount"`
	Value      float64     `json:"value" example:"2"`
	StaffIds   []uuid.UUID `json:"staff_ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a" swaggertype:"array,string"`
	ProductIds []uuid.UUID `json:"product_ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a" swaggertype:"array,string"`
	IsActive   bool        `json:"is_active" example:"true"`
	CreatedAt  time.Time   `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt  time.Time   `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// CommissionRequest is the body of POST /commissions and PUT /commissions/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type CommissionRequest struct {
	Nulls      `json:"-"`
	Name       *string     `json:"name" example:"Barista bonus"`
	Basis      *string     `json:"basis" example:"product" enums:"product,transaction"`
	Type       *string     `json:"type" example:"percent" enums:"percent,amount"`
	Value      *float64    `json:"value" example:"2" minimum:"0"`
	StaffIds   []uuid.UUID `json:"staff_ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a" swaggertype:"array,string"`
	ProductIds []uuid.UUID `json:"product_ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a" swaggertype:"array,string"`
	IsActive   *bool       `json:"is_active" example:"true"`
}

// Apply copies the attributes present in the request onto the commission rule.
func (q *CommissionRequest) Apply(commission *Commission) *Commission {
	if q.Name != nil {
		commission.Name = *q.Name
	}
	if q.Basis != nil {
		commission.Basis = *q.Basis
	}
	if q.Type != nil {
		commission.Type = *q.Type
	}
	if q.Value != nil {
		commission.Value = *q.Value
	}
	if q.StaffIds != nil {
		commission.StaffIds = q.StaffIds
	}
	if q.ProductIds != nil {
		commission.ProductIds = q.ProductIds
	}
	if q.IsActive != nil {
		commission.IsActive = *q.IsActive
	}
	return commission
}

// CommissionFilter selects commissions; build it with NewCommissionFilter().With…().
type CommissionFilter struct {
	CommissionIDs,
	BranchIDs []uuid.UUID
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewCommissionFilter() *CommissionFilter {
	return &CommissionFilter{}
}

func (q *CommissionFilter) WithCommissionIDs(commissionIDs ...uuid.UUID) *CommissionFilter {
	q.CommissionIDs = commissionIDs
	return q
}

func (q *CommissionFilter) WithBranchIDs(branchIDs ...uuid.UUID) *CommissionFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *CommissionFilter) WithKeyword(keyword string) *CommissionFilter {
	q.Keyword = keyword
	return q
}

func (q *CommissionFilter) WithSortings(sortings ...Sorting) *CommissionFilter {
	q.Sortings = sortings
	return q
}

func (q *CommissionFilter) WithLimit(limit int64) *CommissionFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *CommissionFilter) WithPage(page int64) *CommissionFilter {
	q.Page = page
	return q
}
