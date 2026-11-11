package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type stockOpnameController struct {
	authService        *middlewares.Service
	stockOpnameService services.StockOpnameService
	productService     services.ProductService
}

func NewStockOpnameController(
	authService *middlewares.Service,
	stockOpnameService services.StockOpnameService,
	productService services.ProductService,
) *stockOpnameController {
	return &stockOpnameController{
		authService:        authService,
		stockOpnameService: stockOpnameService,
		productService:     productService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /stock-opnames fall through.
func (q *stockOpnameController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Post("/bulk-delete", q.V1BulkDelete)
	private.Get("/:id<guid>", q.V1Show)
	private.Delete("/:id<guid>", q.V1Delete)
	q.mountExtras(private)
}

// stockOpnameError turns the stock opname errors into responses; anything else goes to the error handler as it is.
func stockOpnameError(err error) error {
	switch {
	case errors.Is(err, models.ErrStockOpnameNotFound):
		return helper.NotFound("Stock opname")
	case errors.Is(err, models.ErrStockOpnameConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Stock opname"))
	case errors.Is(err, models.ErrStockOpnameApproved):
		return helper.Conflict("An approved stock opname has already changed stock and cannot be deleted.")
	case errors.Is(err, models.ErrProductNotFound):
		return helper.NotFound("Product")
	case errors.Is(err, models.ErrStockOpnameInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Stock opname"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List stock opnames
//	@Description	Stock opnames of the active branch.
//	@Tags			Stock opname
//	@Param			q			query		string	false	"search in code, performed_by_name"
//	@Param			status		query		string	false	"only rows with this status"
//	@Param			sort		query		string	false	"sort field"		Enums(code, opname_date, performed_by_name, performed_by_role, status, note, reject_reason, item_count, difference_value, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.StockOpname}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-opnames [get]
func (q *stockOpnameController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockOpnameController-V1Index"
	request := sanitizers.FindStockOpnames(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.stockOpnameService.FindStockOpnames(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockOpnames")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one stock opname
//	@Tags		Stock opname
//	@Param		id	path		string	true	"stock opname id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.StockOpname}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/stock-opnames/{id} [get]
func (q *stockOpnameController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockOpnameController-V1Show"
	stockOpnameID, err := sanitizers.ParamID(c, "id", "Stock opname")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.stockOpnameService.FindStockOpname(
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
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary		Create a stock opname
//	@Description	Records the counted stock of the listed products next to the stock on record. Stock only changes when the opname is approved.
//	@Tags			Stock opname
//	@Param			request	body		models.StockOpnameRequest	true	"stock opname"
//	@Success		201		{object}	helper.Response{data=models.StockOpname}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-opnames [post]
func (q *stockOpnameController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockOpnameController-V1Create"
	request, err := sanitizers.ValidateStockOpname(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateStockOpname")
		return err
	}
	now := time.Now()
	stockOpname := request.Apply(&models.StockOpname{
		BranchID:  currentBranchID(c),
		Status:    models.StockOpnameStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	})
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	response, err := q.stockOpnameService.CreateStockOpname(ctx, tx, stockOpname)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStockOpname")
		return stockOpnameError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a stock opname
//	@Description	An approved stock opname has already changed stock and cannot be deleted (409).
//	@Tags			Stock opname
//	@Param			id	path	string	true	"stock opname id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-opnames/{id} [delete]
func (q *stockOpnameController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockOpnameController-V1Delete"
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
	if err = q.stockOpnameService.DeleteStockOpnames(
		ctx,
		tx,
		models.NewStockOpnameFilter().
			WithBranchIDs(currentBranchID(c)).
			WithStockOpnameIDs(stockOpnameID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteStockOpnames")
		return stockOpnameError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}

// V1BulkDelete godoc
//
//	@Summary		Delete several stock opnames
//	@Description	All or nothing: an unknown id (404) or an approved stock opname (409) deletes none.
//	@Tags			Stock opname
//	@Param			request	body		models.BulkDeleteRequest	true	"ids to delete"
//	@Success		200		{object}	helper.Response{data=models.DeletedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-opnames/bulk-delete [post]
func (q *stockOpnameController) V1BulkDelete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockOpnameController-V1BulkDelete"
	stockOpnameIDs, err := sanitizers.BulkDeleteIDs(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBulkDeleteIDs")
		return err
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = q.stockOpnameService.DeleteStockOpnames(
		ctx,
		tx,
		models.NewStockOpnameFilter().
			WithBranchIDs(currentBranchID(c)).
			WithStockOpnameIDs(stockOpnameIDs...),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteStockOpnames")
		return stockOpnameError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, models.DeletedResponse{Deleted: len(stockOpnameIDs)})
}
