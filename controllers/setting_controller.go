package controllers

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/roysitumorang/lestari-backend/config"
	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type settingController struct {
	config           config.Config
	authService      *middlewares.Service
	settingService   services.SettingService
	billingService   services.BillingService
	accountService   services.AccountService
	dataResetService services.DataResetService
	activityService  services.ActivityService
}

func NewSettingController(
	cfg config.Config,
	authService *middlewares.Service,
	settingService services.SettingService,
	billingService services.BillingService,
	accountService services.AccountService,
	dataResetService services.DataResetService,
	activityService services.ActivityService,
) *settingController {
	return &settingController{
		config:           cfg,
		authService:      authService,
		settingService:   settingService,
		billingService:   billingService,
		accountService:   accountService,
		dataResetService: dataResetService,
		activityService:  activityService,
	}
}

// Mount registers the routes of /settings.
func (q *settingController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("/points", q.V1Points)
	private.Put("/points", q.V1UpdatePoints)
	private.Get("/store", q.V1Store)
	private.Put("/store", q.V1UpdateStore)
	private.Get("/online-store", q.V1OnlineStore)
	private.Put("/online-store", q.V1UpdateOnlineStore)
	private.Get("/link-page", q.V1LinkPage)
	private.Put("/link-page", q.V1UpdateLinkPage)
	private.Get("/qr-order", q.V1QROrder)
	private.Put("/qr-order", q.V1UpdateQROrder)
	private.Get("/default-tax", q.V1DefaultTax)
	private.Put("/default-tax", q.V1UpdateDefaultTax)
	private.Get("/service-charge", q.V1ServiceCharge)
	private.Put("/service-charge", q.V1UpdateServiceCharge)
	private.Get("/rounding", q.V1Rounding)
	private.Put("/rounding", q.V1UpdateRounding)
	private.Get("/receipt", q.V1Receipt)
	private.Put("/receipt", q.V1UpdateReceipt)
	private.Get("/display", q.V1Display)
	private.Put("/display", q.V1UpdateDisplay)
	private.Get("/store/qr-code", q.V1StoreQRCode)
	private.Get("/link-page/visitors", q.V1LinkPageVisitors)
	private.Get("/link-page/username-availability", q.V1UsernameAvailability)
	private.Get("/qr-order/menu-book", q.V1MenuBook)
	private.Get("/storage", q.V1Storage)
	private.Post("/storage/purchases", q.V1BuyStorage)
	private.Post("/reset-data/otp", q.V1ResetCode)
	private.Post("/reset-data", q.V1ResetData)
}

// showSetting answers one setting of the active branch.
func showSetting(c fiber.Ctx, ctxt string, settingService services.SettingService, key string) error {
	ctx := c.Context()
	response := models.NewSetting(key)
	if err := settingService.FindSetting(ctx, nil, currentBranchID(c), key, response); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSetting")
		return err
	}
	return helper.OK(c, response)
}

// updateSetting changes the attributes a request sends of one setting of the active branch. Cashiers cannot
// change settings, and some settings need a plugin.
func updateSetting(c fiber.Ctx, ctxt string, settingService services.SettingService, activityService services.ActivityService, key string) error {
	ctx := c.Context()
	principal := helper.P(c)
	if plugin := models.SettingPlugins[key]; plugin != "" && !principal.HasPlugin(plugin) {
		err := helper.PaymentRequired("plugin_required", "This feature needs the {plugin} plugin.", "plugin", plugin)
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPluginRequired")
		return err
	}
	if principal.Role == models.RoleCashier {
		err := helper.Forbidden("Cashiers cannot change settings.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrForbidden")
		return err
	}
	patch, err := sanitizers.ValidateSetting(c, key)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateSetting")
		return err
	}
	var response any
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = settingService.UpdateSetting(ctx, tx, &models.SettingUpdate{
			StoreID:      currentStoreID(c),
			BranchID:     currentBranchID(c),
			HeadOfficeID: currentHeadOfficeID(c),
			Key:          key,
			Patch:        patch,
			At:           time.Now(),
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateSetting")
		}
		return err
	}); err != nil {
		return err
	}
	record(c, activityService, "settings_updated", i18n.Tr("Updated {key} settings", "key", key))
	return helper.OK(c, response)
}

// V1Points godoc
//
//	@Summary	Get the settings of loyalty points
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.PointsSetting}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/points [get]
func (q *settingController) V1Points(c fiber.Ctx) error {
	return showSetting(c, "SettingController-V1Points", q.settingService, models.SettingPoints)
}

// V1UpdatePoints godoc
//
//	@Summary		Update the settings of loyalty points
//	@Description	Only the attributes present in the body are changed; null clears an optional one. When no value differs from the stored one nothing is written. Cashiers cannot change settings. Needs the business plugin.
//	@Tags			Settings
//	@Param			request	body		models.PointsSetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.PointsSetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/points [put]
func (q *settingController) V1UpdatePoints(c fiber.Ctx) error {
	return updateSetting(c, "SettingController-V1UpdatePoints", q.settingService, q.activityService, models.SettingPoints)
}

// V1Store godoc
//
//	@Summary	Get the settings of the store profile
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.StoreSetting}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/store [get]
func (q *settingController) V1Store(c fiber.Ctx) error {
	return showSetting(c, "SettingController-V1Store", q.settingService, models.SettingStore)
}

// V1UpdateStore godoc
//
//	@Summary		Update the settings of the store profile
//	@Description	Only the attributes present in the body are changed; null clears an optional one. When no value differs from the stored one nothing is written. Cashiers cannot change settings. The name is also given to the branch and, at the head office, to the store.
//	@Tags			Settings
//	@Param			request	body		models.StoreSetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.StoreSetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/store [put]
func (q *settingController) V1UpdateStore(c fiber.Ctx) error {
	return updateSetting(c, "SettingController-V1UpdateStore", q.settingService, q.activityService, models.SettingStore)
}

// V1OnlineStore godoc
//
//	@Summary	Get the settings of the online store
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.OnlineStoreSetting}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/online-store [get]
func (q *settingController) V1OnlineStore(c fiber.Ctx) error {
	return showSetting(c, "SettingController-V1OnlineStore", q.settingService, models.SettingOnlineStore)
}

// V1UpdateOnlineStore godoc
//
//	@Summary		Update the settings of the online store
//	@Description	Only the attributes present in the body are changed; null clears an optional one. When no value differs from the stored one nothing is written. Cashiers cannot change settings. The subdomain must be free.
//	@Tags			Settings
//	@Param			request	body		models.OnlineStoreSetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.OnlineStoreSetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/online-store [put]
func (q *settingController) V1UpdateOnlineStore(c fiber.Ctx) error {
	return updateSetting(c, "SettingController-V1UpdateOnlineStore", q.settingService, q.activityService, models.SettingOnlineStore)
}

// V1LinkPage godoc
//
//	@Summary	Get the settings of the link page
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.LinkPageSetting}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/link-page [get]
func (q *settingController) V1LinkPage(c fiber.Ctx) error {
	return showSetting(c, "SettingController-V1LinkPage", q.settingService, models.SettingLinkPage)
}

// V1UpdateLinkPage godoc
//
//	@Summary		Update the settings of the link page
//	@Description	Only the attributes present in the body are changed; null clears an optional one. When no value differs from the stored one nothing is written. Cashiers cannot change settings. The username must be free.
//	@Tags			Settings
//	@Param			request	body		models.LinkPageSetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.LinkPageSetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/link-page [put]
func (q *settingController) V1UpdateLinkPage(c fiber.Ctx) error {
	return updateSetting(c, "SettingController-V1UpdateLinkPage", q.settingService, q.activityService, models.SettingLinkPage)
}

// V1QROrder godoc
//
//	@Summary	Get the settings of QR ordering
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.QROrderSetting}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/qr-order [get]
func (q *settingController) V1QROrder(c fiber.Ctx) error {
	return showSetting(c, "SettingController-V1QROrder", q.settingService, models.SettingQROrder)
}

// V1UpdateQROrder godoc
//
//	@Summary		Update the settings of QR ordering
//	@Description	Only the attributes present in the body are changed; null clears an optional one. When no value differs from the stored one nothing is written. Cashiers cannot change settings.
//	@Tags			Settings
//	@Param			request	body		models.QROrderSetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.QROrderSetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/qr-order [put]
func (q *settingController) V1UpdateQROrder(c fiber.Ctx) error {
	return updateSetting(c, "SettingController-V1UpdateQROrder", q.settingService, q.activityService, models.SettingQROrder)
}

// V1DefaultTax godoc
//
//	@Summary	Get the settings of the default tax
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.DefaultTaxSetting}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/default-tax [get]
func (q *settingController) V1DefaultTax(c fiber.Ctx) error {
	return showSetting(c, "SettingController-V1DefaultTax", q.settingService, models.SettingDefaultTax)
}

// V1UpdateDefaultTax godoc
//
//	@Summary		Update the settings of the default tax
//	@Description	Only the attributes present in the body are changed; null clears an optional one. When no value differs from the stored one nothing is written. Cashiers cannot change settings.
//	@Tags			Settings
//	@Param			request	body		models.DefaultTaxSetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.DefaultTaxSetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/default-tax [put]
func (q *settingController) V1UpdateDefaultTax(c fiber.Ctx) error {
	return updateSetting(c, "SettingController-V1UpdateDefaultTax", q.settingService, q.activityService, models.SettingDefaultTax)
}

// V1ServiceCharge godoc
//
//	@Summary	Get the settings of the service charge
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.ServiceChargeSetting}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/service-charge [get]
func (q *settingController) V1ServiceCharge(c fiber.Ctx) error {
	return showSetting(c, "SettingController-V1ServiceCharge", q.settingService, models.SettingServiceCharge)
}

// V1UpdateServiceCharge godoc
//
//	@Summary		Update the settings of the service charge
//	@Description	Only the attributes present in the body are changed; null clears an optional one. When no value differs from the stored one nothing is written. Cashiers cannot change settings. Needs the business plugin.
//	@Tags			Settings
//	@Param			request	body		models.ServiceChargeSetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.ServiceChargeSetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/service-charge [put]
func (q *settingController) V1UpdateServiceCharge(c fiber.Ctx) error {
	return updateSetting(c, "SettingController-V1UpdateServiceCharge", q.settingService, q.activityService, models.SettingServiceCharge)
}

// V1Rounding godoc
//
//	@Summary	Get the settings of rounding
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.RoundingSetting}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/rounding [get]
func (q *settingController) V1Rounding(c fiber.Ctx) error {
	return showSetting(c, "SettingController-V1Rounding", q.settingService, models.SettingRounding)
}

// V1UpdateRounding godoc
//
//	@Summary		Update the settings of rounding
//	@Description	Only the attributes present in the body are changed; null clears an optional one. When no value differs from the stored one nothing is written. Cashiers cannot change settings. Needs the business plugin.
//	@Tags			Settings
//	@Param			request	body		models.RoundingSetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.RoundingSetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/rounding [put]
func (q *settingController) V1UpdateRounding(c fiber.Ctx) error {
	return updateSetting(c, "SettingController-V1UpdateRounding", q.settingService, q.activityService, models.SettingRounding)
}

// V1Receipt godoc
//
//	@Summary	Get the settings of the receipt
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.ReceiptSetting}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/receipt [get]
func (q *settingController) V1Receipt(c fiber.Ctx) error {
	return showSetting(c, "SettingController-V1Receipt", q.settingService, models.SettingReceipt)
}

// V1UpdateReceipt godoc
//
//	@Summary		Update the settings of the receipt
//	@Description	Only the attributes present in the body are changed; null clears an optional one. When no value differs from the stored one nothing is written. Cashiers cannot change settings.
//	@Tags			Settings
//	@Param			request	body		models.ReceiptSetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.ReceiptSetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/receipt [put]
func (q *settingController) V1UpdateReceipt(c fiber.Ctx) error {
	return updateSetting(c, "SettingController-V1UpdateReceipt", q.settingService, q.activityService, models.SettingReceipt)
}

// V1Display godoc
//
//	@Summary	Get the settings of the display
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.DisplaySetting}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/display [get]
func (q *settingController) V1Display(c fiber.Ctx) error {
	return showSetting(c, "SettingController-V1Display", q.settingService, models.SettingDisplay)
}

// V1UpdateDisplay godoc
//
//	@Summary		Update the settings of the display
//	@Description	Only the attributes present in the body are changed; null clears an optional one. When no value differs from the stored one nothing is written. Cashiers cannot change settings. The language is used for requests that send no Accept-Language.
//	@Tags			Settings
//	@Param			request	body		models.DisplaySetting	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.DisplaySetting}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/display [put]
func (q *settingController) V1UpdateDisplay(c fiber.Ctx) error {
	return updateSetting(c, "SettingController-V1UpdateDisplay", q.settingService, q.activityService, models.SettingDisplay)
}

// V1StoreQRCode godoc
//
//	@Summary		Download the store QR code
//	@Description	A PNG that opens the link page of the branch, or its online store, or its public page.
//	@Tags			Settings
//	@Produce		image/png
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/store/qr-code [get]
func (q *settingController) V1StoreQRCode(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SettingController-V1StoreQRCode"
	url, err := q.settingService.PublicURL(ctx, currentBranchID(c), q.config.PublicURL)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPublicURL")
		return err
	}
	response, err := qrcode.Encode(url, qrcode.Medium, 512)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrEncode")
		return err
	}
	return export.Send(c, "store-qr.png", "image/png", response)
}

// V1LinkPageVisitors godoc
//
//	@Summary	List the visits of the link page
//	@Tags		Settings
//	@Param		page		query		int	false	"page, from 1"	default(1)
//	@Param		per_page	query		int	false	"rows per page"	default(50)	maximum(1000)
//	@Success	200			{object}	helper.Response{data=helper.Pagination{rows=[]models.LinkPageVisit}}
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/link-page/visitors [get]
func (q *settingController) V1LinkPageVisitors(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SettingController-V1LinkPageVisitors"
	limit, pageNo := sanitizers.PageOf(c, 50)
	response, total, err := q.settingService.FindLinkPageVisits(ctx, currentBranchID(c), limit, pageNo)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindLinkPageVisits")
		return err
	}
	return helper.List(c, response, page(limit, pageNo, total))
}

// V1UsernameAvailability godoc
//
//	@Summary		Check a link-page username
//	@Description	Available means well-formed (3–40 lowercase letters, numbers or dashes) and not used by another branch.
//	@Tags			Settings
//	@Param			username	query		string	true	"username to check"
//	@Success		200			{object}	helper.Response{data=models.AvailabilityResponse}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/link-page/username-availability [get]
func (q *settingController) V1UsernameAvailability(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SettingController-V1UsernameAvailability"
	available, err := q.settingService.SlugAvailable(ctx, currentBranchID(c), models.SettingLinkPage, c.Query("username"))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSlugAvailable")
		return err
	}
	return helper.OK(c, models.AvailabilityResponse{Available: available})
}

// V1MenuBook godoc
//
//	@Summary		Download the menu book
//	@Description	A printable PDF of the products shown online, grouped by category (the categories chosen in the QR ordering settings, or all).
//	@Tags			Settings
//	@Produce		application/pdf
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/qr-order/menu-book [get]
func (q *settingController) V1MenuBook(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SettingController-V1MenuBook"
	items, err := q.settingService.FindMenuItems(ctx, currentBranchID(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindMenuItems")
		return err
	}
	var store models.StoreSetting
	if err = q.settingService.FindSetting(ctx, nil, currentBranchID(c), models.SettingStore, &store); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSetting")
		return err
	}
	document := export.NewPDF(595, 842)
	document.Page()
	y := 790.0
	document.Text(40, y, 20, true, store.Name)
	y -= 22
	document.Text(40, y, 11, false, helper.T(c, "Menu"))
	y -= 30
	category := ""
	for _, item := range items {
		if y < 60 {
			document.Page()
			y = 800
		}
		if item.Category != category {
			y -= 6
			document.Text(40, y, 13, true, item.Category)
			y -= 20
			category = item.Category
		}
		document.Text(52, y, 10.5, false, export.Fit(item.Name, 380, 10.5))
		document.TextRight(555, y, 10.5, true, "Rp "+export.Thousands(item.SellingPrice))
		y -= 13
		if item.OnlineDescription != nil && *item.OnlineDescription != "" {
			document.Text(52, y, 8, false, export.Fit(*item.OnlineDescription, 480, 8))
			y -= 12
		}
		y -= 4
	}
	return export.Send(c, "menu.pdf", "application/pdf", document.Bytes())
}

// V1Storage godoc
//
//	@Summary	Get storage usage
//	@Tags		Settings
//	@Success	200	{object}	helper.Response{data=models.Storage}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/settings/storage [get]
func (q *settingController) V1Storage(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SettingController-V1Storage"
	response, err := q.billingService.FindStorage(ctx, currentStoreID(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStorage")
		return err
	}
	return helper.OK(c, response)
}

// V1BuyStorage godoc
//
//	@Summary		Buy storage
//	@Description	Adds a storage package for 30 days, paid with premium coins (402 when the balance is too low).
//	@Tags			Settings
//	@Param			request	body		models.StoragePurchaseRequest	true	"package"
//	@Success		200		{object}	helper.Response{data=models.StoragePurchase}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/storage/purchases [post]
func (q *settingController) V1BuyStorage(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SettingController-V1BuyStorage"
	request, err := sanitizers.StoragePurchase(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrStoragePurchase")
		return err
	}
	var response *models.StoragePurchase
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.billingService.BuyStorage(ctx, tx, currentStoreID(c), request.Package, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBuyStorage")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// mask hides most of an email address or a phone number.
func mask(value string) string {
	for i, r := range value {
		if r == '@' && i > 1 {
			masked := []byte(value)
			for j := 1; j < i; j++ {
				masked[j] = '*'
			}
			return string(masked)
		}
	}
	if len(value) > 4 {
		masked := []byte(value)
		for j := range len(value) - 4 {
			masked[j] = '*'
		}
		return string(masked)
	}
	return value
}

// V1ResetCode godoc
//
//	@Summary		Send the data-reset code
//	@Description	Only the owner can reset data. The code goes to the owner's email, or to their phone by WhatsApp when asked and a number is on file.
//	@Tags			Settings
//	@Param			request	body		models.ResetOTPRequest	false	"where to send the code"
//	@Success		200		{object}	helper.Response{data=models.OTPSentResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		429		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/reset-data/otp [post]
func (q *settingController) V1ResetCode(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SettingController-V1ResetCode"
	if !helper.P(c).IsOwner() {
		err := helper.Forbidden("Only the owner can reset data.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrForbidden")
		return err
	}
	// the body is optional
	request := &models.ResetOTPRequest{}
	_ = helper.Decode(c, request)
	email, phone, err := q.accountService.FindContact(ctx, currentUserID(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindContact")
		return userError(err)
	}
	to, channel := email, "email"
	if request.Channel == "whatsapp" && phone != nil && *phone != "" {
		to, channel = *phone, "whatsapp"
	}
	if err = q.accountService.SendOTP(ctx, currentUserID(c), models.OTPPurposeResetData, to, channel, time.Now(), func(code string) string {
		return helper.T(c, "Your Lestari data-reset code is {code}. Do not share it.", "code", code)
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSendOTP")
		return err
	}
	return helper.OK(c, models.OTPSentResponse{ExpiresIn: models.OTPLifetime, SentTo: mask(to)})
}

// V1ResetData godoc
//
//	@Summary		Reset data
//	@Description	Deletes the chosen kinds of data of the active branch in a background job, after the owner confirms with the code of POST /settings/reset-data/otp. Dated data (sales, purchases, shifts, online orders) can be limited to a year or a month. This cannot be undone.
//	@Tags			Settings
//	@Param			request	body		models.ResetDataRequest	true	"what to delete"
//	@Success		200		{object}	helper.Response{data=models.JobResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/settings/reset-data [post]
func (q *settingController) V1ResetData(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "SettingController-V1ResetData"
	if !helper.P(c).IsOwner() {
		err := helper.Forbidden("Only the owner can reset data.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrForbidden")
		return err
	}
	request, reset, err := sanitizers.ResetData(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrResetData")
		return err
	}
	now := time.Now()
	if err = q.accountService.CheckOTP(ctx, currentUserID(c), models.OTPPurposeResetData, "", request.OTP, "otp", now); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCheckOTP")
		return err
	}
	reset.StoreID, reset.BranchID, reset.At = currentStoreID(c), currentBranchID(c), now
	response, err := q.dataResetService.ResetData(ctx, reset)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrResetData")
		return err
	}
	record(c, q.activityService, "data_reset", i18n.Tr("Reset {types}", "types", joinComma(reset.DataTypes)))
	return helper.OK(c, response)
}
