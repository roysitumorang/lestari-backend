package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type returnController struct {
	authService   *middlewares.Service
	returnService services.ReturnService
}

func NewReturnController(
	authService *middlewares.Service,
	returnService services.ReturnService,
) *returnController {
	return &returnController{
		authService:   authService,
		returnService: returnService,
	}
}

// Mount registers the return routes: the lists under /sales-returns and /purchase-returns, and the creation
// under the sale or purchase returned against.
func (q *returnController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("/sales-returns", q.V1SalesReturns)
	private.Get("/sales-returns/export", q.V1SalesReturnsExport)
	private.Get("/sales-returns/:id<guid>", q.V1SalesReturn)
	private.Post("/sales/:id<guid>/returns", q.V1CreateSalesReturn)
	private.Get("/purchase-returns", q.V1PurchaseReturns)
	private.Get("/purchase-returns/export", q.V1PurchaseReturnsExport)
	private.Get("/purchase-returns/:id<guid>", q.V1PurchaseReturn)
	private.Post("/purchases/:id<guid>/returns", q.V1CreatePurchaseReturn)
}

// returnNames are the names of a return of a kind in messages.
var returnNames = map[string]string{models.ReturnKindSale: "Sales return", models.ReturnKindPurchase: "Purchase return"}

// returnError turns the return errors into responses; anything else goes to the error handler as it is.
func returnError(err error, kind string) error {
	switch {
	case errors.Is(err, models.ErrReturnNotFound):
		return helper.NotFound(returnNames[kind])
	case errors.Is(err, models.ErrSaleNotFound):
		return helper.NotFound("Sale")
	case errors.Is(err, models.ErrPurchaseNotFound):
		return helper.NotFound("Purchase")
	}
	return writeError(err)
}

func (q *returnController) index(c fiber.Ctx, ctxt, kind string) error {
	ctx := c.Context()
	request := sanitizers.FindReturns(c, kind)
	request.BranchID = currentBranchID(c)
	response, total, err := q.returnService.FindReturns(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindReturns")
		return err
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

// returnExportColumns are the columns of the export of a kind: number names the receipt or purchase number.
func returnExportColumns(document, number, party string) []export.Column {
	return []export.Column{
		{Key: "id", Label: "id"},
		{Key: "return_number", Label: "return_number"},
		{Key: "created_at", Label: "created_at", Format: "datetime"},
		{Key: "refund_method", Label: "refund_method"},
		{Key: "refund_amount", Label: "refund_amount", Format: "number"},
		{Key: "reason", Label: "reason"},
		{Key: "cashier_name", Label: "cashier_name"},
		{Key: "item_count", Label: "item_count", Format: "number"},
		{Key: document + "_id", Label: document + "_id"},
		{Key: number, Label: number},
		{Key: party + ".name", Label: party + "_name"},
	}
}

func (q *returnController) export(c fiber.Ctx, ctxt, kind, name, title string, columns []export.Column) error {
	ctx := c.Context()
	request := sanitizers.FindReturns(c, kind)
	request.BranchID, request.Limit, request.Page = currentBranchID(c), exportLimit, 1
	response, _, err := q.returnService.FindReturns(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindReturns")
		return err
	}
	return writeExport(c, ctxt, name, title, columns, response)
}

func (q *returnController) show(c fiber.Ctx, ctxt, kind string) error {
	ctx := c.Context()
	returnID, err := sanitizers.ParamID(c, "id", returnNames[kind])
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.returnService.FindReturn(ctx, nil, currentBranchID(c), returnID, kind)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindReturn")
		return returnError(err, kind)
	}
	return helper.OK(c, response)
}

func (q *returnController) create(c fiber.Ctx, ctxt, kind, what string) error {
	ctx := c.Context()
	documentID, err := sanitizers.ParamID(c, "id", what)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateReturn(c, kind)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateReturn")
		return err
	}
	var response *models.Return
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.returnService.CreateReturn(ctx, tx, &models.ReturnCreate{
			ReturnRequest: request,
			BranchID:      currentBranchID(c),
			DocumentID:    documentID,
			Kind:          kind,
			Actor:         currentActor(c),
			At:            time.Now(),
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateReturn")
		}
		return returnError(err, kind)
	}); err != nil {
		return err
	}
	return helper.Created(c, response)
}

// V1SalesReturns godoc
//
//	@Summary		List sales returns
//	@Description	Goods customers returned, newest first.
//	@Tags			Sales
//	@Param			q			query		string	false	"search in return number, sale number"
//	@Param			start_date	query		string	false	"returns on or after this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"returns on or before this date (YYYY-MM-DD)"
//	@Param			sort		query		string	false	"sort field"		Enums(created_at, return_number, refund_method, refund_amount, cashier_name, item_count)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Return}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/sales-returns [get]
func (q *returnController) V1SalesReturns(c fiber.Ctx) error {
	return q.index(c, "ReturnController-V1SalesReturns", models.ReturnKindSale)
}

// V1SalesReturnsExport godoc
//
//	@Summary		Export sales returns
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Sales
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q			query		string	false	"search in return number, sale number"
//	@Param			start_date	query		string	false	"returns on or after this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"returns on or before this date (YYYY-MM-DD)"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/sales-returns/export [get]
func (q *returnController) V1SalesReturnsExport(c fiber.Ctx) error {
	return q.export(c, "ReturnController-V1SalesReturnsExport", models.ReturnKindSale, "sales-returns", "Sales returns", returnExportColumns("sale", "receipt_number", "customer"))
}

// V1SalesReturn godoc
//
//	@Summary	Show a sales return
//	@Tags		Sales
//	@Param		id	path		string	true	"return id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Return}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/sales-returns/{id} [get]
func (q *returnController) V1SalesReturn(c fiber.Ctx) error {
	return q.show(c, "ReturnController-V1SalesReturn", models.ReturnKindSale)
}

// V1CreateSalesReturn godoc
//
//	@Summary		Return items of a sale
//	@Description	Used by cashier apps. Every item must be on the sale, for no more than was sold; its stock goes back (the ingredients for a recipe) and the refund is worked out from what was paid for it. The number (RJ/YYYYMMDD/NNNN) is generated.
//	@Tags			Sales
//	@Param			id		path		string					true	"sale id (UUID)"
//	@Param			request	body		models.ReturnRequest	true	"items to return"
//	@Success		201		{object}	helper.Response{data=models.Return}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/sales/{id}/returns [post]
func (q *returnController) V1CreateSalesReturn(c fiber.Ctx) error {
	return q.create(c, "ReturnController-V1CreateSalesReturn", models.ReturnKindSale, "Sale")
}

// V1PurchaseReturns godoc
//
//	@Summary		List purchase returns
//	@Description	Goods returned to suppliers, newest first.
//	@Tags			Purchases
//	@Param			q			query		string	false	"search in return number, purchase number"
//	@Param			start_date	query		string	false	"returns on or after this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"returns on or before this date (YYYY-MM-DD)"
//	@Param			sort		query		string	false	"sort field"		Enums(created_at, return_number, refund_method, refund_amount, cashier_name, item_count)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Return}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/purchase-returns [get]
func (q *returnController) V1PurchaseReturns(c fiber.Ctx) error {
	return q.index(c, "ReturnController-V1PurchaseReturns", models.ReturnKindPurchase)
}

// V1PurchaseReturnsExport godoc
//
//	@Summary		Export purchase returns
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Purchases
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q			query		string	false	"search in return number, purchase number"
//	@Param			start_date	query		string	false	"returns on or after this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"returns on or before this date (YYYY-MM-DD)"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/purchase-returns/export [get]
func (q *returnController) V1PurchaseReturnsExport(c fiber.Ctx) error {
	return q.export(c, "ReturnController-V1PurchaseReturnsExport", models.ReturnKindPurchase, "purchase-returns", "Purchase returns", returnExportColumns("purchase", "purchase_number", "supplier"))
}

// V1PurchaseReturn godoc
//
//	@Summary	Show a purchase return
//	@Tags		Purchases
//	@Param		id	path		string	true	"return id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Return}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/purchase-returns/{id} [get]
func (q *returnController) V1PurchaseReturn(c fiber.Ctx) error {
	return q.show(c, "ReturnController-V1PurchaseReturn", models.ReturnKindPurchase)
}

// V1CreatePurchaseReturn godoc
//
//	@Summary		Return items of a purchase
//	@Description	Every item must be on the purchase, for no more than what was bought and not yet returned; its stock goes out and the refund is worked out from what was paid for it. With refund_method reduce_payable the payable of the purchase is lowered. The number (RB/YYYYMMDD/NNNN) is generated.
//	@Tags			Purchases
//	@Param			id		path		string					true	"purchase id (UUID)"
//	@Param			request	body		models.ReturnRequest	true	"items to return"
//	@Success		201		{object}	helper.Response{data=models.Return}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/purchases/{id}/returns [post]
func (q *returnController) V1CreatePurchaseReturn(c fiber.Ctx) error {
	return q.create(c, "ReturnController-V1CreatePurchaseReturn", models.ReturnKindPurchase, "Purchase")
}
