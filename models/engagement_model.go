package models

import (
	"errors"
	"time"
	"uuid"

	"github.com/goccy/go-json"
)

const (
	MissionStatusInProgress = "in_progress"
	MissionStatusCompleted  = "completed"
	MissionStatusClaimed    = "claimed"

	IntegrationStatusActive   = "active"
	IntegrationStatusPending  = "pending"
	IntegrationStatusInactive = "inactive"
)

var (
	// ErrMissionClaimed is returned when the reward of a mission was already claimed.
	ErrMissionClaimed = errors.New("mission reward was already claimed")
	// ErrReferralCodeTaken and ErrReferralCodeExists are the two ways setting a referral code fails.
	ErrReferralCodeTaken  = errors.New("referral code is already taken")
	ErrReferralCodeExists = errors.New("the store already has a referral code")
)

// MissionInfo is a task of the fixed catalogue that earns premium coins. Title and description are English
// texts, translated when listed. Progress names the counter of the store that is compared with Target.
type MissionInfo struct {
	Code,
	Title,
	Description,
	Route string
	Reward   float64
	Target   int64
	Progress string
}

// Counters of a store that missions measure.
const (
	ProgressProducts      = "products"
	ProgressSales         = "sales"
	ProgressStoreLocation = "store_location"
	ProgressStaff         = "staff"
	ProgressPhoneVerified = "phone_verified"
	ProgressTwoFactor     = "two_factor"
)

// Missions is the catalogue of missions.
var Missions = []MissionInfo{
	{Code: "add_products", Title: "Add 10 products", Description: "Build your catalog so the cashier can sell quickly.", Route: "products", Reward: 5000, Target: 10, Progress: ProgressProducts},
	{Code: "first_sale", Title: "Make your first sale", Description: "Record a sale from the cashier app.", Route: "sales", Reward: 2000, Target: 1, Progress: ProgressSales},
	{Code: "store_location", Title: "Set your store location", Description: "Pin your store on the map so customers can find you.", Route: "store", Reward: 2000, Target: 1, Progress: ProgressStoreLocation},
	{Code: "add_staff", Title: "Invite a staff member", Description: "Give your cashier their own login.", Route: "staff", Reward: 3000, Target: 1, Progress: ProgressStaff},
	{Code: "verify_phone", Title: "Verify your phone number", Description: "Secure your account and receive important alerts.", Route: "profile", Reward: 2000, Target: 1, Progress: ProgressPhoneVerified},
	{Code: "two_factor", Title: "Turn on two-step verification", Description: "Protect the owner account with an authenticator app.", Route: "profile", Reward: 2000, Target: 1, Progress: ProgressTwoFactor},
	{Code: "hundred_sales", Title: "Reach 100 sales", Description: "Keep selling — rewards grow with you.", Route: "report-daily-sales", Reward: 10000, Target: 100, Progress: ProgressSales},
}

// Mission is a mission with the progress of the store.
type Mission struct {
	// missions are a fixed catalogue, so the id is the code
	ID           string  `json:"id" example:"add_products"`
	Code         string  `json:"code" example:"add_products"`
	Title        string  `json:"title" example:"Add 10 products"`
	Description  string  `json:"description" example:"Build your catalog so the cashier can sell quickly."`
	RewardType   string  `json:"reward_type" example:"coin"`
	RewardAmount float64 `json:"reward_amount" example:"5000"`
	Progress     int64   `json:"progress" example:"7"`
	Target       int64   `json:"target" example:"10"`
	Status       string  `json:"status" example:"in_progress" enums:"in_progress,completed,claimed"`
	ExpiresAt    *string `json:"expires_at" example:""`
	// the back-office page where the mission is done
	ActionRoute string `json:"action_route" example:"products"`
}

// MissionClaim is the data of a claimed reward.
type MissionClaim struct {
	ID           string  `json:"id" example:"add_products"`
	Status       string  `json:"status" example:"claimed"`
	RewardType   string  `json:"reward_type" example:"coin"`
	RewardAmount float64 `json:"reward_amount" example:"5000"`
}

// ReferredBusiness is a store that registered with the referral code.
type ReferredBusiness struct {
	StoreName    string    `json:"store_name" example:"Toko Maju"`
	JoinedAt     time.Time `json:"joined_at" example:"2026-10-02T09:15:00+07:00"`
	Status       string    `json:"status" example:"subscribed" enums:"registered,subscribed"`
	RewardAmount float64   `json:"reward_amount" example:"25000"`
}

// Referral is the referral programme of a store.
type Referral struct {
	Code               *string             `json:"code" example:"LESTARI"`
	ShareURL           *string             `json:"share_url" example:"https://backoffice.example.com/register?ref=LESTARI"`
	ReferredCount      int64               `json:"referred_count" example:"3"`
	RewardBalance      float64             `json:"reward_balance" example:"50000"`
	ReferredBusinesses []*ReferredBusiness `json:"referred_businesses"`
}

// ReferralCodeRequest is the body of POST /referrals/code; without a code one is generated.
type ReferralCodeRequest struct {
	Code string `json:"code" example:"LESTARI"`
}

// ReferralCode is the data of a new referral code.
type ReferralCode struct {
	Code     string `json:"code" example:"LESTARI"`
	ShareURL string `json:"share_url" example:"https://backoffice.example.com/register?ref=LESTARI"`
}

// RewardTransfer is the data of moving referral rewards to premium coins.
type RewardTransfer struct {
	Transferred   float64 `json:"transferred" example:"50000"`
	CoinBalance   float64 `json:"coin_balance" example:"300000"`
	RewardBalance float64 `json:"reward_balance" example:"0"`
}

// IntegrationInfo is a third-party service of the fixed catalogue.
type IntegrationInfo struct {
	Code,
	Name,
	Description,
	Terms string
}

// Integrations is the catalogue of integrations.
var Integrations = []IntegrationInfo{
	{Code: "dana", Name: "DANA", Description: "Accept DANA e-wallet payments at the cashier and online store.", Terms: "https://www.dana.id/terms"},
	{Code: "jurnal", Name: "Jurnal by Mekari", Description: "Send sales, purchases and expenses to your accounting books automatically.", Terms: "https://www.jurnal.id/terms"},
	{Code: "qris_dynamic", Name: "Dynamic QRIS", Description: "Show a QR code with the exact amount for every sale."},
}

// IntegrationByCode finds an integration of the catalogue, or nil.
func IntegrationByCode(code string) *IntegrationInfo {
	for i := range Integrations {
		if Integrations[i].Code == code {
			return &Integrations[i]
		}
	}
	return nil
}

// Integration is an integration with the state of the store's connection.
type Integration struct {
	Code        string     `json:"code" example:"jurnal"`
	Name        string     `json:"name" example:"Jurnal by Mekari"`
	Description string     `json:"description" example:"Send sales, purchases and expenses to your accounting books automatically."`
	LogoURL     *string    `json:"logo_url" example:""`
	Status      string     `json:"status" example:"inactive" enums:"inactive,pending,active"`
	TermsURL    *string    `json:"terms_url" example:"https://www.jurnal.id/terms"`
	ConnectedAt *time.Time `json:"connected_at" example:"2026-11-08T10:32:00+07:00"`
}

// IntegrationRequest is the body of POST /integrations/{code}/activate.
type IntegrationRequest struct {
	AcceptTerms bool            `json:"accept_terms" example:"true"`
	Settings    json.RawMessage `json:"settings" swaggertype:"object"`
}

// IntegrationState is the data of connecting or disconnecting an integration.
type IntegrationState struct {
	Code        string  `json:"code" example:"jurnal"`
	Status      string  `json:"status" example:"active" enums:"inactive,pending,active"`
	RedirectURL *string `json:"redirect_url,omitempty" example:""`
	// ConnectedAt is read with the state of a store; it is not part of the answer.
	ConnectedAt *time.Time `json:"-"`
}

// LoanProvider is a lending partner. Description and InterestInfo are English texts, translated when listed.
type LoanProvider struct {
	Code         string  `json:"code" example:"modalrakyat"`
	Name         string  `json:"name" example:"Modal Rakyat"`
	Description  string  `json:"description" example:"Working-capital loans for small shops, decided from your sales history."`
	LogoURL      *string `json:"logo_url" example:""`
	MinAmount    float64 `json:"min_amount" example:"1000000"`
	MaxAmount    float64 `json:"max_amount" example:"50000000"`
	TenorsMonths []int64 `json:"tenors_months" example:"3,6,12"`
	InterestInfo string  `json:"interest_info" example:"From 1.5% per month"`
	InfoURL      *string `json:"info_url" example:"http://bit.ly/koinbisniskw"`
}

// LoanProviders is the catalogue of lending partners.
var LoanProviders = []LoanProvider{
	{Code: "modalrakyat", Name: "Modal Rakyat", Description: "Working-capital loans for small shops, decided from your sales history.",
		MinAmount: 1000000, MaxAmount: 50000000, TenorsMonths: []int64{3, 6, 12}, InterestInfo: "From 1.5% per month"},
	{Code: "komunal", Name: "Komunal", Description: "Productive financing for growing businesses.",
		MinAmount: 5000000, MaxAmount: 200000000, TenorsMonths: []int64{6, 12, 24}, InterestInfo: "From 1.2% per month"},
	{Code: "koinworks", Name: "KoinWorks", Description: "Business loans with a fast online process.",
		MinAmount: 10000000, MaxAmount: 500000000, TenorsMonths: []int64{6, 12, 24}, InterestInfo: "From 1% per month", InfoURL: new("http://bit.ly/koinbisniskw")},
}

// LoanProviderByCode finds a lending partner, or nil.
func LoanProviderByCode(code string) *LoanProvider {
	for i := range LoanProviders {
		if LoanProviders[i].Code == code {
			return &LoanProviders[i]
		}
	}
	return nil
}

// LoanApplication is a loan the owner applied for.
type LoanApplication struct {
	ID           uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	StoreID      uuid.UUID `json:"-" db:"-"`
	ProviderCode string    `json:"provider_code" example:"modalrakyat"`
	Amount       float64   `json:"amount" example:"10000000"`
	TenorMonths  int64     `json:"tenor_months" example:"12"`
	Purpose      string    `json:"purpose" example:"Stock for the holiday season"`
	Status       string    `json:"status" example:"submitted"`
	CreatedAt    time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
}

// LoanApplicationRequest is the body of POST /loans/applications.
type LoanApplicationRequest struct {
	ProviderCode string  `json:"provider_code" example:"modalrakyat" enums:"modalrakyat,komunal,koinworks"`
	Amount       float64 `json:"amount" example:"10000000"`
	TenorMonths  int64   `json:"tenor_months" example:"12"`
	Purpose      string  `json:"purpose" example:"Stock for the holiday season"`
	// agreeing that store and sales data are shared with the partner
	Consent bool `json:"consent" example:"true"`
}

// LoanApplied is the data of a new loan application.
type LoanApplied struct {
	ID          uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Status      string    `json:"status" example:"submitted"`
	RedirectURL *string   `json:"redirect_url" example:"http://bit.ly/koinbisniskw"`
}

// StoreRating is feedback of a customer, collected by the cashier app or the link on a receipt.
type StoreRating struct {
	ID           uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	BranchID     uuid.UUID  `json:"-" db:"-"`
	SaleID       *uuid.UUID `json:"-" db:"-"`
	CustomerName string     `json:"-" db:"-"`
	Rating       int64      `json:"rating" example:"5" minimum:"1" maximum:"5"`
	Comment      *string    `json:"comment" example:"Fast and friendly"`
	Tags         []string   `json:"tags" example:"service,price"`
	CreatedAt    time.Time  `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
}

// StoreRatingRequest is the body of POST /store-ratings.
type StoreRatingRequest struct {
	Rating       int64    `json:"rating" example:"5" minimum:"1" maximum:"5"`
	SaleID       string   `json:"sale_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CustomerName string   `json:"customer_name" example:"Siti"`
	Comment      string   `json:"comment" example:"Fast and friendly"`
	Tags         []string `json:"tags" example:"service,price"`
}
