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

type promotionController struct {
	authService      *middlewares.Service
	promotionService services.PromotionService
}

func NewPromotionController(
	authService *middlewares.Service,
	promotionService services.PromotionService,
) *promotionController {
	return &promotionController{
		authService:      authService,
		promotionService: promotionService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /promotions fall through.
func (q *promotionController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", middlewares.RequirePlugin("business"), q.V1Create)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", middlewares.RequirePlugin("business"), q.V1Update)
	private.Delete("/:id<guid>", middlewares.RequirePlugin("business"), q.V1Delete)
}

// promotionError turns the promotion errors into responses; anything else goes to the error handler as it is.
func promotionError(err error) error {
	switch {
	case errors.Is(err, models.ErrPromotionNotFound):
		return helper.NotFound("Promotion")
	case errors.Is(err, models.ErrPromotionConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Promotion"))
	case errors.Is(err, models.ErrPromotionInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Promotion"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List promotions
//	@Description	Promotions of the active branch.
//	@Tags			Promotions
//	@Param			q			query		string	false	"search in name"
//	@Param			status		query		string	false	"active, scheduled or ended"	Enums(active, scheduled, ended)
//	@Param			sort		query		string	false	"sort field"					Enums(name, type, start_date, end_date, start_time, end_time, days, customer_type_ids, discount_type, discount_value, minimum_purchase, buy_quantity, get_quantity, products, is_active, description, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"				Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"					default(1)
//	@Param			per_page	query		int		false	"rows per page"					default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Promotion}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/promotions [get]
func (q *promotionController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PromotionController-V1Index"
	request := sanitizers.FindPromotions(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.promotionService.FindPromotions(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPromotions")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one promotion
//	@Tags		Promotions
//	@Param		id	path		string	true	"promotion id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Promotion}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/promotions/{id} [get]
func (q *promotionController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PromotionController-V1Show"
	promotionID, err := sanitizers.ParamID(c, "id", "Promotion")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.promotionService.FindPromotion(
		ctx,
		nil,
		models.NewPromotionFilter().
			WithBranchIDs(currentBranchID(c)).
			WithPromotionIDs(promotionID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPromotion")
		return promotionError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a promotion
//	@Tags		Promotions
//	@Param		request	body		models.PromotionRequest	true	"promotion"
//	@Success	201		{object}	helper.Response{data=models.Promotion}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/promotions [post]
func (q *promotionController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PromotionController-V1Create"
	request, err := sanitizers.ValidatePromotion(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidatePromotion")
		return err
	}
	now := time.Now()
	promotion := request.Apply(&models.Promotion{
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
	response, err := q.promotionService.CreatePromotion(ctx, tx, promotion)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePromotion")
		return promotionError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a promotion
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Promotions
//	@Param			id		path		string					true	"promotion id (UUID)"
//	@Param			request	body		models.PromotionRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Promotion}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/promotions/{id} [put]
func (q *promotionController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PromotionController-V1Update"
	promotionID, err := sanitizers.ParamID(c, "id", "Promotion")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidatePromotion(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidatePromotion")
		return err
	}
	promotion, err := q.promotionService.FindPromotion(
		ctx,
		nil,
		models.NewPromotionFilter().
			WithBranchIDs(currentBranchID(c)).
			WithPromotionIDs(promotionID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPromotion")
		return promotionError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(promotion).UpdatedAt = time.Now()
	response, err := q.promotionService.UpdatePromotion(ctx, tx, promotion)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePromotion")
		return promotionError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a promotion
//	@Description	A promotion that other records still use cannot be deleted (409).
//	@Tags			Promotions
//	@Param			id	path	string	true	"promotion id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/promotions/{id} [delete]
func (q *promotionController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PromotionController-V1Delete"
	promotionID, err := sanitizers.ParamID(c, "id", "Promotion")
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
	if err = q.promotionService.DeletePromotions(
		ctx,
		tx,
		models.NewPromotionFilter().
			WithBranchIDs(currentBranchID(c)).
			WithPromotionIDs(promotionID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeletePromotions")
		return promotionError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}
