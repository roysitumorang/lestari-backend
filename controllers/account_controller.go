package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type accountController struct {
	authService         *middlewares.Service
	accountService      services.AccountService
	activityService     services.ActivityService
	notificationService services.NotificationService
}

func NewAccountController(
	authService *middlewares.Service,
	accountService services.AccountService,
	activityService services.ActivityService,
	notificationService services.NotificationService,
) *accountController {
	return &accountController{
		authService:         authService,
		accountService:      accountService,
		activityService:     activityService,
		notificationService: notificationService,
	}
}

// Mount registers the routes of /account and /notifications.
func (q *accountController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	account := private.Group("/account")
	account.Get("/profile", q.V1Profile)
	account.Put("/profile", q.V1UpdateProfile)
	account.Put("/ppob-pin", q.V1ChangePPOBPin)
	account.Post("/phone-verification", q.V1SendPhoneCode)
	account.Post("/phone-verification/confirm", q.V1ConfirmPhone)
	account.Post("/two-factor/setup", q.V1TwoFactorSetup)
	account.Post("/two-factor/enable", q.V1TwoFactorEnable)
	account.Post("/two-factor/disable", q.V1TwoFactorDisable)
	account.Get("/activity-logs", q.V1Activity)
	account.Post("/deactivation-requests", q.V1RequestDeactivation)
	notifications := private.Group("/notifications")
	notifications.Get("", q.V1Notifications)
	notifications.Get("/unread-count", q.V1UnreadCount)
	notifications.Post("/read-all", q.V1ReadAll)
	notifications.Get("/:id<guid>", q.V1Notification)
	notifications.Post("/:id<guid>/read", q.V1Read)
}

// listActivity answers the activity log of one user of the store, in the language of the request.
func listActivity(c fiber.Ctx, ctxt string, activityService services.ActivityService, request *models.ActivityFilter) error {
	ctx := c.Context()
	request.StoreID = currentStoreID(c)
	request.Limit, request.Page = sanitizers.PageOf(c, 50)
	response, total, err := activityService.FindActivities(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindActivities")
		return err
	}
	for _, activity := range response {
		activity.Localize(helper.Lang(c))
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

// V1Profile godoc
//
//	@Summary	Get my profile
//	@Tags		Account
//	@Success	200	{object}	helper.Response{data=models.Profile}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/account/profile [get]
func (q *accountController) V1Profile(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1Profile"
	response, err := q.accountService.FindProfile(ctx, nil, currentUserID(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProfile")
		return userError(err)
	}
	return helper.OK(c, response)
}

// V1UpdateProfile godoc
//
//	@Summary		Update my profile
//	@Description	Replaces the editable attributes. A changed phone number has to be verified again. When no value differs from the stored one nothing is written.
//	@Tags			Account
//	@Param			request	body		models.ProfileRequest	true	"profile"
//	@Success		200		{object}	helper.Response{data=models.Profile}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/account/profile [put]
func (q *accountController) V1UpdateProfile(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1UpdateProfile"
	request, err := sanitizers.Profile(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrProfile")
		return err
	}
	var response *models.Profile
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.accountService.UpdateProfile(ctx, tx, currentUserID(c), request, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateProfile")
		}
		return userError(err)
	}); err != nil {
		return err
	}
	record(c, q.activityService, "profile_updated", i18n.Tr("Updated profile"))
	return helper.OK(c, response)
}

// V1ChangePPOBPin godoc
//
//	@Summary		Change my PPOB PIN
//	@Description	The PIN (6 digits) confirms PPOB sales in the cashier app. The account password travels base64-encoded.
//	@Tags			Account
//	@Param			request	body		models.PPOBPinRequest	true	"password and new PIN"
//	@Success		200		{object}	helper.Response{data=models.ChangedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/account/ppob-pin [put]
func (q *accountController) V1ChangePPOBPin(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1ChangePPOBPin"
	request, err := sanitizers.PPOBPin(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPPOBPin")
		return err
	}
	if err = inTx(c, ctxt, func(tx pgx.Tx) error {
		err := q.accountService.ChangePPOBPin(ctx, tx, currentUserID(c), request)
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangePPOBPin")
		}
		return userError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, models.ChangedResponse{Changed: true})
}

// V1SendPhoneCode godoc
//
//	@Summary		Send a phone verification code
//	@Description	Sends a six-digit code to the number by WhatsApp or SMS. One code a minute; a code is valid for five minutes.
//	@Tags			Account
//	@Param			request	body		models.PhoneVerificationRequest	true	"phone number"
//	@Success		200		{object}	helper.Response{data=models.OTPSentResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		429		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/account/phone-verification [post]
func (q *accountController) V1SendPhoneCode(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1SendPhoneCode"
	request, err := sanitizers.PhoneVerification(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPhoneVerification")
		return err
	}
	if err = q.accountService.SendOTP(ctx, currentUserID(c), models.OTPPurposePhone, request.Phone, request.Channel, time.Now(), func(code string) string {
		return helper.T(c, "Your Lestari verification code is {code}", "code", code)
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSendOTP")
		return err
	}
	return helper.OK(c, models.OTPSentResponse{ExpiresIn: models.OTPLifetime})
}

// V1ConfirmPhone godoc
//
//	@Summary	Confirm a phone verification code
//	@Tags		Account
//	@Param		request	body		models.PhoneConfirmationRequest	true	"phone number and code"
//	@Success	200		{object}	helper.Response{data=models.PhoneVerifiedResponse}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/account/phone-verification/confirm [post]
func (q *accountController) V1ConfirmPhone(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1ConfirmPhone"
	request, err := sanitizers.Decode[models.PhoneConfirmationRequest](c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return err
	}
	if err = q.accountService.ConfirmPhone(ctx, currentUserID(c), request, time.Now()); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrConfirmPhone")
		return err
	}
	return helper.OK(c, models.PhoneVerifiedResponse{PhoneVerified: true})
}

// V1TwoFactorSetup godoc
//
//	@Summary		Start two-step verification
//	@Description	Returns a secret to add to an authenticator app (as text, as an otpauth URL and as a QR code). It takes effect once POST /account/two-factor/enable confirms a code.
//	@Tags			Account
//	@Success		200	{object}	helper.Response{data=models.TwoFactorSetup}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/account/two-factor/setup [post]
func (q *accountController) V1TwoFactorSetup(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1TwoFactorSetup"
	var response *models.TwoFactorSetup
	if err := inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.accountService.SetupTwoFactor(ctx, tx, currentUserID(c), helper.P(c).Email); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSetupTwoFactor")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1TwoFactorEnable godoc
//
//	@Summary		Turn on two-step verification
//	@Description	Confirms the setup with a code of the authenticator app and returns eight recovery codes, shown only this once.
//	@Tags			Account
//	@Param			request	body		models.CodeRequest	true	"authenticator code"
//	@Success		200		{object}	helper.Response{data=models.TwoFactorResponse}
//	@Failure		400		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/account/two-factor/enable [post]
func (q *accountController) V1TwoFactorEnable(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1TwoFactorEnable"
	request, err := sanitizers.Decode[models.CodeRequest](c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return err
	}
	var response *models.TwoFactorResponse
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.accountService.EnableTwoFactor(ctx, tx, currentUserID(c), request.Code); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrEnableTwoFactor")
		}
		return userError(err)
	}); err != nil {
		return err
	}
	record(c, q.activityService, "two_factor_enabled", i18n.Tr("Turned on two-step verification"))
	return helper.OK(c, response)
}

// V1TwoFactorDisable godoc
//
//	@Summary		Turn off two-step verification
//	@Description	Needs the account password (base64).
//	@Tags			Account
//	@Param			request	body		models.PasswordConfirmRequest	true	"password"
//	@Success		200		{object}	helper.Response{data=models.TwoFactorResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/account/two-factor/disable [post]
func (q *accountController) V1TwoFactorDisable(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1TwoFactorDisable"
	password, err := sanitizers.PasswordConfirm(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPasswordConfirm")
		return err
	}
	if err = inTx(c, ctxt, func(tx pgx.Tx) error {
		err := q.accountService.DisableTwoFactor(ctx, tx, currentUserID(c), password)
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDisableTwoFactor")
		}
		return userError(err)
	}); err != nil {
		return err
	}
	record(c, q.activityService, "two_factor_disabled", i18n.Tr("Turned off two-step verification"))
	return helper.OK(c, models.TwoFactorResponse{})
}

// V1Activity godoc
//
//	@Summary	List my activity
//	@Tags		Account
//	@Param		page		query		int	false	"page, from 1"	default(1)
//	@Param		per_page	query		int	false	"rows per page"	default(50)	maximum(1000)
//	@Success	200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Activity}}
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/account/activity-logs [get]
func (q *accountController) V1Activity(c fiber.Ctx) error {
	return listActivity(c, "AccountController-V1Activity", q.activityService, &models.ActivityFilter{UserID: currentUserID(c)})
}

// V1RequestDeactivation godoc
//
//	@Summary		Request account deactivation
//	@Description	Only the owner can ask for the store to be closed; the request is handled by Lestari support.
//	@Tags			Account
//	@Success		200	{object}	helper.Response{data=models.DeactivationRequest}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/account/deactivation-requests [post]
func (q *accountController) V1RequestDeactivation(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1RequestDeactivation"
	if !helper.P(c).IsOwner() {
		err := helper.Forbidden("Only the owner can deactivate the account.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrForbidden")
		return err
	}
	var response *models.DeactivationRequest
	if err := inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.accountService.RequestDeactivation(ctx, tx, currentStoreID(c), currentUserID(c), time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRequestDeactivation")
		}
		return err
	}); err != nil {
		return err
	}
	record(c, q.activityService, "deactivation_requested", i18n.Tr("Requested account deactivation"))
	return helper.OK(c, response)
}

// notificationError turns the notification errors into responses.
func notificationError(err error) error {
	if errors.Is(err, models.ErrNotificationNotFound) {
		return helper.NotFound("Notification")
	}
	return err
}

// V1Notifications godoc
//
//	@Summary		List notifications
//	@Description	The notifications of the active branch and of the whole store, newest first, in the language of the request.
//	@Tags			Notifications
//	@Param			source		query		string	false	"only this source, e.g. back_office"
//	@Param			start_date	query		string	false	"on or after this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"on or before this date (YYYY-MM-DD)"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Notification}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/notifications [get]
func (q *accountController) V1Notifications(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1Notifications"
	request := sanitizers.FindNotifications(c)
	request.StoreID, request.BranchID = currentStoreID(c), currentBranchID(c)
	response, total, err := q.notificationService.FindNotifications(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindNotifications")
		return err
	}
	for _, notification := range response {
		notification.Localize(helper.Lang(c))
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

// V1UnreadCount godoc
//
//	@Summary	Count unread notifications
//	@Tags		Notifications
//	@Success	200	{object}	helper.Response{data=models.CountResponse}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/notifications/unread-count [get]
func (q *accountController) V1UnreadCount(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1UnreadCount"
	count, err := q.notificationService.CountUnread(ctx, currentStoreID(c), currentBranchID(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCountUnread")
		return err
	}
	return helper.OK(c, models.CountResponse{Count: count})
}

// V1ReadAll godoc
//
//	@Summary	Mark all notifications as read
//	@Tags		Notifications
//	@Success	200	{object}	helper.Response{data=models.UpdatedResponse}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/notifications/read-all [post]
func (q *accountController) V1ReadAll(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1ReadAll"
	updated, err := q.notificationService.MarkAllRead(ctx, currentStoreID(c), currentBranchID(c), time.Now())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarkAllRead")
		return err
	}
	return helper.OK(c, models.UpdatedResponse{Updated: updated})
}

// V1Notification godoc
//
//	@Summary	Get one notification
//	@Tags		Notifications
//	@Param		id	path		string	true	"notification id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Notification}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/notifications/{id} [get]
func (q *accountController) V1Notification(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1Notification"
	notificationID, err := sanitizers.ParamID(c, "id", "Notification")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.notificationService.FindNotification(ctx, currentStoreID(c), notificationID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindNotification")
		return notificationError(err)
	}
	return helper.OK(c, response.Localize(helper.Lang(c)))
}

// V1Read godoc
//
//	@Summary	Mark a notification as read
//	@Tags		Notifications
//	@Param		id	path		string	true	"notification id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Notification}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/notifications/{id}/read [post]
func (q *accountController) V1Read(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AccountController-V1Read"
	notificationID, err := sanitizers.ParamID(c, "id", "Notification")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.notificationService.MarkRead(ctx, currentStoreID(c), notificationID, time.Now())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarkRead")
		return notificationError(err)
	}
	return helper.OK(c, response.Localize(helper.Lang(c)))
}
