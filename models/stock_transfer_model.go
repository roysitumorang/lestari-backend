package models

import (
	"errors"
	"time"
	"uuid"
)

const (
	StockTransferStatusPending   = "pending"
	StockTransferStatusCompleted = "completed"
	StockTransferStatusAccepted  = "accepted"
	StockTransferStatusRejected  = "rejected"

	StockTransferDirectionIn  = "in"
	StockTransferDirectionOut = "out"

	// StockTransferRoleHeadOffice selects the transfers the head office takes part in, StockTransferRoleBranch
	// the ones between other branches.
	StockTransferRoleHeadOffice = "head_office"
	StockTransferRoleBranch     = "branch"
)

var (
	ErrStockTransferNotFound = errors.New("stock transfer not found")

	// MapStockTransferSortings lists the fields a client may sort by (?sort=).
	MapStockTransferSortings = map[string]struct{}{
		"id":                    {},
		"created_at":            {},
		"updated_at":            {},
		"transfer_number":       {},
		"from_branch_id":        {},
		"to_branch_id":          {},
		"transfer_date":         {},
		"requires_confirmation": {},
		"status":                {},
		"note":                  {},
		"reject_reason":         {},
		"total_quantity":        {},
		"item_count":            {},
		"from_branch.name":      {},
		"to_branch.name":        {},
	}
)

// StockTransfer is one row of stock_transfers: stock sent from one branch of the store to another.
type StockTransfer struct {
	ID      uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	StoreID uuid.UUID `json:"-"`
	// Actor is who is saving; it is set by the controller and never stored on the row.
	Actor                Actor      `json:"-"`
	TransferNumber       string     `json:"transfer_number" example:"TRF/20261108/0001"`
	FromBranchID         uuid.UUID  `json:"from_branch_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	FromBranch           *Reference `json:"from_branch"`
	ToBranchID           uuid.UUID  `json:"to_branch_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
	ToBranch             *Reference `json:"to_branch"`
	TransferDate         string     `json:"transfer_date" example:"2026-11-08"`
	RequiresConfirmation bool       `json:"requires_confirmation" example:"true"`
	Status               string     `json:"status" example:"pending" enums:"pending,completed,accepted,rejected"`
	Note                 *string    `json:"note" example:"Weekly restock"`
	RejectReason         *string    `json:"reject_reason" example:""`
	TotalQuantity        float64    `json:"total_quantity" example:"24"`
	ItemCount            int64      `json:"item_count" example:"3"`
	// out for the sending branch, in for the receiving one
	Direction      string `json:"direction" example:"out" enums:"in,out"`
	FromBranchName string `json:"from_branch_name" example:"Lestari Balige"`
	ToBranchName   string `json:"to_branch_name" example:"Lestari Laguboti"`
	// Items are the products sent; they come with a single stock transfer, not with the list.
	Items     []*StockTransferItem `json:"items,omitempty"`
	CreatedAt time.Time            `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt time.Time            `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// SeenFrom fills the attributes that depend on who looks at the transfer.
func (q *StockTransfer) SeenFrom(branchID uuid.UUID) *StockTransfer {
	q.Direction = StockTransferDirectionIn
	if q.FromBranchID == branchID {
		q.Direction = StockTransferDirectionOut
	}
	if q.FromBranch != nil {
		q.FromBranchName = q.FromBranch.Name
	}
	if q.ToBranch != nil {
		q.ToBranchName = q.ToBranch.Name
	}
	return q
}

// StockTransferItem is one product of a stock transfer. Name and SKU are copied when the transfer is created.
type StockTransferItem struct {
	// Row is the 1-based position in the request, used in validation messages.
	Row         int        `json:"-"`
	ProductID   *uuid.UUID `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	ProductName string     `json:"product_name" example:"Kopi Susu Gula Aren"`
	SKU         *string    `json:"sku" example:"P000001"`
	Quantity    float64    `json:"quantity" example:"12"`
	// null until the receiving branch confirms
	ReceivedQuantity *float64 `json:"received_quantity" example:"12"`
	Note             *string  `json:"note" example:"fragile"`
}

// StockTransferItemRequest is one product in the body of POST /stock-transfers.
type StockTransferItemRequest struct {
	ProductID string  `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Quantity  float64 `json:"quantity" example:"12"`
	Note      string  `json:"note" example:"fragile"`
}

// StockTransferRequest is the body of POST /stock-transfers. A transfer cannot be edited: delete it while it is
// pending and create a new one.
type StockTransferRequest struct {
	ToBranchID   string `json:"to_branch_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
	TransferDate string `json:"transfer_date" example:"2026-11-08"`
	// true: the stock arrives when the receiving branch accepts; false: it arrives at once
	RequiresConfirmation bool   `json:"requires_confirmation" example:"true"`
	Note                 string `json:"note" example:"Weekly restock"`
	// at least one product
	Items []StockTransferItemRequest `json:"items"`
}

// StockTransferReceivedItem is the quantity of one product that arrived.
type StockTransferReceivedItem struct {
	ProductID        string  `json:"product_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	ReceivedQuantity float64 `json:"received_quantity" example:"10"`
}

// StockTransferAcceptRequest is the body of POST /stock-transfers/{id}/accept. A product that is not listed was
// received in full; what did not arrive goes back to the sending branch.
type StockTransferAcceptRequest struct {
	Items []StockTransferReceivedItem `json:"items"`
	Note  string                      `json:"note" example:"two cartons missing"`
}

// StockTransferRejectRequest is the body of POST /stock-transfers/{id}/reject.
type StockTransferRejectRequest struct {
	Reason string `json:"reason" example:"Not ordered"`
}

// StockTransferDecision accepts, rejects or cancels a pending transfer on behalf of the active branch.
type StockTransferDecision struct {
	StockTransferID,
	StoreID,
	BranchID uuid.UUID
	// Received holds the quantities that arrived, by product (accept only).
	Received map[uuid.UUID]float64
	Note     string
	Reason   string
	Actor    Actor
	At       time.Time
}

// StockTransferFilter selects stock transfers; build it with NewStockTransferFilter().With…().
type StockTransferFilter struct {
	StockTransferIDs,
	StoreIDs,
	// PartyBranchIDs keeps the transfers one of these branches sends or receives.
	PartyBranchIDs,
	FromBranchIDs,
	ToBranchIDs []uuid.UUID
	// Role, with HeadOfficeID, is head_office or branch.
	Role         string
	HeadOfficeID uuid.UUID
	Status       *string
	// StartDate and EndDate are YYYY-MM-DD or empty.
	StartDate,
	EndDate string
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewStockTransferFilter() *StockTransferFilter {
	return &StockTransferFilter{}
}

func (q *StockTransferFilter) WithStockTransferIDs(stockTransferIDs ...uuid.UUID) *StockTransferFilter {
	q.StockTransferIDs = stockTransferIDs
	return q
}

func (q *StockTransferFilter) WithStoreIDs(storeIDs ...uuid.UUID) *StockTransferFilter {
	q.StoreIDs = storeIDs
	return q
}

func (q *StockTransferFilter) WithPartyBranchIDs(branchIDs ...uuid.UUID) *StockTransferFilter {
	q.PartyBranchIDs = branchIDs
	return q
}

func (q *StockTransferFilter) WithFromBranchIDs(branchIDs ...uuid.UUID) *StockTransferFilter {
	q.FromBranchIDs = branchIDs
	return q
}

func (q *StockTransferFilter) WithToBranchIDs(branchIDs ...uuid.UUID) *StockTransferFilter {
	q.ToBranchIDs = branchIDs
	return q
}

func (q *StockTransferFilter) WithRole(role string, headOfficeID uuid.UUID) *StockTransferFilter {
	q.Role = role
	q.HeadOfficeID = headOfficeID
	return q
}

func (q *StockTransferFilter) WithStatus(status string) *StockTransferFilter {
	q.Status = &status
	return q
}

func (q *StockTransferFilter) WithStartDate(startDate string) *StockTransferFilter {
	q.StartDate = startDate
	return q
}

func (q *StockTransferFilter) WithEndDate(endDate string) *StockTransferFilter {
	q.EndDate = endDate
	return q
}

func (q *StockTransferFilter) WithKeyword(keyword string) *StockTransferFilter {
	q.Keyword = keyword
	return q
}

func (q *StockTransferFilter) WithSortings(sortings ...Sorting) *StockTransferFilter {
	q.Sortings = sortings
	return q
}

func (q *StockTransferFilter) WithLimit(limit int64) *StockTransferFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *StockTransferFilter) WithPage(page int64) *StockTransferFilter {
	q.Page = page
	return q
}
