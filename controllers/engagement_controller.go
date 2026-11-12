package controllers

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/config"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type engagementController struct {
	config            config.Config
	authService       *middlewares.Service
	engagementService services.EngagementService
	activityService   services.ActivityService
}

func NewEngagementController(
	cfg config.Config,
	authService *middlewares.Service,
	engagementService services.EngagementService,
	activityService services.ActivityService,
) *engagementController {
	return &engagementController{
		config:            cfg,
		authService:       authService,
		engagementService: engagementService,
		activityService:   activityService,
	}
}

// Mount registers the routes of missions, referrals, integrations, loans and store ratings.
func (q *engagementController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("/missions", q.V1Missions)
	private.Post("/missions/:id/claim", q.V1ClaimMission)
	private.Get("/referrals", q.V1Referral)
	private.Post("/referrals/code", q.V1SetReferralCode)
	private.Post("/referrals/rewards/transfer-to-coin", q.V1TransferRewards)
	private.Get("/integrations", q.V1Integrations)
	private.Post("/integrations/:code/activate", q.V1ActivateIntegration)
	private.Post("/integrations/:code/deactivate", q.V1DeactivateIntegration)
	private.Get("/loans/providers", q.V1LoanProviders)
	private.Get("/loans/applications", q.V1LoanApplications)
	private.Post("/loans/applications", q.V1ApplyLoan)
	private.Post("/store-ratings", q.V1RateStore)
}

// V1Missions godoc
//
//	@Summary		List missions
//	@Description	Tasks that earn premium coins, with the progress of the store.
//	@Tags			Missions
//	@Success		200	{object}	helper.Response{data=[]models.Mission}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/missions [get]
func (q *engagementController) V1Missions(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1Missions"
	response, err := q.engagementService.FindMissions(ctx, currentStoreID(c), helper.Lang(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindMissions")
		return err
	}
	return helper.OK(c, response)
}

// V1ClaimMission godoc
//
//	@Summary		Claim the reward of a mission
//	@Description	Adds the reward of a completed mission to the premium coins of the store, once.
//	@Tags			Missions
//	@Param			id	path		string	true	"mission code"	Enums(add_products, first_sale, store_location, add_staff, verify_phone, two_factor, hundred_sales)
//	@Success		200	{object}	helper.Response{data=models.MissionClaim}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/missions/{id}/claim [post]
func (q *engagementController) V1ClaimMission(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1ClaimMission"
	var response *models.MissionClaim
	if err := inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.engagementService.ClaimMission(ctx, tx, currentStoreID(c), c.Params("id"), helper.Lang(c), time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrClaimMission")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1Referral godoc
//
//	@Summary		Get the referral programme
//	@Description	The referral code of the store, the stores that registered with it and the rewards earned.
//	@Tags			Referrals
//	@Success		200	{object}	helper.Response{data=models.Referral}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/referrals [get]
func (q *engagementController) V1Referral(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1Referral"
	response, err := q.engagementService.FindReferral(ctx, currentStoreID(c), q.config.PublicURL)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindReferral")
		return err
	}
	return helper.OK(c, response)
}

// V1SetReferralCode godoc
//
//	@Summary		Create the referral code
//	@Description	Only the owner, and only once. The code has 4–16 letters or numbers; without one a code is generated.
//	@Tags			Referrals
//	@Param			request	body		models.ReferralCodeRequest	false	"code"
//	@Success		200		{object}	helper.Response{data=models.ReferralCode}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/referrals/code [post]
func (q *engagementController) V1SetReferralCode(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1SetReferralCode"
	request, err := sanitizers.Decode[models.ReferralCodeRequest](c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return err
	}
	if !helper.P(c).IsOwner() {
		err = helper.Forbidden("Only the owner can create the referral code.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrForbidden")
		return err
	}
	var response *models.ReferralCode
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.engagementService.SetReferralCode(ctx, tx, currentStoreID(c), request.Code, q.config.PublicURL); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSetReferralCode")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1TransferRewards godoc
//
//	@Summary	Move referral rewards to premium coins
//	@Tags		Referrals
//	@Success	200	{object}	helper.Response{data=models.RewardTransfer}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/referrals/rewards/transfer-to-coin [post]
func (q *engagementController) V1TransferRewards(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1TransferRewards"
	var response *models.RewardTransfer
	if err := inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.engagementService.TransferRewards(ctx, tx, currentStoreID(c)); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrTransferRewards")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1Integrations godoc
//
//	@Summary	List integrations
//	@Tags		Integrations
//	@Success	200	{object}	helper.Response{data=[]models.Integration}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/integrations [get]
func (q *engagementController) V1Integrations(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1Integrations"
	response, err := q.engagementService.FindIntegrations(ctx, currentStoreID(c), helper.Lang(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindIntegrations")
		return err
	}
	return helper.OK(c, response)
}

// integration finds the integration of the path in the catalogue.
func (q *engagementController) integration(c fiber.Ctx, ctxt string) (string, error) {
	code := c.Params("code")
	if models.IntegrationByCode(code) == nil {
		err := helper.NotFound("Integration")
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrIntegrationByCode")
		return "", err
	}
	return code, nil
}

// V1ActivateIntegration godoc
//
//	@Summary		Connect an integration
//	@Description	The terms of service must be accepted. Jurnal connects at once; the other partners confirm the connection, which stays pending until then.
//	@Tags			Integrations
//	@Param			code	path		string						true	"integration code"	Enums(dana, jurnal, qris_dynamic)
//	@Param			request	body		models.IntegrationRequest	true	"consent and settings"
//	@Success		200		{object}	helper.Response{data=models.IntegrationState}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/integrations/{code}/activate [post]
func (q *engagementController) V1ActivateIntegration(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1ActivateIntegration"
	code, err := q.integration(c, ctxt)
	if err != nil {
		return err
	}
	settings, err := sanitizers.IntegrationActivation(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrIntegrationActivation")
		return err
	}
	var response *models.IntegrationState
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.engagementService.ActivateIntegration(ctx, tx, currentStoreID(c), code, settings, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrActivateIntegration")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1DeactivateIntegration godoc
//
//	@Summary	Disconnect an integration
//	@Tags		Integrations
//	@Param		code	path		string	true	"integration code"	Enums(dana, jurnal, qris_dynamic)
//	@Success	200		{object}	helper.Response{data=models.IntegrationState}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/integrations/{code}/deactivate [post]
func (q *engagementController) V1DeactivateIntegration(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1DeactivateIntegration"
	code, err := q.integration(c, ctxt)
	if err != nil {
		return err
	}
	var response *models.IntegrationState
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.engagementService.DeactivateIntegration(ctx, tx, currentStoreID(c), code); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeactivateIntegration")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1LoanProviders godoc
//
//	@Summary	List lending partners
//	@Tags		Loans
//	@Success	200	{object}	helper.Response{data=[]models.LoanProvider}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/loans/providers [get]
func (q *engagementController) V1LoanProviders(c fiber.Ctx) error {
	response := make([]models.LoanProvider, len(models.LoanProviders))
	for i, provider := range models.LoanProviders {
		provider.Description = helper.T(c, provider.Description)
		provider.InterestInfo = helper.T(c, provider.InterestInfo)
		response[i] = provider
	}
	return helper.OK(c, response)
}

// V1LoanApplications godoc
//
//	@Summary	List loan applications
//	@Tags		Loans
//	@Success	200	{object}	helper.Response{data=[]models.LoanApplication}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/loans/applications [get]
func (q *engagementController) V1LoanApplications(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1LoanApplications"
	response, err := q.engagementService.FindLoanApplications(ctx, currentStoreID(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindLoanApplications")
		return err
	}
	return helper.OK(c, response)
}

// V1ApplyLoan godoc
//
//	@Summary		Apply for a loan
//	@Description	Only the owner. The amount and tenor must fit the offer of the lending partner, and the owner must consent to sharing store and sales data with it.
//	@Tags			Loans
//	@Param			request	body		models.LoanApplicationRequest	true	"loan application"
//	@Success		200		{object}	helper.Response{data=models.LoanApplied}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/loans/applications [post]
func (q *engagementController) V1ApplyLoan(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1ApplyLoan"
	request, err := sanitizers.LoanApplication(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLoanApplication")
		return err
	}
	if !helper.P(c).IsOwner() {
		err = helper.Forbidden("Only the owner can apply for a loan.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrForbidden")
		return err
	}
	request.StoreID, request.CreatedAt = currentStoreID(c), time.Now()
	var response *models.LoanApplied
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.engagementService.ApplyLoan(ctx, tx, request); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrApplyLoan")
		}
		return err
	}); err != nil {
		return err
	}
	record(c, q.activityService, "loan_applied", i18n.Tr("Applied for a loan with {provider}", "provider", request.ProviderCode))
	return helper.OK(c, response)
}

// V1RateStore godoc
//
//	@Summary		Record a store rating
//	@Description	Feedback of a customer, collected by the cashier app or the link on a receipt.
//	@Tags			Store ratings
//	@Param			request	body		models.StoreRatingRequest	true	"rating"
//	@Success		201		{object}	helper.Response{data=models.StoreRating}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/store-ratings [post]
func (q *engagementController) V1RateStore(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "EngagementController-V1RateStore"
	request, err := sanitizers.StoreRating(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrStoreRating")
		return err
	}
	request.BranchID, request.CreatedAt = currentBranchID(c), time.Now()
	var response *models.StoreRating
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.engagementService.RateStore(ctx, tx, request); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRateStore")
		}
		return writeError(err)
	}); err != nil {
		return err
	}
	return helper.Created(c, response)
}
