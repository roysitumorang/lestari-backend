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

type supplierController struct {
	authService     *middlewares.Service
	supplierService services.SupplierService
}

func NewSupplierController(
	authService *middlewares.Service,
	supplierService services.SupplierService,
) *supplierController {
	return &supplierController{
		authService:     authService,
		supplierService: supplierService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /suppliers fall through.
func (q *supplierController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Post("/bulk-delete", q.V1BulkDelete)
	private.Post("/import", q.V1Import)
	private.Get("/import-template", q.V1ImportTemplate)
	private.Get("/export", q.V1Export)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", q.V1Update)
	private.Delete("/:id<guid>", q.V1Delete)
}

// supplierError turns the supplier errors into responses; anything else goes to the error handler as it is.
func supplierError(err error) error {
	switch {
	case errors.Is(err, models.ErrSupplierNotFound):
		return helper.NotFound("Supplier")
	case errors.Is(err, models.ErrSupplierConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Supplier"))
	case errors.Is(err, models.ErrSupplierInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Supplier"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List suppliers
//	@Description	Suppliers of the active branch.
//	@Tags			Suppliers
//	@Param			q			query		string	false	"search in name, contact_person, phone, email, code"
//	@Param			sort		query		string	false	"sort field"		Enums(name, contact_person, phone, email, code, payable_balance, address, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Supplier}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/suppliers [get]
func (q *supplierController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SupplierController-V1Index"
	request := sanitizers.FindSuppliers(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.supplierService.FindSuppliers(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSuppliers")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one supplier
//	@Tags		Suppliers
//	@Param		id	path		string	true	"supplier id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Supplier}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/suppliers/{id} [get]
func (q *supplierController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SupplierController-V1Show"
	supplierID, err := sanitizers.ParamID(c, "id", "Supplier")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.supplierService.FindSupplier(
		ctx,
		nil,
		models.NewSupplierFilter().
			WithBranchIDs(currentBranchID(c)).
			WithSupplierIDs(supplierID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSupplier")
		return supplierError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a supplier
//	@Tags		Suppliers
//	@Param		request	body		models.SupplierRequest	true	"supplier"
//	@Success	201		{object}	helper.Response{data=models.Supplier}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/suppliers [post]
func (q *supplierController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SupplierController-V1Create"
	request, err := sanitizers.ValidateSupplier(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateSupplier")
		return err
	}
	now := time.Now()
	supplier := request.Apply(&models.Supplier{
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
	response, err := q.supplierService.CreateSupplier(ctx, tx, supplier)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateSupplier")
		return supplierError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a supplier
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Suppliers
//	@Param			id		path		string					true	"supplier id (UUID)"
//	@Param			request	body		models.SupplierRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Supplier}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/suppliers/{id} [put]
func (q *supplierController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SupplierController-V1Update"
	supplierID, err := sanitizers.ParamID(c, "id", "Supplier")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateSupplier(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateSupplier")
		return err
	}
	supplier, err := q.supplierService.FindSupplier(
		ctx,
		nil,
		models.NewSupplierFilter().
			WithBranchIDs(currentBranchID(c)).
			WithSupplierIDs(supplierID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSupplier")
		return supplierError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(supplier).UpdatedAt = time.Now()
	response, err := q.supplierService.UpdateSupplier(ctx, tx, supplier)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateSupplier")
		return supplierError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a supplier
//	@Description	A supplier that other records still use cannot be deleted (409).
//	@Tags			Suppliers
//	@Param			id	path	string	true	"supplier id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/suppliers/{id} [delete]
func (q *supplierController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SupplierController-V1Delete"
	supplierID, err := sanitizers.ParamID(c, "id", "Supplier")
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
	if err = q.supplierService.DeleteSuppliers(
		ctx,
		tx,
		models.NewSupplierFilter().
			WithBranchIDs(currentBranchID(c)).
			WithSupplierIDs(supplierID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteSuppliers")
		return supplierError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}

// V1BulkDelete godoc
//
//	@Summary		Delete several suppliers
//	@Description	All or nothing: an unknown id (404) or a supplier still in use (409) deletes none.
//	@Tags			Suppliers
//	@Param			request	body		models.BulkDeleteRequest	true	"ids to delete"
//	@Success		200		{object}	helper.Response{data=models.DeletedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/suppliers/bulk-delete [post]
func (q *supplierController) V1BulkDelete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SupplierController-V1BulkDelete"
	supplierIDs, err := sanitizers.BulkDeleteIDs(c)
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
	if err = q.supplierService.DeleteSuppliers(
		ctx,
		tx,
		models.NewSupplierFilter().
			WithBranchIDs(currentBranchID(c)).
			WithSupplierIDs(supplierIDs...),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteSuppliers")
		return supplierError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, models.DeletedResponse{Deleted: len(supplierIDs)})
}

// supplierExportColumns are the columns of the export; the headers are the attribute names so the file can be imported again.
var supplierExportColumns = []export.Column{
	{Key: "id", Label: "id"},
	{Key: "name", Label: "name"},
	{Key: "contact_person", Label: "contact_person"},
	{Key: "phone", Label: "phone"},
	{Key: "email", Label: "email"},
	{Key: "code", Label: "code"},
	{Key: "payable_balance", Label: "payable_balance", Format: "number"},
	{Key: "address", Label: "address"},
	{Key: "created_at", Label: "created_at", Format: "datetime"},
}

// V1Export godoc
//
//	@Summary		Export suppliers
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Suppliers
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format	query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q		query		string	false	"search in name, contact_person, phone, email, code"
//	@Param			sort	query		string	false	"sort field"		Enums(name, contact_person, phone, email, code, payable_balance, address, created_at, updated_at)
//	@Param			order	query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Success		200		{file}		binary
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/suppliers/export [get]
func (q *supplierController) V1Export(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SupplierController-V1Export"
	request := sanitizers.FindSuppliers(c).
		WithBranchIDs(currentBranchID(c)).
		WithLimit(50000).
		WithPage(1)
	response, _, err := q.supplierService.FindSuppliers(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSuppliers")
		return err
	}
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	return export.Write(c, "suppliers", "Suppliers", supplierExportColumns, rows)
}

// V1ImportTemplate godoc
//
//	@Summary		Download the supplier import template
//	@Description	An .xlsx file with one column per attribute; a * marks the required ones.
//	@Tags			Suppliers
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/suppliers/import-template [get]
func (q *supplierController) V1ImportTemplate(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SupplierController-V1ImportTemplate"
	columns := []export.Column{
		{Key: "name", Label: "name*"},
		{Key: "contact_person", Label: "contact_person"},
		{Key: "phone", Label: "phone"},
		{Key: "email", Label: "email"},
		{Key: "code", Label: "code"},
		{Key: "address", Label: "address"},
	}
	example := map[string]any{}
	response, err := export.XLSX(helper.Lang(c), "Suppliers", columns, []map[string]any{example})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrXLSX")
		return err
	}
	return export.Send(c, "suppliers-template.xlsx", xlsxContentType, response)
}

// V1Import godoc
//
//	@Summary		Import suppliers
//	@Description	Upload an .xlsx or .csv file that follows the template. mode=create adds every row; mode=update changes the rows matched by code. Each row is saved on its own, so one bad row does not stop the others.
//	@Tags			Suppliers
//	@Accept			multipart/form-data
//	@Param			file	formData	file	true	".xlsx or .csv file"
//	@Param			mode	formData	string	false	"create or update"	Enums(create, update)	default(create)
//	@Success		200		{object}	helper.Response{data=models.ImportResult}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/suppliers/import [post]
func (q *supplierController) V1Import(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SupplierController-V1Import"
	rows, err := sanitizers.ImportRows(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrImportRows")
		return err
	}
	update := sanitizers.ImportMode(c) == "update"
	response := models.ImportResult{Errors: []models.ImportError{}}
	for i, row := range rows {
		rowNo := i + 2 // the header is row 1
		created, err := q.importRow(c, row, update)
		switch {
		case err != nil:
			response.Failed++
			response.Errors = append(response.Errors, models.ImportError{Row: rowNo, Message: describeError(helper.Lang(c), supplierError(err))})
		case created:
			response.Created++
		default:
			response.Updated++
		}
	}
	return helper.OK(c, response)
}

// importRow saves one imported row in its own transaction; created tells an insert from an update.
func (q *supplierController) importRow(c fiber.Ctx, row map[string]any, update bool) (created bool, err error) {
	ctx := c.Context()
	ctxt := "SupplierController-importRow"
	now := time.Now()
	supplier := &models.Supplier{
		BranchID:  currentBranchID(c),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if update {
		key := helper.Str(row, "code")
		if key == "" {
			return false, helper.BadRequest("{key} is required to update", "key", "code")
		}
		if supplier, err = q.supplierService.FindSupplier(
			ctx,
			nil,
			models.NewSupplierFilter().
				WithBranchIDs(currentBranchID(c)).
				WithCode(key),
		); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSupplier")
			if errors.Is(err, models.ErrSupplierNotFound) {
				err = helper.BadRequest("No {item} with {key} {value}", "item", i18n.Tr("Supplier"), "key", "code", "value", key)
			}
			return false, err
		}
		supplier.UpdatedAt = now
	}
	request, err := sanitizers.ValidateSupplierRow(row, !update)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateSupplierRow")
		return false, err
	}
	request.Apply(supplier)
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return false, err
	}
	defer rollback(ctx, tx, ctxt)
	if update {
		_, err = q.supplierService.UpdateSupplier(ctx, tx, supplier)
	} else {
		_, err = q.supplierService.CreateSupplier(ctx, tx, supplier)
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSaveSupplier")
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return false, err
	}
	return !update, nil
}
