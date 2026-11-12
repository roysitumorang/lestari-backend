package models

import (
	"errors"
	"time"
	"uuid"

	"github.com/goccy/go-json"
)

// Keys of the branch settings.
const (
	SettingPoints        = "points"
	SettingStore         = "store"
	SettingOnlineStore   = "online-store"
	SettingLinkPage      = "link-page"
	SettingQROrder       = "qr-order"
	SettingDefaultTax    = "default-tax"
	SettingServiceCharge = "service-charge"
	SettingRounding      = "rounding"
	SettingReceipt       = "receipt"
	SettingDisplay       = "display"
	SettingCatalog       = "catalog"

	PluginBusiness = "business"
)

var (
	ErrSettingUnknown = errors.New("unknown setting")
	// ErrSlugTaken is returned when another branch already uses a subdomain or link-page username.
	ErrSlugTaken = errors.New("this name is already taken")
)

// SettingPlugins names the plugin a setting needs before it can be changed.
var SettingPlugins = map[string]string{
	SettingPoints:        PluginBusiness,
	SettingServiceCharge: PluginBusiness,
	SettingRounding:      PluginBusiness,
}

// NewSetting returns the setting of a key with its default values, or nil for an unknown key.
func NewSetting(key string) any {
	switch key {
	case SettingPoints:
		return &PointsSetting{EarnSpendAmount: 10000, EarnPoints: 1, RedeemPointValue: 100}
	case SettingStore:
		return &StoreSetting{CountryCode: "ID", Currency: "IDR", Timezone: "Asia/Jakarta", StockValuationMethod: "average"}
	case SettingOnlineStore:
		return &OnlineStoreSetting{SEOEnabled: true, UsePhysicalStock: true, NotifyOwner: true, NotifyStaffIDs: []uuid.UUID{}}
	case SettingLinkPage:
		return &LinkPageSetting{ShowWatermark: true, BackgroundColor: "#0f9d76", Links: json.RawMessage("[]")}
	case SettingQROrder:
		return &QROrderSetting{OrderExpiryHours: 3, PaymentTiming: "pay_later", CashierPaymentEnabled: true, MenuLayout: "tabs",
			CategoryIDs: []uuid.UUID{}, NotifyOwner: true, NotifyStaffIDs: []uuid.UUID{}}
	case SettingDefaultTax:
		return &DefaultTaxSetting{}
	case SettingServiceCharge:
		return &ServiceChargeSetting{Name: "Service charge", ApplyBeforeTax: true}
	case SettingRounding:
		return &RoundingSetting{Mode: "nearest", Multiple: 100}
	case SettingReceipt:
		return &ReceiptSetting{ShowLogo: true, ShowReceiptNumber: true, ShowTotalQuantity: true, FooterNote: new("Thank you for shopping with us!")}
	case SettingDisplay:
		return &DisplaySetting{ColorTheme: "emerald", ShowBreadcrumbs: true, Language: "en"}
	case SettingCatalog:
		return &CatalogSetting{BankTransferEnabled: true, DirectPaymentEnabled: true, TakeawayEnabled: true, Couriers: []string{},
			OwnCourierMode: "city", OwnCourierCityRates: json.RawMessage("[]"), OwnCourierRadiusRates: json.RawMessage("[]")}
	}
	return nil
}

// PointsSetting configures loyalty points (business plugin).
type PointsSetting struct {
	IsEnabled         bool `json:"is_enabled" example:"true"`
	EarnOnCreditSales bool `json:"earn_on_credit_sales" example:"false"`
	// earn_points points for every earn_spend_amount spent
	EarnSpendAmount float64 `json:"earn_spend_amount" example:"10000" minimum:"0"`
	EarnPoints      float64 `json:"earn_points" example:"1" minimum:"0"`
	// what one redeemed point is worth
	RedeemPointValue    float64 `json:"redeem_point_value" example:"100" minimum:"0"`
	MinimumRedeemPoints float64 `json:"minimum_redeem_points" example:"0" minimum:"0"`
	ExpiryDays          float64 `json:"expiry_days" example:"0" minimum:"0"`
}

// StoreSetting is the profile of the branch as documents and the online store show it.
type StoreSetting struct {
	LogoURL              *string    `json:"logo_url" example:"/uploads/logo.png"`
	CoverURL             *string    `json:"cover_url" example:"/uploads/cover.jpg"`
	Name                 string     `json:"name" example:"Apotik Lestari"`
	OwnerName            *string    `json:"owner_name" example:"Abigael Lestari"`
	Phone                *string    `json:"phone" example:"+6281234567890"`
	Motto                *string    `json:"motto" example:"Sehat bersama Lestari"`
	BusinessType         *string    `json:"business_type" example:"Pharmacy & beauty"`
	BusinessTypeOther    *string    `json:"business_type_other" example:""`
	Address              *string    `json:"address" example:"Jl. Sisingamangaraja No. 12, Balige"`
	CountryCode          string     `json:"country_code" example:"ID" enums:"ID,MY,SG,TL"`
	ProvinceID           *uuid.UUID `json:"province_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	CityID               *uuid.UUID `json:"city_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
	PostalCode           *string    `json:"postal_code" example:"22312"`
	Latitude             *float64   `json:"latitude" example:"2.3333" minimum:"-90" maximum:"90"`
	Longitude            *float64   `json:"longitude" example:"99.0667" minimum:"-180" maximum:"180"`
	StockValuationMethod string     `json:"stock_valuation_method" example:"average" enums:"default,fifo,lifo,average"`
	Currency             string     `json:"currency" example:"IDR" enums:"IDR,MYR,SGD,USD,AUD,EUR"`
	Timezone             string     `json:"timezone" example:"Asia/Jakarta" enums:"Asia/Jakarta,Asia/Makassar,Asia/Jayapura"`
	// true: a sale may take stock below zero
	AllowNegativeStock bool `json:"allow_negative_stock" example:"false"`
}

// OnlineStoreSetting configures the public shop of the branch.
type OnlineStoreSetting struct {
	IsEnabled        bool `json:"is_enabled" example:"true"`
	SEOEnabled       bool `json:"seo_enabled" example:"true"`
	PWAEnabled       bool `json:"pwa_enabled" example:"false"`
	UsePhysicalStock bool `json:"use_physical_stock" example:"true"`
	NotifyOwner      bool `json:"notify_owner" example:"true"`
	// 3–40 lowercase letters, numbers or dashes; unique across all stores
	Subdomain      *string     `json:"subdomain" example:"apotiklestari"`
	StoreEmail     *string     `json:"store_email" example:"halo@lestari.id"`
	Description    *string     `json:"description" example:"Apotek keluarga di Balige"`
	Facebook       *string     `json:"facebook" example:"apotiklestari"`
	Twitter        *string     `json:"twitter" example:"apotiklestari"`
	Instagram      *string     `json:"instagram" example:"apotiklestari"`
	NotifyStaffIDs []uuid.UUID `json:"notify_staff_ids" swaggertype:"array,string" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
}

// LinkPageSetting configures the "link in bio" page of the branch.
type LinkPageSetting struct {
	IsEnabled     bool `json:"is_enabled" example:"true"`
	ShowWatermark bool `json:"show_watermark" example:"true"`
	// 3–40 lowercase letters, numbers or dashes; unique across all stores
	Username        string  `json:"username" example:"apotiklestari"`
	Slogan          *string `json:"slogan" example:"Apotek keluarga di Balige"`
	BackgroundColor string  `json:"background_color" example:"#0f9d76"`
	// [{"title": "WhatsApp", "url": "https://wa.me/6281234567890"}]
	Links json.RawMessage `json:"links" swaggertype:"array,object"`
}

// QROrderSetting configures ordering by QR code.
type QROrderSetting struct {
	IsEnabled             bool        `json:"is_enabled" example:"true"`
	QRISEnabled           bool        `json:"qris_enabled" example:"false"`
	CashierPaymentEnabled bool        `json:"cashier_payment_enabled" example:"true"`
	AddonsEnabled         bool        `json:"addons_enabled" example:"false"`
	TableOrderingEnabled  bool        `json:"table_ordering_enabled" example:"false"`
	KioskEnabled          bool        `json:"kiosk_enabled" example:"false"`
	NotifyOwner           bool        `json:"notify_owner" example:"true"`
	OrderExpiryHours      float64     `json:"order_expiry_hours" example:"3" enums:"3,6,12,24"`
	PaymentTiming         string      `json:"payment_timing" example:"pay_later" enums:"pay_later,pay_now"`
	MenuLayout            string      `json:"menu_layout" example:"tabs" enums:"tabs,by_category"`
	CategoryIDs           []uuid.UUID `json:"category_ids" swaggertype:"array,string" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	NotifyStaffIDs        []uuid.UUID `json:"notify_staff_ids" swaggertype:"array,string" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
}

// DefaultTaxSetting is the tax a sale gets when it names none.
type DefaultTaxSetting struct {
	IsEnabled   bool    `json:"is_enabled" example:"true"`
	IsInclusive bool    `json:"is_inclusive" example:"false"`
	Rate        float64 `json:"rate" example:"11" minimum:"0" maximum:"100"`
}

// ServiceChargeSetting adds a percentage to every sale (business plugin).
type ServiceChargeSetting struct {
	IsEnabled      bool    `json:"is_enabled" example:"true"`
	ApplyBeforeTax bool    `json:"apply_before_tax" example:"true"`
	Name           string  `json:"name" example:"Service charge"`
	Rate           float64 `json:"rate" example:"5" minimum:"0" maximum:"100"`
}

// RoundingSetting rounds the total of a sale (business plugin).
type RoundingSetting struct {
	IsEnabled bool    `json:"is_enabled" example:"true"`
	Mode      string  `json:"mode" example:"nearest" enums:"nearest,up,down"`
	Multiple  float64 `json:"multiple" example:"100" enums:"50,100,500,1000"`
}

// ReceiptSetting chooses what a receipt shows.
type ReceiptSetting struct {
	ShowLogo            bool    `json:"show_logo" example:"true"`
	AutoPrint           bool    `json:"auto_print" example:"false"`
	ShowReceiptNumber   bool    `json:"show_receipt_number" example:"true"`
	ShowReceiptCode     bool    `json:"show_receipt_code" example:"false"`
	ShowSequenceNumber  bool    `json:"show_sequence_number" example:"false"`
	ShowUnit            bool    `json:"show_unit" example:"false"`
	ShowCustomerAddress bool    `json:"show_customer_address" example:"false"`
	ShowTotalQuantity   bool    `json:"show_total_quantity" example:"true"`
	ShowSignature       bool    `json:"show_signature" example:"false"`
	ShowPriceType       bool    `json:"show_price_type" example:"false"`
	SeparateAdminFee    bool    `json:"separate_admin_fee" example:"false"`
	ShowPoints          bool    `json:"show_points" example:"false"`
	HideTaxPercentage   bool    `json:"hide_tax_percentage" example:"false"`
	ShowFeedbackLink    bool    `json:"show_feedback_link" example:"false"`
	ShowQRCode          bool    `json:"show_qr_code" example:"false"`
	LogoURL             *string `json:"logo_url" example:"/uploads/receipt-logo.png"`
	HeaderNote          *string `json:"header_note" example:"NPWP 01.234.567.8-901.000"`
	FooterNote          *string `json:"footer_note" example:"Thank you for shopping with us!"`
}

// ReceiptFlags are the true/false attributes of ReceiptSetting.
var ReceiptFlags = []string{"show_logo", "auto_print", "show_receipt_number", "show_receipt_code", "show_sequence_number", "show_unit",
	"show_customer_address", "show_total_quantity", "show_signature", "show_price_type", "separate_admin_fee", "show_points",
	"hide_tax_percentage", "show_feedback_link", "show_qr_code"}

// DisplaySetting is how the back office looks, and the language used when a request names none.
type DisplaySetting struct {
	ColorTheme      string `json:"color_theme" example:"emerald" enums:"emerald,ivory,charcoal"`
	ShowBreadcrumbs bool   `json:"show_breadcrumbs" example:"true"`
	Language        string `json:"language" example:"en" enums:"en,id"`
}

// CatalogSetting configures payment and delivery of the online catalog.
type CatalogSetting struct {
	BankTransferEnabled  bool     `json:"bank_transfer_enabled" example:"true"`
	DirectPaymentEnabled bool     `json:"direct_payment_enabled" example:"true"`
	TakeawayEnabled      bool     `json:"takeaway_enabled" example:"true"`
	CODEnabled           bool     `json:"cod_enabled" example:"false"`
	CourierEnabled       bool     `json:"courier_enabled" example:"false"`
	OwnCourierEnabled    bool     `json:"own_courier_enabled" example:"false"`
	Couriers             []string `json:"couriers" example:"jne,sicepat"`
	OwnCourierMode       string   `json:"own_courier_mode" example:"city" enums:"city,radius"`
	// [{"city_id": "…", "rate": 10000}]
	OwnCourierCityRates json.RawMessage `json:"own_courier_city_rates" swaggertype:"array,object"`
	// [{"max_km": 5, "rate": 8000}]
	OwnCourierRadiusRates json.RawMessage `json:"own_courier_radius_rates" swaggertype:"array,object"`
}

// Couriers are the delivery services the catalog can offer.
var Couriers = []string{"anteraja", "tiki", "esl", "jnt", "jne", "lion", "ncs", "pahala", "pos", "rpx", "sicepat", "wahana"}

// SettingUpdate changes the attributes in Patch of one setting of a branch; a nil value clears an attribute.
type SettingUpdate struct {
	StoreID,
	BranchID,
	HeadOfficeID uuid.UUID
	Key   string
	Patch map[string]any
	At    time.Time
}

// LinkPageVisit is one visit of the link page.
type LinkPageVisit struct {
	IPAddress string    `json:"ip_address" example:"203.0.113.7"`
	VisitedAt time.Time `json:"visited_at" example:"2026-11-08T10:32:00+07:00"`
	Referrer  *string   `json:"referrer" example:"https://instagram.com/"`
}

// MenuItem is one product of the printable menu.
type MenuItem struct {
	Category          string
	Name              string
	SellingPrice      float64
	OnlineDescription *string
}

// Storage is how much of its quota a store uses.
type Storage struct {
	Plan             string  `json:"plan" example:"pro"`
	UsedMB           float64 `json:"used_mb" example:"12.4"`
	QuotaMB          float64 `json:"quota_mb" example:"500"`
	TransactionCount int64   `json:"transaction_count" example:"1840"`
	// until when bought storage lasts (YYYY-MM-DD)
	ExpiresAt *string `json:"expires_at" example:"2026-12-08"`
}

// StoragePurchaseRequest is the body of POST /settings/storage/purchases.
type StoragePurchaseRequest struct {
	Package       string `json:"package" example:"1gb_monthly" enums:"1gb_monthly,5gb_monthly,10gb_monthly"`
	PaymentMethod string `json:"payment_method" example:"coin" enums:"coin"`
}

// StoragePackage is extra storage for 30 days.
type StoragePackage struct {
	MB    float64
	Price float64
}

// StoragePackages are the packages on offer, paid with premium coins.
var StoragePackages = map[string]StoragePackage{
	"1gb_monthly":  {MB: 1024, Price: 15000},
	"5gb_monthly":  {MB: 5120, Price: 50000},
	"10gb_monthly": {MB: 10240, Price: 90000},
}

// StoragePurchase is the data of a storage purchase.
type StoragePurchase struct {
	QuotaMB     float64 `json:"quota_mb" example:"1524"`
	ExpiresAt   *string `json:"expires_at" example:"2026-12-08"`
	CoinBalance float64 `json:"coin_balance" example:"235000"`
}

// ResetOTPRequest is the body of POST /settings/reset-data/otp.
type ResetOTPRequest struct {
	Channel string `json:"channel" example:"email" enums:"email,whatsapp"`
}

// ResetDataRequest is the body of POST /settings/reset-data.
type ResetDataRequest struct {
	DataTypes []string `json:"data_types" example:"sales,shifts"`
	// all, or only one year or month of the dated data
	Scope string `json:"scope" example:"month" enums:"all,year,month"`
	Year  int    `json:"year" example:"2026"`
	Month int    `json:"month" example:"10"`
	OTP   string `json:"otp" example:"123456"`
}

// ResetDataTypes are the kinds of data a reset can delete.
var ResetDataTypes = []string{"images", "products", "sales", "purchases", "customers", "suppliers", "staff", "taxes", "discounts", "categories",
	"fees", "promotions", "shifts", "points", "held_transactions", "online_orders"}

// DataReset is a validated reset of one branch. StartDate and EndDate (YYYY-MM-DD) bound the dated data.
type DataReset struct {
	StoreID,
	BranchID uuid.UUID
	DataTypes []string
	StartDate,
	EndDate string
	At time.Time
}

// Upload is a stored file.
type Upload struct {
	ID       uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	StoreID  uuid.UUID `json:"-"`
	Path     string    `json:"-"`
	URL      string    `json:"url" example:"https://backoffice.example.com/uploads/019a…/3f9c1d.jpg"`
	MimeType string    `json:"mime_type" example:"image/jpeg"`
	Size     int64     `json:"size" example:"48213"`
	Purpose  string    `json:"-"`
}

// Region is a province or a city.
type Region struct {
	ID         uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Name       string     `json:"name" example:"SUMATERA UTARA"`
	ProvinceID *uuid.UUID `json:"province_id,omitempty" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
}
