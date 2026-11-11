package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrCategoryNotFound = errors.New("category not found")
	// ErrCategoryConflict is a unique-constraint violation: another category has the same code or name.
	ErrCategoryConflict = errors.New("category with the same code or name already exists")
	// ErrCategoryInUse is returned when other records still reference the category.
	ErrCategoryInUse = errors.New("category is still used by other records")

	// MapCategorySortings lists the fields a client may sort by (?sort=).
	MapCategorySortings = map[string]struct{}{
		"id":            {},
		"created_at":    {},
		"updated_at":    {},
		"name":          {},
		"color":         {},
		"sort_order":    {},
		"product_count": {},
		"description":   {},
	}
)

// Category is one row of categories.
type Category struct {
	ID           uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID     uuid.UUID `json:"-"`
	Name         string    `json:"name" example:"Beverages"`
	Color        *string   `json:"color" example:"#0F766E"`
	SortOrder    *int64    `json:"sort_order" example:"1"`
	ProductCount int64     `json:"product_count" example:"12"`
	Description  *string   `json:"description" example:"Cold and hot drinks"`
	CreatedAt    time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt    time.Time `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// CategoryRequest is the body of POST /categories and PUT /categories/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type CategoryRequest struct {
	Nulls       `json:"-"`
	Name        *string `json:"name" example:"Beverages"`
	Color       *string `json:"color" example:"#0F766E"`
	SortOrder   *int64  `json:"sort_order" example:"1"`
	Description *string `json:"description" example:"Cold and hot drinks"`
}

// Apply copies the attributes present in the request onto the category.
func (q *CategoryRequest) Apply(category *Category) *Category {
	if q.Name != nil {
		category.Name = *q.Name
	}
	if q.Color != nil || q.IsNull("color") {
		category.Color = q.Color
	}
	if q.SortOrder != nil || q.IsNull("sort_order") {
		category.SortOrder = q.SortOrder
	}
	if q.Description != nil || q.IsNull("description") {
		category.Description = q.Description
	}
	return category
}

// CategoryFilter selects categories; build it with NewCategoryFilter().With…().
type CategoryFilter struct {
	CategoryIDs,
	BranchIDs []uuid.UUID
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewCategoryFilter() *CategoryFilter {
	return &CategoryFilter{}
}

func (q *CategoryFilter) WithCategoryIDs(categoryIDs ...uuid.UUID) *CategoryFilter {
	q.CategoryIDs = categoryIDs
	return q
}

func (q *CategoryFilter) WithBranchIDs(branchIDs ...uuid.UUID) *CategoryFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *CategoryFilter) WithKeyword(keyword string) *CategoryFilter {
	q.Keyword = keyword
	return q
}

func (q *CategoryFilter) WithSortings(sortings ...Sorting) *CategoryFilter {
	q.Sortings = sortings
	return q
}

func (q *CategoryFilter) WithLimit(limit int64) *CategoryFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *CategoryFilter) WithPage(page int64) *CategoryFilter {
	q.Page = page
	return q
}
