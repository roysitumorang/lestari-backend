package sanitizers

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindCustomerTypes reads the list query: q, sort, order, page, per_page.
func FindCustomerTypes(c fiber.Ctx) *models.CustomerTypeFilter {
	response := models.NewCustomerTypeFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapCustomerTypeSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateCustomerType reads a customer type body. On create name is required; on update only what was sent is checked.
func ValidateCustomerType(c fiber.Ctx, create bool) (*models.CustomerTypeRequest, error) {
	ctxt := "CustomerTypeSanitizer-ValidateCustomerType"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateCustomerTypeRow(b, create)
}

// ValidateCustomerTypeRow validates one customer type given as a decoded object (a request body or an imported row).
func ValidateCustomerTypeRow(row map[string]any, create bool) (*models.CustomerTypeRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.CustomerTypeRequest{}
	response.Name = b.text(v, response, "name")
	response.DiscountType = b.text(v, response, "discount_type", "percent", "amount")
	response.DiscountValue = b.number(v, response, "discount_value", new(float64(0)), nil)
	response.PointMultiplier = b.number(v, response, "point_multiplier", new(float64(0)), nil)
	response.Description = b.text(v, response, "description")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	if response.PointMultiplier == nil && create {
		response.PointMultiplier = new(float64(1))
	}
	return response, v.Err()
}
