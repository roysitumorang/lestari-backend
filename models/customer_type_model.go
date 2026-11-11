package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrCustomerTypeNotFound = errors.New("customer type not found")
	// ErrCustomerTypeConflict is a unique-constraint violation: another customer type has the same code or name.
	ErrCustomerTypeConflict = errors.New("customer type with the same code or name already exists")
	// ErrCustomerTypeInUse is returned when other records still reference the customer type.
	ErrCustomerTypeInUse = errors.New("customer type is still used by other records")

	// MapCustomerTypeSortings lists the fields a client may sort by (?sort=).
	MapCustomerTypeSortings = map[string]struct{}{
		"id":               {},
		"created_at":       {},
		"updated_at":       {},
		"name":             {},
		"discount_type":    {},
		"discount_value":   {},
		"point_multiplier": {},
		"customer_count":   {},
		"description":      {},
	}
)

// CustomerType is one row of customer_types.
type CustomerType struct {
	ID              uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID        uuid.UUID `json:"-"`
	Name            string    `json:"name" example:"Member"`
	DiscountType    *string   `json:"discount_type" example:"percent" enums:"percent,amount"`
	DiscountValue   *float64  `json:"discount_value" example:"5"`
	PointMultiplier *float64  `json:"point_multiplier" example:"2"`
	CustomerCount   int64     `json:"customer_count" example:"40"`
	Description     *string   `json:"description" example:"Loyal customers"`
	CreatedAt       time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt       time.Time `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// CustomerTypeRequest is the body of POST /customer-types and PUT /customer-types/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type CustomerTypeRequest struct {
	Nulls           `json:"-"`
	Name            *string  `json:"name" example:"Member"`
	DiscountType    *string  `json:"discount_type" example:"percent" enums:"percent,amount"`
	DiscountValue   *float64 `json:"discount_value" example:"5" minimum:"0"`
	PointMultiplier *float64 `json:"point_multiplier" example:"2" minimum:"0"`
	Description     *string  `json:"description" example:"Loyal customers"`
}

// Apply copies the attributes present in the request onto the customer type.
func (q *CustomerTypeRequest) Apply(customerType *CustomerType) *CustomerType {
	if q.Name != nil {
		customerType.Name = *q.Name
	}
	if q.DiscountType != nil || q.IsNull("discount_type") {
		customerType.DiscountType = q.DiscountType
	}
	if q.DiscountValue != nil || q.IsNull("discount_value") {
		customerType.DiscountValue = q.DiscountValue
	}
	if q.PointMultiplier != nil || q.IsNull("point_multiplier") {
		customerType.PointMultiplier = q.PointMultiplier
	}
	if q.Description != nil || q.IsNull("description") {
		customerType.Description = q.Description
	}
	return customerType
}

// CustomerTypeFilter selects customerTypes; build it with NewCustomerTypeFilter().With…().
type CustomerTypeFilter struct {
	CustomerTypeIDs,
	BranchIDs []uuid.UUID
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewCustomerTypeFilter() *CustomerTypeFilter {
	return &CustomerTypeFilter{}
}

func (q *CustomerTypeFilter) WithCustomerTypeIDs(customerTypeIDs ...uuid.UUID) *CustomerTypeFilter {
	q.CustomerTypeIDs = customerTypeIDs
	return q
}

func (q *CustomerTypeFilter) WithBranchIDs(branchIDs ...uuid.UUID) *CustomerTypeFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *CustomerTypeFilter) WithKeyword(keyword string) *CustomerTypeFilter {
	q.Keyword = keyword
	return q
}

func (q *CustomerTypeFilter) WithSortings(sortings ...Sorting) *CustomerTypeFilter {
	q.Sortings = sortings
	return q
}

func (q *CustomerTypeFilter) WithLimit(limit int64) *CustomerTypeFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *CustomerTypeFilter) WithPage(page int64) *CustomerTypeFilter {
	q.Page = page
	return q
}
