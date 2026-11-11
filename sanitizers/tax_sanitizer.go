package sanitizers

import (
	"log/slog"
	"strings"
	"uuid"

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
	return response.
		WithSortings(sorting(c, models.MapTaxSortings, models.DefaultTaxSortBy)).
		WithLimit(limit).
		WithPage(page)
}

// ValidateTax reads a tax body. With create, name and rate are required; on update only what was sent is checked.
func ValidateTax(c fiber.Ctx, create bool) (*models.TaxRequest, error) {
	ctxt := "TaxSanitizer-ValidateTax"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelWarn, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	v := &helper.Validation{}
	response := &models.TaxRequest{
		Name:        b.text(v, "name"),
		Rate:        b.number(v, "rate"),
		IsInclusive: b.boolean(v, "is_inclusive"),
		IsActive:    b.boolean(v, "is_active"),
	}
	switch {
	case response.Name != nil && *response.Name == "":
		v.Add("name", "Required")
	case create && response.Name == nil && len(v.Fields["name"]) == 0:
		v.Add("name", "Required")
	}
	if create && response.Rate == nil && len(v.Fields["rate"]) == 0 {
		v.Add("rate", "Required")
	}
	if response.Rate != nil {
		switch {
		case *response.Rate < 0:
			v.Add("rate", "Must be at least {min}.", "min", "0")
		case *response.Rate > 100:
			v.Add("rate", "Must be at most {max}.", "max", "100")
		}
	}
	return response, v.Err()
}

// DeleteTaxes reads {"ids": […]} of POST /taxes/bulk-delete.
func DeleteTaxes(c fiber.Ctx) ([]uuid.UUID, error) {
	b, err := parseBody(c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	ids := b.ids(v, "ids")
	return ids, v.Err()
}
