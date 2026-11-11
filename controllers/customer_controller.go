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

type customerController struct {
	authService     *middlewares.Service
	customerService services.CustomerService
}

func NewCustomerController(
	authService *middlewares.Service,
	customerService services.CustomerService,
) *customerController {
	return &customerController{
		authService:     authService,
		customerService: customerService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /customers fall through.
func (q *customerController) Mount(r fiber.Router) {
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

// customerError turns the customer errors into responses; anything else goes to the error handler as it is.
func customerError(err error) error {
	switch {
	case errors.Is(err, models.ErrCustomerNotFound):
		return helper.NotFound("Customer")
	case errors.Is(err, models.ErrCustomerConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Customer"))
	case errors.Is(err, models.ErrCustomerInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Customer"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List customers
//	@Description	Customers of the active branch.
//	@Tags			Customers
//	@Param			q					query		string	false	"search in name, phone, email, code"
//	@Param			customer_type_id	query		string	false	"only rows with this customer type id"
//	@Param			sort				query		string	false	"sort field"		Enums(name, phone, email, code, customer_type_id, birth_date, points, receivable_balance, address, note, customer_type.name, created_at, updated_at)
//	@Param			order				query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page				query		int		false	"page, from 1"		default(1)
//	@Param			per_page			query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200					{object}	helper.Response{data=helper.Pagination{rows=[]models.Customer}}
//	@Failure		401					{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customers [get]
func (q *customerController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerController-V1Index"
	request := sanitizers.FindCustomers(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.customerService.FindCustomers(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomers")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one customer
//	@Tags		Customers
//	@Param		id	path		string	true	"customer id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Customer}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/customers/{id} [get]
func (q *customerController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerController-V1Show"
	customerID, err := sanitizers.ParamID(c, "id", "Customer")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.customerService.FindCustomer(
		ctx,
		nil,
		models.NewCustomerFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCustomerIDs(customerID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomer")
		return customerError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a customer
//	@Tags		Customers
//	@Param		request	body		models.CustomerRequest	true	"customer"
//	@Success	201		{object}	helper.Response{data=models.Customer}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/customers [post]
func (q *customerController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerController-V1Create"
	request, err := sanitizers.ValidateCustomer(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCustomer")
		return err
	}
	now := time.Now()
	customer := request.Apply(&models.Customer{
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
	response, err := q.customerService.CreateCustomer(ctx, tx, customer)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateCustomer")
		return customerError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a customer
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Customers
//	@Param			id		path		string					true	"customer id (UUID)"
//	@Param			request	body		models.CustomerRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Customer}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customers/{id} [put]
func (q *customerController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerController-V1Update"
	customerID, err := sanitizers.ParamID(c, "id", "Customer")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateCustomer(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCustomer")
		return err
	}
	customer, err := q.customerService.FindCustomer(
		ctx,
		nil,
		models.NewCustomerFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCustomerIDs(customerID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomer")
		return customerError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(customer).UpdatedAt = time.Now()
	response, err := q.customerService.UpdateCustomer(ctx, tx, customer)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateCustomer")
		return customerError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a customer
//	@Description	A customer that other records still use cannot be deleted (409).
//	@Tags			Customers
//	@Param			id	path	string	true	"customer id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customers/{id} [delete]
func (q *customerController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerController-V1Delete"
	customerID, err := sanitizers.ParamID(c, "id", "Customer")
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
	if err = q.customerService.DeleteCustomers(
		ctx,
		tx,
		models.NewCustomerFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCustomerIDs(customerID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCustomers")
		return customerError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}

// V1BulkDelete godoc
//
//	@Summary		Delete several customers
//	@Description	All or nothing: an unknown id (404) or a customer still in use (409) deletes none.
//	@Tags			Customers
//	@Param			request	body		models.BulkDeleteRequest	true	"ids to delete"
//	@Success		200		{object}	helper.Response{data=models.DeletedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customers/bulk-delete [post]
func (q *customerController) V1BulkDelete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerController-V1BulkDelete"
	customerIDs, err := sanitizers.BulkDeleteIDs(c)
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
	if err = q.customerService.DeleteCustomers(
		ctx,
		tx,
		models.NewCustomerFilter().
			WithBranchIDs(currentBranchID(c)).
			WithCustomerIDs(customerIDs...),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCustomers")
		return customerError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, models.DeletedResponse{Deleted: len(customerIDs)})
}

// customerExportColumns are the columns of the export; the headers are the attribute names so the file can be imported again.
var customerExportColumns = []export.Column{
	{Key: "id", Label: "id"},
	{Key: "name", Label: "name"},
	{Key: "phone", Label: "phone"},
	{Key: "email", Label: "email"},
	{Key: "code", Label: "code"},
	{Key: "customer_type_id", Label: "customer_type_id"},
	{Key: "customer_type.name", Label: "customer_type_name"},
	{Key: "birth_date", Label: "birth_date", Format: "date"},
	{Key: "points", Label: "points", Format: "number"},
	{Key: "receivable_balance", Label: "receivable_balance", Format: "number"},
	{Key: "address", Label: "address"},
	{Key: "note", Label: "note"},
	{Key: "created_at", Label: "created_at", Format: "datetime"},
}

// V1Export godoc
//
//	@Summary		Export customers
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Customers
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format				query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q					query		string	false	"search in name, phone, email, code"
//	@Param			customer_type_id	query		string	false	"only rows with this customer type id"
//	@Param			sort				query		string	false	"sort field"		Enums(name, phone, email, code, customer_type_id, birth_date, points, receivable_balance, address, note, customer_type.name, created_at, updated_at)
//	@Param			order				query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Success		200					{file}		binary
//	@Failure		401					{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customers/export [get]
func (q *customerController) V1Export(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerController-V1Export"
	request := sanitizers.FindCustomers(c).
		WithBranchIDs(currentBranchID(c)).
		WithLimit(50000).
		WithPage(1)
	response, _, err := q.customerService.FindCustomers(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomers")
		return err
	}
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	return export.Write(c, "customers", "Customers", customerExportColumns, rows)
}

// V1ImportTemplate godoc
//
//	@Summary		Download the customer import template
//	@Description	An .xlsx file with one column per attribute; a * marks the required ones.
//	@Tags			Customers
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customers/import-template [get]
func (q *customerController) V1ImportTemplate(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerController-V1ImportTemplate"
	columns := []export.Column{
		{Key: "name", Label: "name*"},
		{Key: "phone", Label: "phone"},
		{Key: "email", Label: "email"},
		{Key: "code", Label: "code"},
		{Key: "customer_type_id", Label: "customer_type_id"},
		{Key: "birth_date", Label: "birth_date"},
		{Key: "address", Label: "address"},
		{Key: "note", Label: "note"},
	}
	example := map[string]any{"birth_date": "2026-01-31"}
	response, err := export.XLSX(helper.Lang(c), "Customers", columns, []map[string]any{example})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrXLSX")
		return err
	}
	return export.Send(c, "customers-template.xlsx", xlsxContentType, response)
}

// V1Import godoc
//
//	@Summary		Import customers
//	@Description	Upload an .xlsx or .csv file that follows the template. mode=create adds every row; mode=update changes the rows matched by code. Each row is saved on its own, so one bad row does not stop the others.
//	@Tags			Customers
//	@Accept			multipart/form-data
//	@Param			file	formData	file	true	".xlsx or .csv file"
//	@Param			mode	formData	string	false	"create or update"	Enums(create, update)	default(create)
//	@Success		200		{object}	helper.Response{data=models.ImportResult}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customers/import [post]
func (q *customerController) V1Import(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "CustomerController-V1Import"
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
			response.Errors = append(response.Errors, models.ImportError{Row: rowNo, Message: describeError(helper.Lang(c), customerError(err))})
		case created:
			response.Created++
		default:
			response.Updated++
		}
	}
	return helper.OK(c, response)
}

// importRow saves one imported row in its own transaction; created tells an insert from an update.
func (q *customerController) importRow(c fiber.Ctx, row map[string]any, update bool) (created bool, err error) {
	ctx := c.Context()
	ctxt := "CustomerController-importRow"
	now := time.Now()
	customer := &models.Customer{
		BranchID:  currentBranchID(c),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if update {
		key := helper.Str(row, "code")
		if key == "" {
			return false, helper.BadRequest("{key} is required to update", "key", "code")
		}
		if customer, err = q.customerService.FindCustomer(
			ctx,
			nil,
			models.NewCustomerFilter().
				WithBranchIDs(currentBranchID(c)).
				WithCode(key),
		); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomer")
			if errors.Is(err, models.ErrCustomerNotFound) {
				err = helper.BadRequest("No {item} with {key} {value}", "item", i18n.Tr("Customer"), "key", "code", "value", key)
			}
			return false, err
		}
		customer.UpdatedAt = now
	}
	request, err := sanitizers.ValidateCustomerRow(row, !update)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateCustomerRow")
		return false, err
	}
	request.Apply(customer)
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return false, err
	}
	defer rollback(ctx, tx, ctxt)
	if update {
		_, err = q.customerService.UpdateCustomer(ctx, tx, customer)
	} else {
		_, err = q.customerService.CreateCustomer(ctx, tx, customer)
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSaveCustomer")
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return false, err
	}
	return !update, nil
}
