package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type OTPService interface {
	// CreateOTP stores a new six-digit code and returns it for sending; one code a minute per purpose.
	CreateOTP(ctx context.Context, userID uuid.UUID, purpose, target string, at time.Time) (string, error)
	// CheckOTP consumes a valid code. A code can be tried five times.
	CheckOTP(ctx context.Context, userID uuid.UUID, purpose, target, code string, at time.Time) error
}

type otpService struct {
	otpRepository repositories.OTPRepository
}

func NewOTPService(
	otpRepository repositories.OTPRepository,
) OTPService {
	return &otpService{
		otpRepository: otpRepository,
	}
}

func (q *otpService) CreateOTP(ctx context.Context, userID uuid.UUID, purpose, target string, at time.Time) (string, error) {
	ctxt := "OTPService-CreateOTP"
	recent, err := q.otpRepository.CountOTPsSince(ctx, userID, purpose, at.Add(-time.Minute))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCountOTPsSince")
		return "", err
	}
	if recent > 0 {
		return "", models.ErrOTPTooSoon
	}
	code := middlewares.RandomDigits(6)
	if err = q.otpRepository.CreateOTP(ctx, userID, purpose, target, middlewares.SHA256(code), at, at.Add(models.OTPLifetime*time.Second)); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateOTP")
		return "", err
	}
	return code, nil
}

func (q *otpService) CheckOTP(ctx context.Context, userID uuid.UUID, purpose, target, code string, at time.Time) error {
	ctxt := "OTPService-CheckOTP"
	otpID, codeHash, err := q.otpRepository.FindUsableOTP(ctx, userID, purpose, target, at)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.ErrOTPExpired
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindUsableOTP")
		return err
	}
	if codeHash != middlewares.SHA256(strings.TrimSpace(code)) {
		if err = q.otpRepository.CountAttempt(ctx, otpID); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCountAttempt")
		}
		return models.ErrOTPIncorrect
	}
	if err = q.otpRepository.UseOTP(ctx, otpID, at); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUseOTP")
	}
	return err
}
