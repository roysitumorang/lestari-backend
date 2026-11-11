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

type commissionController struct {
	authService       *middlewares.Service
	commissionService services.CommissionService
}

func NewCommissionController(
	authService *middlewares.Service,
	commissionService services.CommissionService,
) *commissionController {
	return &commissionController{
		authService:       authService,
		commissionService: commissionService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /commissions fall through.
func (q *commissionController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", middlewares.RequirePlugin("business"), q.V1Index)
	private.Post("", middlewares.RequirePlugin("business"), q.V1Create)
	private.Get("/:id<guid>", middlewares.RequirePlugin("business"), q.V1Show)
	private.Put("/:id<guid>", middlewares.RequirePlugin("business"), q.V1Update)
	private.Delete("/:id<guid>", middlewares.RequirePlugin("business"), q.V1Delete)
}

// commissionError turns the commission rule errors into responses; anything else goes to the error handler as it is.
func commissionError(err error) error {
	switch {
	case errors.Is(err, models.ErrCommissionNotFound):
		return helper.NotFound("Commission rule")
	case errors.Is(err, models.ErrCommissionConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Commission rule"))
	case errors.Is(err, models.ErrCommissionInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Commission rule"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List commissions
//	@Description	Commissions of the active branch.
//	@Tags			Commissions
//	@Param			q			query		string	false	"search in name"
//	@Param			sort		query		string	false	"sort field"		Enums(name, basis, type, value, staff_ids, product_ids, is_active, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Commission}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/commissions [get]
func (q *commissionController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CommissionController-V1Index"
	request := sanitizers.FindCommissions(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.commissionService.FindCommissions(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCommissions")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one commission rule
//	@Tags		Commissions
//	@Param		id	path		string	true	"commission rule id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Commission}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/commissions/{id} [get]
func (q *commissionController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CommissionController-V1Show"
	commissionID, err := sanitizers.ParamID(c, "id", "Commission rule")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.commissionService.FindCommission(
		ctx,
		nil,
		models.NewCommissionFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCommissionIDs(commissionID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCommission")
		return commissionError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a commission rule
//	@Tags		Commissions
//	@Param		request	body		models.CommissionRequest	true	"commission rule"
//	@Success	201		{object}	helper.Response{data=models.Commission}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/commissions [post]
func (q *commissionController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CommissionController-V1Create"
	request, err := sanitizers.ValidateCommission(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCommission")
		return err
	}
	now := time.Now()
	commission := request.Apply(&models.Commission{
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
	response, err := q.commissionService.CreateCommission(ctx, tx, commission)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateCommission")
		return commissionError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a commission rule
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Commissions
//	@Param			id		path		string						true	"commission rule id (UUID)"
//	@Param			request	body		models.CommissionRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Commission}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/commissions/{id} [put]
func (q *commissionController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CommissionController-V1Update"
	commissionID, err := sanitizers.ParamID(c, "id", "Commission rule")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateCommission(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCommission")
		return err
	}
	commission, err := q.commissionService.FindCommission(
		ctx,
		nil,
		models.NewCommissionFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCommissionIDs(commissionID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCommission")
		return commissionError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(commission).UpdatedAt = time.Now()
	response, err := q.commissionService.UpdateCommission(ctx, tx, commission)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateCommission")
		return commissionError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a commission rule
//	@Description	A commission rule that other records still use cannot be deleted (409).
//	@Tags			Commissions
//	@Param			id	path	string	true	"commission rule id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/commissions/{id} [delete]
func (q *commissionController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CommissionController-V1Delete"
	commissionID, err := sanitizers.ParamID(c, "id", "Commission rule")
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
	if err = q.commissionService.DeleteCommissions(
		ctx,
		tx,
		models.NewCommissionFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCommissionIDs(commissionID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCommissions")
		return commissionError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}
