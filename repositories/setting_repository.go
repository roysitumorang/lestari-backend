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

// SettingRepository stores the settings of a branch: one JSON document per key.
type SettingRepository interface {
	// FindSetting returns the stored document, or nil when the branch never saved this setting.
	FindSetting(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, key string) (json.RawMessage, error)
	SaveSetting(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, key string, value json.RawMessage, updatedAt time.Time) error
	// SlugTaken tells whether another branch uses value for an attribute (subdomain, username) of a setting.
	SlugTaken(ctx context.Context, branchID uuid.UUID, key, attribute, value string) (bool, error)
	// SyncBranchProfile copies the store profile to the branch: phone and address, and the name unless it is the
	// head office.
	SyncBranchProfile(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, name, phone, address string, updatedAt time.Time) error
	SyncStoreName(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, name string, updatedAt time.Time) error
	FindLinkPageVisits(ctx context.Context, branchID uuid.UUID, limit, page int64) ([]*models.LinkPageVisit, int64, error)
	// FindMenuItems lists the products shown online, by category; categoryIDs narrows them when not empty.
	FindMenuItems(ctx context.Context, branchID uuid.UUID, categoryIDs []uuid.UUID) ([]*models.MenuItem, error)
}

type settingRepository struct {
	base
}

func NewSettingRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) SettingRepository {
	return &settingRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

func (q *settingRepository) FindSetting(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, key string) (json.RawMessage, error) {
	ctxt := "SettingRepository-FindSetting"
	response, err := queryValue[json.RawMessage](ctx, q.reader(tx), ctxt, "SELECT t.value FROM settings t WHERE t.branch_id = $1 AND t.key = $2", branchID, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return response, err
}

func (q *settingRepository) SaveSetting(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, key string, value json.RawMessage, updatedAt time.Time) error {
	ctxt := "SettingRepository-SaveSetting"
	_, err := execute(ctx, q.writer(tx), ctxt, `INSERT INTO settings (
		branch_id
		, key
		, value
		, updated_at
	) VALUES ($1, $2, $3::jsonb, $4)
	ON CONFLICT (branch_id, key) DO UPDATE SET
		value = EXCLUDED.value
		, updated_at = EXCLUDED.updated_at`, branchID, key, value, updatedAt)
	return err
}

func (q *settingRepository) SlugTaken(ctx context.Context, branchID uuid.UUID, key, attribute, value string) (bool, error) {
	ctxt := "SettingRepository-SlugTaken"
	return queryValue[bool](ctx, q.dbRead, ctxt, `SELECT EXISTS(
		SELECT 1
		FROM settings t
		WHERE t.key = $1
			AND LOWER(t.value->>$2) = LOWER($3)
			AND t.branch_id <> $4
	)`, key, attribute, value, branchID)
}

func (q *settingRepository) SyncBranchProfile(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, name, phone, address string, updatedAt time.Time) error {
	ctxt := "SettingRepository-SyncBranchProfile"
	_, err := execute(ctx, tx, ctxt, `UPDATE branches SET
		name = CASE WHEN is_head_office THEN name ELSE $2 END
		, phone = COALESCE(NULLIF($3, ''), phone)
		, address = COALESCE(NULLIF($4, ''), address)
		, updated_at = $5
	WHERE id = $1`, branchID, name, phone, address, updatedAt)
	return err
}

func (q *settingRepository) SyncStoreName(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, name string, updatedAt time.Time) error {
	ctxt := "SettingRepository-SyncStoreName"
	_, err := execute(ctx, tx, ctxt, "UPDATE stores SET name = $2, updated_at = $3 WHERE id = $1 AND name <> $2", storeID, name, updatedAt)
	return err
}

func (q *settingRepository) FindLinkPageVisits(ctx context.Context, branchID uuid.UUID, limit, page int64) ([]*models.LinkPageVisit, int64, error) {
	ctxt := "SettingRepository-FindLinkPageVisits"
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1) FROM link_page_visits t WHERE t.branch_id = $1", branchID)
	if err != nil || total == 0 {
		return []*models.LinkPageVisit{}, 0, err
	}
	response, err := queryRows[models.LinkPageVisit](ctx, q.dbRead, ctxt, `SELECT t.ip_address
		, t.visited_at
		, t.referrer
	FROM link_page_visits t
	WHERE t.branch_id = $1
	ORDER BY t.visited_at DESC`+paginate(limit, page), branchID)
	return response, total, err
}

func (q *settingRepository) FindMenuItems(ctx context.Context, branchID uuid.UUID, categoryIDs []uuid.UUID) ([]*models.MenuItem, error) {
	ctxt := "SettingRepository-FindMenuItems"
	if categoryIDs == nil {
		categoryIDs = []uuid.UUID{}
	}
	return queryRows[models.MenuItem](ctx, q.dbRead, ctxt, `SELECT COALESCE(c.name, 'Other') AS category
		, t.name
		, t.selling_price
		, t.online_description
	FROM products t
	LEFT JOIN categories c ON c.id = t.category_id
	WHERE t.branch_id = $1
		AND t.deleted_at IS NULL
		AND t.show_in_online_store
		AND (CARDINALITY($2::uuid[]) = 0 OR t.category_id = ANY($2))
	ORDER BY c.sort_order NULLS LAST
		, c.name NULLS LAST
		, t.name`, branchID, categoryIDs)
}
