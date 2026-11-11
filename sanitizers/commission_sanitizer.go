package sanitizers

import (
	"log/slog"
	"strings"
	"uuid"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindCommissions reads the list query: q, sort, order, page, per_page.
func FindCommissions(c fiber.Ctx) *models.CommissionFilter {
	response := models.NewCommissionFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapCommissionSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateCommission reads a commission rule body. On create name, basis, type, value are required; on update only what was sent is checked.
func ValidateCommission(c fiber.Ctx, create bool) (*models.CommissionRequest, error) {
	ctxt := "CommissionSanitizer-ValidateCommission"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateCommissionRow(b, create)
}

// ValidateCommissionRow validates one commission rule given as a decoded object (a request body or an imported row).
func ValidateCommissionRow(row map[string]any, create bool) (*models.CommissionRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.CommissionRequest{}
	response.Name = b.text(v, response, "name")
	response.Basis = b.text(v, response, "basis", "product", "transaction")
	response.Type = b.text(v, response, "type", "percent", "amount")
	response.Value = b.number(v, response, "value", new(float64(0)), nil)
	response.StaffIds = b.idList(v, response, "staff_ids")
	response.ProductIds = b.idList(v, response, "product_ids")
	response.IsActive = b.boolean(v, response, "is_active")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	if response.Basis == nil && create {
		response.Basis = new("product")
	}
	required(v, "basis", response.Basis == nil && (create || response.IsNull("basis")))
	if response.Type == nil && create {
		response.Type = new("percent")
	}
	required(v, "type", response.Type == nil && (create || response.IsNull("type")))
	required(v, "value", response.Value == nil && (create || response.IsNull("value")))
	if response.StaffIds == nil && (create || response.IsNull("staff_ids")) {
		response.StaffIds = []uuid.UUID{}
	}
	if response.ProductIds == nil && (create || response.IsNull("product_ids")) {
		response.ProductIds = []uuid.UUID{}
	}
	if response.IsActive == nil && (create || response.IsNull("is_active")) {
		response.IsActive = new(true)
	}
	return response, v.Err()
}
