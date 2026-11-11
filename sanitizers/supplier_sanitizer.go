package sanitizers

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindSuppliers reads the list query: q, sort, order, page, per_page.
func FindSuppliers(c fiber.Ctx) *models.SupplierFilter {
	response := models.NewSupplierFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapSupplierSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateSupplier reads a supplier body. On create name is required; on update only what was sent is checked.
func ValidateSupplier(c fiber.Ctx, create bool) (*models.SupplierRequest, error) {
	ctxt := "SupplierSanitizer-ValidateSupplier"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateSupplierRow(b, create)
}

// ValidateSupplierRow validates one supplier given as a decoded object (a request body or an imported row).
func ValidateSupplierRow(row map[string]any, create bool) (*models.SupplierRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.SupplierRequest{}
	response.Name = b.text(v, response, "name")
	response.ContactPerson = b.text(v, response, "contact_person")
	response.Phone = b.text(v, response, "phone")
	response.Email = b.text(v, response, "email")
	response.Code = b.text(v, response, "code")
	response.Address = b.text(v, response, "address")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	return response, v.Err()
}
