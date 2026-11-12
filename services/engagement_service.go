package services

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type EngagementService interface {
	// FindMissions lists the missions with the progress of the store, in a language.
	FindMissions(ctx context.Context, storeID uuid.UUID, lang i18n.Lang) ([]*models.Mission, error)
	// ClaimMission pays the reward of a finished mission, once.
	ClaimMission(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, lang i18n.Lang, at time.Time) (*models.MissionClaim, error)
	FindReferral(ctx context.Context, storeID uuid.UUID, baseURL string) (*models.Referral, error)
	// SetReferralCode gives the store a referral code of 4–16 letters or numbers, or a generated one.
	SetReferralCode(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code, baseURL string) (*models.ReferralCode, error)
	TransferRewards(ctx context.Context, tx pgx.Tx, storeID uuid.UUID) (*models.RewardTransfer, error)
	FindIntegrations(ctx context.Context, storeID uuid.UUID, lang i18n.Lang) ([]*models.Integration, error)
	ActivateIntegration(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, settings json.RawMessage, at time.Time) (*models.IntegrationState, error)
	DeactivateIntegration(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string) (*models.IntegrationState, error)
	FindLoanApplications(ctx context.Context, storeID uuid.UUID) ([]*models.LoanApplication, error)
	ApplyLoan(ctx context.Context, tx pgx.Tx, request *models.LoanApplication) (*models.LoanApplied, error)
	RateStore(ctx context.Context, tx pgx.Tx, request *models.StoreRating) (*models.StoreRating, error)
}

type engagementService struct {
	engagementRepository repositories.EngagementRepository
	storeRepository      repositories.StoreRepository
}

func NewEngagementService(
	engagementRepository repositories.EngagementRepository,
	storeRepository repositories.StoreRepository,
) EngagementService {
	return &engagementService{
		engagementRepository: engagementRepository,
		storeRepository:      storeRepository,
	}
}

func (q *engagementService) FindMissions(ctx context.Context, storeID uuid.UUID, lang i18n.Lang) ([]*models.Mission, error) {
	ctxt := "EngagementService-FindMissions"
	claimed, err := q.engagementRepository.FindClaimedMissions(ctx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindClaimedMissions")
		return nil, err
	}
	// several missions share a counter
	counted := map[string]int64{}
	response := make([]*models.Mission, len(models.Missions))
	for i, info := range models.Missions {
		progress, ok := counted[info.Progress]
		if !ok {
			if progress, err = q.engagementRepository.CountProgress(ctx, storeID, info.Progress); err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCountProgress")
				return nil, err
			}
			counted[info.Progress] = progress
		}
		mission := &models.Mission{
			ID:           info.Code,
			Code:         info.Code,
			Title:        lang.T(info.Title),
			Description:  lang.T(info.Description),
			RewardType:   "coin",
			RewardAmount: info.Reward,
			Progress:     min(progress, info.Target),
			Target:       info.Target,
			Status:       models.MissionStatusInProgress,
			ActionRoute:  info.Route,
		}
		switch {
		case slices.Contains(claimed, info.Code):
			mission.Status = models.MissionStatusClaimed
		case progress >= info.Target:
			mission.Status = models.MissionStatusCompleted
		}
		response[i] = mission
	}
	return response, nil
}

func (q *engagementService) ClaimMission(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, lang i18n.Lang, at time.Time) (*models.MissionClaim, error) {
	ctxt := "EngagementService-ClaimMission"
	missions, err := q.FindMissions(ctx, storeID, lang)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(missions, func(mission *models.Mission) bool { return mission.Code == code })
	if i < 0 {
		return nil, helper.NotFound("Mission")
	}
	mission := missions[i]
	switch mission.Status {
	case models.MissionStatusClaimed:
		return nil, helper.Conflict("This reward was already claimed.")
	case models.MissionStatusInProgress:
		return nil, helper.Conflict("Finish the mission first.")
	}
	if err = q.engagementRepository.CreateMissionClaim(ctx, tx, storeID, code, at); err != nil {
		if errors.Is(err, models.ErrMissionClaimed) {
			return nil, helper.Conflict("This reward was already claimed.")
		}
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateMissionClaim")
		return nil, err
	}
	if err = q.storeRepository.AddCoins(ctx, tx, storeID, mission.RewardAmount); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrAddCoins")
		return nil, err
	}
	return &models.MissionClaim{ID: code, Status: models.MissionStatusClaimed, RewardType: mission.RewardType, RewardAmount: mission.RewardAmount}, nil
}

// referralURL is the prefix of the registration link that carries a referral code.
func referralURL(baseURL string) string {
	return baseURL + "/register?ref="
}

func (q *engagementService) FindReferral(ctx context.Context, storeID uuid.UUID, baseURL string) (*models.Referral, error) {
	ctxt := "EngagementService-FindReferral"
	response, err := q.engagementRepository.FindReferral(ctx, storeID, referralURL(baseURL))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindReferral")
	}
	return response, err
}

func (q *engagementService) SetReferralCode(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code, baseURL string) (*models.ReferralCode, error) {
	ctxt := "EngagementService-SetReferralCode"
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		code = "LST" + strings.ToUpper(middlewares.RandomToken(3))
	}
	if len(code) < 4 || len(code) > 16 || strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
		return nil, helper.Invalid("code", "Use 4–16 letters or numbers.")
	}
	err := q.engagementRepository.SetReferralCode(ctx, tx, storeID, code)
	switch {
	case errors.Is(err, models.ErrReferralCodeTaken):
		return nil, helper.Invalid("code", "This code is already taken.")
	case errors.Is(err, models.ErrReferralCodeExists):
		return nil, helper.Conflict("You already have a referral code.")
	case err != nil:
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSetReferralCode")
		return nil, err
	}
	return &models.ReferralCode{Code: code, ShareURL: referralURL(baseURL) + code}, nil
}

func (q *engagementService) TransferRewards(ctx context.Context, tx pgx.Tx, storeID uuid.UUID) (*models.RewardTransfer, error) {
	ctxt := "EngagementService-TransferRewards"
	response, err := q.engagementRepository.TransferRewards(ctx, tx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrTransferRewards")
		return nil, err
	}
	if response == nil {
		return nil, helper.Conflict("There are no rewards to transfer yet.")
	}
	return response, nil
}

func (q *engagementService) FindIntegrations(ctx context.Context, storeID uuid.UUID, lang i18n.Lang) ([]*models.Integration, error) {
	ctxt := "EngagementService-FindIntegrations"
	states, err := q.engagementRepository.FindIntegrationStates(ctx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindIntegrationStates")
		return nil, err
	}
	response := make([]*models.Integration, len(models.Integrations))
	for i, info := range models.Integrations {
		integration := &models.Integration{
			Code:        info.Code,
			Name:        lang.T(info.Name),
			Description: lang.T(info.Description),
			Status:      models.IntegrationStatusInactive,
			TermsURL:    optional(info.Terms),
		}
		for _, state := range states {
			if state.Code == info.Code {
				integration.Status, integration.ConnectedAt = state.Status, state.ConnectedAt
			}
		}
		response[i] = integration
	}
	return response, nil
}

func (q *engagementService) ActivateIntegration(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, settings json.RawMessage, at time.Time) (*models.IntegrationState, error) {
	ctxt := "EngagementService-ActivateIntegration"
	// Jurnal connects at once; the other partners confirm the connection
	response := &models.IntegrationState{Code: code, Status: models.IntegrationStatusPending}
	if code == "jurnal" {
		response.Status, response.ConnectedAt = models.IntegrationStatusActive, &at
	}
	if len(settings) == 0 {
		settings = json.RawMessage("{}")
	}
	if err := q.engagementRepository.SaveIntegration(ctx, tx, storeID, code, response.Status, settings, response.ConnectedAt); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSaveIntegration")
		return nil, err
	}
	return response, nil
}

func (q *engagementService) DeactivateIntegration(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string) (*models.IntegrationState, error) {
	ctxt := "EngagementService-DeactivateIntegration"
	if err := q.engagementRepository.DisconnectIntegration(ctx, tx, storeID, code); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDisconnectIntegration")
		return nil, err
	}
	return &models.IntegrationState{Code: code, Status: models.IntegrationStatusInactive}, nil
}

func (q *engagementService) FindLoanApplications(ctx context.Context, storeID uuid.UUID) ([]*models.LoanApplication, error) {
	ctxt := "EngagementService-FindLoanApplications"
	response, err := q.engagementRepository.FindLoanApplications(ctx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindLoanApplications")
	}
	return response, err
}

func (q *engagementService) ApplyLoan(ctx context.Context, tx pgx.Tx, request *models.LoanApplication) (*models.LoanApplied, error) {
	ctxt := "EngagementService-ApplyLoan"
	response, err := q.engagementRepository.CreateLoanApplication(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateLoanApplication")
		return nil, err
	}
	if provider := models.LoanProviderByCode(request.ProviderCode); provider != nil {
		response.RedirectURL = provider.InfoURL
	}
	return response, nil
}

func (q *engagementService) RateStore(ctx context.Context, tx pgx.Tx, request *models.StoreRating) (*models.StoreRating, error) {
	ctxt := "EngagementService-RateStore"
	response, err := q.engagementRepository.CreateStoreRating(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStoreRating")
	}
	return response, err
}
