package models

import (
	"errors"
	"time"
	"uuid"

	"github.com/goccy/go-json"
)

var (
	ErrPromotionNotFound = errors.New("promotion not found")
	// ErrPromotionConflict is a unique-constraint violation: another promotion has the same code or name.
	ErrPromotionConflict = errors.New("promotion with the same code or name already exists")
	// ErrPromotionInUse is returned when other records still reference the promotion.
	ErrPromotionInUse = errors.New("promotion is still used by other records")

	// MapPromotionSortings lists the fields a client may sort by (?sort=).
	MapPromotionSortings = map[string]struct{}{
		"id":                {},
		"created_at":        {},
		"updated_at":        {},
		"name":              {},
		"type":              {},
		"start_date":        {},
		"end_date":          {},
		"start_time":        {},
		"end_time":          {},
		"days":              {},
		"customer_type_ids": {},
		"discount_type":     {},
		"discount_value":    {},
		"minimum_purchase":  {},
		"buy_quantity":      {},
		"get_quantity":      {},
		"products":          {},
		"is_active":         {},
		"description":       {},
	}
)

// Promotion is one row of promotions.
type Promotion struct {
	ID              uuid.UUID       `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID        uuid.UUID       `json:"-"`
	Name            string          `json:"name" example:"Weekend deal"`
	Type            string          `json:"type" example:"product_discount" enums:"product_discount,buy_x_get_y,minimum_purchase"`
	StartDate       string          `json:"start_date" example:"2026-11-01"`
	EndDate         string          `json:"end_date" example:"2026-11-30"`
	StartTime       *string         `json:"start_time" example:"09:00"`
	EndTime         *string         `json:"end_time" example:"21:00"`
	Days            []string        `json:"days" example:"saturday,sunday"`
	CustomerTypeIds []uuid.UUID     `json:"customer_type_ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a" swaggertype:"array,string"`
	DiscountType    *string         `json:"discount_type" example:"percent" enums:"percent,amount"`
	DiscountValue   *float64        `json:"discount_value" example:"10"`
	MinimumPurchase *float64        `json:"minimum_purchase" example:"50000"`
	BuyQuantity     *float64        `json:"buy_quantity" example:"2"`
	GetQuantity     *float64        `json:"get_quantity" example:"1"`
	Products        json.RawMessage `json:"products" swaggertype:"array,object"`
	IsActive        bool            `json:"is_active" example:"true"`
	Description     *string         `json:"description" example:"10% off on weekends"`
	CreatedAt       time.Time       `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt       time.Time       `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// PromotionRequest is the body of POST /promotions and PUT /promotions/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type PromotionRequest struct {
	Nulls           `json:"-"`
	Name            *string         `json:"name" example:"Weekend deal"`
	Type            *string         `json:"type" example:"product_discount" enums:"product_discount,buy_x_get_y,minimum_purchase"`
	StartDate       *string         `json:"start_date" example:"2026-11-01"`
	EndDate         *string         `json:"end_date" example:"2026-11-30"`
	StartTime       *string         `json:"start_time" example:"09:00"`
	EndTime         *string         `json:"end_time" example:"21:00"`
	Days            []string        `json:"days" example:"saturday,sunday"`
	CustomerTypeIds []uuid.UUID     `json:"customer_type_ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a" swaggertype:"array,string"`
	DiscountType    *string         `json:"discount_type" example:"percent" enums:"percent,amount"`
	DiscountValue   *float64        `json:"discount_value" example:"10" minimum:"0"`
	MinimumPurchase *float64        `json:"minimum_purchase" example:"50000" minimum:"0"`
	BuyQuantity     *float64        `json:"buy_quantity" example:"2" minimum:"1"`
	GetQuantity     *float64        `json:"get_quantity" example:"1" minimum:"1"`
	Products        json.RawMessage `json:"products" swaggertype:"array,object"`
	IsActive        *bool           `json:"is_active" example:"true"`
	Description     *string         `json:"description" example:"10% off on weekends"`
}

// Apply copies the attributes present in the request onto the promotion.
func (q *PromotionRequest) Apply(promotion *Promotion) *Promotion {
	if q.Name != nil {
		promotion.Name = *q.Name
	}
	if q.Type != nil {
		promotion.Type = *q.Type
	}
	if q.StartDate != nil {
		promotion.StartDate = *q.StartDate
	}
	if q.EndDate != nil {
		promotion.EndDate = *q.EndDate
	}
	if q.StartTime != nil || q.IsNull("start_time") {
		promotion.StartTime = q.StartTime
	}
	if q.EndTime != nil || q.IsNull("end_time") {
		promotion.EndTime = q.EndTime
	}
	if q.Days != nil {
		promotion.Days = q.Days
	}
	if q.CustomerTypeIds != nil {
		promotion.CustomerTypeIds = q.CustomerTypeIds
	}
	if q.DiscountType != nil || q.IsNull("discount_type") {
		promotion.DiscountType = q.DiscountType
	}
	if q.DiscountValue != nil || q.IsNull("discount_value") {
		promotion.DiscountValue = q.DiscountValue
	}
	if q.MinimumPurchase != nil || q.IsNull("minimum_purchase") {
		promotion.MinimumPurchase = q.MinimumPurchase
	}
	if q.BuyQuantity != nil || q.IsNull("buy_quantity") {
		promotion.BuyQuantity = q.BuyQuantity
	}
	if q.GetQuantity != nil || q.IsNull("get_quantity") {
		promotion.GetQuantity = q.GetQuantity
	}
	if q.Products != nil {
		promotion.Products = q.Products
	}
	if q.IsActive != nil {
		promotion.IsActive = *q.IsActive
	}
	if q.Description != nil || q.IsNull("description") {
		promotion.Description = q.Description
	}
	return promotion
}

// PromotionFilter selects promotions; build it with NewPromotionFilter().With…().
type PromotionFilter struct {
	PromotionIDs,
	BranchIDs []uuid.UUID
	Status   string
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewPromotionFilter() *PromotionFilter {
	return &PromotionFilter{}
}

func (q *PromotionFilter) WithPromotionIDs(promotionIDs ...uuid.UUID) *PromotionFilter {
	q.PromotionIDs = promotionIDs
	return q
}

func (q *PromotionFilter) WithBranchIDs(branchIDs ...uuid.UUID) *PromotionFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *PromotionFilter) WithStatus(status string) *PromotionFilter {
	q.Status = status
	return q
}

func (q *PromotionFilter) WithKeyword(keyword string) *PromotionFilter {
	q.Keyword = keyword
	return q
}

func (q *PromotionFilter) WithSortings(sortings ...Sorting) *PromotionFilter {
	q.Sortings = sortings
	return q
}

func (q *PromotionFilter) WithLimit(limit int64) *PromotionFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *PromotionFilter) WithPage(page int64) *PromotionFilter {
	q.Page = page
	return q
}
