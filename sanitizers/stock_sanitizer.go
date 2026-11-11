package sanitizers

import (
	"log/slog"
	"slices"
	"strings"
	"uuid"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/utils/v2"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// stockMovementLimit is the page size of a stock history when the request names none.
const stockMovementLimit int64 = 50

// FindStocks reads the list query: q, category_id, stock_status, sort, order, page, per_page.
func FindStocks(c fiber.Ctx) *models.StockFilter {
	response := models.NewStockFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if categoryID, ok := queryID(c, "category_id"); ok {
		response.WithCategoryID(categoryID)
	}
	if stockStatus := c.Query("stock_status"); slices.Contains([]string{"in_stock", "low", "out"}, stockStatus) {
		response.WithStockStatus(stockStatus)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapStockSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// FindStockMovements reads the history query: start_date, end_date, page, per_page (50 by default).
func FindStockMovements(c fiber.Ctx, branchID, productID uuid.UUID) *models.StockMovementFilter {
	response := &models.StockMovementFilter{
		ProductID: productID,
		BranchID:  branchID,
	}
	response.StartDate, _ = queryDate(c, "start_date")
	response.EndDate, _ = queryDate(c, "end_date")
	if response.Limit, _ = utils.ParseInt(c.Query("per_page")); response.Limit < 1 {
		response.Limit = stockMovementLimit
	}
	response.Limit = min(response.Limit, models.MaxLimit)
	response.Page, _ = utils.ParseInt(c.Query("page"))
	response.Page = max(response.Page, 1)
	return response
}

// ValidateStockAdjustment reads the body of a stock adjustment: mode and quantity are required.
func ValidateStockAdjustment(c fiber.Ctx) (*models.StockAdjustmentRequest, error) {
	ctxt := "StockSanitizer-ValidateStockAdjustment"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	v := &helper.Validation{}
	response := &models.StockAdjustmentRequest{
		Mode:   helper.Str(b, "mode"),
		Reason: helper.Str(b, "reason"),
		Note:   helper.Str(b, "note"),
	}
	if !slices.Contains(models.StockAdjustmentModes, response.Mode) {
		v.Add("mode", "Must be one of: set, add, subtract.")
	}
	quantity, ok := helper.Num(b, "quantity")
	if !ok || quantity < 0 {
		v.Add("quantity", "Enter a quantity of 0 or more.")
	}
	response.Quantity = quantity
	if response.Reason != "" && !slices.Contains(models.StockAdjustmentReasons, response.Reason) {
		v.Add("reason", "Unknown reason.")
	}
	if unitCost, ok := helper.Num(b, "unit_cost"); ok {
		response.UnitCost = &unitCost
	}
	return response, v.Err()
}

// ValidateStockImportRow reads one row of the stock import file: sku and stock are required.
func ValidateStockImportRow(row map[string]any) (*models.StockImportRow, error) {
	response := &models.StockImportRow{
		SKU:  helper.Str(row, "sku"),
		Note: helper.Str(row, "note"),
	}
	if stock, ok := helper.Num(row, "stock"); ok {
		response.Stock = &stock
	}
	if unitCost, ok := helper.Num(row, "unit_cost"); ok {
		response.UnitCost = &unitCost
	}
	if response.SKU == "" || response.Stock == nil {
		return nil, helper.BadRequest("sku and stock are required")
	}
	return response, nil
}
