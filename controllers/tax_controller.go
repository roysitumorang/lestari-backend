package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type taxController struct {
	authService *middlewares.Service
	taxService  services.TaxService
}

func NewTaxController(
	authService *middlewares.Service,
	taxService services.TaxService,
) *taxController {
	return &taxController{
		authService: authService,
		taxService:  taxService,
	}
}

func (q *taxController) Mount(r fiber.Router) {
	r.Group("", q.authService.Middleware()).
		Get("", q.V1Index).
		Post("", q.V1Create).
		Post("/bulk-delete", q.V1BulkDelete).
		Get("/:id", q.V1Show).
		Put("/:id", q.V1Update).
		Delete("/:id", q.V1Delete)
}

// taxError turns the tax errors into responses; anything else goes to the error handler as it is.
func taxError(err error) error {
	if errors.Is(err, models.ErrTaxNotFound) {
		return helper.NotFound("Tax")
	}
	return err
}

// V1Index godoc
//
//	@Summary		List taxes
//	@Description	Taxes of the active branch.
//	@Tags			Taxes
//	@Param			q			query		string	false	"search in name"
//	@Param			is_active	query		bool	false	"only active / inactive taxes"
//	@Param			sort		query		string	false	"sort field"		Enums(name, rate, is_inclusive, is_active, created_at, updated_at)	default(name)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)													default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Tax}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/taxes [get]
func (q *taxController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "TaxController-V1Index"
	request := sanitizers.FindTaxes(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.taxService.FindTaxes(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindTaxes")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one tax
//	@Tags		Taxes
//	@Param		id	path		string	true	"tax id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Tax}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/taxes/{id} [get]
func (q *taxController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	taxID, err := sanitizers.ParamID(c, "id", "Tax")
	if err != nil {
		return err
	}
	response, err := q.taxService.FindTax(
		ctx,
		models.NewTaxFilter().
			WithBranchIDs(currentBranchID(c)).
			WithTaxIDs(taxID),
	)
	if err != nil {
		return taxError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary		Create a tax
//	@Description	name and rate (0–100) are required; is_inclusive defaults to false and is_active to true.
//	@Tags			Taxes
//	@Param			request	body		models.TaxRequest	true	"tax"
//	@Success		201		{object}	helper.Response{data=models.Tax}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/taxes [post]
func (q *taxController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "TaxController-V1Create"
	request, err := sanitizers.ValidateTax(c, true)
	if err != nil {
		return err
	}
	now := time.Now()
	tax := request.Apply(&models.Tax{
		BranchID:  currentBranchID(c),
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	response, err := q.taxService.CreateTax(ctx, tx, tax)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateTax")
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a tax
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Taxes
//	@Param			id		path		string				true	"tax id (UUID)"
//	@Param			request	body		models.TaxRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Tax}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/taxes/{id} [put]
func (q *taxController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "TaxController-V1Update"
	taxID, err := sanitizers.ParamID(c, "id", "Tax")
	if err != nil {
		return err
	}
	request, err := sanitizers.ValidateTax(c, false)
	if err != nil {
		return err
	}
	tax, err := q.taxService.FindTax(
		ctx,
		models.NewTaxFilter().
			WithBranchIDs(currentBranchID(c)).
			WithTaxIDs(taxID),
	)
	if err != nil {
		return taxError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(tax).UpdatedAt = time.Now()
	response, err := q.taxService.UpdateTax(ctx, tx, tax)
	if err != nil {
		return taxError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a tax
//	@Description	Products that used the tax keep working without it.
//	@Tags			Taxes
//	@Param			id	path	string	true	"tax id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/taxes/{id} [delete]
func (q *taxController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "TaxController-V1Delete"
	taxID, err := sanitizers.ParamID(c, "id", "Tax")
	if err != nil {
		return err
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = q.taxService.DeleteTaxes(
		ctx,
		tx,
		models.NewTaxFilter().
			WithBranchIDs(currentBranchID(c)).
			WithTaxIDs(taxID),
	); err != nil {
		return taxError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}

// BulkDeleteRequest is the body of the bulk-delete endpoints.
type BulkDeleteRequest struct {
	IDs []string `json:"ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a,019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
}

// V1BulkDelete godoc
//
//	@Summary		Delete several taxes
//	@Description	All or nothing: an unknown id (404) deletes none.
//	@Tags			Taxes
//	@Param			request	body		BulkDeleteRequest	true	"ids to delete"
//	@Success		200		{object}	helper.Response{data=DeletedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/taxes/bulk-delete [post]
func (q *taxController) V1BulkDelete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "TaxController-V1BulkDelete"
	taxIDs, err := sanitizers.DeleteTaxes(c)
	if err != nil {
		return err
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = q.taxService.DeleteTaxes(
		ctx,
		tx,
		models.NewTaxFilter().
			WithBranchIDs(currentBranchID(c)).
			WithTaxIDs(taxIDs...),
	); err != nil {
		return taxError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, DeletedResponse{Deleted: len(taxIDs)})
}
