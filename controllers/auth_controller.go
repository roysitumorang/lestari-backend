package controllers

import (
	"errors"
	"log/slog"
	"time"
	"uuid"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type authController struct {
	authMiddleware  *middlewares.Service
	authService     services.AuthService
	activityService services.ActivityService
}

func NewAuthController(
	authMiddleware *middlewares.Service,
	authService services.AuthService,
	activityService services.ActivityService,
) *authController {
	return &authController{
		authMiddleware:  authMiddleware,
		authService:     authService,
		activityService: activityService,
	}
}

// Mount registers the routes of /auth: signing in, refreshing and registering are public.
func (q *authController) Mount(r fiber.Router) {
	r.Post("/login", q.V1Login)
	r.Post("/refresh", q.V1Refresh)
	r.Post("/register", q.V1Register)
	private := r.Group("", q.authMiddleware.Middleware())
	private.Post("/logout", q.V1Logout)
	private.Get("/me", q.V1Me)
	private.Put("/password", q.V1ChangePassword)
}

// userError turns the account errors into responses; anything else goes to the error handler as it is.
func userError(err error) error {
	if errors.Is(err, models.ErrUserNotFound) {
		return helper.NotFound("User")
	}
	return writeError(err)
}

// V1Login godoc
//
//	@Summary		Sign in
//	@Description	Returns an access token (send it as "Authorization: Bearer …") and a refresh token. The password travels base64-encoded. With two-step verification on, a first attempt without otp answers 401 two_factor_required.
//	@Tags			Auth
//	@Param			request	body		models.LoginRequest	true	"credentials"
//	@Success		200		{object}	helper.Response{data=models.Session}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/auth/login [post]
func (q *authController) V1Login(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AuthController-V1Login"
	request, err := sanitizers.Login(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLogin")
		return err
	}
	now := time.Now()
	response, credentials, err := q.authService.Login(ctx, request, now)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLogin")
		return userError(err)
	}
	q.activityService.Record(ctx, &models.Activity{
		StoreID:   credentials.StoreID,
		UserID:    credentials.ID,
		Action:    "sign_in",
		Text:      i18n.Tr("Signed in"),
		IPAddress: c.IP(),
		UserAgent: c.Get(fiber.HeaderUserAgent),
		CreatedAt: now,
	})
	return helper.OK(c, response)
}

// V1Refresh godoc
//
//	@Summary		Refresh the session
//	@Description	Exchanges a refresh token for a new access token and a new refresh token; the old one stops working.
//	@Tags			Auth
//	@Param			request	body		models.RefreshRequest	true	"refresh token"
//	@Success		200		{object}	helper.Response{data=models.RefreshedSession}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/auth/refresh [post]
func (q *authController) V1Refresh(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AuthController-V1Refresh"
	request, err := sanitizers.Decode[models.RefreshRequest](c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return err
	}
	response, err := q.authService.Refresh(ctx, request.RefreshToken)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRefresh")
		return err
	}
	return helper.OK(c, response)
}

// V1Register godoc
//
//	@Summary		Register a store
//	@Description	Creates a store on a 14-day Pro trial with its head office, the owner account and default payment methods, and signs the owner in.
//	@Tags			Auth
//	@Param			request	body		models.RegisterRequest	true	"store and owner"
//	@Success		201		{object}	helper.Response{data=models.Session}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/auth/register [post]
func (q *authController) V1Register(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AuthController-V1Register"
	request, err := sanitizers.Register(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRegister")
		return err
	}
	provision := &models.Provision{
		StoreName:    request.StoreName,
		OwnerName:    request.Name,
		Email:        request.Email,
		Password:     request.Password,
		Phone:        request.Phone,
		ReferralCode: request.ReferralCode,
		At:           time.Now(),
	}
	var userID, storeID uuid.UUID
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if userID, storeID, err = q.authService.Provision(ctx, tx, provision); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrProvision")
		}
		return err
	}); err != nil {
		return err
	}
	session, err := q.authService.OpenSession(ctx, nil, userID, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrOpenSession")
		return err
	}
	return helper.Created(c, session)
}

// V1Logout godoc
//
//	@Summary		Sign out
//	@Description	Ends every session of the signed-in user: all their refresh tokens stop working.
//	@Tags			Auth
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/auth/logout [post]
func (q *authController) V1Logout(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AuthController-V1Logout"
	if err := q.authService.Logout(ctx, currentUserID(c)); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLogout")
		return err
	}
	return helper.NoContent(c)
}

// V1Me godoc
//
//	@Summary		Get the signed-in user
//	@Description	The user with their store, active plugins and the branch they work in (X-Branch-Id, or their own).
//	@Tags			Auth
//	@Success		200	{object}	helper.Response{data=models.User}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/auth/me [get]
func (q *authController) V1Me(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AuthController-V1Me"
	response, err := q.authService.FindUser(ctx, currentUserID(c), new(currentBranchID(c)))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindUser")
		return userError(err)
	}
	return helper.OK(c, response)
}

// V1ChangePassword godoc
//
//	@Summary		Change the password
//	@Description	All three passwords travel base64-encoded; the new one needs at least 8 characters.
//	@Tags			Auth
//	@Param			request	body		models.PasswordRequest	true	"current and new password"
//	@Success		200		{object}	helper.Response{data=models.ChangedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/auth/password [put]
func (q *authController) V1ChangePassword(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "AuthController-V1ChangePassword"
	request, err := sanitizers.Password(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPassword")
		return err
	}
	if err = inTx(c, ctxt, func(tx pgx.Tx) error {
		err := q.authService.ChangePassword(ctx, tx, currentUserID(c), request, time.Now())
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangePassword")
		}
		return userError(err)
	}); err != nil {
		return err
	}
	record(c, q.activityService, "password_changed", i18n.Tr("Changed password"))
	return helper.OK(c, models.ChangedResponse{Changed: true})
}
