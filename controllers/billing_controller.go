package controllers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/config"
	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type billingController struct {
	config         config.Config
	authService    *middlewares.Service
	billingService services.BillingService
	ppobService    services.PPOBService
	uploader       *Uploader
}

func NewBillingController(
	cfg config.Config,
	authService *middlewares.Service,
	billingService services.BillingService,
	ppobService services.PPOBService,
	uploader *Uploader,
) *billingController {
	return &billingController{
		config:         cfg,
		authService:    authService,
		billingService: billingService,
		ppobService:    ppobService,
		uploader:       uploader,
	}
}

// Mount registers the payment webhook (public, signed) and the routes of /billing and /plugins.
func (q *billingController) Mount(r fiber.Router) {
	r.Post("/webhooks/payments", q.V1PaymentWebhook)
	private := r.Group("", q.authService.Middleware())
	billing := private.Group("/billing")
	billing.Get("/invoices", q.V1Invoices)
	billing.Get("/invoices/export", q.V1ExportInvoices)
	billing.Post("/subscriptions", q.V1Subscribe)
	billing.Post("/coin-top-ups", q.V1TopUpCoins)
	billing.Post("/vouchers/redeem", q.V1RedeemVoucher)
	billing.Post("/invoices/:id<guid>/payment-proof", q.V1PaymentProof)
	billing.Post("/invoices/:id<guid>/cancel", q.V1CancelInvoice)
	billing.Get("/invoices/:id<guid>/pdf", q.V1InvoicePDF)
	private.Get("/plugins", q.V1Plugins)
	private.Post("/plugins/:code/subscribe", q.V1SubscribePlugin)
	private.Post("/plugins/:code/trial", q.V1TrialPlugin)
}

// billingError turns the billing errors into responses; anything else goes to the error handler as it is.
func billingError(err error) error {
	if errors.Is(err, models.ErrInvoiceNotFound) {
		return helper.NotFound("Invoice")
	}
	return writeError(err)
}

// order bills the store of the signed-in user and answers the invoice.
func (q *billingController) order(c fiber.Ctx, ctxt string, request *models.InvoiceOrder) error {
	ctx := c.Context()
	request.StoreID = currentStoreID(c)
	request.Lang = helper.Lang(c)
	request.At = time.Now()
	var response *models.Invoice
	if err := inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.billingService.CreateInvoice(ctx, tx, request); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateInvoice")
		}
		return billingError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1PaymentWebhook godoc
//
//	@Summary		Receive a payment callback
//	@Description	Called by the payment provider when an invoice or a PPOB deposit is paid. The body is signed: X-Signature is the hex HMAC-SHA256 of the raw body with the webhook secret.
//	@Tags			Billing
//	@Param			X-Signature	header		string							true	"hex HMAC-SHA256 of the body"
//	@Param			request		body		models.PaymentWebhookRequest	true	"payment"
//	@Success		200			{object}	helper.Response{data=models.HandledResponse}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/webhooks/payments [post]
func (q *billingController) V1PaymentWebhook(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BillingController-V1PaymentWebhook"
	mac := hmac.New(sha256.New, []byte(q.config.JWTSecret))
	_, _ = mac.Write(c.Body())
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(strings.ToLower(c.Get("X-Signature")))) {
		err := helper.Unauthorized("invalid_signature", "Invalid signature.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSignature")
		return err
	}
	request, err := sanitizers.Decode[models.PaymentWebhookRequest](c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return err
	}
	response := models.HandledResponse{}
	if request.Status != models.InvoiceStatusPaid {
		return helper.OK(c, response)
	}
	now := time.Now()
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response.Handled, err = q.billingService.MarkInvoicePaid(ctx, tx, request.Reference, now); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarkInvoicePaid")
			return err
		}
		if response.Handled {
			return nil
		}
		// not an invoice: the reference may be a PPOB deposit
		if response.Handled, err = q.ppobService.MarkDepositPaid(ctx, tx, request.Reference, now); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarkDepositPaid")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1Invoices godoc
//
//	@Summary		List invoices
//	@Description	The invoices of the store in a date range (today by default). The summary carries the plan and the premium coin balance.
//	@Tags			Billing
//	@Param			start_date	query		string	false	"from this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"to this date (YYYY-MM-DD)"
//	@Param			status		query		string	false	"only this status"	Enums(pending, paid, cancelled, expired)
//	@Param			q			query		string	false	"search in invoice_number, description"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Invoice,summary=models.BillingSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/billing/invoices [get]
func (q *billingController) V1Invoices(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BillingController-V1Invoices"
	request, err := sanitizers.FindInvoices(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindInvoices")
		return err
	}
	request.StoreID = currentStoreID(c)
	response, total, err := q.billingService.FindInvoices(ctx, request, time.Now())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindInvoices")
		return err
	}
	summary, err := q.billingService.FindBillingSummary(ctx, request.StoreID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBillingSummary")
		return err
	}
	return helper.ListWith(c, response, page(request.Limit, request.Page, total), helper.Extras{Summary: summary})
}

// invoiceExportColumns are the columns of the invoice export.
var invoiceExportColumns = []export.Column{
	{Key: "created_at", Label: "Date", Format: "datetime"},
	{Key: "invoice_number", Label: "Invoice"},
	{Key: "description", Label: "For"},
	{Key: "payment_method", Label: "Method"},
	{Key: "amount", Label: "Amount", Format: "money"},
	{Key: "status", Label: "Status"},
}

// V1ExportInvoices godoc
//
//	@Summary	Export invoices
//	@Tags		Billing
//	@Produce	application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param		format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param		start_date	query		string	false	"from this date (YYYY-MM-DD)"
//	@Param		end_date	query		string	false	"to this date (YYYY-MM-DD)"
//	@Param		status		query		string	false	"only this status"	Enums(pending, paid, cancelled, expired)
//	@Param		q			query		string	false	"search in invoice_number, description"
//	@Success	200			{file}		binary
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/billing/invoices/export [get]
func (q *billingController) V1ExportInvoices(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BillingController-V1ExportInvoices"
	request, err := sanitizers.FindInvoices(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindInvoices")
		return err
	}
	request.StoreID, request.Limit = currentStoreID(c), 0
	response, _, err := q.billingService.FindInvoices(ctx, request, time.Now())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindInvoices")
		return err
	}
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	return export.Write(c, "invoices", "Invoices", invoiceExportColumns, rows)
}

// V1Subscribe godoc
//
//	@Summary		Buy a plan
//	@Description	Creates an invoice for a Pro plan. Paid with premium coins it is settled at once; any other method returns how to pay and the plan starts when the payment arrives.
//	@Tags			Billing
//	@Param			request	body		models.SubscriptionRequest	true	"plan and payment"
//	@Success		200		{object}	helper.Response{data=models.Invoice}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/billing/subscriptions [post]
func (q *billingController) V1Subscribe(c fiber.Ctx) error {
	ctxt := "BillingController-V1Subscribe"
	request, err := sanitizers.Subscription(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrSubscription")
		return err
	}
	plan := models.Plans[request.Plan]
	return q.order(c, ctxt, &models.InvoiceOrder{
		Kind:          models.InvoiceKindSubscription,
		Description:   helper.T(c, "Pro plan, {n} month(s)", "n", plan.Months),
		PaymentMethod: request.PaymentMethod,
		Amount:        plan.Price,
		PromoCode:     request.PromoCode,
		Meta:          models.InvoiceMeta{Plan: plan.Code, Months: plan.Months},
	})
}

// V1TopUpCoins godoc
//
//	@Summary		Top up premium coins
//	@Description	Creates an invoice for premium coins (at least Rp 10,000); the coins arrive when the payment does.
//	@Tags			Billing
//	@Param			request	body		models.CoinTopUpRequest	true	"amount and payment"
//	@Success		200		{object}	helper.Response{data=models.Invoice}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/billing/coin-top-ups [post]
func (q *billingController) V1TopUpCoins(c fiber.Ctx) error {
	ctxt := "BillingController-V1TopUpCoins"
	request, err := sanitizers.CoinTopUp(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrCoinTopUp")
		return err
	}
	return q.order(c, ctxt, &models.InvoiceOrder{
		Kind:          models.InvoiceKindCoinTopUp,
		Description:   helper.T(c, "Premium coin top-up"),
		PaymentMethod: request.PaymentMethod,
		Amount:        request.Amount,
		PromoCode:     request.PromoCode,
	})
}

// V1RedeemVoucher godoc
//
//	@Summary		Redeem a voucher
//	@Description	Gives the reward of a voucher (premium coins, plan days or a plugin) to the store; a store can use a voucher once.
//	@Tags			Billing
//	@Param			request	body		models.VoucherRequest	true	"voucher code"
//	@Success		200		{object}	helper.Response{data=models.VoucherRedemption}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/billing/vouchers/redeem [post]
func (q *billingController) V1RedeemVoucher(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BillingController-V1RedeemVoucher"
	request, err := sanitizers.Decode[models.VoucherRequest](c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return err
	}
	var response *models.VoucherRedemption
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.billingService.RedeemVoucher(ctx, tx, currentStoreID(c), request.Code, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRedeemVoucher")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1PaymentProof godoc
//
//	@Summary		Upload a payment proof
//	@Description	Attaches a picture or PDF of a manual transfer to a pending invoice.
//	@Tags			Billing
//	@Accept			multipart/form-data
//	@Param			id					path		string	true	"invoice id (UUID)"
//	@Param			file				formData	file	true	"proof of the transfer"
//	@Param			sender_account_name	formData	string	false	"name on the sending account"
//	@Success		200					{object}	helper.Response{data=models.Invoice}
//	@Failure		401					{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404					{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409					{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422					{object}	helper.Response{data=helper.ErrorData}
//	@Router			/billing/invoices/{id}/payment-proof [post]
func (q *billingController) V1PaymentProof(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BillingController-V1PaymentProof"
	invoiceID, err := sanitizers.ParamID(c, "id", "Invoice")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	invoice, err := q.billingService.FindInvoice(ctx, nil, currentStoreID(c), invoiceID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindInvoice")
		return billingError(err)
	}
	if invoice.Status != models.InvoiceStatusPending {
		err = helper.Conflict("Only a pending invoice accepts a payment proof.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNotPending")
		return err
	}
	upload, err := q.uploader.Save(c, "file", "payment_proof")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSave")
		return err
	}
	var response *models.Invoice
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.billingService.AttachPaymentProof(ctx, tx, invoice, upload.URL, c.FormValue("sender_account_name"), time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrAttachPaymentProof")
		}
		return billingError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1CancelInvoice godoc
//
//	@Summary	Cancel a pending invoice
//	@Tags		Billing
//	@Param		id	path		string	true	"invoice id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Invoice}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/billing/invoices/{id}/cancel [post]
func (q *billingController) V1CancelInvoice(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BillingController-V1CancelInvoice"
	invoiceID, err := sanitizers.ParamID(c, "id", "Invoice")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	var response *models.Invoice
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.billingService.CancelInvoice(ctx, tx, currentStoreID(c), invoiceID, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCancelInvoice")
		}
		return billingError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// formatTime prints a time for documents, in the time zone of the application.
func formatTime(value time.Time) string {
	return value.In(helper.Location).Format("02 Jan 2006 15:04")
}

// V1InvoicePDF godoc
//
//	@Summary	Download an invoice
//	@Tags		Billing
//	@Produce	application/pdf
//	@Param		id	path		string	true	"invoice id (UUID)"
//	@Success	200	{file}		binary
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/billing/invoices/{id}/pdf [get]
func (q *billingController) V1InvoicePDF(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BillingController-V1InvoicePDF"
	invoiceID, err := sanitizers.ParamID(c, "id", "Invoice")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	invoice, err := q.billingService.FindInvoice(ctx, nil, currentStoreID(c), invoiceID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindInvoice")
		return billingError(err)
	}
	storeName, err := q.billingService.FindStoreName(ctx, currentStoreID(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStoreName")
		return err
	}
	t := helper.Lang(c).T
	document := export.NewPDF(595, 842)
	document.Page()
	document.Text(40, 790, 20, true, t("INVOICE"))
	document.Text(40, 766, 10, false, invoice.InvoiceNumber)
	document.Text(360, 790, 10, true, "Lestari")
	document.Text(360, 776, 9, false, t("Billed to")+": "+storeName)
	document.Text(360, 762, 9, false, t("Date")+": "+formatTime(invoice.CreatedAt))
	document.Text(360, 748, 9, false, t("Status")+": "+strings.ToUpper(t(invoice.Status)))
	y := 700.0
	document.Rect(40, y-4, 515, 16, 0.92)
	document.Text(46, y, 9, true, t("Description"))
	document.TextRight(549, y, 9, true, t("Amount (Rp)"))
	y -= 22
	document.Text(46, y, 10, false, invoice.Description)
	document.TextRight(549, y, 10, false, export.Thousands(invoice.Amount))
	if invoice.AdminFee > 0 {
		y -= 16
		document.Text(46, y, 10, false, t("Admin fee"))
		document.TextRight(549, y, 10, false, export.Thousands(invoice.AdminFee))
	}
	y -= 12
	document.Line(40, y, 555)
	y -= 18
	document.Text(380, y, 11, true, t("Total"))
	document.TextRight(549, y, 11, true, export.Thousands(invoice.Amount+invoice.AdminFee))
	if invoice.PaidAt != nil {
		document.Text(40, 120, 8, false, t("Paid with {method} on {date}.", "method", i18n.Tr(invoice.PaymentMethod), "date", formatTime(*invoice.PaidAt)))
	}
	return export.Send(c, "invoice-"+strings.ReplaceAll(invoice.InvoiceNumber, "/", "-")+".pdf", "application/pdf", document.Bytes())
}

// V1Plugins godoc
//
//	@Summary		List plugins
//	@Description	The plugin catalog with prices and the state of the store's subscription to each.
//	@Tags			Plugins
//	@Success		200	{object}	helper.Response{data=[]models.Plugin}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/plugins [get]
func (q *billingController) V1Plugins(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BillingController-V1Plugins"
	response, err := q.billingService.FindPlugins(ctx, currentStoreID(c), helper.Lang(c), time.Now())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPlugins")
		return err
	}
	return helper.OK(c, response)
}

// plugin finds the plugin of the path in the catalog.
func (q *billingController) plugin(c fiber.Ctx, ctxt string) (*models.PluginInfo, error) {
	response := models.PluginByCode(c.Params("code"))
	if response == nil {
		err := helper.NotFound("Plugin")
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrPluginByCode")
		return nil, err
	}
	return response, nil
}

// V1SubscribePlugin godoc
//
//	@Summary		Subscribe to a plugin
//	@Description	Creates an invoice for a month or a year (ten months' price) of a plugin.
//	@Tags			Plugins
//	@Param			code	path		string								true	"plugin code"	Enums(business, ingredients, desktop, server, food_menu, attendance)
//	@Param			request	body		models.PluginSubscriptionRequest	true	"period and payment"
//	@Success		200		{object}	helper.Response{data=models.Invoice}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/plugins/{code}/subscribe [post]
func (q *billingController) V1SubscribePlugin(c fiber.Ctx) error {
	ctxt := "BillingController-V1SubscribePlugin"
	plugin, err := q.plugin(c, ctxt)
	if err != nil {
		return err
	}
	request, months, err := sanitizers.PluginSubscription(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrPluginSubscription")
		return err
	}
	price := plugin.Monthly
	if months == 12 {
		price = plugin.Monthly * 10
	}
	return q.order(c, ctxt, &models.InvoiceOrder{
		Kind:          models.InvoiceKindPlugin,
		Description:   helper.T(c, "{plugin} plugin, {n} month(s)", "plugin", i18n.Tr(plugin.Name), "n", months),
		PaymentMethod: request.PaymentMethod,
		Amount:        price,
		Meta:          models.InvoiceMeta{Code: plugin.Code, Months: months},
	})
}

// V1TrialPlugin godoc
//
//	@Summary		Start a plugin trial
//	@Description	Seven free days of a plugin, once per plugin.
//	@Tags			Plugins
//	@Param			code	path		string	true	"plugin code"	Enums(business, ingredients, desktop, server, food_menu, attendance)
//	@Success		200		{object}	helper.Response{data=models.StorePlugin}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/plugins/{code}/trial [post]
func (q *billingController) V1TrialPlugin(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BillingController-V1TrialPlugin"
	plugin, err := q.plugin(c, ctxt)
	if err != nil {
		return err
	}
	var response *models.StorePlugin
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.billingService.StartPluginTrial(ctx, tx, currentStoreID(c), plugin.Code, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrStartPluginTrial")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}
