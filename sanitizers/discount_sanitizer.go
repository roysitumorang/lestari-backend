package sanitizers

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindDiscounts reads the list query: q, is_active, sort, order, page, per_page.
func FindDiscounts(c fiber.Ctx) *models.DiscountFilter {
	response := models.NewDiscountFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if isActive, ok := queryBool(c, "is_active"); ok {
		response.WithIsActive(isActive)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapDiscountSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateDiscount reads a discount body. On create name, type, value are required; on update only what was sent is checked.
func ValidateDiscount(c fiber.Ctx, create bool) (*models.DiscountRequest, error) {
	ctxt := "DiscountSanitizer-ValidateDiscount"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateDiscountRow(b, create)
}

// ValidateDiscountRow validates one discount given as a decoded object (a request body or an imported row).
func ValidateDiscountRow(row map[string]any, create bool) (*models.DiscountRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.DiscountRequest{}
	response.Name = b.text(v, response, "name")
	response.Type = b.text(v, response, "type", "percent", "amount")
	response.Value = b.number(v, response, "value", new(float64(0)), nil)
	response.IsActive = b.boolean(v, response, "is_active")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	if response.Type == nil && create {
		response.Type = new("percent")
	}
	required(v, "type", response.Type == nil && (create || response.IsNull("type")))
	required(v, "value", response.Value == nil && (create || response.IsNull("value")))
	if response.IsActive == nil && (create || response.IsNull("is_active")) {
		response.IsActive = new(true)
	}
	return response, v.Err()
}
