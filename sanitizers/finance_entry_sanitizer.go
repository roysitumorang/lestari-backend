package sanitizers

import (
	"log/slog"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindFinanceEntries reads the list query: q, type, start_date, end_date, sort, order, page, per_page.
func FindFinanceEntries(c fiber.Ctx) *models.FinanceEntryFilter {
	response := models.NewFinanceEntryFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if typeValue := c.Query("type"); typeValue != "" && slices.Contains([]string{"income", "expense"}, typeValue) {
		response.WithType(typeValue)
	}
	if startDate, ok := queryDate(c, "start_date"); ok {
		response.WithStartDate(startDate)
	}
	if endDate, ok := queryDate(c, "end_date"); ok {
		response.WithEndDate(endDate)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapFinanceEntrySortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateFinanceEntry reads a entry body. On create entry_date, type, category, amount are required; on update only what was sent is checked.
func ValidateFinanceEntry(c fiber.Ctx, create bool) (*models.FinanceEntryRequest, error) {
	ctxt := "FinanceEntrySanitizer-ValidateFinanceEntry"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateFinanceEntryRow(b, create)
}

// ValidateFinanceEntryRow validates one entry given as a decoded object (a request body or an imported row).
func ValidateFinanceEntryRow(row map[string]any, create bool) (*models.FinanceEntryRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.FinanceEntryRequest{}
	response.EntryDate = b.date(v, response, "entry_date")
	response.Type = b.text(v, response, "type", "income", "expense")
	response.Category = b.text(v, response, "category")
	response.Amount = b.number(v, response, "amount", new(float64(0)), nil)
	response.PaymentMethod = b.text(v, response, "payment_method", "cash", "debit", "credit_card", "qris", "e_wallet", "bank_transfer", "other")
	response.CashDrawerID = b.id(v, response, "cash_drawer_id")
	response.Description = b.text(v, response, "description")
	response.AttachmentURL = b.text(v, response, "attachment_url")
	required(v, "entry_date", response.EntryDate == nil && (create || response.IsNull("entry_date")))
	required(v, "type", response.Type == nil && (create || response.IsNull("type")))
	required(v, "category", response.Category == nil && (create || response.IsNull("category")))
	required(v, "amount", response.Amount == nil && (create || response.IsNull("amount")))
	return response, v.Err()
}
