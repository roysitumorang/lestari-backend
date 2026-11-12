package services

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// trialDays is how long the Pro trial of a new store lasts.
const trialDays = 14

type AuthService interface {
	// Login checks the password and, when two-step verification is on, the authenticator or a recovery code.
	Login(ctx context.Context, request *models.LoginRequest, at time.Time) (*models.Session, *models.Credentials, error)
	Refresh(ctx context.Context, refreshToken string) (*models.RefreshedSession, error)
	// Logout ends every session of the user.
	Logout(ctx context.Context, userID uuid.UUID) error
	FindUser(ctx context.Context, userID uuid.UUID, branchID *uuid.UUID) (*models.User, error)
	ChangePassword(ctx context.Context, tx pgx.Tx, userID uuid.UUID, request *models.PasswordRequest, at time.Time) error
	// Provision creates a store with its head office, its owner and the default data of a branch.
	Provision(ctx context.Context, tx pgx.Tx, request *models.Provision) (userID, storeID uuid.UUID, err error)
	// OpenSession signs a user in without a password check (after registration, for seeding).
	OpenSession(ctx context.Context, tx pgx.Tx, userID, storeID uuid.UUID) (*models.Session, error)
}

type authService struct {
	tokens            *middlewares.Service
	userRepository    repositories.UserRepository
	storeRepository   repositories.StoreRepository
	settingRepository repositories.SettingRepository
}

func NewAuthService(
	tokens *middlewares.Service,
	userRepository repositories.UserRepository,
	storeRepository repositories.StoreRepository,
	settingRepository repositories.SettingRepository,
) AuthService {
	return &authService{
		tokens:            tokens,
		userRepository:    userRepository,
		storeRepository:   storeRepository,
		settingRepository: settingRepository,
	}
}

func (q *authService) Login(ctx context.Context, request *models.LoginRequest, at time.Time) (*models.Session, *models.Credentials, error) {
	ctxt := "AuthService-Login"
	credentials, err := q.userRepository.FindCredentials(ctx, strings.TrimSpace(request.Email))
	if err != nil && !errors.Is(err, models.ErrUserNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCredentials")
		return nil, nil, err
	}
	// one answer for an unknown email and a wrong password
	if err != nil || !middlewares.CheckPassword(credentials.PasswordHash, request.Password) {
		return nil, nil, helper.Unauthorized("invalid_credentials", "Email or password is incorrect.")
	}
	if credentials.Status == models.UserStatusDisabled {
		return nil, nil, helper.Forbidden("This account has been disabled.")
	}
	if credentials.TwoFactorEnabled && credentials.TwoFactorSecret != nil {
		if request.OTP == "" {
			return nil, nil, helper.Unauthorized("two_factor_required", "Enter the code from your authenticator app.")
		}
		if !middlewares.VerifyTOTP(*credentials.TwoFactorSecret, request.OTP) {
			// a recovery code works once
			used := slices.Index(credentials.RecoveryCodes, middlewares.SHA256(strings.TrimSpace(request.OTP)))
			if used < 0 {
				return nil, nil, helper.Unauthorized("invalid_otp", "The verification code is incorrect.")
			}
			if err = q.userRepository.UpdateRecoveryCodes(ctx, credentials.ID, slices.Delete(credentials.RecoveryCodes, used, used+1)); err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateRecoveryCodes")
				return nil, nil, err
			}
		}
	}
	tokens, err := q.tokens.Issue(ctx, credentials.ID.String(), credentials.StoreID.String(), request.RememberMe)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrIssue")
		return nil, nil, err
	}
	if err = q.userRepository.RecordLogin(ctx, credentials.ID, at); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRecordLogin")
		return nil, nil, err
	}
	user, err := q.userRepository.FindUser(ctx, nil, credentials.ID, nil)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindUser")
		return nil, nil, err
	}
	return newSession(tokens, user), credentials, nil
}

func newSession(tokens middlewares.Tokens, user *models.User) *models.Session {
	return &models.Session{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    tokens.TokenType,
		ExpiresIn:    tokens.ExpiresIn,
		User:         user,
	}
}

func (q *authService) Refresh(ctx context.Context, refreshToken string) (*models.RefreshedSession, error) {
	ctxt := "AuthService-Refresh"
	tokens, err := q.tokens.Refresh(ctx, refreshToken)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRefresh")
		return nil, err
	}
	return &models.RefreshedSession{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, ExpiresIn: tokens.ExpiresIn}, nil
}

func (q *authService) Logout(ctx context.Context, userID uuid.UUID) error {
	ctxt := "AuthService-Logout"
	err := q.tokens.RevokeAll(ctx, userID.String())
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRevokeAll")
	}
	return err
}

func (q *authService) FindUser(ctx context.Context, userID uuid.UUID, branchID *uuid.UUID) (*models.User, error) {
	ctxt := "AuthService-FindUser"
	response, err := q.userRepository.FindUser(ctx, nil, userID, branchID)
	if err != nil && !errors.Is(err, models.ErrUserNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindUser")
	}
	return response, err
}

func (q *authService) ChangePassword(ctx context.Context, tx pgx.Tx, userID uuid.UUID, request *models.PasswordRequest, at time.Time) error {
	ctxt := "AuthService-ChangePassword"
	passwordHash, err := q.userRepository.FindPasswordHash(ctx, userID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPasswordHash")
		return err
	}
	v := &helper.Validation{}
	if !middlewares.CheckPassword(passwordHash, request.CurrentPassword) {
		v.Add("current_password", "Current password is incorrect.")
	}
	if len(request.NewPassword) < 8 {
		v.Add("new_password", "Use at least 8 characters.")
	}
	if request.NewPassword != request.NewPasswordConfirmation {
		v.Add("new_password_confirmation", "Passwords do not match.")
	}
	if err = v.Err(); err != nil {
		return err
	}
	if passwordHash, err = middlewares.HashPassword(request.NewPassword); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrHashPassword")
		return err
	}
	if err = q.userRepository.UpdatePassword(ctx, tx, userID, passwordHash, at); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePassword")
	}
	return err
}

func (q *authService) Provision(ctx context.Context, tx pgx.Tx, request *models.Provision) (userID, storeID uuid.UUID, err error) {
	ctxt := "AuthService-Provision"
	storeName, ownerName := strings.TrimSpace(request.StoreName), strings.TrimSpace(request.OwnerName)
	var referredBy *uuid.UUID
	if request.ReferralCode != "" {
		if referredBy, err = q.storeRepository.FindStoreByReferralCode(ctx, tx, request.ReferralCode); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStoreByReferralCode")
			return
		}
	}
	trialEndsAt := request.At.AddDate(0, 0, trialDays)
	if storeID, err = q.storeRepository.CreateStore(ctx, tx, storeName, trialEndsAt, referredBy, request.At); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStore")
		return
	}
	branchID, err := q.storeRepository.CreateHeadOffice(ctx, tx, storeID, storeName, request.Phone, request.Email, trialEndsAt, request.At)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateHeadOffice")
		return
	}
	passwordHash, err := middlewares.HashPassword(request.Password)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrHashPassword")
		return
	}
	if userID, err = q.storeRepository.CreateAccount(ctx, tx, storeID, branchID, ownerName, strings.TrimSpace(request.Email), passwordHash, request.Phone, models.RoleOwner, request.At); err != nil {
		if errors.Is(err, models.ErrEmailTaken) {
			err = helper.Invalid("email", "This email is already registered.")
			return
		}
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateAccount")
		return
	}
	if err = q.storeRepository.SeedBranch(ctx, tx, branchID, request.At); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSeedBranch")
		return
	}
	store := models.NewSetting(models.SettingStore).(*models.StoreSetting)
	store.Name = storeName
	store.OwnerName = &ownerName
	store.Phone = &request.Phone
	value, err := marshalSetting(store, nil)
	if err == nil {
		err = q.settingRepository.SaveSetting(ctx, tx, branchID, models.SettingStore, value, request.At)
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSaveSetting")
	}
	return
}

func (q *authService) OpenSession(ctx context.Context, tx pgx.Tx, userID, storeID uuid.UUID) (*models.Session, error) {
	ctxt := "AuthService-OpenSession"
	tokens, err := q.tokens.Issue(ctx, userID.String(), storeID.String(), true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrIssue")
		return nil, err
	}
	user, err := q.userRepository.FindUser(ctx, tx, userID, nil)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindUser")
		return nil, err
	}
	return newSession(tokens, user), nil
}
