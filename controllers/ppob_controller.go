package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type ppobController struct {
	authService *middlewares.Service
	ppobService services.PPOBService
	uploader    *Uploader
}

func NewPPOBController(
	authService *middlewares.Service,
	ppobService services.PPOBService,
	uploader *Uploader,
) *ppobController {
	return &ppobController{
		authService: authService,
		ppobService: ppobService,
		uploader:    uploader,
	}
}

// Mount registers the routes of /ppob.
func (q *ppobController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("/deposits", q.V1Deposits)
	private.Post("/deposits", q.V1CreateDeposit)
	private.Post("/deposits/:id<guid>/payment-proof", q.V1DepositProof)
	private.Post("/deposits/:id<guid>/cancel", q.V1CancelDeposit)
	private.Post("/transactions", q.V1Sell)
	private.Get("/prices/prepaid", q.V1PrepaidPrices)
	private.Get("/prices/prepaid/export", q.V1ExportPrepaidPrices)
	private.Get("/prices/prepaid/import-template", q.V1PrepaidImportTemplate)
	private.Post("/prices/prepaid/import", q.V1ImportPrepaidPrices)
	private.Put("/prices/prepaid/:id<guid>", q.V1UpdatePrepaidPrice)
	private.Get("/prices/postpaid", q.V1PostpaidPrices)
	private.Get("/prices/postpaid/export", q.V1ExportPostpaidPrices)
	private.Get("/prices/postpaid/import-template", q.V1PostpaidImportTemplate)
	private.Post("/prices/postpaid/import", q.V1ImportPostpaidPrices)
	private.Put("/prices/postpaid/:id<guid>", q.V1UpdatePostpaidPrice)
}

// ppobError turns the PPOB errors into responses; anything else goes to the error handler as it is.
func ppobError(err error) error {
	switch {
	case errors.Is(err, models.ErrPPOBPriceNotFound):
		return helper.NotFound("Price")
	case errors.Is(err, models.ErrPPOBDepositNotFound):
		return helper.NotFound("Deposit")
	}
	return writeError(err)
}

// priceAttribute is the attribute a branch sets for a kind of PPOB product, and costAttribute what the
// provider charges.
func priceAttribute(kind string) (price, cost string) {
	if kind == models.PPOBKindPostpaid {
		return "admin_fee", "base_admin_fee"
	}
	return "selling_price", "base_price"
}

func (q *ppobController) listPrices(c fiber.Ctx, ctxt, kind string) error {
	ctx := c.Context()
	request := sanitizers.FindPPOBPrices(c).
		WithBranchIDs(currentBranchID(c)).
		WithKind(kind)
	response, total, err := q.ppobService.FindPPOBPrices(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBPrices")
		return err
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

func (q *ppobController) exportPrices(c fiber.Ctx, ctxt, kind string) error {
	ctx := c.Context()
	request := sanitizers.FindPPOBPrices(c).
		WithBranchIDs(currentBranchID(c)).
		WithKind(kind).
		WithLimit(50000).
		WithPage(1)
	response, _, err := q.ppobService.FindPPOBPrices(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBPrices")
		return err
	}
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	price, cost := priceAttribute(kind)
	columns := []export.Column{
		{Key: "id", Label: "id"},
		{Key: "category", Label: "category"},
		{Key: "provider", Label: "provider"},
		{Key: "product_code", Label: "product_code"},
		{Key: "product_name", Label: "product_name"},
		{Key: "is_active", Label: "is_active", Format: "boolean"},
		{Key: cost, Label: cost, Format: "number"},
		{Key: price, Label: price, Format: "number"},
		{Key: "created_at", Label: "created_at", Format: "datetime"},
	}
	return export.Write(c, "ppob-"+kind+"-prices", "PPOB "+kind+" prices", columns, rows)
}

func (q *ppobController) priceTemplate(c fiber.Ctx, ctxt, kind string) error {
	ctx := c.Context()
	price, _ := priceAttribute(kind)
	columns := []export.Column{
		{Key: "product_code", Label: "product_code*"},
		{Key: "is_active", Label: "is_active"},
		{Key: price, Label: price + "*"},
	}
	example := map[string]any{"is_active": "true", price: float64(0)}
	response, err := export.XLSX(helper.Lang(c), "PPOB "+kind+" prices", columns, []map[string]any{example})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrXLSX")
		return err
	}
	return export.Send(c, "ppob-"+kind+"-prices-template.xlsx", xlsxContentType, response)
}

// savePrice applies a validated request to the price a filter selects, in a transaction of its own.
func (q *ppobController) savePrice(c fiber.Ctx, ctxt string, filter *models.PPOBPriceFilter, request *models.PPOBPriceRequest) (*models.PPOBPrice, error) {
	ctx := c.Context()
	price, err := q.ppobService.FindPPOBPrice(ctx, nil, filter)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBPrice")
		return nil, err
	}
	var response *models.PPOBPrice
	err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		request.Apply(price).UpdatedAt = time.Now()
		if response, err = q.ppobService.UpdatePPOBPrice(ctx, tx, price); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePPOBPrice")
		}
		return err
	})
	return response, err
}

func (q *ppobController) updatePrice(c fiber.Ctx, ctxt, kind string) error {
	ctx := c.Context()
	priceID, err := sanitizers.ParamID(c, "id", "Price")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidatePPOBPrice(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidatePPOBPrice")
		return err
	}
	response, err := q.savePrice(c, ctxt, models.NewPPOBPriceFilter().WithBranchIDs(currentBranchID(c)).WithKind(kind).WithPPOBPriceIDs(priceID), request)
	if err != nil {
		return ppobError(err)
	}
	return helper.OK(c, response)
}

func (q *ppobController) importPrices(c fiber.Ctx, ctxt, kind string) error {
	ctx := c.Context()
	rows, err := sanitizers.ImportRows(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrImportRows")
		return err
	}
	update := sanitizers.ImportMode(c) == "update"
	response := models.ImportResult{Errors: []models.ImportError{}}
	for i, row := range rows {
		err := q.importPrice(c, ctxt, kind, row, update)
		if err == nil {
			response.Updated++
			continue
		}
		response.Failed++
		response.Errors = append(response.Errors, models.ImportError{Row: i + 2, Message: describeError(helper.Lang(c), ppobError(err))})
	}
	return helper.OK(c, response)
}

// importPrice saves one imported row; prices can only be updated, matched by product_code.
func (q *ppobController) importPrice(c fiber.Ctx, ctxt, kind string, row map[string]any, update bool) error {
	if !update {
		return helper.Forbidden("New PPOB products come from the provider catalog.")
	}
	productCode := helper.Str(row, "product_code")
	if productCode == "" {
		return helper.BadRequest("{key} is required to update", "key", "product_code")
	}
	request, err := sanitizers.ValidatePPOBPriceRow(row)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrValidatePPOBPriceRow")
		return err
	}
	_, err = q.savePrice(c, ctxt, models.NewPPOBPriceFilter().WithBranchIDs(currentBranchID(c)).WithKind(kind).WithProductCode(productCode), request)
	if errors.Is(err, models.ErrPPOBPriceNotFound) {
		err = helper.BadRequest("No {item} with {key} {value}", "item", i18n.Tr("Price"), "key", "product_code", "value", productCode)
	}
	return err
}

// V1PrepaidPrices godoc
//
//	@Summary		List prepaid PPOB prices
//	@Description	The prepaid products of the PPOB price list of the active branch (pulsa, data, tokens, vouchers) with their cost and selling price.
//	@Tags			PPOB
//	@Param			q			query		string	false	"search in provider, product_code, product_name"
//	@Param			category	query		string	false	"only rows with this category"
//	@Param			sort		query		string	false	"sort field"		Enums(category, provider, product_code, product_name, is_active, base_price, selling_price, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.PPOBPrice}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/prices/prepaid [get]
func (q *ppobController) V1PrepaidPrices(c fiber.Ctx) error {
	return q.listPrices(c, "PPOBController-V1PrepaidPrices", models.PPOBKindPrepaid)
}

// V1ExportPrepaidPrices godoc
//
//	@Summary	Export prepaid PPOB prices
//	@Tags		PPOB
//	@Produce	application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param		format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param		q			query		string	false	"search in provider, product_code, product_name"
//	@Param		category	query		string	false	"only rows with this category"
//	@Success	200			{file}		binary
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/ppob/prices/prepaid/export [get]
func (q *ppobController) V1ExportPrepaidPrices(c fiber.Ctx) error {
	return q.exportPrices(c, "PPOBController-V1ExportPrepaidPrices", models.PPOBKindPrepaid)
}

// V1PrepaidImportTemplate godoc
//
//	@Summary		Download the prepaid PPOB price import template
//	@Description	An .xlsx file with the columns product_code*, is_active and selling_price*.
//	@Tags			PPOB
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/prices/prepaid/import-template [get]
func (q *ppobController) V1PrepaidImportTemplate(c fiber.Ctx) error {
	return q.priceTemplate(c, "PPOBController-V1PrepaidImportTemplate", models.PPOBKindPrepaid)
}

// V1ImportPrepaidPrices godoc
//
//	@Summary		Import prepaid PPOB prices
//	@Description	Upload an .xlsx or .csv file that follows the template with mode=update: rows are matched by product_code. New PPOB products come from the provider catalog, so mode=create rejects every row.
//	@Tags			PPOB
//	@Accept			multipart/form-data
//	@Param			file	formData	file	true	".xlsx or .csv file"
//	@Param			mode	formData	string	false	"create or update"	Enums(create, update)	default(create)
//	@Success		200		{object}	helper.Response{data=models.ImportResult}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/prices/prepaid/import [post]
func (q *ppobController) V1ImportPrepaidPrices(c fiber.Ctx) error {
	return q.importPrices(c, "PPOBController-V1ImportPrepaidPrices", models.PPOBKindPrepaid)
}

// V1UpdatePrepaidPrice godoc
//
//	@Summary		Update a prepaid PPOB price
//	@Description	Changes selling_price or is_active. The selling price cannot be below the cost. When no value differs from the stored one nothing is written.
//	@Tags			PPOB
//	@Param			id		path		string					true	"price id (UUID)"
//	@Param			request	body		models.PPOBPriceRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.PPOBPrice}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/prices/prepaid/{id} [put]
func (q *ppobController) V1UpdatePrepaidPrice(c fiber.Ctx) error {
	return q.updatePrice(c, "PPOBController-V1UpdatePrepaidPrice", models.PPOBKindPrepaid)
}

// V1PostpaidPrices godoc
//
//	@Summary		List postpaid PPOB prices
//	@Description	The postpaid products of the PPOB price list of the active branch (bills) with the admin fee the provider and the branch charge.
//	@Tags			PPOB
//	@Param			q			query		string	false	"search in provider, product_code, product_name"
//	@Param			category	query		string	false	"only rows with this category"
//	@Param			sort		query		string	false	"sort field"		Enums(category, provider, product_code, product_name, is_active, base_admin_fee, admin_fee, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.PPOBPrice}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/prices/postpaid [get]
func (q *ppobController) V1PostpaidPrices(c fiber.Ctx) error {
	return q.listPrices(c, "PPOBController-V1PostpaidPrices", models.PPOBKindPostpaid)
}

// V1ExportPostpaidPrices godoc
//
//	@Summary	Export postpaid PPOB prices
//	@Tags		PPOB
//	@Produce	application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param		format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param		q			query		string	false	"search in provider, product_code, product_name"
//	@Param		category	query		string	false	"only rows with this category"
//	@Success	200			{file}		binary
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/ppob/prices/postpaid/export [get]
func (q *ppobController) V1ExportPostpaidPrices(c fiber.Ctx) error {
	return q.exportPrices(c, "PPOBController-V1ExportPostpaidPrices", models.PPOBKindPostpaid)
}

// V1PostpaidImportTemplate godoc
//
//	@Summary		Download the postpaid PPOB price import template
//	@Description	An .xlsx file with the columns product_code*, is_active and admin_fee*.
//	@Tags			PPOB
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/prices/postpaid/import-template [get]
func (q *ppobController) V1PostpaidImportTemplate(c fiber.Ctx) error {
	return q.priceTemplate(c, "PPOBController-V1PostpaidImportTemplate", models.PPOBKindPostpaid)
}

// V1ImportPostpaidPrices godoc
//
//	@Summary		Import postpaid PPOB prices
//	@Description	Upload an .xlsx or .csv file that follows the template with mode=update: rows are matched by product_code. New PPOB products come from the provider catalog, so mode=create rejects every row.
//	@Tags			PPOB
//	@Accept			multipart/form-data
//	@Param			file	formData	file	true	".xlsx or .csv file"
//	@Param			mode	formData	string	false	"create or update"	Enums(create, update)	default(create)
//	@Success		200		{object}	helper.Response{data=models.ImportResult}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/prices/postpaid/import [post]
func (q *ppobController) V1ImportPostpaidPrices(c fiber.Ctx) error {
	return q.importPrices(c, "PPOBController-V1ImportPostpaidPrices", models.PPOBKindPostpaid)
}

// V1UpdatePostpaidPrice godoc
//
//	@Summary		Update a postpaid PPOB price
//	@Description	Changes admin_fee or is_active. When no value differs from the stored one nothing is written.
//	@Tags			PPOB
//	@Param			id		path		string					true	"price id (UUID)"
//	@Param			request	body		models.PPOBPriceRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.PPOBPrice}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/prices/postpaid/{id} [put]
func (q *ppobController) V1UpdatePostpaidPrice(c fiber.Ctx) error {
	return q.updatePrice(c, "PPOBController-V1UpdatePostpaidPrice", models.PPOBKindPostpaid)
}

// V1Deposits godoc
//
//	@Summary		List PPOB deposits
//	@Description	The top-ups of the PPOB balance in a date range (today by default). The summary carries the balance and the total of the PPOB sales.
//	@Tags			PPOB
//	@Param			start_date	query		string	false	"from this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"to this date (YYYY-MM-DD)"
//	@Param			q			query		string	false	"search in reference_id, payment_method"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.PPOBDeposit,summary=models.PPOBSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/deposits [get]
func (q *ppobController) V1Deposits(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PPOBController-V1Deposits"
	request, err := sanitizers.FindPPOBDeposits(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBDeposits")
		return err
	}
	request.BranchID = currentBranchID(c)
	response, total, err := q.ppobService.FindPPOBDeposits(ctx, request, time.Now())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBDeposits")
		return err
	}
	summary, err := q.ppobService.FindSummary(ctx, request.BranchID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSummary")
		return err
	}
	return helper.ListWith(c, response, page(request.Limit, request.Page, total), helper.Extras{Summary: summary})
}

// V1CreateDeposit godoc
//
//	@Summary		Top up the PPOB balance
//	@Description	Creates a pending deposit of at least Rp 100,000 with payment instructions; the balance cannot exceed Rp 2,000,000. It is credited when the payment arrives.
//	@Tags			PPOB
//	@Param			request	body		models.PPOBDepositRequest	true	"amount and payment"
//	@Success		200		{object}	helper.Response{data=models.PPOBDeposit}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/deposits [post]
func (q *ppobController) V1CreateDeposit(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PPOBController-V1CreateDeposit"
	request, err := sanitizers.PPOBDeposit(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPPOBDeposit")
		return err
	}
	var response *models.PPOBDeposit
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.ppobService.CreatePPOBDeposit(ctx, tx, currentBranchID(c), request, helper.Lang(c), time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePPOBDeposit")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1DepositProof godoc
//
//	@Summary	Upload the payment proof of a deposit
//	@Tags		PPOB
//	@Accept		multipart/form-data
//	@Param		id		path		string	true	"deposit id (UUID)"
//	@Param		file	formData	file	true	"proof of the transfer"
//	@Success	200		{object}	helper.Response{data=models.PPOBDeposit}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/ppob/deposits/{id}/payment-proof [post]
func (q *ppobController) V1DepositProof(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PPOBController-V1DepositProof"
	depositID, err := sanitizers.ParamID(c, "id", "Deposit")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	deposit, err := q.ppobService.FindPPOBDeposit(ctx, nil, currentBranchID(c), depositID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBDeposit")
		return ppobError(err)
	}
	if deposit.Status != models.PPOBDepositStatusPending {
		err = helper.Conflict("Only a pending top-up accepts a payment proof.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNotPending")
		return err
	}
	upload, err := q.uploader.Save(c, "file", "payment_proof")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSave")
		return err
	}
	var response *models.PPOBDeposit
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.ppobService.AttachDepositProof(ctx, tx, deposit, upload.URL, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrAttachDepositProof")
		}
		return ppobError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1CancelDeposit godoc
//
//	@Summary	Cancel a pending deposit
//	@Tags		PPOB
//	@Param		id	path		string	true	"deposit id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.PPOBDeposit}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/ppob/deposits/{id}/cancel [post]
func (q *ppobController) V1CancelDeposit(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PPOBController-V1CancelDeposit"
	depositID, err := sanitizers.ParamID(c, "id", "Deposit")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	var response *models.PPOBDeposit
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.ppobService.CancelDeposit(ctx, tx, currentBranchID(c), depositID, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCancelDeposit")
		}
		return ppobError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1Sell godoc
//
//	@Summary		Record a PPOB sale
//	@Description	Used by the cashier app. The cost of the product is charged to the PPOB balance of the branch (402 when it is too low). A postpaid product needs the bill amount.
//	@Tags			PPOB
//	@Param			request	body		models.PPOBSaleRequest	true	"product and customer"
//	@Success		201		{object}	helper.Response{data=models.PPOBTransaction}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/ppob/transactions [post]
func (q *ppobController) V1Sell(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PPOBController-V1Sell"
	request, err := sanitizers.PPOBSale(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPPOBSale")
		return err
	}
	var response *models.PPOBTransaction
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.ppobService.Sell(ctx, tx, currentBranchID(c), request, currentActor(c), time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSell")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.Created(c, response)
}
