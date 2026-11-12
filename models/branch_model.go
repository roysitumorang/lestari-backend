package models

import (
	"errors"
	"time"
	"uuid"
)

const (
	// BranchRenewalMonthly is the monthly price of one branch; a year gets 15% off.
	BranchRenewalMonthly = 99000.0

	JobKindCopyProducts = "copy_products"
)

var (
	ErrBranchNotFound = errors.New("branch not found")

	// MapBranchSortings lists the fields a client may sort by (?sort=).
	MapBranchSortings = map[string]struct{}{
		"id":                      {},
		"created_at":              {},
		"updated_at":              {},
		"name":                    {},
		"code":                    {},
		"manager_name":            {},
		"phone":                   {},
		"email":                   {},
		"address":                 {},
		"is_head_office":          {},
		"subscription_expires_at": {},
	}
)

// Branch is one outlet of a store. Every store has a head office; other branches have a login of their own.
type Branch struct {
	ID                    uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	StoreID               uuid.UUID  `json:"-"`
	Name                  string     `json:"name" example:"Lestari Laguboti"`
	Code                  *string    `json:"code" example:"LGB"`
	ManagerName           *string    `json:"manager_name" example:"Sari"`
	Phone                 *string    `json:"phone" example:"+6281277770000"`
	Email                 *string    `json:"email" example:"laguboti@lestari.id"`
	Address               *string    `json:"address" example:"Laguboti, Toba"`
	IsHeadOffice          bool       `json:"is_head_office" example:"false"`
	SubscriptionExpiresAt *time.Time `json:"subscription_expires_at" example:"2027-11-08T10:32:00+07:00"`
	CreatedAt             time.Time  `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt             time.Time  `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
	// Password is the new password of the branch login, when one was sent.
	Password string `json:"-" db:"-"`
}

// BranchRequest is the body of POST /branches and PUT /branches/{id}. On update only the attributes present
// are changed. Email and password are the login of the branch.
type BranchRequest struct {
	Nulls       `json:"-"`
	Name        *string `json:"name" example:"Lestari Laguboti"`
	Code        *string `json:"code" example:"LGB"`
	ManagerName *string `json:"manager_name" example:"Sari"`
	Phone       *string `json:"phone" example:"+6281277770000"`
	Email       *string `json:"email" example:"laguboti@lestari.id"`
	// base64 of the password, at least 8 characters; required on create
	Password *string `json:"password" example:"Y2FiYW5nMTIzNDU="`
	Address  *string `json:"address" example:"Laguboti, Toba"`
}

// Apply copies the attributes present in the request onto the branch.
func (q *BranchRequest) Apply(branch *Branch) *Branch {
	if q.Name != nil {
		branch.Name = *q.Name
	}
	if q.Code != nil || q.IsNull("code") {
		branch.Code = q.Code
	}
	if q.ManagerName != nil || q.IsNull("manager_name") {
		branch.ManagerName = q.ManagerName
	}
	if q.Phone != nil || q.IsNull("phone") {
		branch.Phone = q.Phone
	}
	if q.Email != nil {
		branch.Email = q.Email
	}
	if q.Address != nil || q.IsNull("address") {
		branch.Address = q.Address
	}
	if q.Password != nil {
		branch.Password = *q.Password
	}
	return branch
}

// BranchFilter selects branches that are not deleted; build it with NewBranchFilter().With…().
type BranchFilter struct {
	BranchIDs,
	StoreIDs []uuid.UUID
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewBranchFilter() *BranchFilter {
	return &BranchFilter{}
}

func (q *BranchFilter) WithBranchIDs(branchIDs ...uuid.UUID) *BranchFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *BranchFilter) WithStoreIDs(storeIDs ...uuid.UUID) *BranchFilter {
	q.StoreIDs = storeIDs
	return q
}

func (q *BranchFilter) WithKeyword(keyword string) *BranchFilter {
	q.Keyword = keyword
	return q
}

func (q *BranchFilter) WithSortings(sortings ...Sorting) *BranchFilter {
	q.Sortings = sortings
	return q
}

func (q *BranchFilter) WithLimit(limit int64) *BranchFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *BranchFilter) WithPage(page int64) *BranchFilter {
	q.Page = page
	return q
}

// CopyProductsRequest is the body of POST /branches/{id}/copy-products.
type CopyProductsRequest struct {
	// categories to copy; all when empty
	CategoryIDs []uuid.UUID `json:"category_ids" swaggertype:"array,string" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	// true: the copies start with the stock of the head office
	IncludeStock bool `json:"include_stock" example:"false"`
}

// ProductCopy copies the products of the head office to another branch of the store.
type ProductCopy struct {
	StoreID,
	HeadOfficeID,
	BranchID uuid.UUID
	CategoryIDs  []uuid.UUID
	IncludeStock bool
	At           time.Time
}

// BranchBilling is the subscription of one branch.
type BranchBilling struct {
	BranchID     uuid.UUID  `json:"branch_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	ID           uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchName   string     `json:"branch_name" example:"Lestari Laguboti"`
	Plan         string     `json:"plan" example:"pro"`
	ExpiresAt    *time.Time `json:"expires_at" example:"2027-11-08T10:32:00+07:00"`
	Status       string     `json:"status" example:"active" enums:"active,expiring,expired"`
	RenewalPrice float64    `json:"renewal_price" example:"99000"`
}

// BranchRenewalRequest is the body of POST /billing/branch-renewals.
type BranchRenewalRequest struct {
	BranchIDs     []uuid.UUID `json:"branch_ids" swaggertype:"array,string" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Period        string      `json:"period" example:"monthly" enums:"monthly,quarterly,yearly"`
	PaymentMethod string      `json:"payment_method" example:"qris" enums:"coin,virtual_account,qris,e_wallet,credit_card,convenience_store,manual_transfer"`
	PromoCode     string      `json:"promo_code" example:"HEMAT10"`
}
