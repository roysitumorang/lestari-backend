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

type customerTypeController struct {
	authService         *middlewares.Service
	customerTypeService services.CustomerTypeService
}

func NewCustomerTypeController(
	authService *middlewares.Service,
	customerTypeService services.CustomerTypeService,
) *customerTypeController {
	return &customerTypeController{
		authService:         authService,
		customerTypeService: customerTypeService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /customer-types fall through.
func (q *customerTypeController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", middlewares.RequirePlugin("business"), q.V1Create)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", middlewares.RequirePlugin("business"), q.V1Update)
	private.Delete("/:id<guid>", middlewares.RequirePlugin("business"), q.V1Delete)
}

// customerTypeError turns the customer type errors into responses; anything else goes to the error handler as it is.
func customerTypeError(err error) error {
	switch {
	case errors.Is(err, models.ErrCustomerTypeNotFound):
		return helper.NotFound("Customer type")
	case errors.Is(err, models.ErrCustomerTypeConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Customer type"))
	case errors.Is(err, models.ErrCustomerTypeInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Customer type"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List customer types
//	@Description	Customer types of the active branch.
//	@Tags			Customer types
//	@Param			q			query		string	false	"search in name"
//	@Param			sort		query		string	false	"sort field"		Enums(name, discount_type, discount_value, point_multiplier, customer_count, description, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.CustomerType}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customer-types [get]
func (q *customerTypeController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerTypeController-V1Index"
	request := sanitizers.FindCustomerTypes(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.customerTypeService.FindCustomerTypes(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomerTypes")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one customer type
//	@Tags		Customer types
//	@Param		id	path		string	true	"customer type id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.CustomerType}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/customer-types/{id} [get]
func (q *customerTypeController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerTypeController-V1Show"
	customerTypeID, err := sanitizers.ParamID(c, "id", "Customer type")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.customerTypeService.FindCustomerType(
		ctx,
		nil,
		models.NewCustomerTypeFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCustomerTypeIDs(customerTypeID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomerType")
		return customerTypeError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a customer type
//	@Tags		Customer types
//	@Param		request	body		models.CustomerTypeRequest	true	"customer type"
//	@Success	201		{object}	helper.Response{data=models.CustomerType}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/customer-types [post]
func (q *customerTypeController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerTypeController-V1Create"
	request, err := sanitizers.ValidateCustomerType(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCustomerType")
		return err
	}
	now := time.Now()
	customerType := request.Apply(&models.CustomerType{
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
	response, err := q.customerTypeService.CreateCustomerType(ctx, tx, customerType)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateCustomerType")
		return customerTypeError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a customer type
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Customer types
//	@Param			id		path		string						true	"customer type id (UUID)"
//	@Param			request	body		models.CustomerTypeRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.CustomerType}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customer-types/{id} [put]
func (q *customerTypeController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerTypeController-V1Update"
	customerTypeID, err := sanitizers.ParamID(c, "id", "Customer type")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateCustomerType(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCustomerType")
		return err
	}
	customerType, err := q.customerTypeService.FindCustomerType(
		ctx,
		nil,
		models.NewCustomerTypeFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCustomerTypeIDs(customerTypeID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomerType")
		return customerTypeError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(customerType).UpdatedAt = time.Now()
	response, err := q.customerTypeService.UpdateCustomerType(ctx, tx, customerType)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateCustomerType")
		return customerTypeError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a customer type
//	@Description	A customer type that other records still use cannot be deleted (409).
//	@Tags			Customer types
//	@Param			id	path	string	true	"customer type id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customer-types/{id} [delete]
func (q *customerTypeController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerTypeController-V1Delete"
	customerTypeID, err := sanitizers.ParamID(c, "id", "Customer type")
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
	if err = q.customerTypeService.DeleteCustomerTypes(
		ctx,
		tx,
		models.NewCustomerTypeFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCustomerTypeIDs(customerTypeID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCustomerTypes")
		return customerTypeError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}
