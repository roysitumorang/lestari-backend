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

type paymentAccountController struct {
	authService           *middlewares.Service
	paymentAccountService services.PaymentAccountService
}

func NewPaymentAccountController(
	authService *middlewares.Service,
	paymentAccountService services.PaymentAccountService,
) *paymentAccountController {
	return &paymentAccountController{
		authService:           authService,
		paymentAccountService: paymentAccountService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /catalog/payment-accounts fall through.
func (q *paymentAccountController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", q.V1Update)
	private.Delete("/:id<guid>", q.V1Delete)
}

// paymentAccountError turns the account errors into responses; anything else goes to the error handler as it is.
func paymentAccountError(err error) error {
	switch {
	case errors.Is(err, models.ErrPaymentAccountNotFound):
		return helper.NotFound("Account")
	case errors.Is(err, models.ErrPaymentAccountConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Account"))
	case errors.Is(err, models.ErrPaymentAccountInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Account"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List payout accounts
//	@Description	Payout accounts of the active branch.
//	@Tags			Online catalog
//	@Param			q			query		string	false	"search in provider, account_name"
//	@Param			type		query		string	false	"only rows with this type"	Enums(bank, e_wallet, qris)
//	@Param			sort		query		string	false	"sort field"				Enums(type, provider, account_name, account_number, qris_image_url, is_active, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"			Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"				default(1)
//	@Param			per_page	query		int		false	"rows per page"				default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.PaymentAccount}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/catalog/payment-accounts [get]
func (q *paymentAccountController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PaymentAccountController-V1Index"
	request := sanitizers.FindPaymentAccounts(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.paymentAccountService.FindPaymentAccounts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPaymentAccounts")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one account
//	@Tags		Online catalog
//	@Param		id	path		string	true	"account id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.PaymentAccount}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/catalog/payment-accounts/{id} [get]
func (q *paymentAccountController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PaymentAccountController-V1Show"
	paymentAccountID, err := sanitizers.ParamID(c, "id", "Account")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.paymentAccountService.FindPaymentAccount(
		ctx,
		nil,
		models.NewPaymentAccountFilter().
			WithBranchIDs(currentBranchID(c)).
			WithPaymentAccountIDs(paymentAccountID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPaymentAccount")
		return paymentAccountError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a account
//	@Tags		Online catalog
//	@Param		request	body		models.PaymentAccountRequest	true	"account"
//	@Success	201		{object}	helper.Response{data=models.PaymentAccount}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/catalog/payment-accounts [post]
func (q *paymentAccountController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PaymentAccountController-V1Create"
	request, err := sanitizers.ValidatePaymentAccount(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidatePaymentAccount")
		return err
	}
	now := time.Now()
	paymentAccount := request.Apply(&models.PaymentAccount{
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
	response, err := q.paymentAccountService.CreatePaymentAccount(ctx, tx, paymentAccount)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePaymentAccount")
		return paymentAccountError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a account
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Online catalog
//	@Param			id		path		string							true	"account id (UUID)"
//	@Param			request	body		models.PaymentAccountRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.PaymentAccount}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/catalog/payment-accounts/{id} [put]
func (q *paymentAccountController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PaymentAccountController-V1Update"
	paymentAccountID, err := sanitizers.ParamID(c, "id", "Account")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidatePaymentAccount(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidatePaymentAccount")
		return err
	}
	paymentAccount, err := q.paymentAccountService.FindPaymentAccount(
		ctx,
		nil,
		models.NewPaymentAccountFilter().
			WithBranchIDs(currentBranchID(c)).
			WithPaymentAccountIDs(paymentAccountID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPaymentAccount")
		return paymentAccountError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(paymentAccount).UpdatedAt = time.Now()
	response, err := q.paymentAccountService.UpdatePaymentAccount(ctx, tx, paymentAccount)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePaymentAccount")
		return paymentAccountError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a account
//	@Description	A account that other records still use cannot be deleted (409).
//	@Tags			Online catalog
//	@Param			id	path	string	true	"account id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/catalog/payment-accounts/{id} [delete]
func (q *paymentAccountController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PaymentAccountController-V1Delete"
	paymentAccountID, err := sanitizers.ParamID(c, "id", "Account")
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
	if err = q.paymentAccountService.DeletePaymentAccounts(
		ctx,
		tx,
		models.NewPaymentAccountFilter().
			WithBranchIDs(currentBranchID(c)).
			WithPaymentAccountIDs(paymentAccountID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeletePaymentAccounts")
		return paymentAccountError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}
