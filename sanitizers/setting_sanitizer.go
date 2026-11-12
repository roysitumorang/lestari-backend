package sanitizers

import (
	"log/slog"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// settingPatch collects the attributes of a setting that a request sends: validated values, and nil for the
// ones sent as null, which clears them.
type settingPatch struct {
	b     body
	v     *helper.Validation
	nulls *models.Nulls
	out   map[string]any
}

// keep stores a value that was sent, or nil when the attribute was sent empty.
func keep[T any](q *settingPatch, key string, value *T) {
	switch {
	case value != nil:
		q.out[key] = *value
	case q.nulls.IsNull(key):
		q.out[key] = nil
	}
}

func (q *settingPatch) flags(keys ...string) {
	for _, key := range keys {
		keep(q, key, q.b.boolean(q.v, q.nulls, key))
	}
}

func (q *settingPatch) texts(keys ...string) {
	for _, key := range keys {
		keep(q, key, q.b.text(q.v, q.nulls, key))
	}
}

func (q *settingPatch) choice(key string, values ...string) {
	keep(q, key, q.b.text(q.v, q.nulls, key, values...))
}

func (q *settingPatch) number(key string, lo, hi *float64) {
	keep(q, key, q.b.number(q.v, q.nulls, key, lo, hi))
}

// step reads a number that must be one of a few values.
func (q *settingPatch) step(key string, values ...float64) {
	value := q.b.number(q.v, q.nulls, key, nil, nil)
	if value != nil && !slices.Contains(values, *value) {
		names := make([]string, len(values))
		for i, x := range values {
			names[i] = helper.Sprintf("%g", x)
		}
		q.v.Add(key, "Must be one of: {values}.", "values", strings.Join(names, ", "))
		return
	}
	keep(q, key, value)
}

func (q *settingPatch) ids(keys ...string) {
	for _, key := range keys {
		if value := q.b.idList(q.v, q.nulls, key); value != nil {
			q.out[key] = value
		} else if q.nulls.IsNull(key) {
			q.out[key] = []any{}
		}
	}
}

func (q *settingPatch) documents(keys ...string) {
	for _, key := range keys {
		if value := q.b.document(q.v, q.nulls, key); value != nil {
			q.out[key] = value
		} else if q.nulls.IsNull(key) {
			q.out[key] = []any{}
		}
	}
}

// required refuses to clear an attribute.
func (q *settingPatch) required(key string) {
	if value, sent := q.out[key]; sent && value == nil {
		q.v.Add(key, "Required")
	}
}

var zero, hundred = new(float64(0)), new(float64(100))

// ValidateSetting reads the body of PUT /settings/{key} (and the catalog settings): the attributes that are sent,
// each checked against the type, range and choices of that setting.
func ValidateSetting(c fiber.Ctx, key string) (map[string]any, error) {
	ctxt := "SettingSanitizer-ValidateSetting"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	q := &settingPatch{b: b, v: &helper.Validation{}, nulls: &models.Nulls{}, out: map[string]any{}}
	switch key {
	case models.SettingPoints:
		q.flags("is_enabled", "earn_on_credit_sales")
		for _, attribute := range []string{"earn_spend_amount", "earn_points", "redeem_point_value", "minimum_redeem_points", "expiry_days"} {
			q.number(attribute, zero, nil)
		}
	case models.SettingStore:
		q.texts("logo_url", "cover_url", "name", "owner_name", "phone", "motto", "business_type", "business_type_other", "address", "postal_code")
		q.required("name")
		q.choice("country_code", "ID", "MY", "SG", "TL")
		keep(q, "province_id", q.b.id(q.v, q.nulls, "province_id"))
		keep(q, "city_id", q.b.id(q.v, q.nulls, "city_id"))
		q.number("latitude", new(float64(-90)), new(float64(90)))
		q.number("longitude", new(float64(-180)), new(float64(180)))
		q.choice("stock_valuation_method", "default", "fifo", "lifo", "average")
		q.choice("currency", "IDR", "MYR", "SGD", "USD", "AUD", "EUR")
		q.choice("timezone", "Asia/Jakarta", "Asia/Makassar", "Asia/Jayapura")
		q.flags("allow_negative_stock")
	case models.SettingOnlineStore:
		q.flags("is_enabled", "seo_enabled", "pwa_enabled", "use_physical_stock", "notify_owner")
		q.texts("subdomain", "store_email", "description", "facebook", "twitter", "instagram")
		q.ids("notify_staff_ids")
	case models.SettingLinkPage:
		q.flags("is_enabled", "show_watermark")
		q.texts("username", "slogan", "background_color")
		q.required("username")
		q.documents("links")
	case models.SettingQROrder:
		q.flags("is_enabled", "qris_enabled", "cashier_payment_enabled", "addons_enabled", "table_ordering_enabled", "kiosk_enabled", "notify_owner")
		q.step("order_expiry_hours", 3, 6, 12, 24)
		q.choice("payment_timing", "pay_later", "pay_now")
		q.choice("menu_layout", "tabs", "by_category")
		q.ids("category_ids", "notify_staff_ids")
	case models.SettingDefaultTax:
		q.flags("is_enabled", "is_inclusive")
		q.number("rate", zero, hundred)
	case models.SettingServiceCharge:
		q.flags("is_enabled", "apply_before_tax")
		q.texts("name")
		q.number("rate", zero, hundred)
	case models.SettingRounding:
		q.flags("is_enabled")
		q.choice("mode", "nearest", "up", "down")
		q.step("multiple", 50, 100, 500, 1000)
	case models.SettingReceipt:
		q.flags(models.ReceiptFlags...)
		q.texts("logo_url", "header_note", "footer_note")
	case models.SettingDisplay:
		q.choice("color_theme", "emerald", "ivory", "charcoal")
		q.flags("show_breadcrumbs")
		q.choice("language", "en", "id")
	case models.SettingCatalog:
		q.flags("bank_transfer_enabled", "direct_payment_enabled", "takeaway_enabled", "cod_enabled", "courier_enabled", "own_courier_enabled")
		if couriers := q.b.texts(q.v, q.nulls, "couriers", models.Couriers...); couriers != nil {
			q.out["couriers"] = couriers
		} else if q.nulls.IsNull("couriers") {
			q.out["couriers"] = []any{}
		}
		q.choice("own_courier_mode", "city", "radius")
		q.documents("own_courier_city_rates", "own_courier_radius_rates")
	default:
		return nil, models.ErrSettingUnknown
	}
	return q.out, q.v.Err()
}

// StoragePurchase reads the body of a storage purchase: a known package, paid with coins.
func StoragePurchase(c fiber.Ctx) (*models.StoragePurchaseRequest, error) {
	response, err := Decode[models.StoragePurchaseRequest](c)
	if err != nil {
		return nil, err
	}
	if _, ok := models.StoragePackages[response.Package]; !ok {
		return nil, helper.Invalid("package", "Unknown storage package.")
	}
	if response.PaymentMethod != "" && response.PaymentMethod != models.PaymentMethodCoin {
		return nil, helper.Invalid("payment_method", "Storage can only be paid with premium coins.")
	}
	return response, nil
}

// ResetData reads the body of a data reset and turns its scope into the dates that bound the dated data.
func ResetData(c fiber.Ctx) (*models.ResetDataRequest, *models.DataReset, error) {
	request, err := Decode[models.ResetDataRequest](c)
	if err != nil {
		return nil, nil, err
	}
	v := &helper.Validation{}
	if len(request.DataTypes) == 0 {
		v.Add("data_types", "Choose what to delete.")
	}
	for _, dataType := range request.DataTypes {
		if !slices.Contains(models.ResetDataTypes, dataType) {
			v.Add("data_types", "Unknown data type {type}.", "type", dataType)
		}
	}
	response := &models.DataReset{DataTypes: request.DataTypes, StartDate: "1900-01-01", EndDate: "2999-12-31"}
	switch request.Scope {
	case "", "all":
	case "year", "month":
		if request.Year < 2000 || request.Year > 2100 {
			v.Add("year", "Choose a year.")
		}
		response.StartDate, response.EndDate = helper.Sprintf("%d-01-01", request.Year), helper.Sprintf("%d-12-31", request.Year)
		if request.Scope == "month" {
			if request.Month < 1 || request.Month > 12 {
				v.Add("month", "Choose a month.")
			} else {
				response.StartDate = helper.Sprintf("%d-%02d-01", request.Year, request.Month)
				response.EndDate = lastDayOfMonth(request.Year, request.Month)
			}
		}
	default:
		v.Add("scope", "Must be one of: all, year, month.")
	}
	if strings.TrimSpace(request.OTP) == "" {
		v.Add("otp", "Required")
	}
	return request, response, v.Err()
}
