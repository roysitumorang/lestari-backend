package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type financeEntryController struct {
	authService         *middlewares.Service
	financeEntryService services.FinanceEntryService
}

func NewFinanceEntryController(
	authService *middlewares.Service,
	financeEntryService services.FinanceEntryService,
) *financeEntryController {
	return &financeEntryController{
		authService:         authService,
		financeEntryService: financeEntryService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /finance-entries fall through.
func (q *financeEntryController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Get("/export", q.V1Export)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", q.V1Update)
	private.Delete("/:id<guid>", q.V1Delete)
}

// financeEntryError turns the entry errors into responses; anything else goes to the error handler as it is.
func financeEntryError(err error) error {
	switch {
	case errors.Is(err, models.ErrFinanceEntryNotFound):
		return helper.NotFound("Entry")
	case errors.Is(err, models.ErrFinanceEntryConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Entry"))
	case errors.Is(err, models.ErrFinanceEntryInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Entry"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List finance entries
//	@Description	Finance entries of the active branch.
//	@Tags			Cash in & out
//	@Param			q			query		string	false	"search in category, description"
//	@Param			type		query		string	false	"only rows with this type"	Enums(income, expense)
//	@Param			start_date	query		string	false	"entries on or after this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"entries on or before this date (YYYY-MM-DD)"
//	@Param			sort		query		string	false	"sort field"		Enums(entry_date, type, category, amount, payment_method, cash_drawer_id, description, attachment_url, cash_drawer.name, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.FinanceEntry}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/finance-entries [get]
func (q *financeEntryController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FinanceEntryController-V1Index"
	request := sanitizers.FindFinanceEntries(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.financeEntryService.FindFinanceEntries(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFinanceEntries")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one entry
//	@Tags		Cash in & out
//	@Param		id	path		string	true	"entry id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.FinanceEntry}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/finance-entries/{id} [get]
func (q *financeEntryController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FinanceEntryController-V1Show"
	financeEntryID, err := sanitizers.ParamID(c, "id", "Entry")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.financeEntryService.FindFinanceEntry(
		ctx,
		nil,
		models.NewFinanceEntryFilter().
			WithBranchIDs(currentBranchID(c)).
			WithFinanceEntryIDs(financeEntryID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFinanceEntry")
		return financeEntryError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a entry
//	@Tags		Cash in & out
//	@Param		request	body		models.FinanceEntryRequest	true	"entry"
//	@Success	201		{object}	helper.Response{data=models.FinanceEntry}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/finance-entries [post]
func (q *financeEntryController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FinanceEntryController-V1Create"
	request, err := sanitizers.ValidateFinanceEntry(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateFinanceEntry")
		return err
	}
	now := time.Now()
	financeEntry := request.Apply(&models.FinanceEntry{
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
	response, err := q.financeEntryService.CreateFinanceEntry(ctx, tx, financeEntry)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateFinanceEntry")
		return financeEntryError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a entry
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Cash in & out
//	@Param			id		path		string						true	"entry id (UUID)"
//	@Param			request	body		models.FinanceEntryRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.FinanceEntry}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/finance-entries/{id} [put]
func (q *financeEntryController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FinanceEntryController-V1Update"
	financeEntryID, err := sanitizers.ParamID(c, "id", "Entry")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateFinanceEntry(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateFinanceEntry")
		return err
	}
	financeEntry, err := q.financeEntryService.FindFinanceEntry(
		ctx,
		nil,
		models.NewFinanceEntryFilter().
			WithBranchIDs(currentBranchID(c)).
			WithFinanceEntryIDs(financeEntryID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFinanceEntry")
		return financeEntryError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(financeEntry).UpdatedAt = time.Now()
	response, err := q.financeEntryService.UpdateFinanceEntry(ctx, tx, financeEntry)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateFinanceEntry")
		return financeEntryError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a entry
//	@Description	A entry that other records still use cannot be deleted (409).
//	@Tags			Cash in & out
//	@Param			id	path	string	true	"entry id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/finance-entries/{id} [delete]
func (q *financeEntryController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FinanceEntryController-V1Delete"
	financeEntryID, err := sanitizers.ParamID(c, "id", "Entry")
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
	if err = q.financeEntryService.DeleteFinanceEntries(
		ctx,
		tx,
		models.NewFinanceEntryFilter().
			WithBranchIDs(currentBranchID(c)).
			WithFinanceEntryIDs(financeEntryID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteFinanceEntries")
		return financeEntryError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}

// financeEntryExportColumns are the columns of the export; the headers are the attribute names so the file can be imported again.
var financeEntryExportColumns = []export.Column{
	{Key: "id", Label: "id"},
	{Key: "entry_date", Label: "entry_date", Format: "date"},
	{Key: "type", Label: "type"},
	{Key: "category", Label: "category"},
	{Key: "amount", Label: "amount", Format: "number"},
	{Key: "payment_method", Label: "payment_method"},
	{Key: "cash_drawer_id", Label: "cash_drawer_id"},
	{Key: "cash_drawer.name", Label: "cash_drawer_name"},
	{Key: "description", Label: "description"},
	{Key: "attachment_url", Label: "attachment_url"},
	{Key: "created_at", Label: "created_at", Format: "datetime"},
}

// V1Export godoc
//
//	@Summary		Export finance entries
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Cash in & out
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q			query		string	false	"search in category, description"
//	@Param			type		query		string	false	"only rows with this type"	Enums(income, expense)
//	@Param			start_date	query		string	false	"entries on or after this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"entries on or before this date (YYYY-MM-DD)"
//	@Param			sort		query		string	false	"sort field"		Enums(entry_date, type, category, amount, payment_method, cash_drawer_id, description, attachment_url, cash_drawer.name, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/finance-entries/export [get]
func (q *financeEntryController) V1Export(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "FinanceEntryController-V1Export"
	request := sanitizers.FindFinanceEntries(c).
		WithBranchIDs(currentBranchID(c)).
		WithLimit(50000).
		WithPage(1)
	response, _, err := q.financeEntryService.FindFinanceEntries(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFinanceEntries")
		return err
	}
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	return export.Write(c, "finance-entries", "Finance entries", financeEntryExportColumns, rows)
}
