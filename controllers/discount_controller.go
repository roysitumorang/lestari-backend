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

type discountController struct {
	authService     *middlewares.Service
	discountService services.DiscountService
}

func NewDiscountController(
	authService *middlewares.Service,
	discountService services.DiscountService,
) *discountController {
	return &discountController{
		authService:     authService,
		discountService: discountService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /discounts fall through.
func (q *discountController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Post("/bulk-delete", q.V1BulkDelete)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", q.V1Update)
	private.Delete("/:id<guid>", q.V1Delete)
}

// discountError turns the discount errors into responses; anything else goes to the error handler as it is.
func discountError(err error) error {
	switch {
	case errors.Is(err, models.ErrDiscountNotFound):
		return helper.NotFound("Discount")
	case errors.Is(err, models.ErrDiscountConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Discount"))
	case errors.Is(err, models.ErrDiscountInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Discount"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List discounts
//	@Description	Discounts of the active branch.
//	@Tags			Discounts
//	@Param			q			query		string	false	"search in name"
//	@Param			is_active	query		bool	false	"only rows with this is active"
//	@Param			sort		query		string	false	"sort field"		Enums(name, type, value, is_active, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Discount}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/discounts [get]
func (q *discountController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "DiscountController-V1Index"
	request := sanitizers.FindDiscounts(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.discountService.FindDiscounts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDiscounts")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one discount
//	@Tags		Discounts
//	@Param		id	path		string	true	"discount id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Discount}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/discounts/{id} [get]
func (q *discountController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "DiscountController-V1Show"
	discountID, err := sanitizers.ParamID(c, "id", "Discount")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.discountService.FindDiscount(
		ctx,
		nil,
		models.NewDiscountFilter().
			WithBranchIDs(currentBranchID(c)).
			WithDiscountIDs(discountID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDiscount")
		return discountError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a discount
//	@Tags		Discounts
//	@Param		request	body		models.DiscountRequest	true	"discount"
//	@Success	201		{object}	helper.Response{data=models.Discount}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/discounts [post]
func (q *discountController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "DiscountController-V1Create"
	request, err := sanitizers.ValidateDiscount(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateDiscount")
		return err
	}
	now := time.Now()
	discount := request.Apply(&models.Discount{
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
	response, err := q.discountService.CreateDiscount(ctx, tx, discount)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateDiscount")
		return discountError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a discount
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Discounts
//	@Param			id		path		string					true	"discount id (UUID)"
//	@Param			request	body		models.DiscountRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Discount}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/discounts/{id} [put]
func (q *discountController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "DiscountController-V1Update"
	discountID, err := sanitizers.ParamID(c, "id", "Discount")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateDiscount(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateDiscount")
		return err
	}
	discount, err := q.discountService.FindDiscount(
		ctx,
		nil,
		models.NewDiscountFilter().
			WithBranchIDs(currentBranchID(c)).
			WithDiscountIDs(discountID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDiscount")
		return discountError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(discount).UpdatedAt = time.Now()
	response, err := q.discountService.UpdateDiscount(ctx, tx, discount)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateDiscount")
		return discountError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a discount
//	@Description	A discount that other records still use cannot be deleted (409).
//	@Tags			Discounts
//	@Param			id	path	string	true	"discount id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/discounts/{id} [delete]
func (q *discountController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "DiscountController-V1Delete"
	discountID, err := sanitizers.ParamID(c, "id", "Discount")
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
	if err = q.discountService.DeleteDiscounts(
		ctx,
		tx,
		models.NewDiscountFilter().
			WithBranchIDs(currentBranchID(c)).
			WithDiscountIDs(discountID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteDiscounts")
		return discountError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}

// V1BulkDelete godoc
//
//	@Summary		Delete several discounts
//	@Description	All or nothing: an unknown id (404) or a discount still in use (409) deletes none.
//	@Tags			Discounts
//	@Param			request	body		models.BulkDeleteRequest	true	"ids to delete"
//	@Success		200		{object}	helper.Response{data=models.DeletedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/discounts/bulk-delete [post]
func (q *discountController) V1BulkDelete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "DiscountController-V1BulkDelete"
	discountIDs, err := sanitizers.BulkDeleteIDs(c)
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
	if err = q.discountService.DeleteDiscounts(
		ctx,
		tx,
		models.NewDiscountFilter().
			WithBranchIDs(currentBranchID(c)).
			WithDiscountIDs(discountIDs...),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteDiscounts")
		return discountError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, models.DeletedResponse{Deleted: len(discountIDs)})
}
