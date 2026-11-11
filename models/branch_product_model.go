package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrBranchProductNotFound = errors.New("branch product not found")

	// MapBranchProductSortings lists the fields a client may sort by (?sort=).
	MapBranchProductSortings = map[string]struct{}{
		"id":             {},
		"created_at":     {},
		"updated_at":     {},
		"branch_id":      {},
		"branch_name":    {},
		"product_name":   {},
		"sku":            {},
		"category_id":    {},
		"stock":          {},
		"selling_price":  {},
		"purchase_price": {},
		"is_available":   {},
	}
)

// BranchProduct is a product as one branch of the store sells it: its own prices and availability.
type BranchProduct struct {
	ID            uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID      uuid.UUID  `json:"branch_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
	BranchName    string     `json:"branch_name" example:"Lestari Laguboti"`
	ProductName   string     `json:"product_name" example:"Kopi Susu Gula Aren"`
	SKU           string     `json:"sku" example:"P000001"`
	CategoryID    *uuid.UUID `json:"category_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Stock         float64    `json:"stock" example:"24"`
	SellingPrice  float64    `json:"selling_price" example:"18000"`
	PurchasePrice float64    `json:"purchase_price" example:"9500"`
	IsAvailable   bool       `json:"is_available" example:"true"`
	CreatedAt     time.Time  `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt     time.Time  `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// BranchProductRequest is the body of PUT /branch-products/{id}. Only the attributes present are changed.
type BranchProductRequest struct {
	Nulls         `json:"-"`
	SellingPrice  *float64 `json:"selling_price" example:"18000" minimum:"0"`
	PurchasePrice *float64 `json:"purchase_price" example:"9500" minimum:"0"`
	IsAvailable   *bool    `json:"is_available" example:"true"`
}

// Apply copies the attributes present in the request onto the branch product.
func (q *BranchProductRequest) Apply(branchProduct *BranchProduct) *BranchProduct {
	if q.SellingPrice != nil {
		branchProduct.SellingPrice = *q.SellingPrice
	}
	if q.PurchasePrice != nil {
		branchProduct.PurchasePrice = *q.PurchasePrice
	}
	if q.IsAvailable != nil {
		branchProduct.IsAvailable = *q.IsAvailable
	}
	return branchProduct
}

// BranchPriceSyncRequest is the body of POST /branch-products/sync-prices.
type BranchPriceSyncRequest struct {
	// branches that take over the selling prices of the head office
	BranchIDs []string `json:"branch_ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
	// products to sync (ids of the head office or of the branch); all products when empty
	ProductIDs []string `json:"product_ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
}

// BranchPriceSync is a validated price sync from the head office.
type BranchPriceSync struct {
	StoreID,
	HeadOfficeID uuid.UUID
	BranchIDs,
	ProductIDs []uuid.UUID
	At time.Time
}

// UpdatedResponse is the data of the endpoints that change several rows at once.
type UpdatedResponse struct {
	Updated int64 `json:"updated" example:"12"`
}

// BranchProductFilter selects branch products; build it with NewBranchProductFilter().With…().
type BranchProductFilter struct {
	BranchProductIDs,
	// StoreIDs keeps the products of the (not deleted) branches of these stores.
	StoreIDs []uuid.UUID
	BranchID,
	CategoryID *uuid.UUID
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewBranchProductFilter() *BranchProductFilter {
	return &BranchProductFilter{}
}

func (q *BranchProductFilter) WithBranchProductIDs(branchProductIDs ...uuid.UUID) *BranchProductFilter {
	q.BranchProductIDs = branchProductIDs
	return q
}

func (q *BranchProductFilter) WithStoreIDs(storeIDs ...uuid.UUID) *BranchProductFilter {
	q.StoreIDs = storeIDs
	return q
}

func (q *BranchProductFilter) WithBranchID(branchID uuid.UUID) *BranchProductFilter {
	q.BranchID = &branchID
	return q
}

func (q *BranchProductFilter) WithCategoryID(categoryID uuid.UUID) *BranchProductFilter {
	q.CategoryID = &categoryID
	return q
}

func (q *BranchProductFilter) WithKeyword(keyword string) *BranchProductFilter {
	q.Keyword = keyword
	return q
}

func (q *BranchProductFilter) WithSortings(sortings ...Sorting) *BranchProductFilter {
	q.Sortings = sortings
	return q
}

func (q *BranchProductFilter) WithLimit(limit int64) *BranchProductFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *BranchProductFilter) WithPage(page int64) *BranchProductFilter {
	q.Page = page
	return q
}
