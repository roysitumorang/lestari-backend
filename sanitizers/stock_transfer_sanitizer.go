package sanitizers

import (
	"log/slog"
	"slices"
	"strings"
	"uuid"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindStockTransfers reads the list query: q, status, start_date, end_date, sort, order, page, per_page. The
// direction and role filters need the active branch and are read with StockTransferDirection and StockTransferRole.
func FindStockTransfers(c fiber.Ctx) *models.StockTransferFilter {
	response := models.NewStockTransferFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if status := c.Query("status"); status != "" {
		response.WithStatus(status)
	}
	if startDate, ok := queryDate(c, "start_date"); ok {
		response.WithStartDate(startDate)
	}
	if endDate, ok := queryDate(c, "end_date"); ok {
		response.WithEndDate(endDate)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapStockTransferSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// StockTransferDirection reads ?direction=in|out; anything else is "".
func StockTransferDirection(c fiber.Ctx) string {
	if direction := c.Query("direction"); slices.Contains([]string{models.StockTransferDirectionIn, models.StockTransferDirectionOut}, direction) {
		return direction
	}
	return ""
}

// StockTransferRole reads ?role=head_office|branch; anything else is "".
func StockTransferRole(c fiber.Ctx) string {
	if role := c.Query("role"); slices.Contains([]string{models.StockTransferRoleHeadOffice, models.StockTransferRoleBranch}, role) {
		return role
	}
	return ""
}

// ValidateStockTransfer reads the body of a new transfer: to_branch_id, transfer_date and at least one item are
// required.
func ValidateStockTransfer(c fiber.Ctx) (*models.StockTransfer, error) {
	ctxt := "StockTransferSanitizer-ValidateStockTransfer"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	v := &helper.Validation{}
	nulls := &models.Nulls{}
	response := &models.StockTransfer{Status: models.StockTransferStatusCompleted}
	toBranchID := b.id(v, nulls, "to_branch_id")
	required(v, "to_branch_id", toBranchID == nil)
	if toBranchID != nil {
		response.ToBranchID = *toBranchID
	}
	transferDate := b.date(v, nulls, "transfer_date")
	required(v, "transfer_date", transferDate == nil)
	if transferDate != nil {
		response.TransferDate = *transferDate
	}
	if requiresConfirmation := b.boolean(v, nulls, "requires_confirmation"); requiresConfirmation != nil && *requiresConfirmation {
		response.RequiresConfirmation = true
		response.Status = models.StockTransferStatusPending
	}
	response.Note = b.text(v, nulls, "note")
	for i, row := range helper.Items(b, "items") {
		productID, hasProduct := helper.UUID(row, "product_id")
		quantity, hasQuantity := helper.Num(row, "quantity")
		if !hasProduct && !hasQuantity {
			continue // an empty row of the form
		}
		id, err := uuid.Parse(productID)
		if !hasProduct || !hasQuantity || quantity <= 0 || err != nil {
			v.Add("items", "Row {row} needs a product and a quantity above 0.", "row", i+1)
			continue
		}
		item := &models.StockTransferItem{
			Row:       i + 1,
			ProductID: &id,
			Quantity:  quantity,
		}
		if note := helper.Str(row, "note"); note != "" {
			item.Note = &note
		}
		response.Items = append(response.Items, item)
	}
	if len(response.Items) == 0 && len(v.Fields["items"]) == 0 {
		v.Add("items", "Add at least one item.")
	}
	return response, v.Err()
}

// ValidateStockTransferAccept reads the optional received quantities and note of an acceptance.
func ValidateStockTransferAccept(c fiber.Ctx) (received map[uuid.UUID]float64, note string, err error) {
	ctxt := "StockTransferSanitizer-ValidateStockTransferAccept"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, "", err
	}
	received = map[uuid.UUID]float64{}
	for _, row := range helper.Items(b, "items") {
		productID, hasProduct := helper.UUID(row, "product_id")
		quantity, hasQuantity := helper.Num(row, "received_quantity")
		if id, err := uuid.Parse(productID); hasProduct && hasQuantity && err == nil {
			received[id] = quantity
		}
	}
	return received, helper.Str(b, "note"), nil
}

// ValidateStockTransferReject reads the reason of a rejection, which is required.
func ValidateStockTransferReject(c fiber.Ctx) (string, error) {
	ctxt := "StockTransferSanitizer-ValidateStockTransferReject"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return "", err
	}
	reason := helper.Str(b, "reason")
	if reason == "" {
		return "", helper.Invalid("reason", "Required")
	}
	return reason, nil
}
