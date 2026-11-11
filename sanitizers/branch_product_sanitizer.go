package sanitizers

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindBranchProducts reads the list query: q, branch_id, category_id, sort, order, page, per_page.
func FindBranchProducts(c fiber.Ctx) *models.BranchProductFilter {
	response := models.NewBranchProductFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if branchID, ok := queryID(c, "branch_id"); ok {
		response.WithBranchID(branchID)
	}
	if categoryID, ok := queryID(c, "category_id"); ok {
		response.WithCategoryID(categoryID)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapBranchProductSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateBranchProduct reads the body of an update; only what was sent is checked, and selling_price cannot be
// cleared.
func ValidateBranchProduct(c fiber.Ctx) (*models.BranchProductRequest, error) {
	ctxt := "BranchProductSanitizer-ValidateBranchProduct"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	v := &helper.Validation{}
	response := &models.BranchProductRequest{}
	response.SellingPrice = b.number(v, response, "selling_price", new(float64(0)), nil)
	response.PurchasePrice = b.number(v, response, "purchase_price", new(float64(0)), nil)
	response.IsAvailable = b.boolean(v, response, "is_available")
	required(v, "selling_price", response.SellingPrice == nil && response.IsNull("selling_price"))
	return response, v.Err()
}

// ValidateBranchPriceSync reads the body of a price sync: at least one branch is required.
func ValidateBranchPriceSync(c fiber.Ctx) (*models.BranchPriceSync, error) {
	ctxt := "BranchProductSanitizer-ValidateBranchPriceSync"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	v := &helper.Validation{}
	nulls := &models.Nulls{}
	response := &models.BranchPriceSync{
		BranchIDs:  b.idList(v, nulls, "branch_ids"),
		ProductIDs: b.idList(v, nulls, "product_ids"),
	}
	if len(response.BranchIDs) == 0 && len(v.Fields["branch_ids"]) == 0 {
		v.Add("branch_ids", "Choose at least one branch.")
	}
	return response, v.Err()
}
