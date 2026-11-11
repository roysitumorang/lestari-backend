package controllers

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/sanitizers"
)

// mountExtras registers the stock opname routes beyond create, read and delete.
func (q *stockOpnameController) mountExtras(private fiber.Router) {
	private.Get("/import-template", q.V1ImportTemplate)
	private.Post("/import", q.V1Import)
	private.Post("/:id<guid>/approve", q.V1Approve)
	private.Post("/:id<guid>/reject", q.V1Reject)
	private.Get("/:id<guid>/export", q.V1ExportItems)
}

// decide approves or rejects the stock opname of the path and returns it.
func (q *stockOpnameController) decide(c fiber.Ctx, ctxt, status, reason string) error {
	ctx := c.Context()
	stockOpnameID, err := sanitizers.ParamID(c, "id", "Stock opname")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = q.stockOpnameService.DecideStockOpname(ctx, tx, &models.StockOpnameDecision{
		StockOpnameID: stockOpnameID,
		BranchID:      currentBranchID(c),
		Status:        status,
		Reason:        reason,
		Actor:         currentActor(c),
		At:            time.Now(),
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecideStockOpname")
		return stockOpnameError(err)
	}
	response, err := q.stockOpnameService.FindStockOpname(
		ctx,
		tx,
		models.NewStockOpnameFilter().
			WithBranchIDs(currentBranchID(c)).
			WithStockOpnameIDs(stockOpnameID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockOpname")
		return stockOpnameError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Approve godoc
//
//	@Summary		Approve a stock opname
//	@Description	Sets the stock of every counted product to its counted quantity and records the differences as stock movements. Only a pending stock opname can be approved.
//	@Tags			Stock opname
//	@Param			id	path		string	true	"stock opname id (UUID)"
//	@Success		200	{object}	helper.Response{data=models.StockOpname}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-opnames/{id}/approve [post]
func (q *stockOpnameController) V1Approve(c fiber.Ctx) error {
	return q.decide(c, "StockOpnameController-V1Approve", models.StockOpnameStatusApproved, "")
}

// V1Reject godoc
//
//	@Summary		Reject a stock opname
//	@Description	Closes a pending stock opname without changing stock.
//	@Tags			Stock opname
//	@Param			id		path		string							true	"stock opname id (UUID)"
//	@Param			request	body		models.StockOpnameRejectRequest	false	"why it is rejected"
//	@Success		200		{object}	helper.Response{data=models.StockOpname}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-opnames/{id}/reject [post]
func (q *stockOpnameController) V1Reject(c fiber.Ctx) error {
	ctxt := "StockOpnameController-V1Reject"
	request, err := sanitizers.ValidateStockOpnameReject(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrValidateStockOpnameReject")
		return err
	}
	return q.decide(c, ctxt, models.StockOpnameStatusRejected, request.Reason)
}

// stockOpnameItemColumns are the columns of the export of one stock opname.
var stockOpnameItemColumns = []export.Column{
	{Key: "product_name", Label: "Product"},
	{Key: "sku", Label: "SKU"},
	{Key: "system_stock", Label: "System", Format: "number"},
	{Key: "physical_stock", Label: "Counted", Format: "number"},
	{Key: "difference", Label: "Difference", Format: "number"},
	{Key: "note", Label: "Note"},
}

// V1ExportItems godoc
//
//	@Summary	Export the items of a stock opname
//	@Tags		Stock opname
//	@Produce	application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param		id		path		string	true	"stock opname id (UUID)"
//	@Param		format	query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Success	200		{file}		binary
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/stock-opnames/{id}/export [get]
func (q *stockOpnameController) V1ExportItems(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockOpnameController-V1ExportItems"
	stockOpnameID, err := sanitizers.ParamID(c, "id", "Stock opname")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	stockOpname, err := q.stockOpnameService.FindStockOpname(
		ctx,
		nil,
		models.NewStockOpnameFilter().
			WithBranchIDs(currentBranchID(c)).
			WithStockOpnameIDs(stockOpnameID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockOpname")
		return stockOpnameError(err)
	}
	rows, err := exportRows(stockOpname.Items)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	return export.Write(
		c,
		"stock-opname-"+strings.ReplaceAll(stockOpname.Code, "/", "-"),
		helper.T(c, "Stock opname {code}", "code", stockOpname.Code),
		stockOpnameItemColumns,
		rows,
	)
}

// V1ImportTemplate godoc
//
//	@Summary		Download the stock opname import template
//	@Description	An .xlsx file with the columns sku*, physical_stock* and note.
//	@Tags			Stock opname
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-opnames/import-template [get]
func (q *stockOpnameController) V1ImportTemplate(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockOpnameController-V1ImportTemplate"
	columns := []export.Column{
		{Key: "sku", Label: "sku*"},
		{Key: "physical_stock", Label: "physical_stock*"},
		{Key: "note", Label: "note"},
	}
	example := map[string]any{"sku": "P000001", "physical_stock": float64(12)}
	response, err := export.XLSX(helper.Lang(c), "Stock opname", columns, []map[string]any{example})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrXLSX")
		return err
	}
	return export.Send(c, "stock-opname-template.xlsx", xlsxContentType, response)
}

// V1Import godoc
//
//	@Summary		Import a stock count
//	@Description	Upload an .xlsx or .csv file that follows the template. The rows become the items of one new pending stock opname dated today; a row with an unknown SKU is reported and left out.
//	@Tags			Stock opname
//	@Accept			multipart/form-data
//	@Param			file				formData	file	true	".xlsx or .csv file"
//	@Param			performed_by_name	formData	string	false	"who counted; the signed-in user by default"
//	@Param			filename			formData	string	false	"name shown in the note of the opname"
//	@Success		200					{object}	helper.Response{data=models.ImportResult}
//	@Failure		401					{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422					{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-opnames/import [post]
func (q *stockOpnameController) V1Import(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockOpnameController-V1Import"
	rows, err := sanitizers.ImportRows(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrImportRows")
		return err
	}
	response := models.ImportResult{Errors: []models.ImportError{}}
	items := make([]*models.StockOpnameItem, 0, len(rows))
	for i, row := range rows {
		rowNo := i + 2 // the header is row 1
		sku := helper.Str(row, "sku")
		product, err := q.productService.FindProduct(
			ctx,
			nil,
			models.NewProductFilter().
				WithBranchIDs(currentBranchID(c)).
				WithSKU(sku),
		)
		if sku == "" || errors.Is(err, models.ErrProductNotFound) {
			response.Failed++
			response.Errors = append(response.Errors, models.ImportError{
				Row:     rowNo,
				Message: helper.T(c, "No {item} with {key} {value}", "item", i18n.Tr("Product"), "key", "sku", "value", sku),
			})
			continue
		}
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProduct")
			return err
		}
		item := &models.StockOpnameItem{Row: rowNo, ProductID: &product.ID}
		item.PhysicalStock, _ = helper.Num(row, "physical_stock")
		if note := helper.Str(row, "note"); note != "" {
			item.Note = &note
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return helper.OK(c, response)
	}
	principal := helper.P(c)
	now := time.Now()
	note := helper.T(c, "Imported from {file}", "file", c.FormValue("filename", helper.T(c, "spreadsheet")))
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if _, err = q.stockOpnameService.CreateStockOpname(ctx, tx, &models.StockOpname{
		BranchID:        currentBranchID(c),
		OpnameDate:      now.In(helper.Location).Format(time.DateOnly),
		PerformedByName: c.FormValue("performed_by_name", principal.Name),
		PerformedByRole: principal.Role,
		Status:          models.StockOpnameStatusPending,
		Note:            &note,
		Items:           items,
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStockOpname")
		return stockOpnameError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	response.Created = 1
	return helper.OK(c, response)
}
