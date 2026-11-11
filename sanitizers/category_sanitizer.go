package sanitizers

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindCategories reads the list query: q, sort, order, page, per_page.
func FindCategories(c fiber.Ctx) *models.CategoryFilter {
	response := models.NewCategoryFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapCategorySortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateCategory reads a category body. On create name is required; on update only what was sent is checked.
func ValidateCategory(c fiber.Ctx, create bool) (*models.CategoryRequest, error) {
	ctxt := "CategorySanitizer-ValidateCategory"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateCategoryRow(b, create)
}

// ValidateCategoryRow validates one category given as a decoded object (a request body or an imported row).
func ValidateCategoryRow(row map[string]any, create bool) (*models.CategoryRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.CategoryRequest{}
	response.Name = b.text(v, response, "name")
	response.Color = b.text(v, response, "color")
	response.SortOrder = b.integer(v, response, "sort_order", nil, nil)
	response.Description = b.text(v, response, "description")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	return response, v.Err()
}
