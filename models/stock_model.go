package models

import (
	"errors"
	"time"
	"uuid"
)

const (
	StockAdjustmentModeSet      = "set"
	StockAdjustmentModeAdd      = "add"
	StockAdjustmentModeSubtract = "subtract"

	StockMovementTypeAdjustment  = "adjustment"
	StockMovementTypeOpname      = "opname"
	StockMovementTypeTransferIn  = "transfer_in"
	StockMovementTypeTransferOut = "transfer_out"
)

var (
	ErrStockNotFound = errors.New("stock not found")

	// StockAdjustmentModes and StockAdjustmentReasons are the values POST /inventory/stocks/{product_id}/adjustments takes.
	StockAdjustmentModes   = []string{StockAdjustmentModeSet, StockAdjustmentModeAdd, StockAdjustmentModeSubtract}
	StockAdjustmentReasons = []string{"correction", "damaged", "expired", "lost", "received", "other"}

	// MapStockSortings lists the fields a client may sort by (?sort=).
	MapStockSortings = map[string]struct{}{
		"product_id":    {},
		"created_at":    {},
		"updated_at":    {},
		"image_url":     {},
		"name":          {},
		"sku":           {},
		"category_id":   {},
		"category_name": {},
		"stock":         {},
		"unit":          {},
		"minimum_stock": {},
		"unit_cost":     {},
		"stock_value":   {},
	}
)

// Stock is the stock level of one product that tracks stock.
type Stock struct {
	ProductID    uuid.UUID  `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID     uuid.UUID  `json:"-"`
	ImageURL     *string    `json:"image_url" example:"/uploads/kopi-susu.jpg"`
	Name         string     `json:"name" example:"Kopi Susu Gula Aren"`
	SKU          string     `json:"sku" example:"P000001"`
	CategoryID   *uuid.UUID `json:"category_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CategoryName *string    `json:"category_name" example:"Beverages"`
	Stock        float64    `json:"stock" example:"24"`
	Unit         *string    `json:"unit" example:"cup"`
	MinimumStock *float64   `json:"minimum_stock" example:"5"`
	UnitCost     float64    `json:"unit_cost" example:"9500"`
	StockValue   float64    `json:"stock_value" example:"228000"`
	CreatedAt    time.Time  `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt    time.Time  `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// StockFilter selects stock levels; build it with NewStockFilter().With…().
type StockFilter struct {
	ProductIDs,
	BranchIDs []uuid.UUID
	CategoryID  *uuid.UUID
	StockStatus string
	Keyword     string
	Sortings    []Sorting
	Limit,
	Page int64
}

func NewStockFilter() *StockFilter {
	return &StockFilter{}
}

func (q *StockFilter) WithProductIDs(productIDs ...uuid.UUID) *StockFilter {
	q.ProductIDs = productIDs
	return q
}

func (q *StockFilter) WithBranchIDs(branchIDs ...uuid.UUID) *StockFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *StockFilter) WithCategoryID(categoryID uuid.UUID) *StockFilter {
	q.CategoryID = &categoryID
	return q
}

// WithStockStatus keeps the rows that are in_stock, low (at or under the minimum) or out.
func (q *StockFilter) WithStockStatus(stockStatus string) *StockFilter {
	q.StockStatus = stockStatus
	return q
}

func (q *StockFilter) WithKeyword(keyword string) *StockFilter {
	q.Keyword = keyword
	return q
}

func (q *StockFilter) WithSortings(sortings ...Sorting) *StockFilter {
	q.Sortings = sortings
	return q
}

func (q *StockFilter) WithLimit(limit int64) *StockFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *StockFilter) WithPage(page int64) *StockFilter {
	q.Page = page
	return q
}

// StockMovement is one line of a product's stock history.
type StockMovement struct {
	ID             uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID       uuid.UUID `json:"-"`
	ProductID      uuid.UUID `json:"-"`
	Actor          Actor     `json:"-"`
	CreatedAt      time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	Type           string    `json:"type" example:"adjustment" enums:"adjustment,opname,sale,purchase,return,transfer_in,transfer_out"`
	Reference      string    `json:"reference" example:"Manual adjustment"`
	QuantityChange float64   `json:"quantity_change" example:"-2"`
	StockAfter     float64   `json:"stock_after" example:"22"`
	UnitCost       *float64  `json:"unit_cost" example:"9500"`
	ActorName      string    `json:"actor_name" example:"Roy"`
	Note           *string   `json:"note" example:"damaged: dropped on delivery"`
}

// StockMovementFilter selects the history of one product.
type StockMovementFilter struct {
	ProductID,
	BranchID uuid.UUID
	// StartDate and EndDate are YYYY-MM-DD or empty.
	StartDate,
	EndDate string
	Limit,
	Page int64
}

// StockChange adds Change (negative to take out) to the stock of a product and, when the product tracks stock,
// records the movement. At is the caller's time.Now().
type StockChange struct {
	BranchID,
	ProductID uuid.UUID
	Change float64
	Type,
	Reference string
	UnitCost *float64
	Actor    Actor
	Note     string
	At       time.Time
}

// StockAdjustmentRequest is the body of POST /inventory/stocks/{product_id}/adjustments.
type StockAdjustmentRequest struct {
	// set makes the stock exactly quantity; add and subtract change it by quantity
	Mode     string  `json:"mode" example:"set" enums:"set,add,subtract"`
	Quantity float64 `json:"quantity" example:"40" minimum:"0"`
	Reason   string  `json:"reason" example:"correction" enums:"correction,damaged,expired,lost,received,other"`
	// with mode add: the cost of the units received; the product's purchase price becomes the weighted average
	UnitCost *float64 `json:"unit_cost" example:"9500"`
	Note     string   `json:"note" example:"recount after closing"`
}

// StockAdjustment is a validated adjustment of one product.
type StockAdjustment struct {
	StockAdjustmentRequest
	BranchID,
	ProductID uuid.UUID
	Actor Actor
	At    time.Time
}

// StockImportRow is one row of the stock import file: the stock of the product with this SKU becomes Stock.
type StockImportRow struct {
	SKU      string
	Stock    *float64
	UnitCost *float64
	Note     string
}

// StockImport is one imported row with who imports it, where and when.
type StockImport struct {
	StockImportRow
	BranchID uuid.UUID
	Actor    Actor
	At       time.Time
}

// SKUResponse is the data of GET /products/generate-sku.
type SKUResponse struct {
	SKU string `json:"sku" example:"P000042"`
}

// AvailabilityResponse is the data of GET /products/check-sku.
type AvailabilityResponse struct {
	Available bool `json:"available" example:"true"`
}

const (
	ProductItemTypeProduct = "product"
	ProductItemTypeService = "service"
)

const (
	BarcodeSymbologyCode128 = "code128"
	BarcodeSymbologyQR      = "qr"

	PaperSizeA4         = "a4"
	PaperSizeLabel33x15 = "label_33x15"
	PaperSizeLabel50x25 = "label_50x25"
)

// BarcodeSheet is the layout of the printable barcode sheet.
type BarcodeSheet struct {
	Copies    int
	Symbology string
	ShowPrice bool
	PaperSize string
}
