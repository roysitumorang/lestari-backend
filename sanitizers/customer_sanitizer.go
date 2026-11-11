package sanitizers

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindCustomers reads the list query: q, customer_type_id, sort, order, page, per_page.
func FindCustomers(c fiber.Ctx) *models.CustomerFilter {
	response := models.NewCustomerFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if customerTypeID, ok := queryID(c, "customer_type_id"); ok {
		response.WithCustomerTypeID(customerTypeID)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapCustomerSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateCustomer reads a customer body. On create name is required; on update only what was sent is checked.
func ValidateCustomer(c fiber.Ctx, create bool) (*models.CustomerRequest, error) {
	ctxt := "CustomerSanitizer-ValidateCustomer"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateCustomerRow(b, create)
}

// ValidateCustomerRow validates one customer given as a decoded object (a request body or an imported row).
func ValidateCustomerRow(row map[string]any, create bool) (*models.CustomerRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.CustomerRequest{}
	response.Name = b.text(v, response, "name")
	response.Phone = b.text(v, response, "phone")
	response.Email = b.text(v, response, "email")
	response.Code = b.text(v, response, "code")
	response.CustomerTypeID = b.id(v, response, "customer_type_id")
	response.BirthDate = b.date(v, response, "birth_date")
	response.Address = b.text(v, response, "address")
	response.Note = b.text(v, response, "note")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	return response, v.Err()
}
