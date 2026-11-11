package models

import (
	"errors"
	"time"
	"uuid"
)

const (
	StockOpnameStatusPending  = "pending"
	StockOpnameStatusApproved = "approved"
	StockOpnameStatusRejected = "rejected"
)

var (
	ErrStockOpnameNotFound = errors.New("stock opname not found")
	// ErrStockOpnameConflict is a unique-constraint violation: another stock opname has the same code or name.
	ErrStockOpnameConflict = errors.New("stock opname with the same code or name already exists")
	// ErrStockOpnameApproved is returned when deleting an approved stock opname, which has already changed stock.
	ErrStockOpnameApproved = errors.New("an approved stock opname cannot be deleted")
	// ErrStockOpnameInUse is returned when other records still reference the stock opname.
	ErrStockOpnameInUse = errors.New("stock opname is still used by other records")

	// MapStockOpnameSortings lists the fields a client may sort by (?sort=).
	MapStockOpnameSortings = map[string]struct{}{
		"id":                {},
		"created_at":        {},
		"updated_at":        {},
		"code":              {},
		"opname_date":       {},
		"performed_by_name": {},
		"performed_by_role": {},
		"status":            {},
		"note":              {},
		"reject_reason":     {},
		"item_count":        {},
		"difference_value":  {},
	}
)

// StockOpname is one row of stock_opnames.
type StockOpname struct {
	ID              uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID        uuid.UUID `json:"-"`
	Code            string    `json:"code" example:"SO/20261108/0001"`
	OpnameDate      string    `json:"opname_date" example:"2026-11-08"`
	PerformedByName string    `json:"performed_by_name" example:"Roy"`
	PerformedByRole string    `json:"performed_by_role" example:"owner" enums:"owner,administrator,manager,cashier"`
	Status          string    `json:"status" example:"pending" enums:"pending,approved,rejected"`
	Note            *string   `json:"note" example:"Monthly count"`
	RejectReason    *string   `json:"reject_reason" example:""`
	ItemCount       int64     `json:"item_count" example:"12"`
	DifferenceValue float64   `json:"difference_value" example:"-15000"`
	// Items are the counted products; they are sent with a single stock opname, not with the list.
	Items     []*StockOpnameItem `json:"items,omitempty"`
	CreatedAt time.Time          `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt time.Time          `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// StockOpnameItem is one counted product of a stock opname. The product name, the stock on record and the unit
// cost are copied when the opname is created, so later changes of the product do not alter it.
type StockOpnameItem struct {
	// Row is the 1-based position in the request, used in validation messages.
	Row           int        `json:"-"`
	ProductID     *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	ProductName   string     `json:"product_name" example:"Kopi Susu Gula Aren"`
	SKU           *string    `json:"sku" example:"P000001"`
	SystemStock   float64    `json:"system_stock" example:"24"`
	PhysicalStock float64    `json:"physical_stock" example:"22"`
	Difference    float64    `json:"difference" example:"-2"`
	UnitCost      float64    `json:"unit_cost" example:"9500"`
	Note          *string    `json:"note" example:"two cups broken"`
	// CurrentStock is the stock of the product now; approving sets it to PhysicalStock.
	CurrentStock float64 `json:"-"`
}

// StockOpnameItemRequest is one counted product in the body of POST /stock-opnames.
type StockOpnameItemRequest struct {
	ProductID     string  `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	PhysicalStock float64 `json:"physical_stock" example:"22" minimum:"0"`
	Note          string  `json:"note" example:"two cups broken"`
}

// StockOpnameRequest is the body of POST /stock-opnames. A stock opname cannot be edited: delete it and create a
// new one.
type StockOpnameRequest struct {
	Nulls           `json:"-"`
	OpnameDate      *string `json:"opname_date" example:"2026-11-08"`
	PerformedByName *string `json:"performed_by_name" example:"Roy"`
	PerformedByRole *string `json:"performed_by_role" example:"owner" enums:"owner,administrator,manager,cashier"`
	Note            *string `json:"note" example:"Monthly count"`
	// at least one counted product
	Items []StockOpnameItemRequest `json:"items"`
	// CountedItems are the validated items.
	CountedItems []*StockOpnameItem `json:"-" swaggerignore:"true"`
}

// Apply copies the attributes present in the request onto the stock opname.
func (q *StockOpnameRequest) Apply(stockOpname *StockOpname) *StockOpname {
	if q.OpnameDate != nil {
		stockOpname.OpnameDate = *q.OpnameDate
	}
	if q.PerformedByName != nil {
		stockOpname.PerformedByName = *q.PerformedByName
	}
	if q.PerformedByRole != nil {
		stockOpname.PerformedByRole = *q.PerformedByRole
	}
	if q.Note != nil || q.IsNull("note") {
		stockOpname.Note = q.Note
	}
	stockOpname.Items = q.CountedItems
	return stockOpname
}

// StockOpnameRejectRequest is the body of POST /stock-opnames/{id}/reject.
type StockOpnameRejectRequest struct {
	Reason string `json:"reason" example:"Counted during opening hours"`
}

// StockOpnameDecision approves or rejects a pending stock opname.
type StockOpnameDecision struct {
	StockOpnameID,
	BranchID uuid.UUID
	Status string
	Reason string
	Actor  Actor
	At     time.Time
}

// StockOpnameFilter selects stockOpnames; build it with NewStockOpnameFilter().With…().
type StockOpnameFilter struct {
	StockOpnameIDs,
	BranchIDs []uuid.UUID
	Status   *string
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewStockOpnameFilter() *StockOpnameFilter {
	return &StockOpnameFilter{}
}

func (q *StockOpnameFilter) WithStockOpnameIDs(stockOpnameIDs ...uuid.UUID) *StockOpnameFilter {
	q.StockOpnameIDs = stockOpnameIDs
	return q
}

func (q *StockOpnameFilter) WithBranchIDs(branchIDs ...uuid.UUID) *StockOpnameFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *StockOpnameFilter) WithStatus(status string) *StockOpnameFilter {
	q.Status = &status
	return q
}

func (q *StockOpnameFilter) WithKeyword(keyword string) *StockOpnameFilter {
	q.Keyword = keyword
	return q
}

func (q *StockOpnameFilter) WithSortings(sortings ...Sorting) *StockOpnameFilter {
	q.Sortings = sortings
	return q
}

func (q *StockOpnameFilter) WithLimit(limit int64) *StockOpnameFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *StockOpnameFilter) WithPage(page int64) *StockOpnameFilter {
	q.Page = page
	return q
}
