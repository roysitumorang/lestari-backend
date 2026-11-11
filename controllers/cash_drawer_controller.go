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

type cashDrawerController struct {
	authService       *middlewares.Service
	cashDrawerService services.CashDrawerService
}

func NewCashDrawerController(
	authService *middlewares.Service,
	cashDrawerService services.CashDrawerService,
) *cashDrawerController {
	return &cashDrawerController{
		authService:       authService,
		cashDrawerService: cashDrawerService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /cash-drawers fall through.
func (q *cashDrawerController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", q.V1Update)
	private.Delete("/:id<guid>", q.V1Delete)
}

// cashDrawerError turns the cash drawer errors into responses; anything else goes to the error handler as it is.
func cashDrawerError(err error) error {
	switch {
	case errors.Is(err, models.ErrCashDrawerNotFound):
		return helper.NotFound("Cash drawer")
	case errors.Is(err, models.ErrCashDrawerConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Cash drawer"))
	case errors.Is(err, models.ErrCashDrawerInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Cash drawer"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List cash drawers
//	@Description	Cash drawers of the active branch.
//	@Tags			Cash drawers
//	@Param			q			query		string	false	"search in name"
//	@Param			sort		query		string	false	"sort field"		Enums(name, is_active, current_shift_status, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.CashDrawer}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/cash-drawers [get]
func (q *cashDrawerController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CashDrawerController-V1Index"
	request := sanitizers.FindCashDrawers(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.cashDrawerService.FindCashDrawers(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCashDrawers")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one cash drawer
//	@Tags		Cash drawers
//	@Param		id	path		string	true	"cash drawer id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.CashDrawer}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/cash-drawers/{id} [get]
func (q *cashDrawerController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CashDrawerController-V1Show"
	cashDrawerID, err := sanitizers.ParamID(c, "id", "Cash drawer")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.cashDrawerService.FindCashDrawer(
		ctx,
		nil,
		models.NewCashDrawerFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCashDrawerIDs(cashDrawerID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCashDrawer")
		return cashDrawerError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a cash drawer
//	@Tags		Cash drawers
//	@Param		request	body		models.CashDrawerRequest	true	"cash drawer"
//	@Success	201		{object}	helper.Response{data=models.CashDrawer}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/cash-drawers [post]
func (q *cashDrawerController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CashDrawerController-V1Create"
	request, err := sanitizers.ValidateCashDrawer(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCashDrawer")
		return err
	}
	now := time.Now()
	cashDrawer := request.Apply(&models.CashDrawer{
		BranchID:  currentBranchID(c),
		CreatedAt: now,
		UpdatedAt: now,
	})
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	response, err := q.cashDrawerService.CreateCashDrawer(ctx, tx, cashDrawer)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateCashDrawer")
		return cashDrawerError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a cash drawer
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Cash drawers
//	@Param			id		path		string						true	"cash drawer id (UUID)"
//	@Param			request	body		models.CashDrawerRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.CashDrawer}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/cash-drawers/{id} [put]
func (q *cashDrawerController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CashDrawerController-V1Update"
	cashDrawerID, err := sanitizers.ParamID(c, "id", "Cash drawer")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateCashDrawer(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCashDrawer")
		return err
	}
	cashDrawer, err := q.cashDrawerService.FindCashDrawer(
		ctx,
		nil,
		models.NewCashDrawerFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCashDrawerIDs(cashDrawerID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCashDrawer")
		return cashDrawerError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(cashDrawer).UpdatedAt = time.Now()
	response, err := q.cashDrawerService.UpdateCashDrawer(ctx, tx, cashDrawer)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateCashDrawer")
		return cashDrawerError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a cash drawer
//	@Description	A cash drawer that other records still use cannot be deleted (409).
//	@Tags			Cash drawers
//	@Param			id	path	string	true	"cash drawer id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/cash-drawers/{id} [delete]
func (q *cashDrawerController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CashDrawerController-V1Delete"
	cashDrawerID, err := sanitizers.ParamID(c, "id", "Cash drawer")
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
	if err = q.cashDrawerService.DeleteCashDrawers(
		ctx,
		tx,
		models.NewCashDrawerFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCashDrawerIDs(cashDrawerID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCashDrawers")
		return cashDrawerError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}
