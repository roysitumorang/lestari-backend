package sanitizers

import (
	"log/slog"
	"strings"
	"uuid"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindStockOpnames reads the list query: q, status, sort, order, page, per_page.
func FindStockOpnames(c fiber.Ctx) *models.StockOpnameFilter {
	response := models.NewStockOpnameFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if status := c.Query("status"); status != "" {
		response.WithStatus(status)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapStockOpnameSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateStockOpname reads a stock opname body. opname_date, performed_by_name, performed_by_role and at least one item are required.
func ValidateStockOpname(c fiber.Ctx, create bool) (*models.StockOpnameRequest, error) {
	ctxt := "StockOpnameSanitizer-ValidateStockOpname"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateStockOpnameRow(b, create)
}

// ValidateStockOpnameRow validates one stock opname given as a decoded object (a request body or an imported row).
func ValidateStockOpnameRow(row map[string]any, create bool) (*models.StockOpnameRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.StockOpnameRequest{}
	response.OpnameDate = b.date(v, response, "opname_date")
	response.PerformedByName = b.text(v, response, "performed_by_name")
	response.PerformedByRole = b.text(v, response, "performed_by_role", "owner", "administrator", "manager", "cashier")
	response.Note = b.text(v, response, "note")
	required(v, "opname_date", response.OpnameDate == nil && (create || response.IsNull("opname_date")))
	required(v, "performed_by_name", response.PerformedByName == nil && (create || response.IsNull("performed_by_name")))
	required(v, "performed_by_role", response.PerformedByRole == nil && (create || response.IsNull("performed_by_role")))
	for i, item := range helper.Items(row, "items") {
		productID, hasProduct := helper.UUID(item, "product_id")
		physicalStock, hasStock := helper.Num(item, "physical_stock")
		if !hasProduct && !hasStock {
			continue // an empty row of the form
		}
		id, err := uuid.Parse(productID)
		if !hasProduct || !hasStock || physicalStock < 0 || err != nil {
			v.Add("items", "Row {row} needs a product and a counted quantity of 0 or more.", "row", i+1)
			continue
		}
		counted := &models.StockOpnameItem{
			Row:           i + 1,
			ProductID:     &id,
			PhysicalStock: physicalStock,
		}
		if note := helper.Str(item, "note"); note != "" {
			counted.Note = &note
		}
		response.CountedItems = append(response.CountedItems, counted)
	}
	if len(response.CountedItems) == 0 && len(v.Fields) == 0 {
		v.Add("items", "Add at least one counted product.")
	}
	return response, v.Err()
}

// ValidateStockOpnameReject reads the optional reason of a rejection.
func ValidateStockOpnameReject(c fiber.Ctx) (*models.StockOpnameRejectRequest, error) {
	ctxt := "StockOpnameSanitizer-ValidateStockOpnameReject"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return &models.StockOpnameRejectRequest{Reason: helper.Str(b, "reason")}, nil
}
