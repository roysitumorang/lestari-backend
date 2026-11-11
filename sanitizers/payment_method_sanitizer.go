package sanitizers

import (
	"log/slog"
	"slices"
	"strings"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindPaymentMethods reads the list query: q, type, is_active, sort, order, page, per_page.
func FindPaymentMethods(c fiber.Ctx) *models.PaymentMethodFilter {
	response := models.NewPaymentMethodFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if typeValue := c.Query("type"); typeValue != "" && slices.Contains([]string{"cash", "debit", "credit_card", "qris", "e_wallet", "bank_transfer", "other"}, typeValue) {
		response.WithType(typeValue)
	}
	if isActive, ok := queryBool(c, "is_active"); ok {
		response.WithIsActive(isActive)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapPaymentMethodSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidatePaymentMethod reads a payment method body. On create name, type are required; on update only what was sent is checked.
func ValidatePaymentMethod(c fiber.Ctx, create bool) (*models.PaymentMethodRequest, error) {
	ctxt := "PaymentMethodSanitizer-ValidatePaymentMethod"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidatePaymentMethodRow(b, create)
}

// ValidatePaymentMethodRow validates one payment method given as a decoded object (a request body or an imported row).
func ValidatePaymentMethodRow(row map[string]any, create bool) (*models.PaymentMethodRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.PaymentMethodRequest{}
	response.Name = b.text(v, response, "name")
	response.Type = b.text(v, response, "type", "cash", "debit", "credit_card", "qris", "e_wallet", "bank_transfer", "other")
	response.Code = b.text(v, response, "code")
	response.MdrType = b.text(v, response, "mdr_type", "percent", "amount")
	response.MdrValue = b.number(v, response, "mdr_value", new(float64(0)), nil)
	response.ChargeFeeToCustomer = b.boolean(v, response, "charge_fee_to_customer")
	response.EdcCardRates = b.document(v, response, "edc_card_rates")
	response.IsActive = b.boolean(v, response, "is_active")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	required(v, "type", response.Type == nil && (create || response.IsNull("type")))
	if response.ChargeFeeToCustomer == nil && (create || response.IsNull("charge_fee_to_customer")) {
		response.ChargeFeeToCustomer = new(false)
	}
	if response.EdcCardRates == nil && (create || response.IsNull("edc_card_rates")) {
		response.EdcCardRates = json.RawMessage("[]")
	}
	if response.IsActive == nil && (create || response.IsNull("is_active")) {
		response.IsActive = new(true)
	}
	return response, v.Err()
}
