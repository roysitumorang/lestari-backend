package repositories

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OTPRepository stores one-time codes (as hashes). A code belongs to a user and a purpose and, for a phone
// verification, to the number it was sent to.
type OTPRepository interface {
	CountOTPsSince(ctx context.Context, userID uuid.UUID, purpose string, since time.Time) (int64, error)
	CreateOTP(ctx context.Context, userID uuid.UUID, purpose, target, codeHash string, createdAt, expiresAt time.Time) error
	// FindUsableOTP returns the newest code that is unused, not expired at `at` and was tried fewer than five times.
	FindUsableOTP(ctx context.Context, userID uuid.UUID, purpose, target string, at time.Time) (id uuid.UUID, codeHash string, err error)
	CountAttempt(ctx context.Context, otpID uuid.UUID) error
	UseOTP(ctx context.Context, otpID uuid.UUID, usedAt time.Time) error
}

type otpRepository struct {
	base
}

func NewOTPRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) OTPRepository {
	return &otpRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

func (q *otpRepository) CountOTPsSince(ctx context.Context, userID uuid.UUID, purpose string, since time.Time) (int64, error) {
	ctxt := "OTPRepository-CountOTPsSince"
	return queryValue[int64](ctx, q.dbWrite, ctxt, `SELECT COUNT(1)
	FROM otp_codes t
	WHERE t.user_id = $1
		AND t.purpose = $2
		AND t.created_at > $3`, userID, purpose, since)
}

func (q *otpRepository) CreateOTP(ctx context.Context, userID uuid.UUID, purpose, target, codeHash string, createdAt, expiresAt time.Time) error {
	ctxt := "OTPRepository-CreateOTP"
	_, err := execute(ctx, q.dbWrite, ctxt, `INSERT INTO otp_codes (
		user_id
		, purpose
		, target
		, code_hash
		, created_at
		, expires_at
	) VALUES ($1, $2, $3, $4, $5, $6)`, userID, purpose, target, codeHash, createdAt, expiresAt)
	return err
}

func (q *otpRepository) FindUsableOTP(ctx context.Context, userID uuid.UUID, purpose, target string, at time.Time) (id uuid.UUID, codeHash string, err error) {
	ctxt := "OTPRepository-FindUsableOTP"
	type otp struct {
		ID       uuid.UUID
		CodeHash string
	}
	response, err := queryRow[otp](ctx, q.dbWrite, ctxt, `SELECT t.id
		, t.code_hash
	FROM otp_codes t
	WHERE t.user_id = $1
		AND t.purpose = $2
		AND ($3 = '' OR t.target = $3)
		AND t.used_at IS NULL
		AND t.expires_at > $4
		AND t.attempts < 5
	ORDER BY t.id DESC
	LIMIT 1`, userID, purpose, target, at)
	if err != nil {
		return id, "", err
	}
	return response.ID, response.CodeHash, nil
}

func (q *otpRepository) CountAttempt(ctx context.Context, otpID uuid.UUID) error {
	ctxt := "OTPRepository-CountAttempt"
	_, err := execute(ctx, q.dbWrite, ctxt, "UPDATE otp_codes SET attempts = attempts + 1 WHERE id = $1", otpID)
	return err
}

func (q *otpRepository) UseOTP(ctx context.Context, otpID uuid.UUID, usedAt time.Time) error {
	ctxt := "OTPRepository-UseOTP"
	_, err := execute(ctx, q.dbWrite, ctxt, "UPDATE otp_codes SET used_at = $2 WHERE id = $1", otpID, usedAt)
	return err
}
