package sanitizers

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindCashDrawers reads the list query: q, sort, order, page, per_page.
func FindCashDrawers(c fiber.Ctx) *models.CashDrawerFilter {
	response := models.NewCashDrawerFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapCashDrawerSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateCashDrawer reads a cash drawer body. On create name is required; on update only what was sent is checked.
func ValidateCashDrawer(c fiber.Ctx, create bool) (*models.CashDrawerRequest, error) {
	ctxt := "CashDrawerSanitizer-ValidateCashDrawer"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateCashDrawerRow(b, create)
}

// ValidateCashDrawerRow validates one cash drawer given as a decoded object (a request body or an imported row).
func ValidateCashDrawerRow(row map[string]any, create bool) (*models.CashDrawerRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.CashDrawerRequest{}
	response.Name = b.text(v, response, "name")
	response.IsActive = b.boolean(v, response, "is_active")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	if response.IsActive == nil && (create || response.IsNull("is_active")) {
		response.IsActive = new(true)
	}
	return response, v.Err()
}
