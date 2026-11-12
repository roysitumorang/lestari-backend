package models

import (
	"errors"
	"time"
	"uuid"
)

var (
	ErrPaymentAccountNotFound = errors.New("account not found")
	// ErrPaymentAccountConflict is a unique-constraint violation: another account has the same code or name.
	ErrPaymentAccountConflict = errors.New("account with the same code or name already exists")
	// ErrPaymentAccountInUse is returned when other records still reference the account.
	ErrPaymentAccountInUse = errors.New("account is still used by other records")

	// MapPaymentAccountSortings lists the fields a client may sort by (?sort=).
	MapPaymentAccountSortings = map[string]struct{}{
		"id":             {},
		"created_at":     {},
		"updated_at":     {},
		"type":           {},
		"provider":       {},
		"account_name":   {},
		"account_number": {},
		"qris_image_url": {},
		"is_active":      {},
	}
)

// PaymentAccount is one row of catalog_payment_accounts.
type PaymentAccount struct {
	ID            uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID      uuid.UUID `json:"-"`
	Type          string    `json:"type" example:"bank" enums:"bank,e_wallet,qris"`
	Provider      string    `json:"provider" example:"BCA"`
	AccountName   string    `json:"account_name" example:"Apotik Lestari"`
	AccountNumber *string   `json:"account_number" example:"1234567890"`
	QRISImageURL  *string   `json:"qris_image_url" example:"/uploads/qris.png"`
	IsActive      bool      `json:"is_active" example:"true"`
	CreatedAt     time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt     time.Time `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// PaymentAccountRequest is the body of POST /catalog/payment-accounts and PUT /catalog/payment-accounts/{id}. On update only the attributes present are changed;
// an attribute sent as null clears an optional value.
type PaymentAccountRequest struct {
	Nulls         `json:"-"`
	Type          *string `json:"type" example:"bank" enums:"bank,e_wallet,qris"`
	Provider      *string `json:"provider" example:"BCA"`
	AccountName   *string `json:"account_name" example:"Apotik Lestari"`
	AccountNumber *string `json:"account_number" example:"1234567890"`
	QRISImageURL  *string `json:"qris_image_url" example:"/uploads/qris.png"`
	IsActive      *bool   `json:"is_active" example:"true"`
}

// Apply copies the attributes present in the request onto the account.
func (q *PaymentAccountRequest) Apply(paymentAccount *PaymentAccount) *PaymentAccount {
	if q.Type != nil {
		paymentAccount.Type = *q.Type
	}
	if q.Provider != nil {
		paymentAccount.Provider = *q.Provider
	}
	if q.AccountName != nil {
		paymentAccount.AccountName = *q.AccountName
	}
	if q.AccountNumber != nil || q.IsNull("account_number") {
		paymentAccount.AccountNumber = q.AccountNumber
	}
	if q.QRISImageURL != nil || q.IsNull("qris_image_url") {
		paymentAccount.QRISImageURL = q.QRISImageURL
	}
	if q.IsActive != nil {
		paymentAccount.IsActive = *q.IsActive
	}
	return paymentAccount
}

// PaymentAccountFilter selects paymentAccounts; build it with NewPaymentAccountFilter().With…().
type PaymentAccountFilter struct {
	PaymentAccountIDs,
	BranchIDs []uuid.UUID
	Type     *string
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewPaymentAccountFilter() *PaymentAccountFilter {
	return &PaymentAccountFilter{}
}

func (q *PaymentAccountFilter) WithPaymentAccountIDs(paymentAccountIDs ...uuid.UUID) *PaymentAccountFilter {
	q.PaymentAccountIDs = paymentAccountIDs
	return q
}

func (q *PaymentAccountFilter) WithBranchIDs(branchIDs ...uuid.UUID) *PaymentAccountFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *PaymentAccountFilter) WithType(typeValue string) *PaymentAccountFilter {
	q.Type = &typeValue
	return q
}

func (q *PaymentAccountFilter) WithKeyword(keyword string) *PaymentAccountFilter {
	q.Keyword = keyword
	return q
}

func (q *PaymentAccountFilter) WithSortings(sortings ...Sorting) *PaymentAccountFilter {
	q.Sortings = sortings
	return q
}

func (q *PaymentAccountFilter) WithLimit(limit int64) *PaymentAccountFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *PaymentAccountFilter) WithPage(page int64) *PaymentAccountFilter {
	q.Page = page
	return q
}
