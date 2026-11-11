package sanitizers

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindTaxes reads the list query: q, is_active, sort, order, page, per_page.
func FindTaxes(c fiber.Ctx) *models.TaxFilter {
	response := models.NewTaxFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if isActive, ok := queryBool(c, "is_active"); ok {
		response.WithIsActive(isActive)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapTaxSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateTax reads a tax body. On create name, rate are required; on update only what was sent is checked.
func ValidateTax(c fiber.Ctx, create bool) (*models.TaxRequest, error) {
	ctxt := "TaxSanitizer-ValidateTax"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateTaxRow(b, create)
}

// ValidateTaxRow validates one tax given as a decoded object (a request body or an imported row).
func ValidateTaxRow(row map[string]any, create bool) (*models.TaxRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.TaxRequest{}
	response.Name = b.text(v, response, "name")
	response.Rate = b.number(v, response, "rate", new(float64(0)), new(float64(100)))
	response.IsInclusive = b.boolean(v, response, "is_inclusive")
	response.IsActive = b.boolean(v, response, "is_active")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	required(v, "rate", response.Rate == nil && (create || response.IsNull("rate")))
	if response.IsInclusive == nil && (create || response.IsNull("is_inclusive")) {
		response.IsInclusive = new(false)
	}
	if response.IsActive == nil && (create || response.IsNull("is_active")) {
		response.IsActive = new(true)
	}
	return response, v.Err()
}
