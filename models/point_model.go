package models

import (
	"time"
	"uuid"
)

const (
	PointTypeEarned   = "earned"
	PointTypeRedeemed = "redeemed"
	PointTypeAdjusted = "adjusted"
)

// PointChange adds Points (negative to take away) to the balance of a customer and records it in the ledger.
// The balance never goes below zero.
type PointChange struct {
	BranchID,
	CustomerID uuid.UUID
	Points int64
	Type,
	Reference,
	Note string
	At time.Time
}

// PointEntry is one line of a customer's point history.
type PointEntry struct {
	ID           uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CreatedAt    time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	Type         string    `json:"type" example:"earned" enums:"earned,redeemed,adjusted"`
	Points       int64     `json:"points" example:"12"`
	BalanceAfter int64     `json:"balance_after" example:"132"`
	Reference    *string   `json:"reference" example:"S/20261108/0007"`
	Note         *string   `json:"note" example:"Birthday bonus"`
}

// PointAdjustmentRequest is the body of POST /customers/{id}/points.
type PointAdjustmentRequest struct {
	Mode   string  `json:"mode" example:"add" enums:"add,subtract"`
	Points float64 `json:"points" example:"50" minimum:"1"`
	Note   string  `json:"note" example:"Birthday bonus"`
}

// PointsResponse is the balance after an adjustment.
type PointsResponse struct {
	Points int64 `json:"points" example:"170"`
}

// CustomerCode is a customer as printed on a QR card.
type CustomerCode struct {
	ID    uuid.UUID
	Name  string
	Code  string
	Phone *string
}

// PointImport is one row of the points import: the customer with this code gets (mode add) or ends up with
// (mode set) Points.
type PointImport struct {
	BranchID uuid.UUID
	Code     string
	Points   int64
	Set      bool
	At       time.Time
}
