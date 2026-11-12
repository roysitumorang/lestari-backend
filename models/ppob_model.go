package models

import (
	"errors"
	"time"
	"uuid"
)

const (
	PPOBDepositStatusPending   = "pending"
	PPOBDepositStatusSuccess   = "success"
	PPOBDepositStatusCancelled = "cancelled"

	// PPOBMinimumDeposit and PPOBMaximumBalance bound a top-up of the PPOB balance.
	PPOBMinimumDeposit = 100000
	PPOBMaximumBalance = 2000000
)

var (
	ErrPPOBPriceNotFound   = errors.New("ppob price not found")
	ErrPPOBDepositNotFound = errors.New("ppob deposit not found")

	// MapPPOBPriceSortings lists the fields a client may sort by (?sort=).
	MapPPOBPriceSortings = map[string]struct{}{
		"id":             {},
		"created_at":     {},
		"updated_at":     {},
		"category":       {},
		"provider":       {},
		"product_code":   {},
		"product_name":   {},
		"is_active":      {},
		"base_price":     {},
		"selling_price":  {},
		"base_admin_fee": {},
		"admin_fee":      {},
	}
)

// PPOBPrice is one product of the PPOB price list of a branch. A prepaid product (pulsa, tokens, vouchers) has a
// cost and a selling price; a postpaid one (bills) has the provider's admin fee and the fee the branch charges.
type PPOBPrice struct {
	ID           uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID     uuid.UUID `json:"-"`
	Kind         string    `json:"-"`
	Category     string    `json:"category" example:"pulsa"`
	Provider     *string   `json:"provider" example:"Telkomsel"`
	ProductCode  string    `json:"product_code" example:"TSEL10"`
	ProductName  string    `json:"product_name" example:"Telkomsel 10.000"`
	IsActive     bool      `json:"is_active" example:"true"`
	BasePrice    *float64  `json:"base_price,omitempty" example:"10250"`
	SellingPrice *float64  `json:"selling_price,omitempty" example:"12000"`
	BaseAdminFee *float64  `json:"base_admin_fee,omitempty" example:"2500"`
	AdminFee     *float64  `json:"admin_fee,omitempty" example:"3500"`
	CreatedAt    time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt    time.Time `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
}

// PPOBPriceRequest is the body of PUT /ppob/prices/{kind}/{id}: selling_price for a prepaid product, admin_fee
// for a postpaid one. Only the attributes present are changed.
type PPOBPriceRequest struct {
	IsActive     *bool    `json:"is_active" example:"true"`
	SellingPrice *float64 `json:"selling_price" example:"12000" minimum:"0"`
	AdminFee     *float64 `json:"admin_fee" example:"3500" minimum:"0"`
}

// Apply copies the attributes present in the request onto the price.
func (q *PPOBPriceRequest) Apply(price *PPOBPrice) *PPOBPrice {
	if q.IsActive != nil {
		price.IsActive = *q.IsActive
	}
	if q.SellingPrice != nil {
		price.SellingPrice = q.SellingPrice
	}
	if q.AdminFee != nil {
		price.AdminFee = q.AdminFee
	}
	return price
}

// PPOBPriceFilter selects PPOB prices; build it with NewPPOBPriceFilter().With…().
type PPOBPriceFilter struct {
	PPOBPriceIDs,
	BranchIDs []uuid.UUID
	Kind        string
	Category    string
	ProductCode string
	Keyword     string
	Sortings    []Sorting
	Limit,
	Page int64
}

func NewPPOBPriceFilter() *PPOBPriceFilter {
	return &PPOBPriceFilter{}
}

func (q *PPOBPriceFilter) WithPPOBPriceIDs(ppobPriceIDs ...uuid.UUID) *PPOBPriceFilter {
	q.PPOBPriceIDs = ppobPriceIDs
	return q
}

func (q *PPOBPriceFilter) WithBranchIDs(branchIDs ...uuid.UUID) *PPOBPriceFilter {
	q.BranchIDs = branchIDs
	return q
}

func (q *PPOBPriceFilter) WithKind(kind string) *PPOBPriceFilter {
	q.Kind = kind
	return q
}

func (q *PPOBPriceFilter) WithCategory(category string) *PPOBPriceFilter {
	q.Category = category
	return q
}

// WithProductCode matches the product code exactly, ignoring case (the key of an import).
func (q *PPOBPriceFilter) WithProductCode(productCode string) *PPOBPriceFilter {
	q.ProductCode = productCode
	return q
}

func (q *PPOBPriceFilter) WithKeyword(keyword string) *PPOBPriceFilter {
	q.Keyword = keyword
	return q
}

func (q *PPOBPriceFilter) WithSortings(sortings ...Sorting) *PPOBPriceFilter {
	q.Sortings = sortings
	return q
}

func (q *PPOBPriceFilter) WithLimit(limit int64) *PPOBPriceFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *PPOBPriceFilter) WithPage(page int64) *PPOBPriceFilter {
	q.Page = page
	return q
}

// PPOBDeposit is a top-up of the PPOB balance of a branch.
type PPOBDeposit struct {
	ID                  uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID            uuid.UUID  `json:"-"`
	ReferenceID         string     `json:"reference_id" example:"PPOB-261108-482913"`
	Type                string     `json:"type" example:"top_up"`
	PaymentMethod       *string    `json:"payment_method" example:"qris"`
	Amount              float64    `json:"amount" example:"200000"`
	AdminFee            float64    `json:"admin_fee" example:"4000"`
	BalanceAfter        *float64   `json:"balance_after" example:"350000"`
	Status              string     `json:"status" example:"pending" enums:"pending,success,cancelled,expired"`
	PaymentURL          *string    `json:"payment_url" example:"https://pay.example.com/PPOB-261108-482913"`
	PaymentInstructions *string    `json:"payment_instructions" example:"Pay Rp 204000 via QRIS and include reference PPOB-261108-482913."`
	PaymentProofURL     *string    `json:"payment_proof_url" example:"/uploads/proof.jpg"`
	ExpiresAt           *time.Time `json:"expires_at" example:"2026-11-09T10:32:00+07:00"`
	CreatedAt           time.Time  `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
}

// PPOBDepositFilter selects the deposits of a branch. StartDate and EndDate are YYYY-MM-DD.
type PPOBDepositFilter struct {
	BranchID uuid.UUID
	StartDate,
	EndDate,
	Keyword string
	Limit,
	Page int64
}

// PPOBSummary is the summary of the deposit list.
type PPOBSummary struct {
	// deposits that arrived minus the cost of the PPOB sales
	Balance          float64 `json:"balance" example:"350000"`
	TransactionTotal float64 `json:"transaction_total" example:"1250000"`
}

// PPOBDepositRequest is the body of POST /ppob/deposits.
type PPOBDepositRequest struct {
	Amount        float64 `json:"amount" example:"200000" minimum:"100000"`
	PaymentMethod string  `json:"payment_method" example:"qris" enums:"virtual_account,qris,e_wallet,credit_card,convenience_store,manual_transfer"`
	// a voucher that waives the admin fee
	VoucherCode string `json:"voucher_code" example:"PPOBGRATIS"`
}

// PPOBSaleRequest is the body of POST /ppob/transactions.
type PPOBSaleRequest struct {
	PriceID        string `json:"price_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CustomerNumber string `json:"customer_number" example:"081234567890"`
	// the bill of a postpaid product, as the biller reports it
	BillAmount float64 `json:"bill_amount" example:"152000"`
}

// PPOBTransaction is a PPOB sale made by a cashier; its cost is charged to the PPOB balance of the branch.
type PPOBTransaction struct {
	ID             uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID       uuid.UUID `json:"-" db:"-"`
	ReferenceID    string    `json:"reference_id" example:"TRX261108103200123"`
	ProductName    string    `json:"product_name" example:"Telkomsel 10.000"`
	CustomerNumber string    `json:"customer_number" example:"081234567890"`
	BasePrice      float64   `json:"base_price" example:"10250"`
	SellingPrice   float64   `json:"selling_price" example:"12000"`
	Profit         float64   `json:"profit" example:"1750"`
	Status         string    `json:"status" example:"success"`
	Cashier        Actor     `json:"-" db:"-"`
	CreatedAt      time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
}
