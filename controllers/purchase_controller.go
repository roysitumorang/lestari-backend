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

type purchaseController struct {
	authService     *middlewares.Service
	purchaseService services.PurchaseService
}

func NewPurchaseController(
	authService *middlewares.Service,
	purchaseService services.PurchaseService,
) *purchaseController {
	return &purchaseController{
		authService:     authService,
		purchaseService: purchaseService,
	}
}

// Mount registers the purchase routes under /purchases.
func (q *purchaseController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Get("/export", q.V1Export)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", q.V1Update)
	private.Delete("/:id<guid>", q.V1Delete)
}

// purchaseError turns the purchase errors into responses; anything else goes to the error handler as it is.
func purchaseError(err error) error {
	if errors.Is(err, models.ErrPurchaseNotFound) {
		return helper.NotFound("Purchase")
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List purchases
//	@Description	Purchases of the active branch, newest first.
//	@Tags			Purchases
//	@Param			q				query		string	false	"search in number, supplier name, invoice number"
//	@Param			supplier_id		query		string	false	"only this supplier (UUID)"
//	@Param			payment_type	query		string	false	"only cash or only credit purchases"	Enums(cash, credit)
//	@Param			start_date		query		string	false	"purchases on or after this date (YYYY-MM-DD)"
//	@Param			end_date		query		string	false	"purchases on or before this date (YYYY-MM-DD)"
//	@Param			sort			query		string	false	"sort field"		Enums(purchase_date, receipt_number, supplier_name, invoice_number, payment_type, due_date, subtotal, grand_total, paid_amount, staff_name, status, created_at)
//	@Param			order			query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page			query		int		false	"page, from 1"		default(1)
//	@Param			per_page		query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.Purchase}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/purchases [get]
func (q *purchaseController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PurchaseController-V1Index"
	request := sanitizers.FindPurchases(c)
	request.BranchID = currentBranchID(c)
	response, total, err := q.purchaseService.FindPurchases(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchases")
		return err
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

var purchaseExportColumns = []export.Column{
	{Key: "id", Label: "id"},
	{Key: "receipt_number", Label: "receipt_number"},
	{Key: "supplier_id", Label: "supplier_id"},
	{Key: "supplier_name", Label: "supplier_name"},
	{Key: "purchase_date", Label: "purchase_date", Format: "date"},
	{Key: "invoice_number", Label: "invoice_number"},
	{Key: "payment_type", Label: "payment_type"},
	{Key: "payment_method", Label: "payment_method"},
	{Key: "due_date", Label: "due_date", Format: "date"},
	{Key: "paid_amount", Label: "paid_amount", Format: "number"},
	{Key: "subtotal", Label: "subtotal", Format: "number"},
	{Key: "discount_total", Label: "discount_total", Format: "number"},
	{Key: "tax_total", Label: "tax_total", Format: "number"},
	{Key: "fee_total", Label: "fee_total", Format: "number"},
	{Key: "grand_total", Label: "grand_total", Format: "number"},
	{Key: "staff_name", Label: "staff_name"},
	{Key: "status", Label: "status"},
	{Key: "note", Label: "note"},
	{Key: "created_at", Label: "created_at", Format: "datetime"},
}

// V1Export godoc
//
//	@Summary		Export purchases
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Purchases
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q				query		string	false	"search in number, supplier name, invoice number"
//	@Param			supplier_id		query		string	false	"only this supplier (UUID)"
//	@Param			payment_type	query		string	false	"only cash or only credit purchases"	Enums(cash, credit)
//	@Param			start_date		query		string	false	"purchases on or after this date (YYYY-MM-DD)"
//	@Param			end_date		query		string	false	"purchases on or before this date (YYYY-MM-DD)"
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/purchases/export [get]
func (q *purchaseController) V1Export(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PurchaseController-V1Export"
	request := sanitizers.FindPurchases(c)
	request.BranchID, request.Limit, request.Page = currentBranchID(c), exportLimit, 1
	response, _, err := q.purchaseService.FindPurchases(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchases")
		return err
	}
	return writeExport(c, ctxt, "purchases", "Purchases", purchaseExportColumns, response)
}

// write validates a purchase and runs a create or an update in a transaction.
func (q *purchaseController) write(c fiber.Ctx, ctxt string, create bool) (response *models.Purchase, err error) {
	ctx := c.Context()
	request, err := sanitizers.ValidatePurchase(c, create)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidatePurchase")
		return nil, err
	}
	request.BranchID, request.Actor, request.At = currentBranchID(c), currentActor(c), time.Now()
	if !create {
		if request.ID, err = sanitizers.ParamID(c, "id", "Purchase"); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
			return nil, err
		}
	}
	err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if create {
			response, err = q.purchaseService.CreatePurchase(ctx, tx, request)
		} else {
			response, err = q.purchaseService.UpdatePurchase(ctx, tx, request)
		}
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrWritePurchase")
		}
		return purchaseError(err)
	})
	return
}

// V1Create godoc
//
//	@Summary		Record a purchase
//	@Description	The number (PO/YYYYMMDD/NNNN) is generated. The stock of every item comes in at its cost after discount and the purchase price of the product becomes the weighted average. A credit purchase needs a supplier and, unless paid in full, becomes a payable.
//	@Tags			Purchases
//	@Param			request	body		models.PurchaseRequest	true	"purchase"
//	@Success		201		{object}	helper.Response{data=models.Purchase}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/purchases [post]
func (q *purchaseController) V1Create(c fiber.Ctx) error {
	response, err := q.write(c, "PurchaseController-V1Create", true)
	if err != nil {
		return err
	}
	return helper.Created(c, response)
}

// V1Show godoc
//
//	@Summary	Show a purchase
//	@Tags		Purchases
//	@Param		id	path		string	true	"purchase id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Purchase}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/purchases/{id} [get]
func (q *purchaseController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PurchaseController-V1Show"
	purchaseID, err := sanitizers.ParamID(c, "id", "Purchase")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.purchaseService.FindPurchase(ctx, nil, currentBranchID(c), purchaseID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchase")
		return purchaseError(err)
	}
	return helper.OK(c, response)
}

// V1Update godoc
//
//	@Summary		Update a purchase
//	@Description	Only the attributes sent are changed, and only those that differ are written. Sending items replaces every line: the stock of the old lines is taken back and the new lines are received. The totals and the payable follow.
//	@Tags			Purchases
//	@Param			id		path		string					true	"purchase id (UUID)"
//	@Param			request	body		models.PurchaseRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Purchase}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/purchases/{id} [put]
func (q *purchaseController) V1Update(c fiber.Ctx) error {
	response, err := q.write(c, "PurchaseController-V1Update", false)
	if err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a purchase
//	@Description	The stock of its items is taken back.
//	@Tags			Purchases
//	@Param			id	path	string	true	"purchase id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/purchases/{id} [delete]
func (q *purchaseController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PurchaseController-V1Delete"
	purchaseID, err := sanitizers.ParamID(c, "id", "Purchase")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	if err = inTx(c, ctxt, func(tx pgx.Tx) error {
		err := q.purchaseService.DeletePurchase(ctx, tx, &models.PurchaseWrite{
			ID:       purchaseID,
			BranchID: currentBranchID(c),
			Actor:    currentActor(c),
			At:       time.Now(),
		})
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeletePurchase")
		}
		return purchaseError(err)
	}); err != nil {
		return err
	}
	return helper.NoContent(c)
}
