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

type debtController struct {
	authService *middlewares.Service
	debtService services.DebtService
}

func NewDebtController(
	authService *middlewares.Service,
	debtService services.DebtService,
) *debtController {
	return &debtController{
		authService: authService,
		debtService: debtService,
	}
}

// Mount registers the payable routes under /payables and the receivable routes under /receivables.
func (q *debtController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("/payables", q.V1Payables)
	private.Get("/payables/export", q.V1PayablesExport)
	private.Get("/payables/:id<guid>", q.V1Payable)
	private.Post("/payables/:id<guid>/payments", q.V1PayPayable)
	private.Get("/receivables", q.V1Receivables)
	private.Get("/receivables/export", q.V1ReceivablesExport)
	private.Get("/receivables/:id<guid>", q.V1Receivable)
	private.Post("/receivables/:id<guid>/payments", q.V1PayReceivable)
}

// debtNames are the names of a debt of a kind in messages.
var debtNames = map[string]string{models.DebtKindPayable: "Payable", models.DebtKindReceivable: "Receivable"}

// debtError turns the debt errors into responses; anything else goes to the error handler as it is.
func debtError(err error, kind string) error {
	switch {
	case errors.Is(err, models.ErrDebtNotFound):
		return helper.NotFound(debtNames[kind])
	case errors.Is(err, models.ErrDebtPaid):
		return helper.Conflict("This {kind} is already paid.", "kind", i18n.Tr(debtNames[kind]))
	}
	return writeError(err)
}

func (q *debtController) index(c fiber.Ctx, ctxt, kind string) error {
	ctx := c.Context()
	request := sanitizers.FindDebts(c, kind)
	request.BranchID, request.Today = currentBranchID(c), time.Now().In(helper.Location).Format(time.DateOnly)
	response, total, err := q.debtService.FindDebts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDebts")
		return err
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

// debtExportColumns are the columns of the export of a kind: the party is the supplier or the customer.
func debtExportColumns(party string) []export.Column {
	return []export.Column{
		{Key: "id", Label: "id"},
		{Key: "reference_number", Label: "reference_number"},
		{Key: party + "_id", Label: party + "_id"},
		{Key: party + "_name", Label: party + "_name"},
		{Key: "transaction_date", Label: "transaction_date", Format: "date"},
		{Key: "due_date", Label: "due_date", Format: "date"},
		{Key: "total_amount", Label: "total_amount", Format: "number"},
		{Key: "paid_amount", Label: "paid_amount", Format: "number"},
		{Key: "remaining_amount", Label: "remaining_amount", Format: "number"},
		{Key: "status", Label: "status"},
		{Key: "created_at", Label: "created_at", Format: "datetime"},
	}
}

func (q *debtController) export(c fiber.Ctx, ctxt, kind, party, name, title string) error {
	ctx := c.Context()
	request := sanitizers.FindDebts(c, kind)
	request.BranchID, request.Limit, request.Page = currentBranchID(c), exportLimit, 1
	request.Today = time.Now().In(helper.Location).Format(time.DateOnly)
	response, _, err := q.debtService.FindDebts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDebts")
		return err
	}
	return writeExport(c, ctxt, name, title, debtExportColumns(party), response)
}

func (q *debtController) show(c fiber.Ctx, ctxt, kind string) error {
	ctx := c.Context()
	debtID, err := sanitizers.ParamID(c, "id", debtNames[kind])
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.debtService.FindDebt(ctx, currentBranchID(c), debtID, kind)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDebt")
		return debtError(err, kind)
	}
	return helper.OK(c, response)
}

func (q *debtController) pay(c fiber.Ctx, ctxt, kind string) error {
	ctx := c.Context()
	debtID, err := sanitizers.ParamID(c, "id", debtNames[kind])
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateDebtPayment(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateDebtPayment")
		return err
	}
	var response *models.DebtPaymentResponse
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.debtService.PayDebt(ctx, tx, &models.DebtPaymentCreate{
			DebtPaymentRequest: request,
			BranchID:           currentBranchID(c),
			DebtID:             debtID,
			Kind:               kind,
			At:                 time.Now(),
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPayDebt")
		}
		return debtError(err, kind)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1Payables godoc
//
//	@Summary		List payables
//	@Description	What the branch owes its suppliers, from credit purchases; unpaid first, by due date.
//	@Tags			Debts
//	@Param			q			query		string	false	"search in reference number, supplier name"
//	@Param			supplier_id	query		string	false	"only this supplier (UUID)"
//	@Param			status		query		string	false	"only rows with this status"	Enums(unpaid, paid)
//	@Param			overdue		query		bool	false	"only unpaid rows past their due date"
//	@Param			sort		query		string	false	"sort field"		Enums(reference_number, supplier_name, transaction_date, due_date, total_amount, paid_amount, remaining_amount, status)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Debt}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/payables [get]
func (q *debtController) V1Payables(c fiber.Ctx) error {
	return q.index(c, "DebtController-V1Payables", models.DebtKindPayable)
}

// V1PayablesExport godoc
//
//	@Summary		Export payables
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Debts
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q			query		string	false	"search in reference number, supplier name"
//	@Param			supplier_id	query		string	false	"only this supplier (UUID)"
//	@Param			status		query		string	false	"only rows with this status"	Enums(unpaid, paid)
//	@Param			overdue		query		bool	false	"only unpaid rows past their due date"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/payables/export [get]
func (q *debtController) V1PayablesExport(c fiber.Ctx) error {
	return q.export(c, "DebtController-V1PayablesExport", models.DebtKindPayable, "supplier", "payables", "Payables")
}

// V1Payable godoc
//
//	@Summary		Show a payable
//	@Description	A payable with the payments recorded against it.
//	@Tags			Debts
//	@Param			id	path		string	true	"payable id (UUID)"
//	@Success		200	{object}	helper.Response{data=models.Debt}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/payables/{id} [get]
func (q *debtController) V1Payable(c fiber.Ctx) error {
	return q.show(c, "DebtController-V1Payable", models.DebtKindPayable)
}

// V1PayPayable godoc
//
//	@Summary		Record a payment of a payable
//	@Description	The amount may not exceed what remains. A payable paid in full completes the purchase it came from.
//	@Tags			Debts
//	@Param			id		path		string						true	"payable id (UUID)"
//	@Param			request	body		models.DebtPaymentRequest	true	"payment"
//	@Success		200		{object}	helper.Response{data=models.DebtPaymentResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/payables/{id}/payments [post]
func (q *debtController) V1PayPayable(c fiber.Ctx) error {
	return q.pay(c, "DebtController-V1PayPayable", models.DebtKindPayable)
}

// V1Receivables godoc
//
//	@Summary		List receivables
//	@Description	What the branch is owed by its customers, from credit sales; unpaid first, by due date.
//	@Tags			Debts
//	@Param			q			query		string	false	"search in reference number, customer name"
//	@Param			customer_id	query		string	false	"only this customer (UUID)"
//	@Param			status		query		string	false	"only rows with this status"	Enums(unpaid, paid)
//	@Param			overdue		query		bool	false	"only unpaid rows past their due date"
//	@Param			sort		query		string	false	"sort field"		Enums(reference_number, customer_name, transaction_date, due_date, total_amount, paid_amount, remaining_amount, status)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Debt}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/receivables [get]
func (q *debtController) V1Receivables(c fiber.Ctx) error {
	return q.index(c, "DebtController-V1Receivables", models.DebtKindReceivable)
}

// V1ReceivablesExport godoc
//
//	@Summary		Export receivables
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Debts
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q			query		string	false	"search in reference number, customer name"
//	@Param			customer_id	query		string	false	"only this customer (UUID)"
//	@Param			status		query		string	false	"only rows with this status"	Enums(unpaid, paid)
//	@Param			overdue		query		bool	false	"only unpaid rows past their due date"
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/receivables/export [get]
func (q *debtController) V1ReceivablesExport(c fiber.Ctx) error {
	return q.export(c, "DebtController-V1ReceivablesExport", models.DebtKindReceivable, "customer", "receivables", "Receivables")
}

// V1Receivable godoc
//
//	@Summary		Show a receivable
//	@Description	A receivable with the payments recorded against it.
//	@Tags			Debts
//	@Param			id	path		string	true	"receivable id (UUID)"
//	@Success		200	{object}	helper.Response{data=models.Debt}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/receivables/{id} [get]
func (q *debtController) V1Receivable(c fiber.Ctx) error {
	return q.show(c, "DebtController-V1Receivable", models.DebtKindReceivable)
}

// V1PayReceivable godoc
//
//	@Summary		Record a payment on a receivable
//	@Description	The amount may not exceed what remains. A receivable paid in full completes the sale it came from.
//	@Tags			Debts
//	@Param			id		path		string						true	"receivable id (UUID)"
//	@Param			request	body		models.DebtPaymentRequest	true	"payment"
//	@Success		200		{object}	helper.Response{data=models.DebtPaymentResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/receivables/{id}/payments [post]
func (q *debtController) V1PayReceivable(c fiber.Ctx) error {
	return q.pay(c, "DebtController-V1PayReceivable", models.DebtKindReceivable)
}
