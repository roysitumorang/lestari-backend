package repositories

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
)

// UserRepository reads and changes the account of a user: credentials, profile and two-step verification.
// Staff management has its own repository.
type UserRepository interface {
	// FindCredentials finds an account that is not deleted by its email, ignoring case.
	FindCredentials(ctx context.Context, email string) (*models.Credentials, error)
	// FindUser describes a user with their store, plugins and the branch they work in (branchID, or their own).
	FindUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID, branchID *uuid.UUID) (*models.User, error)
	FindPasswordHash(ctx context.Context, userID uuid.UUID) (string, error)
	UpdatePassword(ctx context.Context, tx pgx.Tx, userID uuid.UUID, passwordHash string, updatedAt time.Time) error
	UpdatePPOBPin(ctx context.Context, tx pgx.Tx, userID uuid.UUID, pinHash string) error
	UpdateRecoveryCodes(ctx context.Context, userID uuid.UUID, recoveryCodes []string) error
	// RecordLogin stores the time of a sign-in and activates an invited account.
	RecordLogin(ctx context.Context, userID uuid.UUID, at time.Time) error
	FindProfile(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*models.Profile, error)
	// UpdateProfile writes only the attributes that differ from the stored ones.
	UpdateProfile(ctx context.Context, tx pgx.Tx, request *models.Profile) (*models.Profile, error)
	// FindContact returns the email and phone a code can be sent to.
	FindContact(ctx context.Context, userID uuid.UUID) (email string, phone *string, err error)
	VerifyPhone(ctx context.Context, tx pgx.Tx, userID uuid.UUID, phone string) error
	// SetPendingTwoFactor stores the secret of a setup that is not confirmed yet.
	SetPendingTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, secret string) error
	FindPendingTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*string, error)
	EnableTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, recoveryCodes []string) error
	DisableTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error
	CreateDeactivationRequest(ctx context.Context, tx pgx.Tx, storeID, userID uuid.UUID, createdAt time.Time) (*models.DeactivationRequest, error)
}

type userRepository struct {
	base
}

func NewUserRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) UserRepository {
	return &userRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

func (q *userRepository) FindCredentials(ctx context.Context, email string) (*models.Credentials, error) {
	ctxt := "UserRepository-FindCredentials"
	response, err := queryRow[models.Credentials](ctx, q.dbRead, ctxt, `SELECT t.id
		, t.store_id
		, t.password_hash
		, t.status
		, t.two_factor_enabled
		, t.two_factor_secret
		, t.recovery_codes
	FROM users t
	WHERE LOWER(t.email) = LOWER($1)
		AND t.deleted_at IS NULL`, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrUserNotFound
	}
	return response, err
}

func (q *userRepository) FindUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID, branchID *uuid.UUID) (*models.User, error) {
	ctxt := "UserRepository-FindUser"
	response, err := queryRow[models.User](ctx, q.reader(tx), ctxt, `SELECT t.id
		, t.name
		, t.email
		, t.role
		, t.avatar_url
		, t.permissions
		, t.two_factor_enabled
		, t.phone
		, t.phone_verified
		, JSON_BUILD_OBJECT(
			'id', s.id
			, 'name', s.name
			, 'logo_url', (
				SELECT st.value->>'logo_url'
				FROM settings st
				JOIN branches hb ON hb.id = st.branch_id
				WHERE hb.store_id = s.id
					AND hb.is_head_office
					AND st.key = 'store'
			)
			, 'plan', s.plan
			, 'plan_expires_at', s.plan_expires_at
		) AS store
		, (
			SELECT JSON_BUILD_OBJECT('id', b.id, 'name', b.name, 'is_head_office', b.is_head_office)
			FROM branches b
			WHERE b.id = COALESCE($2::uuid, t.branch_id, (
				SELECT hb.id FROM branches hb WHERE hb.store_id = s.id AND hb.is_head_office ORDER BY hb.id LIMIT 1
			))
		) AS branch
		, COALESCE((
			SELECT ARRAY_AGG(sp.code ORDER BY sp.code)
			FROM store_plugins sp
			WHERE sp.store_id = s.id
				AND sp.status IN ('active', 'trial')
				AND (sp.expires_at IS NULL OR sp.expires_at > NOW())
		), '{}') AS active_plugins
	FROM users t
	JOIN stores s ON s.id = t.store_id
	WHERE t.id = $1`, userID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if response.Role == models.RoleOwner || response.Role == models.RoleAdministrator {
		response.Permissions = []string{"*"}
	}
	return response, nil
}

func (q *userRepository) FindPasswordHash(ctx context.Context, userID uuid.UUID) (string, error) {
	ctxt := "UserRepository-FindPasswordHash"
	response, err := queryValue[string](ctx, q.dbRead, ctxt, "SELECT t.password_hash FROM users t WHERE t.id = $1", userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", models.ErrUserNotFound
	}
	return response, err
}

func (q *userRepository) UpdatePassword(ctx context.Context, tx pgx.Tx, userID uuid.UUID, passwordHash string, updatedAt time.Time) error {
	ctxt := "UserRepository-UpdatePassword"
	_, err := execute(ctx, q.writer(tx), ctxt, "UPDATE users SET password_hash = $2, updated_at = $3 WHERE id = $1", userID, passwordHash, updatedAt)
	return err
}

func (q *userRepository) UpdatePPOBPin(ctx context.Context, tx pgx.Tx, userID uuid.UUID, pinHash string) error {
	ctxt := "UserRepository-UpdatePPOBPin"
	_, err := execute(ctx, q.writer(tx), ctxt, "UPDATE users SET ppob_pin_hash = $2 WHERE id = $1", userID, pinHash)
	return err
}

func (q *userRepository) UpdateRecoveryCodes(ctx context.Context, userID uuid.UUID, recoveryCodes []string) error {
	ctxt := "UserRepository-UpdateRecoveryCodes"
	_, err := execute(ctx, q.dbWrite, ctxt, "UPDATE users SET recovery_codes = $2 WHERE id = $1", userID, recoveryCodes)
	return err
}

func (q *userRepository) RecordLogin(ctx context.Context, userID uuid.UUID, at time.Time) error {
	ctxt := "UserRepository-RecordLogin"
	_, err := execute(ctx, q.dbWrite, ctxt, `UPDATE users SET
		last_login_at = $2
		, status = CASE WHEN status = 'invited' THEN 'active' ELSE status END
	WHERE id = $1`, userID, at)
	return err
}

func (q *userRepository) FindProfile(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*models.Profile, error) {
	ctxt := "UserRepository-FindProfile"
	response, err := queryRow[models.Profile](ctx, q.reader(tx), ctxt, `SELECT t.id
		, t.avatar_url
		, t.name
		, t.username
		, t.email
		, t.phone
		, t.phone_verified
		, t.birth_year::bigint AS birth_year
		, t.gender
		, t.address
		, t.notifications
		, t.two_factor_enabled
		, t.updated_at
	FROM users t
	WHERE t.id = $1`, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrUserNotFound
	}
	return response, err
}

func (q *userRepository) UpdateProfile(ctx context.Context, tx pgx.Tx, request *models.Profile) (*models.Profile, error) {
	ctxt := "UserRepository-UpdateProfile"
	current, err := queryRow[models.Profile](ctx, tx, ctxt, `SELECT t.avatar_url
		, t.name
		, t.username
		, t.phone
		, t.phone_verified
		, t.birth_year::bigint AS birth_year
		, t.gender
		, t.address
		, t.notifications
	FROM users t
	WHERE t.id = $1
	FOR NO KEY UPDATE`, request.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	var update changes
	update.set(current.Name != request.Name, "name", request.Name)
	update.set(!equalPtr(current.Username, request.Username), "username", request.Username)
	update.set(!equalPtr(current.AvatarURL, request.AvatarURL), "avatar_url", request.AvatarURL)
	update.set(!equalPtr(current.BirthYear, request.BirthYear), "birth_year", request.BirthYear)
	update.set(!equalPtr(current.Gender, request.Gender), "gender", request.Gender)
	update.set(!equalPtr(current.Address, request.Address), "address", request.Address)
	update.set(!equalPtr(current.Phone, request.Phone), "phone", request.Phone)
	update.set(current.PhoneVerified != request.PhoneVerified, "phone_verified", request.PhoneVerified)
	update.set(!equalJSON(current.Notifications, request.Notifications), "notifications", request.Notifications, "::jsonb")
	if err = update.apply(ctx, tx, ctxt, "users", request.ID, request.UpdatedAt); err != nil {
		return nil, err
	}
	return q.FindProfile(ctx, tx, request.ID)
}

func (q *userRepository) FindContact(ctx context.Context, userID uuid.UUID) (email string, phone *string, err error) {
	ctxt := "UserRepository-FindContact"
	type contact struct {
		Email string
		Phone *string
	}
	response, err := queryRow[contact](ctx, q.dbRead, ctxt, "SELECT t.email, t.phone FROM users t WHERE t.id = $1", userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, models.ErrUserNotFound
	}
	if err != nil {
		return "", nil, err
	}
	return response.Email, response.Phone, nil
}

func (q *userRepository) VerifyPhone(ctx context.Context, tx pgx.Tx, userID uuid.UUID, phone string) error {
	ctxt := "UserRepository-VerifyPhone"
	_, err := execute(ctx, q.writer(tx), ctxt, "UPDATE users SET phone = $2, phone_verified = TRUE WHERE id = $1", userID, phone)
	return err
}

func (q *userRepository) SetPendingTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, secret string) error {
	ctxt := "UserRepository-SetPendingTwoFactor"
	_, err := execute(ctx, q.writer(tx), ctxt, "UPDATE users SET two_factor_pending = $2 WHERE id = $1", userID, secret)
	return err
}

func (q *userRepository) FindPendingTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*string, error) {
	ctxt := "UserRepository-FindPendingTwoFactor"
	response, err := queryValue[*string](ctx, q.reader(tx), ctxt, "SELECT t.two_factor_pending FROM users t WHERE t.id = $1", userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrUserNotFound
	}
	return response, err
}

func (q *userRepository) EnableTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, recoveryCodes []string) error {
	ctxt := "UserRepository-EnableTwoFactor"
	_, err := execute(ctx, q.writer(tx), ctxt, `UPDATE users SET
		two_factor_secret = two_factor_pending
		, two_factor_pending = NULL
		, two_factor_enabled = TRUE
		, recovery_codes = $2
	WHERE id = $1`, userID, recoveryCodes)
	return err
}

func (q *userRepository) DisableTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	ctxt := "UserRepository-DisableTwoFactor"
	_, err := execute(ctx, q.writer(tx), ctxt, `UPDATE users SET
		two_factor_enabled = FALSE
		, two_factor_secret = NULL
		, recovery_codes = '{}'
	WHERE id = $1`, userID)
	return err
}

func (q *userRepository) CreateDeactivationRequest(ctx context.Context, tx pgx.Tx, storeID, userID uuid.UUID, createdAt time.Time) (*models.DeactivationRequest, error) {
	ctxt := "UserRepository-CreateDeactivationRequest"
	return queryRow[models.DeactivationRequest](ctx, q.writer(tx), ctxt, `INSERT INTO deactivation_requests (
		store_id
		, user_id
		, created_at
	) VALUES ($1, $2, $3)
	RETURNING id, status, created_at`, storeID, userID, createdAt)
}
