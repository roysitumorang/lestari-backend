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

type feeController struct {
	authService *middlewares.Service
	feeService  services.FeeService
}

func NewFeeController(
	authService *middlewares.Service,
	feeService services.FeeService,
) *feeController {
	return &feeController{
		authService: authService,
		feeService:  feeService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /fees fall through.
func (q *feeController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", q.V1Update)
	private.Delete("/:id<guid>", q.V1Delete)
}

// feeError turns the fee errors into responses; anything else goes to the error handler as it is.
func feeError(err error) error {
	switch {
	case errors.Is(err, models.ErrFeeNotFound):
		return helper.NotFound("Fee")
	case errors.Is(err, models.ErrFeeConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Fee"))
	case errors.Is(err, models.ErrFeeInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Fee"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List fees
//	@Description	Fees of the active branch.
//	@Tags			Fees
//	@Param			q			query		string	false	"search in name"
//	@Param			applies_to	query		string	false	"only rows with this applies to"	Enums(sale, purchase, both)
//	@Param			sort		query		string	false	"sort field"						Enums(name, amount_type, amount, applies_to, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"					Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"						default(1)
//	@Param			per_page	query		int		false	"rows per page"						default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Fee}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/fees [get]
func (q *feeController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FeeController-V1Index"
	request := sanitizers.FindFees(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.feeService.FindFees(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFees")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one fee
//	@Tags		Fees
//	@Param		id	path		string	true	"fee id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Fee}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/fees/{id} [get]
func (q *feeController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FeeController-V1Show"
	feeID, err := sanitizers.ParamID(c, "id", "Fee")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.feeService.FindFee(
		ctx,
		nil,
		models.NewFeeFilter().
			WithBranchIDs(currentBranchID(c)).
			WithFeeIDs(feeID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFee")
		return feeError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a fee
//	@Tags		Fees
//	@Param		request	body		models.FeeRequest	true	"fee"
//	@Success	201		{object}	helper.Response{data=models.Fee}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/fees [post]
func (q *feeController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FeeController-V1Create"
	request, err := sanitizers.ValidateFee(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateFee")
		return err
	}
	now := time.Now()
	fee := request.Apply(&models.Fee{
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
	response, err := q.feeService.CreateFee(ctx, tx, fee)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateFee")
		return feeError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a fee
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Fees
//	@Param			id		path		string				true	"fee id (UUID)"
//	@Param			request	body		models.FeeRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Fee}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/fees/{id} [put]
func (q *feeController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FeeController-V1Update"
	feeID, err := sanitizers.ParamID(c, "id", "Fee")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateFee(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateFee")
		return err
	}
	fee, err := q.feeService.FindFee(
		ctx,
		nil,
		models.NewFeeFilter().
			WithBranchIDs(currentBranchID(c)).
			WithFeeIDs(feeID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFee")
		return feeError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(fee).UpdatedAt = time.Now()
	response, err := q.feeService.UpdateFee(ctx, tx, fee)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateFee")
		return feeError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a fee
//	@Description	A fee that other records still use cannot be deleted (409).
//	@Tags			Fees
//	@Param			id	path	string	true	"fee id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/fees/{id} [delete]
func (q *feeController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FeeController-V1Delete"
	feeID, err := sanitizers.ParamID(c, "id", "Fee")
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
	if err = q.feeService.DeleteFees(
		ctx,
		tx,
		models.NewFeeFilter().
			WithBranchIDs(currentBranchID(c)).
			WithFeeIDs(feeID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteFees")
		return feeError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}
