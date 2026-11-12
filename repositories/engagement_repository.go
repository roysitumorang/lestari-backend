package repositories

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
)

// EngagementRepository stores missions, referrals, integrations, loan applications and store ratings.
type EngagementRepository interface {
	FindClaimedMissions(ctx context.Context, storeID uuid.UUID) ([]string, error)
	// CountProgress returns the counter of a store that a mission measures (models.Progress…).
	CountProgress(ctx context.Context, storeID uuid.UUID, counter string) (int64, error)
	// CreateMissionClaim records a claim; a second one is ErrMissionClaimed.
	CreateMissionClaim(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, claimedAt time.Time) error
	// FindReferral describes the referral programme of a store; shareURL is the prefix of the link to share.
	FindReferral(ctx context.Context, storeID uuid.UUID, shareURL string) (*models.Referral, error)
	// SetReferralCode gives a store its referral code: ErrReferralCodeTaken when another store has it,
	// ErrReferralCodeExists when the store already has one.
	SetReferralCode(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string) error
	// TransferRewards moves the referral rewards of a store to its premium coins; nil when there are none.
	TransferRewards(ctx context.Context, tx pgx.Tx, storeID uuid.UUID) (*models.RewardTransfer, error)
	FindIntegrationStates(ctx context.Context, storeID uuid.UUID) ([]*models.IntegrationState, error)
	SaveIntegration(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code, status string, settings json.RawMessage, connectedAt *time.Time) error
	DisconnectIntegration(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string) error
	FindLoanApplications(ctx context.Context, storeID uuid.UUID) ([]*models.LoanApplication, error)
	CreateLoanApplication(ctx context.Context, tx pgx.Tx, request *models.LoanApplication) (*models.LoanApplied, error)
	CreateStoreRating(ctx context.Context, tx pgx.Tx, request *models.StoreRating) (*models.StoreRating, error)
}

type engagementRepository struct {
	base
}

func NewEngagementRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) EngagementRepository {
	return &engagementRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

func (q *engagementRepository) FindClaimedMissions(ctx context.Context, storeID uuid.UUID) ([]string, error) {
	ctxt := "EngagementRepository-FindClaimedMissions"
	return queryValue[[]string](ctx, q.dbWrite, ctxt, "SELECT COALESCE(ARRAY_AGG(t.code), '{}') FROM mission_claims t WHERE t.store_id = $1", storeID)
}

// progressQueries count what a mission measures for store $1.
var progressQueries = map[string]string{
	models.ProgressProducts: `SELECT COUNT(1)
	FROM products p
	JOIN branches b ON b.id = p.branch_id
	WHERE b.store_id = $1
		AND p.deleted_at IS NULL`,
	models.ProgressSales: `SELECT COUNT(1)
	FROM sales s
	JOIN branches b ON b.id = s.branch_id
	WHERE b.store_id = $1`,
	models.ProgressStoreLocation: `SELECT COUNT(1)
	FROM settings st
	JOIN branches b ON b.id = st.branch_id
	WHERE b.store_id = $1
		AND st.key = 'store'
		AND st.value->>'latitude' IS NOT NULL`,
	models.ProgressStaff:         "SELECT COUNT(1) FROM users u WHERE u.store_id = $1 AND u.role <> 'owner' AND u.deleted_at IS NULL",
	models.ProgressPhoneVerified: "SELECT COUNT(1) FROM users u WHERE u.store_id = $1 AND u.role = 'owner' AND u.phone_verified",
	models.ProgressTwoFactor:     "SELECT COUNT(1) FROM users u WHERE u.store_id = $1 AND u.role = 'owner' AND u.two_factor_enabled",
}

func (q *engagementRepository) CountProgress(ctx context.Context, storeID uuid.UUID, counter string) (int64, error) {
	ctxt := "EngagementRepository-CountProgress"
	query, ok := progressQueries[counter]
	if !ok {
		return 0, errors.New("unknown mission counter " + counter)
	}
	return queryValue[int64](ctx, q.dbWrite, ctxt, query, storeID)
}

func (q *engagementRepository) CreateMissionClaim(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, claimedAt time.Time) error {
	ctxt := "EngagementRepository-CreateMissionClaim"
	_, err := execute(ctx, tx, ctxt, "INSERT INTO mission_claims (store_id, code, claimed_at) VALUES ($1, $2, $3)", storeID, code, claimedAt)
	if IsUniqueViolation(err) {
		return models.ErrMissionClaimed
	}
	return err
}

func (q *engagementRepository) FindReferral(ctx context.Context, storeID uuid.UUID, shareURL string) (*models.Referral, error) {
	ctxt := "EngagementRepository-FindReferral"
	return queryRow[models.Referral](ctx, q.dbWrite, ctxt, `SELECT t.referral_code AS code
		, CASE WHEN t.referral_code IS NULL THEN NULL ELSE $2 || t.referral_code END AS share_url
		, (SELECT COUNT(1) FROM stores r WHERE r.referred_by_store_id = t.id) AS referred_count
		, t.reward_balance
		, COALESCE((
			SELECT JSON_AGG(JSON_BUILD_OBJECT(
				'store_name', r.name
				, 'joined_at', r.created_at
				, 'status', CASE WHEN r.plan = 'pro' THEN 'subscribed' ELSE 'registered' END
				, 'reward_amount', CASE WHEN r.plan = 'pro' THEN 25000 ELSE 0 END
			) ORDER BY r.created_at DESC)
			FROM stores r
			WHERE r.referred_by_store_id = t.id
		), '[]') AS referred_businesses
	FROM stores t
	WHERE t.id = $1`, storeID, shareURL)
}

func (q *engagementRepository) SetReferralCode(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string) error {
	ctxt := "EngagementRepository-SetReferralCode"
	set, err := execute(ctx, tx, ctxt, "UPDATE stores SET referral_code = $2 WHERE id = $1 AND referral_code IS NULL", storeID, code)
	if IsUniqueViolation(err) {
		return models.ErrReferralCodeTaken
	}
	if err == nil && set == 0 {
		err = models.ErrReferralCodeExists
	}
	return err
}

func (q *engagementRepository) TransferRewards(ctx context.Context, tx pgx.Tx, storeID uuid.UUID) (*models.RewardTransfer, error) {
	ctxt := "EngagementRepository-TransferRewards"
	reward, err := queryValue[float64](ctx, tx, ctxt, "SELECT t.reward_balance FROM stores t WHERE t.id = $1 FOR NO KEY UPDATE", storeID)
	if err != nil || reward <= 0 {
		return nil, err
	}
	return queryRow[models.RewardTransfer](ctx, tx, ctxt, `UPDATE stores SET
		coin_balance = coin_balance + reward_balance
		, reward_balance = 0
	WHERE id = $1
	RETURNING $2::numeric AS transferred
		, coin_balance
		, reward_balance`, storeID, reward)
}

func (q *engagementRepository) FindIntegrationStates(ctx context.Context, storeID uuid.UUID) ([]*models.IntegrationState, error) {
	ctxt := "EngagementRepository-FindIntegrationStates"
	return queryRows[models.IntegrationState](ctx, q.dbWrite, ctxt, `SELECT t.code
		, t.status
		, t.connected_at
	FROM store_integrations t
	WHERE t.store_id = $1`, storeID)
}

func (q *engagementRepository) SaveIntegration(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code, status string, settings json.RawMessage, connectedAt *time.Time) error {
	ctxt := "EngagementRepository-SaveIntegration"
	_, err := execute(ctx, tx, ctxt, `INSERT INTO store_integrations (
		store_id
		, code
		, status
		, settings
		, connected_at
	) VALUES ($1, $2, $3, $4::jsonb, $5)
	ON CONFLICT (store_id, code) DO UPDATE SET
		status = EXCLUDED.status
		, settings = EXCLUDED.settings
		, connected_at = EXCLUDED.connected_at`, storeID, code, status, settings, connectedAt)
	return err
}

func (q *engagementRepository) DisconnectIntegration(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string) error {
	ctxt := "EngagementRepository-DisconnectIntegration"
	_, err := execute(ctx, tx, ctxt, `UPDATE store_integrations SET
		status = 'inactive'
		, connected_at = NULL
	WHERE store_id = $1
		AND code = $2
		AND status <> 'inactive'`, storeID, code)
	return err
}

func (q *engagementRepository) FindLoanApplications(ctx context.Context, storeID uuid.UUID) ([]*models.LoanApplication, error) {
	ctxt := "EngagementRepository-FindLoanApplications"
	return queryRows[models.LoanApplication](ctx, q.dbWrite, ctxt, `SELECT t.id
		, t.provider_code
		, t.amount
		, t.tenor_months
		, t.purpose
		, t.status
		, t.created_at
	FROM loan_applications t
	WHERE t.store_id = $1
	ORDER BY t.created_at DESC`, storeID)
}

func (q *engagementRepository) CreateLoanApplication(ctx context.Context, tx pgx.Tx, request *models.LoanApplication) (*models.LoanApplied, error) {
	ctxt := "EngagementRepository-CreateLoanApplication"
	return queryRow[models.LoanApplied](ctx, tx, ctxt, `INSERT INTO loan_applications (
		store_id
		, provider_code
		, amount
		, tenor_months
		, purpose
		, created_at
	) VALUES ($1, $2, $3, $4, $5, $6)
	RETURNING id, status`, request.StoreID, request.ProviderCode, request.Amount, request.TenorMonths, request.Purpose, request.CreatedAt)
}

func (q *engagementRepository) CreateStoreRating(ctx context.Context, tx pgx.Tx, request *models.StoreRating) (*models.StoreRating, error) {
	ctxt := "EngagementRepository-CreateStoreRating"
	return queryRow[models.StoreRating](ctx, tx, ctxt, `INSERT INTO store_ratings (
		branch_id
		, sale_id
		, customer_name
		, rating
		, comment
		, tags
		, created_at
	) VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7)
	RETURNING id, rating, comment, tags, created_at`,
		request.BranchID, request.SaleID, request.CustomerName, request.Rating, request.Comment, request.Tags, request.CreatedAt)
}
