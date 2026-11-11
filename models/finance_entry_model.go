package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrFinanceEntryNotFound = errors.New("entry not found")
	// ErrFinanceEntryConflict is a unique-constraint violation: another entry has the same code or name.
	ErrFinanceEntryConflict = errors.New("entry with the same code or name already exists")
	// ErrFinanceEntryInUse is returned when other records still reference the entry.
	ErrFinanceEntryInUse = errors.New("entry is still used by other records")

	// MapFinanceEntrySortings lists the fields a client may sort by (?sort=).
	MapFinanceEntrySortings = map[string]struct{}{
		"id":               {},
		"created_at":       {},
		"updated_at":       {},
		"entry_date":       {},
		"type":             {},
		"category":         {},
		"amount":           {},
		"payment_method":   {},
		"cash_drawer_id":   {},
		"description":      {},
		"attachment_url":   {},
		"cash_drawer.name": {},
	}
)

// FinanceEntry is one row of finance_entries.
type FinanceEntry struct {
	ID            uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID      uuid.UUID  `json:"-"`
	EntryDate     string     `json:"entry_date" example:"2026-11-08"`
	Type          string     `json:"type" example:"expense" enums:"income,expense"`
	Category      string     `json:"category" example:"Rent"`
	Amount        float64    `json:"amount" example:"1500000"`
	PaymentMethod *string    `json:"payment_method" example:"cash" enums:"cash,debit,credit_card,qris,e_wallet,bank_transfer,other"`
	CashDrawerID  *uuid.UUID `json:"cash_drawer_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CashDrawer    *Reference `json:"cash_drawer"`
	Description   *string    `json:"description" example:"Shop rent for November"`
	AttachmentURL *string    `json:"attachment_url" example:"/uploads/receipt.jpg"`
	CreatedAt     time.Time  `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt     time.Time  `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// FinanceEntryRequest is the body of POST /finance-entries and PUT /finance-entries/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type FinanceEntryRequest struct {
	Nulls         `json:"-"`
	EntryDate     *string    `json:"entry_date" example:"2026-11-08"`
	Type          *string    `json:"type" example:"expense" enums:"income,expense"`
	Category      *string    `json:"category" example:"Rent"`
	Amount        *float64   `json:"amount" example:"1500000" minimum:"0"`
	PaymentMethod *string    `json:"payment_method" example:"cash" enums:"cash,debit,credit_card,qris,e_wallet,bank_transfer,other"`
	CashDrawerID  *uuid.UUID `json:"cash_drawer_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Description   *string    `json:"description" example:"Shop rent for November"`
	AttachmentURL *string    `json:"attachment_url" example:"/uploads/receipt.jpg"`
}

// Apply copies the attributes present in the request onto the entry.
func (q *FinanceEntryRequest) Apply(financeEntry *FinanceEntry) *FinanceEntry {
	if q.EntryDate != nil {
		financeEntry.EntryDate = *q.EntryDate
	}
	if q.Type != nil {
		financeEntry.Type = *q.Type
	}
	if q.Category != nil {
		financeEntry.Category = *q.Category
	}
	if q.Amount != nil {
		financeEntry.Amount = *q.Amount
	}
	if q.PaymentMethod != nil || q.IsNull("payment_method") {
		financeEntry.PaymentMethod = q.PaymentMethod
	}
	if q.CashDrawerID != nil || q.IsNull("cash_drawer_id") {
		financeEntry.CashDrawerID = q.CashDrawerID
	}
	if q.Description != nil || q.IsNull("description") {
		financeEntry.Description = q.Description
	}
	if q.AttachmentURL != nil || q.IsNull("attachment_url") {
		financeEntry.AttachmentURL = q.AttachmentURL
	}
	return financeEntry
}

// FinanceEntryFilter selects financeEntries; build it with NewFinanceEntryFilter().With…().
type FinanceEntryFilter struct {
	FinanceEntryIDs,
	BranchIDs []uuid.UUID
	Type      *string
	StartDate string
	EndDate   string
	Keyword   string
	Sortings  []Sorting
	Limit,
	Page int64
}

func NewFinanceEntryFilter() *FinanceEntryFilter {
	return &FinanceEntryFilter{}
}

func (q *FinanceEntryFilter) WithFinanceEntryIDs(financeEntryIDs ...uuid.UUID) *FinanceEntryFilter {
	q.FinanceEntryIDs = financeEntryIDs
	return q
}

func (q *FinanceEntryFilter) WithBranchIDs(branchIDs ...uuid.UUID) *FinanceEntryFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *FinanceEntryFilter) WithType(typeValue string) *FinanceEntryFilter {
	q.Type = &typeValue
	return q
}

func (q *FinanceEntryFilter) WithStartDate(startDate string) *FinanceEntryFilter {
	q.StartDate = startDate
	return q
}

func (q *FinanceEntryFilter) WithEndDate(endDate string) *FinanceEntryFilter {
	q.EndDate = endDate
	return q
}

func (q *FinanceEntryFilter) WithKeyword(keyword string) *FinanceEntryFilter {
	q.Keyword = keyword
	return q
}

func (q *FinanceEntryFilter) WithSortings(sortings ...Sorting) *FinanceEntryFilter {
	q.Sortings = sortings
	return q
}

func (q *FinanceEntryFilter) WithLimit(limit int64) *FinanceEntryFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *FinanceEntryFilter) WithPage(page int64) *FinanceEntryFilter {
	q.Page = page
	return q
}
