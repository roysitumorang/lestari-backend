package controllers

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type orderController struct {
	authService     *middlewares.Service
	orderService    services.OrderService
	settingService  services.SettingService
	activityService services.ActivityService
}

func NewOrderController(
	authService *middlewares.Service,
	orderService services.OrderService,
	settingService services.SettingService,
	activityService services.ActivityService,
) *orderController {
	return &orderController{
		authService:     authService,
		orderService:    orderService,
		settingService:  settingService,
		activityService: activityService,
	}
}

// Mount registers the online-order routes under /catalog/orders and the catalog settings under
// /settings/catalog.
func (q *orderController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("/catalog/orders", q.V1Index)
	private.Post("/catalog/orders", q.V1Create)
	private.Get("/catalog/orders/export", q.V1Export)
	private.Get("/catalog/orders/:id<guid>", q.V1Show)
	private.Patch("/catalog/orders/:id<guid>/status", q.V1Status)
	private.Get("/catalog/orders/:id<guid>/invoice", q.V1Invoice)
	private.Get("/settings/catalog", q.V1ShowSetting)
	private.Put("/settings/catalog", q.V1UpdateSetting)
}

// orderError turns the order errors into responses; anything else goes to the error handler as it is.
func orderError(err error) error {
	if errors.Is(err, models.ErrOrderNotFound) {
		return helper.NotFound("Order")
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List online orders
//	@Description	Orders of the online catalog of the active branch, newest first.
//	@Tags			Online catalog
//	@Param			q				query		string	false	"search in order number, customer name"
//	@Param			payment_method	query		string	false	"only orders paid this way"		Enums(bank_transfer, direct)
//	@Param			status			query		string	false	"only orders with this status"	Enums(unpaid, awaiting_confirmation, payment_rejected, processing, shipped, delivered, completed, returned, cancelled)
//	@Param			start_date		query		string	false	"orders on or after this date (YYYY-MM-DD)"
//	@Param			end_date		query		string	false	"orders on or before this date (YYYY-MM-DD)"
//	@Param			sort_by			query		string	false	"grand_total lists the biggest orders first"	Enums(grand_total)
//	@Param			sort			query		string	false	"sort field"									Enums(created_at, order_number, customer_name, payment_method, subtotal, shipping_fee, grand_total, status)
//	@Param			order			query		string	false	"sort direction"								Enums(asc, desc)	default(asc)
//	@Param			page			query		int		false	"page, from 1"									default(1)
//	@Param			per_page		query		int		false	"rows per page"									default(25)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.Order}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/catalog/orders [get]
func (q *orderController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "OrderController-V1Index"
	request := sanitizers.FindOrders(c)
	request.BranchID = currentBranchID(c)
	response, total, err := q.orderService.FindOrders(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindOrders")
		return err
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

var orderExportColumns = []export.Column{
	{Key: "id", Label: "id"},
	{Key: "order_number", Label: "order_number"},
	{Key: "customer_name", Label: "customer_name"},
	{Key: "customer_phone", Label: "customer_phone"},
	{Key: "shipping_address", Label: "shipping_address"},
	{Key: "shipping_method", Label: "shipping_method"},
	{Key: "courier", Label: "courier"},
	{Key: "tracking_number", Label: "tracking_number"},
	{Key: "payment_method", Label: "payment_method"},
	{Key: "payment_proof_url", Label: "payment_proof_url"},
	{Key: "subtotal", Label: "subtotal", Format: "number"},
	{Key: "shipping_fee", Label: "shipping_fee", Format: "number"},
	{Key: "grand_total", Label: "grand_total", Format: "number"},
	{Key: "customer_note", Label: "customer_note"},
	{Key: "status", Label: "status"},
	{Key: "status_reason", Label: "status_reason"},
	{Key: "created_at", Label: "created_at", Format: "datetime"},
}

// V1Export godoc
//
//	@Summary		Export online orders
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Online catalog
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q				query		string	false	"search in order number, customer name"
//	@Param			payment_method	query		string	false	"only orders paid this way"		Enums(bank_transfer, direct)
//	@Param			status			query		string	false	"only orders with this status"	Enums(unpaid, awaiting_confirmation, payment_rejected, processing, shipped, delivered, completed, returned, cancelled)
//	@Param			start_date		query		string	false	"orders on or after this date (YYYY-MM-DD)"
//	@Param			end_date		query		string	false	"orders on or before this date (YYYY-MM-DD)"
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/catalog/orders/export [get]
func (q *orderController) V1Export(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "OrderController-V1Export"
	request := sanitizers.FindOrders(c)
	request.BranchID, request.Limit, request.Page = currentBranchID(c), exportLimit, 1
	response, _, err := q.orderService.FindOrders(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindOrders")
		return err
	}
	return writeExport(c, ctxt, "orders", "Online orders", orderExportColumns, response)
}

// V1Create godoc
//
//	@Summary		Record an online order
//	@Description	The items are priced at the selling price of products shown in the online store. An order with a payment proof starts awaiting confirmation, any other unpaid; the branch is notified. The number (ORD/YYYYMMDD/NNNN) is generated.
//	@Tags			Online catalog
//	@Param			request	body		models.OrderRequest	true	"order"
//	@Success		201		{object}	helper.Response{data=models.Order}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/catalog/orders [post]
func (q *orderController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "OrderController-V1Create"
	request, err := sanitizers.ValidateOrder(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateOrder")
		return err
	}
	var response *models.Order
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.orderService.CreateOrder(ctx, tx, &models.OrderCreate{
			OrderRequest: request,
			StoreID:      currentStoreID(c),
			BranchID:     currentBranchID(c),
			At:           time.Now(),
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateOrder")
		}
		return orderError(err)
	}); err != nil {
		return err
	}
	return helper.Created(c, response)
}

// findOrder reads the order of the path with its items.
func (q *orderController) findOrder(c fiber.Ctx, ctxt string) (*models.Order, error) {
	ctx := c.Context()
	orderID, err := sanitizers.ParamID(c, "id", "Order")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return nil, err
	}
	response, err := q.orderService.FindOrder(ctx, nil, currentBranchID(c), orderID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindOrder")
		return nil, orderError(err)
	}
	return response, nil
}

// V1Show godoc
//
//	@Summary	Show an online order
//	@Tags		Online catalog
//	@Param		id	path		string	true	"order id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Order}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/catalog/orders/{id} [get]
func (q *orderController) V1Show(c fiber.Ctx) error {
	response, err := q.findOrder(c, "OrderController-V1Show")
	if err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1Status godoc
//
//	@Summary		Change the status of an online order
//	@Description	Allowed changes: unpaid → processing, cancelled; awaiting_confirmation → processing, payment_rejected, cancelled; payment_rejected → awaiting_confirmation, cancelled; processing → shipped, completed, cancelled; shipped → delivered, completed, returned; delivered → completed, returned. When the online store uses the physical stock, the stock is taken when the order starts being processed and given back when it is cancelled or returned. Cancelling and rejecting a payment need a reason.
//	@Tags			Online catalog
//	@Param			id		path		string						true	"order id (UUID)"
//	@Param			request	body		models.OrderStatusRequest	true	"new status"
//	@Success		200		{object}	helper.Response{data=models.Order}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/catalog/orders/{id}/status [patch]
func (q *orderController) V1Status(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "OrderController-V1Status"
	orderID, err := sanitizers.ParamID(c, "id", "Order")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateOrderStatus(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateOrderStatus")
		return err
	}
	var response *models.Order
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.orderService.ChangeOrderStatus(ctx, tx, &models.OrderStatusChange{
			OrderStatusRequest: request,
			BranchID:           currentBranchID(c),
			OrderID:            orderID,
			Actor:              currentActor(c),
			At:                 time.Now(),
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeOrderStatus")
		}
		return orderError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1Invoice godoc
//
//	@Summary	Print the invoice of an online order
//	@Tags		Online catalog
//	@Produce	application/pdf
//	@Param		id	path		string	true	"order id (UUID)"
//	@Success	200	{file}		binary
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/catalog/orders/{id}/invoice [get]
func (q *orderController) V1Invoice(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "OrderController-V1Invoice"
	order, err := q.findOrder(c, ctxt)
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
	document.Text(40, 790, 18, true, tr("INVOICE"))
	document.Text(40, 770, 10, true, name)
	document.Text(40, 756, 9, false, address)
	document.Text(40, 744, 9, false, phone)
	document.Text(360, 790, 9, false, tr("Order")+": "+order.OrderNumber)
	document.Text(360, 776, 9, false, tr("Date")+": "+formatTime(order.CreatedAt))
	document.Text(360, 762, 9, false, tr("Status")+": "+tr(order.Status))
	document.Text(40, 712, 9, true, tr("Bill to")+":")
	document.Text(40, 698, 9, false, order.CustomerName)
	document.Text(40, 686, 9, false, textOr(order.CustomerPhone, ""))
	document.Text(40, 674, 9, false, export.Fit(textOr(order.ShippingAddress, ""), 300, 9))
	y := 640.0
	document.Rect(40, y-4, 515, 16, 0.92)
	document.Text(46, y, 9, true, tr("Item"))
	document.Text(330, y, 9, true, tr("Qty"))
	document.Text(390, y, 9, true, tr("Price"))
	document.TextRight(549, y, 9, true, tr("Subtotal"))
	y -= 20
	for _, item := range order.Items {
		document.Text(46, y, 9, false, export.Fit(item.ProductName, 270, 9))
		document.Text(330, y, 9, false, export.Thousands(item.Quantity))
		document.Text(390, y, 9, false, export.Thousands(item.UnitPrice))
		document.TextRight(549, y, 9, false, export.Thousands(item.Subtotal))
		y -= 15
	}
	y -= 6
	document.Line(330, y, 555)
	for _, row := range []struct {
		label string
		value float64
		bold  bool
	}{
		{tr("Subtotal"), order.Subtotal, false},
		{tr("Shipping"), order.ShippingFee, false},
		{tr("Total"), order.GrandTotal, true},
	} {
		y -= 16
		document.Text(390, y, 9.5, row.bold, row.label)
		document.TextRight(549, y, 9.5, row.bold, export.Thousands(row.value))
	}
	if err = export.Send(c, "order-"+strings.ReplaceAll(order.OrderNumber, "/", "-")+".pdf", "application/pdf", document.Bytes()); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSend")
	}
	return err
}

// V1ShowSetting godoc
//
//	@Summary		Show the online catalog settings
//	@Description	How online orders are paid and delivered.
//	@Tags			Online catalog
//	@Success		200	{object}	helper.Response{data=models.CatalogSetting}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/catalog [get]
func (q *orderController) V1ShowSetting(c fiber.Ctx) error {
	return showSetting(c, "OrderController-V1ShowSetting", q.settingService, models.SettingCatalog)
}

// V1UpdateSetting godoc
//
//	@Summary		Update the online catalog settings
//	@Description	Only the attributes sent are changed. Cashiers cannot change settings.
//	@Tags			Online catalog
//	@Param			request	body		models.CatalogSetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.CatalogSetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/catalog [put]
func (q *orderController) V1UpdateSetting(c fiber.Ctx) error {
	return updateSetting(c, "OrderController-V1UpdateSetting", q.settingService, q.activityService, models.SettingCatalog)
}
