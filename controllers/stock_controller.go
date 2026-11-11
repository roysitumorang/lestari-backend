package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type stockController struct {
	authService  *middlewares.Service
	stockService services.StockService
}

func NewStockController(
	authService *middlewares.Service,
	stockService services.StockService,
) *stockController {
	return &stockController{
		authService:  authService,
		stockService: stockService,
	}
}

// Mount registers the routes of /inventory/stocks.
func (q *stockController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Get("/export", q.V1Export)
	private.Get("/import-template", q.V1ImportTemplate)
	private.Post("/import", q.V1Import)
	private.Post("/recalculate", q.V1Recalculate)
	private.Post("/:product_id<guid>/adjustments", q.V1Adjust)
	private.Get("/:product_id<guid>/movements", q.V1Movements)
}

// stockError turns the stock errors into responses; anything else goes to the error handler as it is.
func stockError(err error) error {
	switch {
	case errors.Is(err, models.ErrStockNotFound):
		return helper.NotFound("Stock")
	case errors.Is(err, models.ErrProductNotFound):
		return helper.NotFound("Product")
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List stock levels
//	@Description	The stock, unit cost and stock value of every product of the active branch that tracks stock.
//	@Tags			Stock
//	@Param			q				query		string	false	"search in name, sku"
//	@Param			category_id		query		string	false	"only products of this category"
//	@Param			stock_status	query		string	false	"in_stock, low or out"	Enums(in_stock, low, out)
//	@Param			sort			query		string	false	"sort field"			Enums(name, sku, category_name, stock, unit, minimum_stock, unit_cost, stock_value, created_at, updated_at)
//	@Param			order			query		string	false	"sort direction"		Enums(asc, desc)	default(asc)
//	@Param			page			query		int		false	"page, from 1"			default(1)
//	@Param			per_page		query		int		false	"rows per page"			default(25)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.Stock}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/inventory/stocks [get]
func (q *stockController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockController-V1Index"
	request := sanitizers.FindStocks(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.stockService.FindStocks(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStocks")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// stockExportColumns are the columns of the export.
var stockExportColumns = []export.Column{
	{Key: "product_id", Label: "id"},
	{Key: "image_url", Label: "image_url"},
	{Key: "name", Label: "name"},
	{Key: "sku", Label: "sku"},
	{Key: "category_id", Label: "category_id"},
	{Key: "category_name", Label: "category_name"},
	{Key: "stock", Label: "stock", Format: "number"},
	{Key: "unit", Label: "unit"},
	{Key: "minimum_stock", Label: "minimum_stock", Format: "number"},
	{Key: "unit_cost", Label: "unit_cost", Format: "number"},
	{Key: "stock_value", Label: "stock_value", Format: "number"},
	{Key: "created_at", Label: "created_at", Format: "datetime"},
}

// V1Export godoc
//
//	@Summary		Export stock levels
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Stock
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q				query		string	false	"search in name, sku"
//	@Param			category_id		query		string	false	"only products of this category"
//	@Param			stock_status	query		string	false	"in_stock, low or out"	Enums(in_stock, low, out)
//	@Param			sort			query		string	false	"sort field"			Enums(name, sku, category_name, stock, unit, minimum_stock, unit_cost, stock_value, created_at, updated_at)
//	@Param			order			query		string	false	"sort direction"		Enums(asc, desc)	default(asc)
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/inventory/stocks/export [get]
func (q *stockController) V1Export(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockController-V1Export"
	request := sanitizers.FindStocks(c).
		WithBranchIDs(currentBranchID(c)).
		WithLimit(50000).
		WithPage(1)
	response, _, err := q.stockService.FindStocks(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStocks")
		return err
	}
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	return export.Write(c, "stock", "Stock", stockExportColumns, rows)
}

// V1ImportTemplate godoc
//
//	@Summary		Download the stock import template
//	@Description	An .xlsx file with the columns sku*, stock*, unit_cost and note.
//	@Tags			Stock
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/inventory/stocks/import-template [get]
func (q *stockController) V1ImportTemplate(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockController-V1ImportTemplate"
	columns := []export.Column{
		{Key: "sku", Label: "sku*"},
		{Key: "stock", Label: "stock*"},
		{Key: "unit_cost", Label: "unit_cost"},
		{Key: "note", Label: "note"},
	}
	example := map[string]any{"sku": "P000001", "stock": float64(10), "unit_cost": float64(5000), "note": helper.T(c, "Initial count")}
	response, err := export.XLSX(helper.Lang(c), "Stock", columns, []map[string]any{example})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrXLSX")
		return err
	}
	return export.Send(c, "stock-template.xlsx", xlsxContentType, response)
}

// V1Import godoc
//
//	@Summary		Import stock counts
//	@Description	Upload an .xlsx or .csv file that follows the template. The stock of each product, found by its SKU, becomes the stock of its row and the difference is recorded as an adjustment; unit_cost, when given, becomes the purchase price. A row with an unknown SKU is reported and skipped.
//	@Tags			Stock
//	@Accept			multipart/form-data
//	@Param			file	formData	file	true	".xlsx or .csv file"
//	@Success		200		{object}	helper.Response{data=models.ImportResult}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/inventory/stocks/import [post]
func (q *stockController) V1Import(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockController-V1Import"
	rows, err := sanitizers.ImportRows(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrImportRows")
		return err
	}
	now := time.Now()
	response := models.ImportResult{Errors: []models.ImportError{}}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	for i, row := range rows {
		rowNo := i + 2 // the header is row 1
		request, err := sanitizers.ValidateStockImportRow(row)
		if err == nil {
			err = q.stockService.ImportStock(ctx, tx, &models.StockImport{
				StockImportRow: *request,
				BranchID:       currentBranchID(c),
				Actor:          currentActor(c),
				At:             now,
			})
			if errors.Is(err, models.ErrProductNotFound) {
				err = helper.BadRequest("No {item} with {key} {value}", "item", i18n.Tr("Product"), "key", "sku", "value", request.SKU)
			}
		}
		if err == nil {
			response.Updated++
			continue
		}
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrImportStock")
		if he := (&helper.Error{}); !errors.As(err, &he) {
			// not a problem of this row: nothing of the file is saved
			return err
		}
		response.Failed++
		response.Errors = append(response.Errors, models.ImportError{Row: rowNo, Message: describeError(helper.Lang(c), err)})
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Recalculate godoc
//
//	@Summary		Recalculate stock
//	@Description	Starts a background job that sets the stock of every tracked product of the active branch to the sum of its stock movements.
//	@Tags			Stock
//	@Success		200	{object}	helper.Response{data=models.JobResponse}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/inventory/stocks/recalculate [post]
func (q *stockController) V1Recalculate(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockController-V1Recalculate"
	response, err := q.stockService.RecalculateStocks(ctx, currentStoreID(c), currentBranchID(c), time.Now())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRecalculateStocks")
		return err
	}
	return helper.OK(c, response)
}

// V1Adjust godoc
//
//	@Summary		Adjust the stock of a product
//	@Description	Sets, adds to or subtracts from the stock and records a stock movement. Nothing is recorded when the stock stays the same.
//	@Tags			Stock
//	@Param			product_id	path		string							true	"product id (UUID)"
//	@Param			request		body		models.StockAdjustmentRequest	true	"adjustment"
//	@Success		200			{object}	helper.Response{data=models.Stock}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/inventory/stocks/{product_id}/adjustments [post]
func (q *stockController) V1Adjust(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockController-V1Adjust"
	productID, err := sanitizers.ParamID(c, "product_id", "Product")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateStockAdjustment(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateStockAdjustment")
		return err
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = q.stockService.AdjustStock(ctx, tx, &models.StockAdjustment{
		StockAdjustmentRequest: *request,
		BranchID:               currentBranchID(c),
		ProductID:              productID,
		Actor:                  currentActor(c),
		At:                     time.Now(),
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrAdjustStock")
		return stockError(err)
	}
	response, err := q.stockService.FindStock(
		ctx,
		tx,
		models.NewStockFilter().
			WithBranchIDs(currentBranchID(c)).
			WithProductIDs(productID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStock")
		return stockError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Movements godoc
//
//	@Summary	List the stock history of a product
//	@Tags		Stock
//	@Param		product_id	path		string	true	"product id (UUID)"
//	@Param		start_date	query		string	false	"movements on or after this date (YYYY-MM-DD)"
//	@Param		end_date	query		string	false	"movements on or before this date (YYYY-MM-DD)"
//	@Param		page		query		int		false	"page, from 1"	default(1)
//	@Param		per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success	200			{object}	helper.Response{data=helper.Pagination{rows=[]models.StockMovement}}
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/inventory/stocks/{product_id}/movements [get]
func (q *stockController) V1Movements(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockController-V1Movements"
	productID, err := sanitizers.ParamID(c, "product_id", "Product")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request := sanitizers.FindStockMovements(c, currentBranchID(c), productID)
	response, total, err := q.stockService.FindStockMovements(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockMovements")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}
