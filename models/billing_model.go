package models

import (
	"errors"
	"time"
	"uuid"

	"github.com/goccy/go-json"

	"github.com/roysitumorang/lestari-backend/i18n"
)

const (
	InvoiceKindCoinTopUp     = "coin_topup"
	InvoiceKindSubscription  = "subscription"
	InvoiceKindPlugin        = "plugin"
	InvoiceKindBranchRenewal = "branch_renewal"
	InvoiceKindStorage       = "storage"

	InvoiceStatusPending   = "pending"
	InvoiceStatusPaid      = "paid"
	InvoiceStatusCancelled = "cancelled"
	InvoiceStatusExpired   = "expired"

	PaymentMethodCoin           = "coin"
	PaymentMethodCreditCard     = "credit_card"
	PaymentMethodManualTransfer = "manual_transfer"

	VoucherRewardCoin             = "coin"
	VoucherRewardSubscriptionDays = "subscription_days"
	VoucherRewardPlugin           = "plugin"

	PluginStatusInactive = "inactive"
	PluginStatusExpired  = "expired"

	// PluginTrialDays is how long a free plugin trial lasts.
	PluginTrialDays = 7
)

var (
	ErrInvoiceNotFound = errors.New("invoice not found")
	// ErrInsufficientCoins is returned when the premium coin balance cannot pay for something.
	ErrInsufficientCoins = errors.New("premium coin balance is too low")
	// ErrVoucherInvalid is an unknown, expired or used-up voucher; ErrVoucherUsed one this store already redeemed.
	ErrVoucherInvalid = errors.New("voucher is not valid")
	ErrVoucherUsed    = errors.New("voucher was already used")

	// PaymentChannels are the ways an invoice can be paid besides premium coins.
	PaymentChannels = []string{"virtual_account", "qris", "e_wallet", "credit_card", "convenience_store", "manual_transfer"}
)

// Plan is a subscription that can be bought.
type Plan struct {
	Code   string
	Months int
	Price  float64
}

// Plans are the subscriptions on offer, by the code a client sends.
var Plans = map[string]Plan{
	"pro_monthly":   {Code: "pro", Months: 1, Price: 99000},
	"pro_quarterly": {Code: "pro", Months: 3, Price: 279000},
	"pro_yearly":    {Code: "pro", Months: 12, Price: 999000},
}

// PluginInfo describes a plugin of the catalog; name, description and features are English texts that are
// translated when listed.
type PluginInfo struct {
	Code,
	Name,
	Description string
	Features []string
	Monthly  float64
}

// PluginCatalog is every plugin a store can subscribe to.
var PluginCatalog = []PluginInfo{
	{Code: "business", Name: "Business", Description: "Customer types, promotions, points, commissions, service charge, rounding and multi-branch tools.", Features: []string{"Customer types & special prices", "Promotions & loyalty points", "Staff commissions", "Service charge & rounding", "Branch products & transfer confirmation"}, Monthly: 49000},
	{Code: "ingredients", Name: "Raw Materials", Description: "Recipes that deduct raw-material stock when a product is sold.", Features: []string{"Recipes per product", "Unit conversion", "Raw-material stock reports"}, Monthly: 29000},
	{Code: "desktop", Name: "Desktop POS", Description: "Run the cashier on Windows and macOS.", Features: []string{"Windows & macOS cashier", "Receipt and label printers"}, Monthly: 39000},
	{Code: "server", Name: "Local Server", Description: "Keep selling when the internet is down; sync when it is back.", Features: []string{"Offline mode for several devices", "Automatic sync"}, Monthly: 59000},
	{Code: "food_menu", Name: "Food Menu", Description: "Add-ons, modifiers and a kitchen display for restaurants.", Features: []string{"Add-ons & modifiers", "Kitchen order tickets", "Table management"}, Monthly: 29000},
	{Code: "attendance", Name: "Staff Attendance", Description: "Clock-in and clock-out with photo and location.", Features: []string{"Selfie clock-in", "GPS location", "Attendance reports"}, Monthly: 25000},
}

// PluginByCode finds a plugin of the catalog, or nil.
func PluginByCode(code string) *PluginInfo {
	for i := range PluginCatalog {
		if PluginCatalog[i].Code == code {
			return &PluginCatalog[i]
		}
	}
	return nil
}

// Invoice is something the store pays Lestari for: a subscription, a plugin, premium coins, storage.
type Invoice struct {
	ID            uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	StoreID       uuid.UUID `json:"-"`
	InvoiceNumber string    `json:"invoice_number" example:"INV/20261108/0001"`
	Kind          string    `json:"kind" example:"subscription" enums:"subscription,plugin,coin_topup,branch_renewal,storage"`
	Description   string    `json:"description" example:"Pro plan, 1 month(s)"`
	PaymentMethod string    `json:"payment_method" example:"qris" enums:"coin,virtual_account,qris,e_wallet,credit_card,convenience_store,manual_transfer"`
	Amount        float64   `json:"amount" example:"99000"`
	AdminFee      float64   `json:"admin_fee" example:"4000"`
	Status        string    `json:"status" example:"pending" enums:"pending,paid,cancelled,expired"`
	PaymentURL    *string   `json:"payment_url" example:"https://pay.example.com/INV-20261108-0001"`
	// how to pay, in the language of the request
	PaymentInstructions *string         `json:"payment_instructions" example:"Pay Rp 103000 via QRIS and include reference INV/20261108/0001."`
	PaymentProofURL     *string         `json:"payment_proof_url" example:"/uploads/proof.jpg"`
	Meta                json.RawMessage `json:"-"`
	ExpiresAt           *time.Time      `json:"expires_at" example:"2026-11-09T10:32:00+07:00"`
	PaidAt              *time.Time      `json:"paid_at" example:"2026-11-08T10:40:00+07:00"`
	CreatedAt           time.Time       `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	// the id again, for clients that open the payment page from the answer of a purchase
	InvoiceID *uuid.UUID `json:"invoice_id,omitempty" db:"-" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
}

// InvoiceMeta is what an invoice buys, applied when it is paid.
type InvoiceMeta struct {
	Plan      string      `json:"plan,omitempty"`
	Code      string      `json:"code,omitempty"`
	Months    int         `json:"months,omitempty"`
	BranchIDs []uuid.UUID `json:"branch_ids,omitempty"`
}

// InvoiceOrder asks for a new invoice. Paid with coins it is settled at once; otherwise the payment provider
// is asked how to pay.
type InvoiceOrder struct {
	StoreID       uuid.UUID
	Kind          string
	Description   string
	PaymentMethod string
	Amount        float64
	// PromoCode is an optional discount voucher.
	PromoCode string
	Meta      InvoiceMeta
	Lang      i18n.Lang
	At        time.Time
}

// InvoiceFilter selects the invoices of a store. StartDate and EndDate are YYYY-MM-DD.
type InvoiceFilter struct {
	StoreID uuid.UUID
	StartDate,
	EndDate,
	Status,
	Keyword string
	Limit,
	Page int64
}

// BillingSummary is the summary of the invoice list: the plan and the premium coin balance.
type BillingSummary struct {
	PlanName      string     `json:"plan_name" example:"Pro"`
	PlanExpiresAt *time.Time `json:"plan_expires_at" example:"2027-11-08T10:32:00+07:00"`
	CoinBalance   float64    `json:"coin_balance" example:"250000"`
}

// SubscriptionRequest is the body of POST /billing/subscriptions.
type SubscriptionRequest struct {
	Plan          string `json:"plan" example:"pro_monthly" enums:"pro_monthly,pro_quarterly,pro_yearly"`
	PaymentMethod string `json:"payment_method" example:"qris" enums:"coin,virtual_account,qris,e_wallet,credit_card,convenience_store,manual_transfer"`
	PromoCode     string `json:"promo_code" example:"HEMAT10"`
}

// CoinTopUpRequest is the body of POST /billing/coin-top-ups.
type CoinTopUpRequest struct {
	Amount        float64 `json:"amount" example:"50000" minimum:"10000"`
	PaymentMethod string  `json:"payment_method" example:"qris" enums:"virtual_account,qris,e_wallet,credit_card,convenience_store,manual_transfer"`
	PromoCode     string  `json:"promo_code" example:"HEMAT10"`
}

// VoucherRequest is the body of POST /billing/vouchers/redeem.
type VoucherRequest struct {
	Code string `json:"code" example:"WELCOME50K"`
}

// VoucherRedemption is the data of a redeemed voucher.
type VoucherRedemption struct {
	Code         string  `json:"code" example:"WELCOME50K"`
	RewardType   string  `json:"reward_type" example:"coin" enums:"coin,subscription_days,plugin,ppob_free_fee"`
	RewardAmount float64 `json:"reward_amount" example:"50000"`
	CoinBalance  float64 `json:"coin_balance" example:"300000"`
}

// PaymentWebhookRequest is what a payment provider posts to /webhooks/payments.
type PaymentWebhookRequest struct {
	// invoice number or PPOB deposit reference
	Reference string `json:"reference" example:"INV/20261108/0001"`
	Status    string `json:"status" example:"paid"`
}

// HandledResponse tells whether a webhook changed anything.
type HandledResponse struct {
	Handled bool `json:"handled" example:"true"`
}

// StorePlugin is the subscription state of one plugin of a store.
type StorePlugin struct {
	Code      string     `json:"code" example:"business"`
	Status    string     `json:"status" example:"trial" enums:"active,trial"`
	TrialUsed bool       `json:"-"`
	ExpiresAt *time.Time `json:"expires_at" example:"2026-11-15T10:32:00+07:00"`
}

// Plugin is a plugin of the catalog with the state of the store's subscription.
type Plugin struct {
	Code           string     `json:"code" example:"business"`
	Name           string     `json:"name" example:"Business"`
	Description    string     `json:"description" example:"Customer types, promotions, points, commissions, service charge, rounding and multi-branch tools."`
	Features       []string   `json:"features" example:"Customer types & special prices,Promotions & loyalty points"`
	PriceMonthly   float64    `json:"price_monthly" example:"49000"`
	PriceYearly    float64    `json:"price_yearly" example:"490000"`
	Status         string     `json:"status" example:"inactive" enums:"inactive,trial,active,expired"`
	TrialAvailable bool       `json:"trial_available" example:"true"`
	ExpiresAt      *time.Time `json:"expires_at" example:"2026-11-15T10:32:00+07:00"`
}

// PluginSubscriptionRequest is the body of POST /plugins/{code}/subscribe.
type PluginSubscriptionRequest struct {
	Period        string `json:"period" example:"monthly" enums:"monthly,yearly"`
	PaymentMethod string `json:"payment_method" example:"coin" enums:"coin,virtual_account,qris,e_wallet,credit_card,convenience_store,manual_transfer"`
}
