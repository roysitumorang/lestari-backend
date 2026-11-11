package models

import (
	"errors"
	"time"
	"uuid"

	"github.com/goccy/go-json"
)

var (
	ErrProductNotFound = errors.New("product not found")
	// ErrProductConflict is a unique-constraint violation: another product has the same code or name.
	ErrProductConflict = errors.New("product with the same code or name already exists")
	// ErrProductInUse is returned when other records still reference the product.
	ErrProductInUse = errors.New("product is still used by other records")

	// MapProductSortings lists the fields a client may sort by (?sort=).
	MapProductSortings = map[string]struct{}{
		"id":                   {},
		"created_at":           {},
		"updated_at":           {},
		"image_url":            {},
		"name":                 {},
		"item_type":            {},
		"sku":                  {},
		"barcode":              {},
		"category_id":          {},
		"unit":                 {},
		"selling_price":        {},
		"purchase_price":       {},
		"price_type":           {},
		"tax_id":               {},
		"discount_type":        {},
		"discount_value":       {},
		"wholesale_prices":     {},
		"customer_type_prices": {},
		"track_stock":          {},
		"stock":                {},
		"minimum_stock":        {},
		"shelf_location":       {},
		"weight_grams":         {},
		"track_imei":           {},
		"units":                {},
		"variants":             {},
		"ingredients":          {},
		"points_earned":        {},
		"points_to_redeem":     {},
		"show_in_online_store": {},
		"description":          {},
		"online_description":   {},
		"category.name":        {},
		"tax.name":             {},
	}
)

// Product is one row of products.
type Product struct {
	ID       uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID uuid.UUID `json:"-"`
	// Actor is who is saving; it is set by the controller and never stored on the row.
	Actor              Actor           `json:"-"`
	ImageURL           *string         `json:"image_url" example:"/uploads/kopi-susu.jpg"`
	Name               string          `json:"name" example:"Kopi Susu Gula Aren"`
	ItemType           string          `json:"item_type" example:"product" enums:"product,service"`
	SKU                string          `json:"sku" example:"P000001"`
	Barcode            *string         `json:"barcode" example:"8991234567890"`
	CategoryID         *uuid.UUID      `json:"category_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Category           *Reference      `json:"category"`
	Unit               *string         `json:"unit" example:"cup"`
	SellingPrice       float64         `json:"selling_price" example:"18000"`
	PurchasePrice      float64         `json:"purchase_price" example:"9500"`
	PriceType          string          `json:"price_type" example:"single" enums:"single,wholesale,customer_type"`
	TaxID              *uuid.UUID      `json:"tax_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Tax                *Reference      `json:"tax"`
	DiscountType       *string         `json:"discount_type" example:"percent" enums:"percent,amount"`
	DiscountValue      *float64        `json:"discount_value" example:"10"`
	WholesalePrices    json.RawMessage `json:"wholesale_prices" swaggertype:"array,object"`
	CustomerTypePrices json.RawMessage `json:"customer_type_prices" swaggertype:"array,object"`
	TrackStock         bool            `json:"track_stock" example:"true"`
	Stock              float64         `json:"stock" example:"24"`
	MinimumStock       *float64        `json:"minimum_stock" example:"5"`
	ShelfLocation      *string         `json:"shelf_location" example:"A-3"`
	WeightGrams        *float64        `json:"weight_grams" example:"250"`
	TrackIMEI          bool            `json:"track_imei" example:"true"`
	Units              json.RawMessage `json:"units" swaggertype:"array,object"`
	Variants           json.RawMessage `json:"variants" swaggertype:"array,object"`
	Ingredients        json.RawMessage `json:"ingredients" swaggertype:"array,object"`
	PointsEarned       *float64        `json:"points_earned" example:"1"`
	PointsToRedeem     *float64        `json:"points_to_redeem" example:"20"`
	ShowInOnlineStore  bool            `json:"show_in_online_store" example:"true"`
	Description        *string         `json:"description" example:"Iced milk coffee with palm sugar"`
	OnlineDescription  *string         `json:"online_description" example:"Best seller"`
	CreatedAt          time.Time       `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt          time.Time       `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// ProductRequest is the body of POST /products and PUT /products/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type ProductRequest struct {
	Nulls              `json:"-"`
	ImageURL           *string         `json:"image_url" example:"/uploads/kopi-susu.jpg"`
	Name               *string         `json:"name" example:"Kopi Susu Gula Aren"`
	ItemType           *string         `json:"item_type" example:"product" enums:"product,service"`
	SKU                *string         `json:"sku" example:"P000001"`
	Barcode            *string         `json:"barcode" example:"8991234567890"`
	CategoryID         *uuid.UUID      `json:"category_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Unit               *string         `json:"unit" example:"cup"`
	SellingPrice       *float64        `json:"selling_price" example:"18000" minimum:"0"`
	PurchasePrice      *float64        `json:"purchase_price" example:"9500" minimum:"0"`
	PriceType          *string         `json:"price_type" example:"single" enums:"single,wholesale,customer_type"`
	TaxID              *uuid.UUID      `json:"tax_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	DiscountType       *string         `json:"discount_type" example:"percent" enums:"percent,amount"`
	DiscountValue      *float64        `json:"discount_value" example:"10" minimum:"0"`
	WholesalePrices    json.RawMessage `json:"wholesale_prices" swaggertype:"array,object"`
	CustomerTypePrices json.RawMessage `json:"customer_type_prices" swaggertype:"array,object"`
	TrackStock         *bool           `json:"track_stock" example:"true"`
	Stock              *float64        `json:"stock" example:"24"`
	MinimumStock       *float64        `json:"minimum_stock" example:"5" minimum:"0"`
	ShelfLocation      *string         `json:"shelf_location" example:"A-3"`
	WeightGrams        *float64        `json:"weight_grams" example:"250" minimum:"0"`
	TrackIMEI          *bool           `json:"track_imei" example:"true"`
	Units              json.RawMessage `json:"units" swaggertype:"array,object"`
	Variants           json.RawMessage `json:"variants" swaggertype:"array,object"`
	Ingredients        json.RawMessage `json:"ingredients" swaggertype:"array,object"`
	PointsEarned       *float64        `json:"points_earned" example:"1" minimum:"0"`
	PointsToRedeem     *float64        `json:"points_to_redeem" example:"20" minimum:"0"`
	ShowInOnlineStore  *bool           `json:"show_in_online_store" example:"true"`
	Description        *string         `json:"description" example:"Iced milk coffee with palm sugar"`
	OnlineDescription  *string         `json:"online_description" example:"Best seller"`
}

// Apply copies the attributes present in the request onto the product.
func (q *ProductRequest) Apply(product *Product) *Product {
	if q.ImageURL != nil || q.IsNull("image_url") {
		product.ImageURL = q.ImageURL
	}
	if q.Name != nil {
		product.Name = *q.Name
	}
	if q.ItemType != nil {
		product.ItemType = *q.ItemType
	}
	if q.SKU != nil {
		product.SKU = *q.SKU
	}
	if q.Barcode != nil || q.IsNull("barcode") {
		product.Barcode = q.Barcode
	}
	if q.CategoryID != nil || q.IsNull("category_id") {
		product.CategoryID = q.CategoryID
	}
	if q.Unit != nil || q.IsNull("unit") {
		product.Unit = q.Unit
	}
	if q.SellingPrice != nil {
		product.SellingPrice = *q.SellingPrice
	}
	if q.PurchasePrice != nil {
		product.PurchasePrice = *q.PurchasePrice
	}
	if q.PriceType != nil {
		product.PriceType = *q.PriceType
	}
	if q.TaxID != nil || q.IsNull("tax_id") {
		product.TaxID = q.TaxID
	}
	if q.DiscountType != nil || q.IsNull("discount_type") {
		product.DiscountType = q.DiscountType
	}
	if q.DiscountValue != nil || q.IsNull("discount_value") {
		product.DiscountValue = q.DiscountValue
	}
	if q.WholesalePrices != nil {
		product.WholesalePrices = q.WholesalePrices
	}
	if q.CustomerTypePrices != nil {
		product.CustomerTypePrices = q.CustomerTypePrices
	}
	if q.TrackStock != nil {
		product.TrackStock = *q.TrackStock
	}
	if q.Stock != nil {
		product.Stock = *q.Stock
	}
	if q.MinimumStock != nil || q.IsNull("minimum_stock") {
		product.MinimumStock = q.MinimumStock
	}
	if q.ShelfLocation != nil || q.IsNull("shelf_location") {
		product.ShelfLocation = q.ShelfLocation
	}
	if q.WeightGrams != nil || q.IsNull("weight_grams") {
		product.WeightGrams = q.WeightGrams
	}
	if q.TrackIMEI != nil {
		product.TrackIMEI = *q.TrackIMEI
	}
	if q.Units != nil {
		product.Units = q.Units
	}
	if q.Variants != nil {
		product.Variants = q.Variants
	}
	if q.Ingredients != nil {
		product.Ingredients = q.Ingredients
	}
	if q.PointsEarned != nil || q.IsNull("points_earned") {
		product.PointsEarned = q.PointsEarned
	}
	if q.PointsToRedeem != nil || q.IsNull("points_to_redeem") {
		product.PointsToRedeem = q.PointsToRedeem
	}
	if q.ShowInOnlineStore != nil {
		product.ShowInOnlineStore = *q.ShowInOnlineStore
	}
	if q.Description != nil || q.IsNull("description") {
		product.Description = q.Description
	}
	if q.OnlineDescription != nil || q.IsNull("online_description") {
		product.OnlineDescription = q.OnlineDescription
	}
	return product
}

// ProductFilter selects products; build it with NewProductFilter().With…().
type ProductFilter struct {
	ProductIDs,
	BranchIDs []uuid.UUID
	ItemType    *string
	CategoryID  *uuid.UUID
	StockStatus string
	// SKU matches the import key exactly, ignoring case.
	SKU      string
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewProductFilter() *ProductFilter {
	return &ProductFilter{}
}

func (q *ProductFilter) WithProductIDs(productIDs ...uuid.UUID) *ProductFilter {
	q.ProductIDs = productIDs
	return q
}

func (q *ProductFilter) WithBranchIDs(branchIDs ...uuid.UUID) *ProductFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *ProductFilter) WithItemType(itemType string) *ProductFilter {
	q.ItemType = &itemType
	return q
}

func (q *ProductFilter) WithCategoryID(categoryID uuid.UUID) *ProductFilter {
	q.CategoryID = &categoryID
	return q
}

func (q *ProductFilter) WithStockStatus(stockStatus string) *ProductFilter {
	q.StockStatus = stockStatus
	return q
}

func (q *ProductFilter) WithSKU(sKU string) *ProductFilter {
	q.SKU = sKU
	return q
}

func (q *ProductFilter) WithKeyword(keyword string) *ProductFilter {
	q.Keyword = keyword
	return q
}

func (q *ProductFilter) WithSortings(sortings ...Sorting) *ProductFilter {
	q.Sortings = sortings
	return q
}

func (q *ProductFilter) WithLimit(limit int64) *ProductFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *ProductFilter) WithPage(page int64) *ProductFilter {
	q.Page = page
	return q
}
