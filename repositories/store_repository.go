package repositories

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

// StoreRepository changes what belongs to the store as a whole: its plan, premium coins, storage and plugins.
type StoreRepository interface {
	FindStoreName(ctx context.Context, storeID uuid.UUID) (string, error)
	FindCoinBalance(ctx context.Context, tx pgx.Tx, storeID uuid.UUID) (float64, error)
	// SpendCoins deducts premium coins, or returns ErrInsufficientCoins.
	SpendCoins(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, amount float64) error
	AddCoins(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, amount float64) error
	// ExtendPlan sets the plan and adds months and days to its expiry (counted from `at` when it has lapsed);
	// the head office follows the store.
	ExtendPlan(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, plan string, months, days int, at time.Time) error
	ExtendBranches(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, branchIDs []uuid.UUID, months int, at time.Time) error
	// ActivatePlugin subscribes the store to a plugin for months and days more.
	ActivatePlugin(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, months, days int, at time.Time) error
	FindStorePlugins(ctx context.Context, tx pgx.Tx, storeID uuid.UUID) ([]*models.StorePlugin, error)
	// StartPluginTrial starts (or restarts) the free trial that ends at expiresAt.
	StartPluginTrial(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, expiresAt time.Time) (*models.StorePlugin, error)
	FindStorage(ctx context.Context, storeID uuid.UUID) (*models.Storage, error)
	// ExtendStorage sets the quota to the free 500 MB plus extraMB for 30 more days.
	ExtendStorage(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, extraMB float64, at time.Time) (*models.StoragePurchase, error)
	// FindStoreByReferralCode returns the store that owns a referral code, or nil.
	FindStoreByReferralCode(ctx context.Context, tx pgx.Tx, referralCode string) (*uuid.UUID, error)
	// CreateStore creates a store on the Pro trial that ends at trialEndsAt.
	CreateStore(ctx context.Context, tx pgx.Tx, name string, trialEndsAt time.Time, referredByStoreID *uuid.UUID, createdAt time.Time) (uuid.UUID, error)
	CreateHeadOffice(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, name, phone, email string, subscriptionExpiresAt, createdAt time.Time) (uuid.UUID, error)
	// CreateAccount creates a user of a branch; an email in use is ErrEmailTaken.
	CreateAccount(ctx context.Context, tx pgx.Tx, storeID, branchID uuid.UUID, name, email, passwordHash, phone, role string, createdAt time.Time) (uuid.UUID, error)
	// SeedBranch gives a new branch its default payment methods, cash drawer and PPOB price list.
	SeedBranch(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, createdAt time.Time) error
	// ResetData deletes the chosen kinds of data of a branch and returns how many rows went, by kind.
	ResetData(ctx context.Context, tx pgx.Tx, request *models.DataReset) (map[string]int64, error)
}

type storeRepository struct {
	base
}

func NewStoreRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) StoreRepository {
	return &storeRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

func (q *storeRepository) FindStoreName(ctx context.Context, storeID uuid.UUID) (string, error) {
	ctxt := "StoreRepository-FindStoreName"
	return queryValue[string](ctx, q.dbRead, ctxt, "SELECT t.name FROM stores t WHERE t.id = $1", storeID)
}

func (q *storeRepository) FindCoinBalance(ctx context.Context, tx pgx.Tx, storeID uuid.UUID) (float64, error) {
	ctxt := "StoreRepository-FindCoinBalance"
	return queryValue[float64](ctx, q.reader(tx), ctxt, "SELECT t.coin_balance FROM stores t WHERE t.id = $1", storeID)
}

func (q *storeRepository) SpendCoins(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, amount float64) error {
	ctxt := "StoreRepository-SpendCoins"
	spent, err := execute(ctx, tx, ctxt, `UPDATE stores SET
		coin_balance = coin_balance - $2
	WHERE id = $1
		AND coin_balance >= $2`, storeID, amount)
	if err != nil {
		return err
	}
	if spent == 0 {
		return models.ErrInsufficientCoins
	}
	return nil
}

func (q *storeRepository) AddCoins(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, amount float64) error {
	ctxt := "StoreRepository-AddCoins"
	_, err := execute(ctx, tx, ctxt, "UPDATE stores SET coin_balance = coin_balance + $2 WHERE id = $1", storeID, amount)
	return err
}

func (q *storeRepository) ExtendPlan(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, plan string, months, days int, at time.Time) error {
	ctxt := "StoreRepository-ExtendPlan"
	if _, err := execute(ctx, tx, ctxt, `UPDATE stores SET
		plan = $2
		, plan_expires_at = GREATEST(COALESCE(plan_expires_at, $5), $5) + MAKE_INTERVAL(months => $3, days => $4)
		, updated_at = $5
	WHERE id = $1`, storeID, plan, months, days, at); err != nil {
		return err
	}
	_, err := execute(ctx, tx, ctxt, `UPDATE branches SET
		subscription_expires_at = (SELECT s.plan_expires_at FROM stores s WHERE s.id = $1)
	WHERE store_id = $1
		AND is_head_office`, storeID)
	return err
}

func (q *storeRepository) ExtendBranches(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, branchIDs []uuid.UUID, months int, at time.Time) error {
	ctxt := "StoreRepository-ExtendBranches"
	_, err := execute(ctx, tx, ctxt, `UPDATE branches SET
		subscription_expires_at = GREATEST(COALESCE(subscription_expires_at, $4), $4) + MAKE_INTERVAL(months => $3)
	WHERE store_id = $1
		AND id = ANY($2)`, storeID, branchIDs, months, at)
	return err
}

func (q *storeRepository) ActivatePlugin(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, months, days int, at time.Time) error {
	ctxt := "StoreRepository-ActivatePlugin"
	_, err := execute(ctx, tx, ctxt, `INSERT INTO store_plugins (
		store_id
		, code
		, status
		, trial_used
		, expires_at
	) VALUES ($1, $2, 'active', FALSE, $5::timestamptz + MAKE_INTERVAL(months => $3, days => $4))
	ON CONFLICT (store_id, code) DO UPDATE SET
		status = 'active'
		, expires_at = GREATEST(COALESCE(store_plugins.expires_at, $5), $5) + MAKE_INTERVAL(months => $3, days => $4)`, storeID, code, months, days, at)
	return err
}

func (q *storeRepository) FindStorePlugins(ctx context.Context, tx pgx.Tx, storeID uuid.UUID) ([]*models.StorePlugin, error) {
	ctxt := "StoreRepository-FindStorePlugins"
	return queryRows[models.StorePlugin](ctx, q.reader(tx), ctxt, `SELECT t.code
		, t.status
		, t.trial_used
		, t.expires_at
	FROM store_plugins t
	WHERE t.store_id = $1`, storeID)
}

func (q *storeRepository) StartPluginTrial(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, expiresAt time.Time) (*models.StorePlugin, error) {
	ctxt := "StoreRepository-StartPluginTrial"
	return queryRow[models.StorePlugin](ctx, tx, ctxt, `INSERT INTO store_plugins (
		store_id
		, code
		, status
		, trial_used
		, expires_at
	) VALUES ($1, $2, 'trial', TRUE, $3)
	ON CONFLICT (store_id, code) DO UPDATE SET
		status = 'trial'
		, trial_used = TRUE
		, expires_at = EXCLUDED.expires_at
	RETURNING code, status, trial_used, expires_at`, storeID, code, expiresAt)
}

func (q *storeRepository) FindStorage(ctx context.Context, storeID uuid.UUID) (*models.Storage, error) {
	ctxt := "StoreRepository-FindStorage"
	response, err := queryRow[models.Storage](ctx, q.dbRead, ctxt, `SELECT t.plan
		, ROUND((
			(SELECT COALESCE(SUM(u.size), 0) FROM uploads u WHERE u.store_id = t.id) / 1048576.0
			+ (SELECT COUNT(1) FROM sales sa JOIN branches b ON b.id = sa.branch_id WHERE b.store_id = t.id) * 0.004
			+ (SELECT COUNT(1) FROM products pr JOIN branches b ON b.id = pr.branch_id WHERE b.store_id = t.id) * 0.002
		)::numeric, 2) AS used_mb
		, t.storage_quota_mb AS quota_mb
		, (SELECT COUNT(1) FROM sales sa JOIN branches b ON b.id = sa.branch_id WHERE b.store_id = t.id) AS transaction_count
		, t.storage_extra_expires_at::text AS expires_at
	FROM stores t
	WHERE t.id = $1`, storeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &models.Storage{}, nil
	}
	return response, err
}

func (q *storeRepository) ExtendStorage(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, extraMB float64, at time.Time) (*models.StoragePurchase, error) {
	ctxt := "StoreRepository-ExtendStorage"
	return queryRow[models.StoragePurchase](ctx, tx, ctxt, `UPDATE stores SET
		storage_quota_mb = 500 + $2
		, storage_extra_expires_at = GREATEST(COALESCE(storage_extra_expires_at, $3::date), $3::date) + 30
		, updated_at = $4
	WHERE id = $1
	RETURNING storage_quota_mb AS quota_mb
		, storage_extra_expires_at::text AS expires_at
		, coin_balance`, storeID, extraMB, at.In(helperLocation()).Format(time.DateOnly), at)
}

// resetStatements are the statements that delete one kind of data. $1 is the branch, $2 and $3 bound the dated
// kinds, $4 is the time of the reset.
var resetStatements = map[string][]string{
	"images":   {"UPDATE products SET image_url = NULL WHERE branch_id = $1 AND image_url IS NOT NULL"},
	"products": {"UPDATE products SET deleted_at = $4 WHERE branch_id = $1 AND deleted_at IS NULL"},
	"sales": {
		"DELETE FROM returns WHERE branch_id = $1 AND kind = 'sale' AND created_at::date BETWEEN $2::date AND $3::date",
		"DELETE FROM sales WHERE branch_id = $1 AND created_at::date BETWEEN $2::date AND $3::date",
	},
	"purchases": {
		"DELETE FROM returns WHERE branch_id = $1 AND kind = 'purchase' AND created_at::date BETWEEN $2::date AND $3::date",
		"DELETE FROM purchases WHERE branch_id = $1 AND purchase_date BETWEEN $2::date AND $3::date",
	},
	"customers":  {"DELETE FROM customers WHERE branch_id = $1"},
	"suppliers":  {"DELETE FROM suppliers WHERE branch_id = $1"},
	"staff":      {"UPDATE users SET deleted_at = $4 WHERE branch_id = $1 AND role <> 'owner' AND deleted_at IS NULL"},
	"taxes":      {"DELETE FROM taxes WHERE branch_id = $1"},
	"discounts":  {"DELETE FROM discounts WHERE branch_id = $1"},
	"categories": {"DELETE FROM categories WHERE branch_id = $1"},
	"fees":       {"DELETE FROM fees WHERE branch_id = $1"},
	"promotions": {"DELETE FROM promotions WHERE branch_id = $1"},
	"shifts":     {"DELETE FROM shifts WHERE branch_id = $1 AND opened_at::date BETWEEN $2::date AND $3::date"},
	"points": {
		"DELETE FROM point_ledger WHERE customer_id IN (SELECT c.id FROM customers c WHERE c.branch_id = $1)",
		"UPDATE customers SET points = 0 WHERE branch_id = $1 AND points <> 0",
	},
	"online_orders": {"DELETE FROM catalog_orders WHERE branch_id = $1 AND created_at::date BETWEEN $2::date AND $3::date"},
	// held bills live in the cashier app
	"held_transactions": {},
}

func (q *storeRepository) ResetData(ctx context.Context, tx pgx.Tx, request *models.DataReset) (map[string]int64, error) {
	ctxt := "StoreRepository-ResetData"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	response := map[string]int64{}
	for _, dataType := range request.DataTypes {
		for _, statement := range resetStatements[dataType] {
			// every statement gets the four parameters; the casts keep the unused ones typed
			sb.Reset()
			_, _ = sb.WriteString("WITH params AS (SELECT $1::uuid, $2::date, $3::date, $4::timestamptz) ")
			_, _ = sb.WriteString(statement)
			deleted, err := execute(ctx, tx, ctxt, sb.String(), request.BranchID, request.StartDate, request.EndDate, request.At)
			if err != nil {
				return nil, err
			}
			response[dataType] += deleted
		}
	}
	return response, nil
}

func (q *storeRepository) FindStoreByReferralCode(ctx context.Context, tx pgx.Tx, referralCode string) (*uuid.UUID, error) {
	ctxt := "StoreRepository-FindStoreByReferralCode"
	response, err := queryValue[uuid.UUID](ctx, q.reader(tx), ctxt, "SELECT t.id FROM stores t WHERE UPPER(t.referral_code) = UPPER($1)", referralCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &response, err
}

func (q *storeRepository) CreateStore(ctx context.Context, tx pgx.Tx, name string, trialEndsAt time.Time, referredByStoreID *uuid.UUID, createdAt time.Time) (uuid.UUID, error) {
	ctxt := "StoreRepository-CreateStore"
	return queryValue[uuid.UUID](ctx, tx, ctxt, `INSERT INTO stores (
		name
		, plan
		, plan_expires_at
		, referred_by_store_id
		, created_at
		, updated_at
	) VALUES ($1, 'pro_trial', $2, $3, $4, $4)
	RETURNING id`, name, trialEndsAt, referredByStoreID, createdAt)
}

func (q *storeRepository) CreateHeadOffice(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, name, phone, email string, subscriptionExpiresAt, createdAt time.Time) (uuid.UUID, error) {
	ctxt := "StoreRepository-CreateHeadOffice"
	return queryValue[uuid.UUID](ctx, tx, ctxt, `INSERT INTO branches (
		store_id
		, name
		, code
		, is_head_office
		, phone
		, email
		, subscription_expires_at
		, created_at
		, updated_at
	) VALUES ($1, $2, 'HQ', TRUE, $3, $4, $5, $6, $6)
	RETURNING id`, storeID, name, phone, email, subscriptionExpiresAt, createdAt)
}

func (q *storeRepository) CreateAccount(ctx context.Context, tx pgx.Tx, storeID, branchID uuid.UUID, name, email, passwordHash, phone, role string, createdAt time.Time) (uuid.UUID, error) {
	ctxt := "StoreRepository-CreateAccount"
	response, err := queryValue[uuid.UUID](ctx, tx, ctxt, `INSERT INTO users (
		store_id
		, branch_id
		, name
		, email
		, password_hash
		, phone
		, role
		, notifications
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, LOWER($4), $5, NULLIF($6, ''), $7, $8::jsonb, $9, $9)
	RETURNING id`, storeID, branchID, name, email, passwordHash, phone, role, models.DefaultNotifications, createdAt)
	if IsUniqueViolation(err) {
		return response, models.ErrEmailTaken
	}
	return response, err
}

func (q *storeRepository) SeedBranch(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, createdAt time.Time) error {
	ctxt := "StoreRepository-SeedBranch"
	if _, err := execute(ctx, tx, ctxt, `INSERT INTO payment_methods (branch_id, name, type, created_at, updated_at) VALUES
		($1, 'Cash', 'cash', $2, $2)
		, ($1, 'Debit card', 'debit', $2, $2)
		, ($1, 'QRIS', 'qris', $2, $2)
		, ($1, 'Bank transfer', 'bank_transfer', $2, $2)`, branchID, createdAt); err != nil {
		return err
	}
	if _, err := execute(ctx, tx, ctxt, "INSERT INTO cash_drawers (branch_id, name, created_at, updated_at) VALUES ($1, 'Main drawer', $2, $2)", branchID, createdAt); err != nil {
		return err
	}
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO ppob_prices (
		branch_id
		, kind
		, category
		, provider
		, product_code
		, product_name
		, base_price
		, selling_price
		, base_admin_fee
		, admin_fee
	) VALUES `)
	params := make([]any, 0, 1+len(models.PPOBCatalog)*9)
	params = append(params, branchID)
	for i, item := range models.PPOBCatalog {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		params = append(params, item.Kind, item.Category, item.Provider, item.Code, item.Name, item.BasePrice, item.SellingPrice, item.BaseAdminFee, item.AdminFee)
		n := len(params)
		_, _ = sb.WriteString(sprintf("($1, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)", n-8, n-7, n-6, n-5, n-4, n-3, n-2, n-1, n))
	}
	_, _ = sb.WriteString(" ON CONFLICT DO NOTHING")
	_, err := execute(ctx, tx, ctxt, sb.String(), params...)
	return err
}
