package sanitizers

import (
	"log/slog"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindPaymentAccounts reads the list query: q, type, sort, order, page, per_page.
func FindPaymentAccounts(c fiber.Ctx) *models.PaymentAccountFilter {
	response := models.NewPaymentAccountFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if typeValue := c.Query("type"); typeValue != "" && slices.Contains([]string{"bank", "e_wallet", "qris"}, typeValue) {
		response.WithType(typeValue)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapPaymentAccountSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidatePaymentAccount reads a account body. On create type, provider, account_name are required; on update only what was sent is checked.
func ValidatePaymentAccount(c fiber.Ctx, create bool) (*models.PaymentAccountRequest, error) {
	ctxt := "PaymentAccountSanitizer-ValidatePaymentAccount"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidatePaymentAccountRow(b, create)
}

// ValidatePaymentAccountRow validates one account given as a decoded object (a request body or an imported row).
func ValidatePaymentAccountRow(row map[string]any, create bool) (*models.PaymentAccountRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.PaymentAccountRequest{}
	response.Type = b.text(v, response, "type", "bank", "e_wallet", "qris")
	response.Provider = b.text(v, response, "provider")
	response.AccountName = b.text(v, response, "account_name")
	response.AccountNumber = b.text(v, response, "account_number")
	response.QRISImageURL = b.text(v, response, "qris_image_url")
	response.IsActive = b.boolean(v, response, "is_active")
	if response.Type == nil && create {
		response.Type = new("bank")
	}
	required(v, "type", response.Type == nil && (create || response.IsNull("type")))
	required(v, "provider", response.Provider == nil && (create || response.IsNull("provider")))
	required(v, "account_name", response.AccountName == nil && (create || response.IsNull("account_name")))
	if response.IsActive == nil && (create || response.IsNull("is_active")) {
		response.IsActive = new(true)
	}
	return response, v.Err()
}
