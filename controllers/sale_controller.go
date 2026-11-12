package controllers

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"
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

type saleController struct {
	authService     *middlewares.Service
	saleService     services.SaleService
	shiftService    services.ShiftService
	customerService services.CustomerService
	settingService  services.SettingService
	activityService services.ActivityService
}

func NewSaleController(
	authService *middlewares.Service,
	saleService services.SaleService,
	shiftService services.ShiftService,
	customerService services.CustomerService,
	settingService services.SettingService,
	activityService services.ActivityService,
) *saleController {
	return &saleController{
		authService:     authService,
		saleService:     saleService,
		shiftService:    shiftService,
		customerService: customerService,
		settingService:  settingService,
		activityService: activityService,
	}
}

// Mount registers the sale routes under /sales and the shift routes under /shifts.
func (q *saleController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("/sales", q.V1Index)
	private.Post("/sales", q.V1Create)
	private.Get("/sales/export", q.V1Export)
	private.Get("/sales/:id<guid>", q.V1Show)
	private.Delete("/sales/:id<guid>", q.V1Delete)
	private.Get("/sales/:id<guid>/receipt", q.V1Receipt)
	private.Get("/sales/:id<guid>/delivery-note", q.V1DeliveryNote)
	private.Post("/shifts", q.V1OpenShift)
	private.Post("/shifts/:id<guid>/close", q.V1CloseShift)
}

// saleError turns the sale errors into responses; anything else goes to the error handler as it is.
func saleError(err error) error {
	if errors.Is(err, models.ErrSaleNotFound) {
		return helper.NotFound("Sale")
	}
	return writeError(err)
}

// writeExport sends models as an export file with these columns.
func writeExport(c fiber.Ctx, ctxt, name, title string, columns []export.Column, response any) error {
	ctx := c.Context()
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	if err = export.Write(c, name, title, columns, rows); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrWrite")
	}
	return err
}

// exportLimit caps the rows of a list export.
const exportLimit = 50000

// V1Index godoc
//
//	@Summary		List sales
//	@Description	Sales of the active branch, newest first.
//	@Tags			Sales
//	@Param			q				query		string	false	"search in receipt number, customer name, label"
//	@Param			cashier_id		query		string	false	"only this cashier (UUID)"
//	@Param			customer_id		query		string	false	"only this customer (UUID)"
//	@Param			payment_method	query		string	false	"only sales paid with this method"	Enums(cash, debit, credit_card, qris, e_wallet, bank_transfer, other)
//	@Param			payment_type	query		string	false	"only cash or only credit sales"	Enums(cash, credit)
//	@Param			status			query		string	false	"only sales with this status"		Enums(completed, unpaid)
//	@Param			start_date		query		string	false	"sales on or after this date (YYYY-MM-DD)"
//	@Param			end_date		query		string	false	"sales on or before this date (YYYY-MM-DD)"
//	@Param			sort			query		string	false	"sort field"		Enums(created_at, receipt_number, cashier_name, customer_name, payment_method, payment_type, subtotal, discount_total, tax_total, grand_total, paid_amount, profit, status, due_date)
//	@Param			order			query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page			query		int		false	"page, from 1"		default(1)
//	@Param			per_page		query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.Sale}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/sales [get]
func (q *saleController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SaleController-V1Index"
	request := sanitizers.FindSales(c)
	request.BranchID = currentBranchID(c)
	response, total, err := q.saleService.FindSales(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSales")
		return err
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

var saleExportColumns = []export.Column{
	{Key: "id", Label: "id"},
	{Key: "receipt_number", Label: "receipt_number"},
	{Key: "cashier_name", Label: "cashier_name"},
	{Key: "customer_name", Label: "customer_name"},
	{Key: "payment_method", Label: "payment_method"},
	{Key: "payment_type", Label: "payment_type"},
	{Key: "subtotal", Label: "subtotal", Format: "number"},
	{Key: "discount_total", Label: "discount_total", Format: "number"},
	{Key: "tax_name", Label: "tax_name"},
	{Key: "tax_total", Label: "tax_total", Format: "number"},
	{Key: "service_charge", Label: "service_charge", Format: "number"},
	{Key: "fee_total", Label: "fee_total", Format: "number"},
	{Key: "rounding", Label: "rounding", Format: "number"},
	{Key: "grand_total", Label: "grand_total", Format: "number"},
	{Key: "paid_amount", Label: "paid_amount", Format: "number"},
	{Key: "change_amount", Label: "change_amount", Format: "number"},
	{Key: "profit", Label: "profit", Format: "number"},
	{Key: "points_earned", Label: "points_earned", Format: "number"},
	{Key: "points_redeemed", Label: "points_redeemed", Format: "number"},
	{Key: "label", Label: "label"},
	{Key: "customer_type", Label: "customer_type"},
	{Key: "note", Label: "note"},
	{Key: "status", Label: "status"},
	{Key: "due_date", Label: "due_date", Format: "date"},
	{Key: "created_at", Label: "created_at", Format: "datetime"},
}

// V1Export godoc
//
//	@Summary		Export sales
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Sales
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q				query		string	false	"search in receipt number, customer name, label"
//	@Param			cashier_id		query		string	false	"only this cashier (UUID)"
//	@Param			customer_id		query		string	false	"only this customer (UUID)"
//	@Param			payment_method	query		string	false	"only sales paid with this method"
//	@Param			payment_type	query		string	false	"only cash or only credit sales"	Enums(cash, credit)
//	@Param			status			query		string	false	"only sales with this status"		Enums(completed, unpaid)
//	@Param			start_date		query		string	false	"sales on or after this date (YYYY-MM-DD)"
//	@Param			end_date		query		string	false	"sales on or before this date (YYYY-MM-DD)"
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/sales/export [get]
func (q *saleController) V1Export(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SaleController-V1Export"
	request := sanitizers.FindSales(c)
	request.BranchID, request.Limit, request.Page = currentBranchID(c), exportLimit, 1
	response, _, err := q.saleService.FindSales(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSales")
		return err
	}
	return writeExport(c, ctxt, "sales", "Sales", saleExportColumns, response)
}

// V1Create godoc
//
//	@Summary		Record a sale
//	@Description	Used by cashier apps. The price of every line follows the product (variant, wholesale tier, customer-type price) unless unit_price is sent; product and customer-type discounts, running promotions, redeemed points, the service charge, the tax (tax_id or the default tax of the branch), fees and rounding are applied, the stock is taken (ingredients for a recipe), points are earned and a credit sale that is not paid in full becomes a receivable.
//	@Tags			Sales
//	@Param			request	body		models.SaleRequest	true	"sale"
//	@Success		201		{object}	helper.Response{data=models.Sale}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/sales [post]
func (q *saleController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SaleController-V1Create"
	request, at, err := sanitizers.ValidateSale(c, time.Now())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateSale")
		return err
	}
	var response *models.Sale
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.saleService.CreateSale(ctx, tx, &models.SaleCreate{
			SaleRequest:    request,
			BranchID:       currentBranchID(c),
			Actor:          currentActor(c),
			BusinessPlugin: helper.P(c).HasPlugin(models.PluginBusiness),
			At:             at,
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateSale")
		}
		return saleError(err)
	}); err != nil {
		return err
	}
	return helper.Created(c, response)
}

// findSale reads the sale of the path with its items and fees.
func (q *saleController) findSale(c fiber.Ctx, ctxt string) (*models.Sale, error) {
	ctx := c.Context()
	saleID, err := sanitizers.ParamID(c, "id", "Sale")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return nil, err
	}
	response, err := q.saleService.FindSale(ctx, nil, currentBranchID(c), saleID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSale")
		return nil, saleError(err)
	}
	return response, nil
}

// V1Show godoc
//
//	@Summary	Show a sale
//	@Tags		Sales
//	@Param		id	path		string	true	"sale id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Sale}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/sales/{id} [get]
func (q *saleController) V1Show(c fiber.Ctx) error {
	response, err := q.findSale(c, "SaleController-V1Show")
	if err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a sale
//	@Description	Voids the sale: the stock and the points go back and the deletion is written to the activity log. Cashiers cannot delete sales.
//	@Tags			Sales
//	@Param			id	path	string	true	"sale id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/sales/{id} [delete]
func (q *saleController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SaleController-V1Delete"
	saleID, err := sanitizers.ParamID(c, "id", "Sale")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	if helper.P(c).Role == models.RoleCashier {
		err = helper.Forbidden("Cashiers cannot delete sales.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrForbidden")
		return err
	}
	if err = inTx(c, ctxt, func(tx pgx.Tx) error {
		_, err := q.saleService.DeleteSale(ctx, tx, currentBranchID(c), saleID, currentActor(c), time.Now())
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteSale")
		}
		return saleError(err)
	}); err != nil {
		return err
	}
	record(c, q.activityService, "sale_deleted", i18n.Tr("Deleted sale"))
	return helper.NoContent(c)
}

// storeHeader is the name, address and phone printed on the documents of a branch.
func storeHeader(c fiber.Ctx, ctxt string, settingService services.SettingService) (name, address, phone string, err error) {
	var store models.StoreSetting
	if err = settingService.FindSetting(c.Context(), nil, currentBranchID(c), models.SettingStore, &store); err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrFindSetting")
		return
	}
	return store.Name, textOr(store.Address, ""), textOr(store.Phone, ""), nil
}

// textOr is the text of an optional value, or fallback when there is none.
func textOr(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

// V1Receipt godoc
//
//	@Summary		Print the receipt of a sale
//	@Description	A PDF for an 80 mm receipt printer, laid out by the receipt settings of the branch.
//	@Tags			Sales
//	@Produce		application/pdf
//	@Param			id	path		string	true	"sale id (UUID)"
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/sales/{id}/receipt [get]
func (q *saleController) V1Receipt(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SaleController-V1Receipt"
	sale, err := q.findSale(c, ctxt)
	if err != nil {
		return err
	}
	var receipt models.ReceiptSetting
	if err = q.settingService.FindSetting(ctx, nil, currentBranchID(c), models.SettingReceipt, &receipt); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSetting")
		return err
	}
	name, address, phone, err := storeHeader(c, ctxt, q.settingService)
	if err != nil {
		return err
	}
	tr := helper.Lang(c).T
	height := 260.0 + float64(len(sale.Items))*24
	document := export.NewPDF(226, height)
	document.Page()
	y := height - 24
	center := func(size float64, bold bool, text string) {
		document.Text((226-export.TextWidth(text, size))/2, y, size, bold, text)
		y -= size + 4
	}
	center(11, true, name)
	for _, text := range []string{address, phone, textOr(receipt.HeaderNote, "")} {
		if text != "" {
			center(7, false, export.Fit(text, 210, 7))
		}
	}
	y -= 4
	document.Line(8, y+6, 218)
	if receipt.ShowReceiptNumber {
		document.Text(8, y-4, 7, false, tr("No")+": "+sale.ReceiptNumber)
		y -= 10
	}
	document.Text(8, y-4, 7, false, formatTime(sale.CreatedAt)+"  ·  "+sale.CashierName)
	y -= 12
	if sale.Customer != nil {
		document.Text(8, y-4, 7, false, tr("Customer")+": "+sale.Customer.Name)
		y -= 10
	}
	document.Line(8, y, 218)
	y -= 10
	totalQuantity := 0.0
	for _, item := range sale.Items {
		document.Text(8, y, 7, false, export.Fit(item.ProductName, 200, 7))
		y -= 9
		totalQuantity += item.Quantity
		document.Text(14, y, 7, false, export.Thousands(item.Quantity)+" x "+export.Thousands(item.UnitPrice))
		document.TextRight(218, y, 7, false, export.Thousands(item.Subtotal))
		y -= 11
	}
	document.Line(8, y+4, 218)
	y -= 6
	row := func(label string, value float64, bold bool) {
		if value == 0 && !bold {
			return
		}
		document.Text(8, y, 7.5, bold, label)
		document.TextRight(218, y, 7.5, bold, export.Thousands(value))
		y -= 11
	}
	if receipt.ShowTotalQuantity {
		document.Text(8, y, 7.5, false, tr("Total qty")+": "+export.Thousands(totalQuantity))
		y -= 11
	}
	row(tr("Subtotal"), sale.Subtotal, false)
	row(tr("Discount"), -sale.DiscountTotal, false)
	row(tr("Service charge"), sale.ServiceCharge, false)
	row(textOr(sale.TaxName, tr("Tax")), sale.TaxTotal, false)
	row(tr("Fees"), sale.FeeTotal, false)
	row(tr("Rounding"), sale.Rounding, false)
	row(tr("TOTAL"), sale.GrandTotal, true)
	row(tr("Paid ({method})", "method", i18n.Tr(sale.PaymentMethod)), sale.PaidAmount+sale.ChangeAmount, false)
	row(tr("Change"), sale.ChangeAmount, false)
	if receipt.ShowPoints && sale.PointsEarned > 0 {
		document.Text(8, y, 7, false, tr("Points earned")+": "+export.Thousands(float64(sale.PointsEarned)))
		y -= 10
	}
	y -= 6
	center(7, false, textOr(receipt.FooterNote, tr("Thank you!")))
	if receipt.ShowSignature {
		y -= 26
		document.Line(130, y, 218)
		document.Text(150, y-9, 7, false, tr("Signature"))
	}
	if err = export.Send(c, "receipt-"+strings.ReplaceAll(sale.ReceiptNumber, "/", "-")+".pdf", "application/pdf", document.Bytes()); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSend")
	}
	return err
}

// V1DeliveryNote godoc
//
//	@Summary		Print the delivery note of a sale
//	@Description	An A4 PDF listing the items to deliver, with the customer's address and signature lines.
//	@Tags			Sales
//	@Produce		application/pdf
//	@Param			id	path		string	true	"sale id (UUID)"
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/sales/{id}/delivery-note [get]
func (q *saleController) V1DeliveryNote(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SaleController-V1DeliveryNote"
	sale, err := q.findSale(c, ctxt)
	if err != nil {
		return err
	}
	name, address, phone, err := storeHeader(c, ctxt, q.settingService)
	if err != nil {
		return err
	}
	tr := helper.Lang(c).T
	document := export.NewPDF(595, 842)
	document.Page()
	document.Text(40, 790, 16, true, tr("DELIVERY NOTE"))
	document.Text(40, 770, 10, true, name)
	document.Text(40, 756, 9, false, address)
	document.Text(40, 744, 9, false, phone)
	document.Text(360, 790, 9, false, tr("No")+": "+sale.ReceiptNumber)
	document.Text(360, 776, 9, false, tr("Date")+": "+formatTime(sale.CreatedAt))
	document.Text(40, 712, 9, true, tr("Deliver to")+":")
	if sale.CustomerID != nil {
		customer, err := q.customerService.FindCustomer(ctx, nil, models.NewCustomerFilter().WithBranchIDs(currentBranchID(c)).WithCustomerIDs(*sale.CustomerID))
		if err != nil && !errors.Is(err, models.ErrCustomerNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomer")
			return err
		}
		if customer != nil {
			document.Text(40, 698, 9, false, customer.Name)
			document.Text(40, 686, 9, false, textOr(customer.Address, ""))
			document.Text(40, 674, 9, false, textOr(customer.Phone, ""))
		}
	}
	y := 640.0
	document.Rect(40, y-4, 515, 16, 0.92)
	document.Text(46, y, 9, true, "#")
	document.Text(70, y, 9, true, tr("Item"))
	document.Text(420, y, 9, true, tr("Qty"))
	document.Text(470, y, 9, true, tr("Checked"))
	y -= 20
	for i, item := range sale.Items {
		if y < 120 {
			document.Page()
			y = 800
		}
		document.Text(46, y, 9, false, strconv.Itoa(i+1))
		document.Text(70, y, 9, false, export.Fit(item.ProductName, 340, 9))
		document.Text(420, y, 9, false, export.Thousands(item.Quantity))
		document.Rect(478, y-2, 10, 10, 0.85)
		y -= 16
	}
	y -= 40
	for i, label := range []string{tr("Sender"), tr("Courier"), tr("Receiver")} {
		x := 40 + float64(i)*180
		document.Line(x, y, x+140)
		document.Text(x, y-12, 9, false, label)
	}
	if err = export.Send(c, "delivery-note.pdf", "application/pdf", document.Bytes()); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSend")
	}
	return err
}

// V1OpenShift godoc
//
//	@Summary		Open a cashier shift
//	@Description	Used by cashier apps. A cash drawer has one open shift at a time.
//	@Tags			Sales
//	@Param			request	body		models.ShiftOpenRequest	true	"cash drawer and opening cash"
//	@Success		201		{object}	helper.Response{data=models.Shift}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/shifts [post]
func (q *saleController) V1OpenShift(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SaleController-V1OpenShift"
	request, err := sanitizers.Decode[models.ShiftOpenRequest](c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return err
	}
	response, err := q.shiftService.OpenShift(ctx, currentBranchID(c), currentUserID(c), request, time.Now())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrOpenShift")
		return err
	}
	return helper.Created(c, response)
}

// V1CloseShift godoc
//
//	@Summary		Close a cashier shift
//	@Description	Used by cashier apps: records the counted cash and the cash put in and taken out, and answers the shift with its expected cash and its sales per payment method.
//	@Tags			Sales
//	@Param			id		path		string						true	"shift id (UUID)"
//	@Param			request	body		models.ShiftCloseRequest	true	"counted cash"
//	@Success		200		{object}	helper.Response{data=models.Shift}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/shifts/{id}/close [post]
func (q *saleController) V1CloseShift(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SaleController-V1CloseShift"
	shiftID, err := sanitizers.ParamID(c, "id", "Shift")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.Decode[models.ShiftCloseRequest](c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return err
	}
	response, err := q.shiftService.CloseShift(ctx, currentBranchID(c), shiftID, request, time.Now())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCloseShift")
		return shiftError(err)
	}
	return helper.OK(c, response)
}
