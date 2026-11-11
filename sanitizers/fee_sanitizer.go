package sanitizers

import (
	"log/slog"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindFees reads the list query: q, applies_to, sort, order, page, per_page.
func FindFees(c fiber.Ctx) *models.FeeFilter {
	response := models.NewFeeFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if appliesTo := c.Query("applies_to"); appliesTo != "" && slices.Contains([]string{"sale", "purchase", "both"}, appliesTo) {
		response.WithAppliesTo(appliesTo)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapFeeSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateFee reads a fee body. On create name, amount_type, amount are required; on update only what was sent is checked.
func ValidateFee(c fiber.Ctx, create bool) (*models.FeeRequest, error) {
	ctxt := "FeeSanitizer-ValidateFee"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateFeeRow(b, create)
}

// ValidateFeeRow validates one fee given as a decoded object (a request body or an imported row).
func ValidateFeeRow(row map[string]any, create bool) (*models.FeeRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.FeeRequest{}
	response.Name = b.text(v, response, "name")
	response.AmountType = b.text(v, response, "amount_type", "percent", "amount")
	response.Amount = b.number(v, response, "amount", new(float64(0)), nil)
	response.AppliesTo = b.text(v, response, "applies_to", "sale", "purchase", "both")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	if response.AmountType == nil && create {
		response.AmountType = new("amount")
	}
	required(v, "amount_type", response.AmountType == nil && (create || response.IsNull("amount_type")))
	required(v, "amount", response.Amount == nil && (create || response.IsNull("amount")))
	if response.AppliesTo == nil && create {
		response.AppliesTo = new("sale")
	}
	return response, v.Err()
}
