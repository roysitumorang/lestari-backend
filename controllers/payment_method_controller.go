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

type paymentMethodController struct {
	authService          *middlewares.Service
	paymentMethodService services.PaymentMethodService
}

func NewPaymentMethodController(
	authService *middlewares.Service,
	paymentMethodService services.PaymentMethodService,
) *paymentMethodController {
	return &paymentMethodController{
		authService:          authService,
		paymentMethodService: paymentMethodService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /payment-methods fall through.
func (q *paymentMethodController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", q.V1Update)
	private.Delete("/:id<guid>", q.V1Delete)
}

// paymentMethodError turns the payment method errors into responses; anything else goes to the error handler as it is.
func paymentMethodError(err error) error {
	switch {
	case errors.Is(err, models.ErrPaymentMethodNotFound):
		return helper.NotFound("Payment method")
	case errors.Is(err, models.ErrPaymentMethodConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Payment method"))
	case errors.Is(err, models.ErrPaymentMethodInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Payment method"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List payment methods
//	@Description	Payment methods of the active branch.
//	@Tags			Payment methods
//	@Param			q			query		string	false	"search in name"
//	@Param			type		query		string	false	"only rows with this type"	Enums(cash, debit, credit_card, qris, e_wallet, bank_transfer, other)
//	@Param			is_active	query		bool	false	"only rows with this is active"
//	@Param			sort		query		string	false	"sort field"		Enums(name, type, code, mdr_type, mdr_value, charge_fee_to_customer, edc_card_rates, is_active, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.PaymentMethod}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/payment-methods [get]
func (q *paymentMethodController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PaymentMethodController-V1Index"
	request := sanitizers.FindPaymentMethods(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.paymentMethodService.FindPaymentMethods(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPaymentMethods")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one payment method
//	@Tags		Payment methods
//	@Param		id	path		string	true	"payment method id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.PaymentMethod}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/payment-methods/{id} [get]
func (q *paymentMethodController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PaymentMethodController-V1Show"
	paymentMethodID, err := sanitizers.ParamID(c, "id", "Payment method")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.paymentMethodService.FindPaymentMethod(
		ctx,
		nil,
		models.NewPaymentMethodFilter().
			WithBranchIDs(currentBranchID(c)).
			WithPaymentMethodIDs(paymentMethodID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPaymentMethod")
		return paymentMethodError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a payment method
//	@Tags		Payment methods
//	@Param		request	body		models.PaymentMethodRequest	true	"payment method"
//	@Success	201		{object}	helper.Response{data=models.PaymentMethod}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/payment-methods [post]
func (q *paymentMethodController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PaymentMethodController-V1Create"
	request, err := sanitizers.ValidatePaymentMethod(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidatePaymentMethod")
		return err
	}
	now := time.Now()
	paymentMethod := request.Apply(&models.PaymentMethod{
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
	response, err := q.paymentMethodService.CreatePaymentMethod(ctx, tx, paymentMethod)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePaymentMethod")
		return paymentMethodError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a payment method
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Payment methods
//	@Param			id		path		string						true	"payment method id (UUID)"
//	@Param			request	body		models.PaymentMethodRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.PaymentMethod}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/payment-methods/{id} [put]
func (q *paymentMethodController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PaymentMethodController-V1Update"
	paymentMethodID, err := sanitizers.ParamID(c, "id", "Payment method")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidatePaymentMethod(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidatePaymentMethod")
		return err
	}
	paymentMethod, err := q.paymentMethodService.FindPaymentMethod(
		ctx,
		nil,
		models.NewPaymentMethodFilter().
			WithBranchIDs(currentBranchID(c)).
			WithPaymentMethodIDs(paymentMethodID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPaymentMethod")
		return paymentMethodError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(paymentMethod).UpdatedAt = time.Now()
	response, err := q.paymentMethodService.UpdatePaymentMethod(ctx, tx, paymentMethod)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePaymentMethod")
		return paymentMethodError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a payment method
//	@Description	A payment method that other records still use cannot be deleted (409).
//	@Tags			Payment methods
//	@Param			id	path	string	true	"payment method id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/payment-methods/{id} [delete]
func (q *paymentMethodController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PaymentMethodController-V1Delete"
	paymentMethodID, err := sanitizers.ParamID(c, "id", "Payment method")
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
	if err = q.paymentMethodService.DeletePaymentMethods(
		ctx,
		tx,
		models.NewPaymentMethodFilter().
			WithBranchIDs(currentBranchID(c)).
			WithPaymentMethodIDs(paymentMethodID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeletePaymentMethods")
		return paymentMethodError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}
