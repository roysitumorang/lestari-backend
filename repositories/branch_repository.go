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

type BranchRepository interface {
	FindBranches(ctx context.Context, request *models.BranchFilter) ([]*models.Branch, int64, error)
	FindBranch(ctx context.Context, tx pgx.Tx, request *models.BranchFilter) (*models.Branch, error)
	// CreateBranch creates a branch whose subscription follows the plan of the store.
	CreateBranch(ctx context.Context, tx pgx.Tx, request *models.Branch) (*models.Branch, error)
	// UpdateBranch writes only the attributes that differ from the stored ones.
	UpdateBranch(ctx context.Context, tx pgx.Tx, request *models.Branch) (*models.Branch, error)
	// DeleteBranch marks a branch and its users as deleted.
	DeleteBranch(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, deletedAt time.Time) error
	// UpdateBranchLogin changes the email and, when not empty, the password hash of the login of a branch; an
	// email in use is ErrEmailTaken.
	UpdateBranchLogin(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, email, passwordHash string) error
	// CountBranches counts the branches of a store among branchIDs.
	CountBranches(ctx context.Context, storeID uuid.UUID, branchIDs []uuid.UUID) (int64, error)
	FindBranchBillings(ctx context.Context, storeID uuid.UUID, at time.Time, limit, page int64) ([]*models.BranchBilling, int64, error)
	// CopyProducts copies the categories and products of the head office that the branch does not have yet (by
	// name and by SKU) and returns how many products were copied.
	CopyProducts(ctx context.Context, tx pgx.Tx, request *models.ProductCopy) (int64, error)
}

type branchRepository struct {
	base
}

func NewBranchRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) BranchRepository {
	return &branchRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const branchColumns = `t.id
	, t.store_id
	, t.name
	, t.code
	, t.manager_name
	, t.phone
	, t.email
	, t.address
	, t.is_head_office
	, t.subscription_expires_at
	, t.created_at
	, t.updated_at`

// branchSortings maps the sortable fields to their SQL expressions.
var branchSortings = map[string]string{
	"id":                      "t.id",
	"created_at":              "t.created_at",
	"updated_at":              "t.updated_at",
	"name":                    "t.name",
	"code":                    "t.code",
	"manager_name":            "t.manager_name",
	"phone":                   "t.phone",
	"email":                   "t.email",
	"address":                 "t.address",
	"is_head_office":          "t.is_head_office",
	"subscription_expires_at": "t.subscription_expires_at",
}

func (q *branchRepository) buildCondition(request *models.BranchFilter) *conditions {
	response := &conditions{}
	response.raw("t.deleted_at IS NULL")
	if len(request.BranchIDs) > 0 {
		response.add("t.id = ANY($%d)", request.BranchIDs)
	}
	if len(request.StoreIDs) > 0 {
		response.add("t.store_id = ANY($%d)", request.StoreIDs)
	}
	if request.Keyword != "" {
		response.add("(t.name ILIKE $%d OR t.code ILIKE $%d)", "%"+request.Keyword+"%")
	}
	return response
}

func (q *branchRepository) FindBranches(ctx context.Context, request *models.BranchFilter) ([]*models.Branch, int64, error) {
	ctxt := "BranchRepository-FindBranches"
	condition := q.buildCondition(request)
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1) FROM branches t "+condition.where(), condition.params...)
	if err != nil || total == 0 {
		return []*models.Branch{}, 0, err
	}
	response, err := queryRows[models.Branch](ctx, q.dbRead, ctxt, "SELECT "+branchColumns+" FROM branches t "+condition.where()+
		orderBy(request.Sortings, branchSortings, "t.is_head_office DESC, t.name")+paginate(request.Limit, request.Page), condition.params...)
	return response, total, err
}

func (q *branchRepository) FindBranch(ctx context.Context, tx pgx.Tx, request *models.BranchFilter) (*models.Branch, error) {
	ctxt := "BranchRepository-FindBranch"
	condition := q.buildCondition(request)
	response, err := queryRow[models.Branch](ctx, q.reader(tx), ctxt, "SELECT "+branchColumns+" FROM branches t "+condition.where()+" LIMIT 1", condition.params...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrBranchNotFound
	}
	return response, err
}

func (q *branchRepository) CreateBranch(ctx context.Context, tx pgx.Tx, request *models.Branch) (*models.Branch, error) {
	ctxt := "BranchRepository-CreateBranch"
	id, err := queryValue[uuid.UUID](ctx, tx, ctxt, `INSERT INTO branches (
		store_id
		, name
		, code
		, manager_name
		, phone
		, email
		, address
		, subscription_expires_at
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, (SELECT s.plan_expires_at FROM stores s WHERE s.id = $1), $8, $9)
	RETURNING id`,
		request.StoreID,
		request.Name,
		request.Code,
		request.ManagerName,
		request.Phone,
		request.Email,
		request.Address,
		request.CreatedAt,
		request.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return q.FindBranch(ctx, tx, models.NewBranchFilter().WithBranchIDs(id))
}

func (q *branchRepository) UpdateBranch(ctx context.Context, tx pgx.Tx, request *models.Branch) (*models.Branch, error) {
	ctxt := "BranchRepository-UpdateBranch"
	current, err := queryRow[models.Branch](ctx, tx, ctxt, `SELECT t.name
		, t.code
		, t.manager_name
		, t.phone
		, t.email
		, t.address
	FROM branches t
	WHERE t.id = $1
		AND t.store_id = $2
		AND t.deleted_at IS NULL
	FOR NO KEY UPDATE`, request.ID, request.StoreID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrBranchNotFound
	}
	if err != nil {
		return nil, err
	}
	var update changes
	update.set(current.Name != request.Name, "name", request.Name)
	update.set(!equalPtr(current.Code, request.Code), "code", request.Code)
	update.set(!equalPtr(current.ManagerName, request.ManagerName), "manager_name", request.ManagerName)
	update.set(!equalPtr(current.Phone, request.Phone), "phone", request.Phone)
	update.set(!equalPtr(current.Email, request.Email), "email", request.Email)
	update.set(!equalPtr(current.Address, request.Address), "address", request.Address)
	if err = update.apply(ctx, tx, ctxt, "branches", request.ID, request.UpdatedAt); err != nil {
		return nil, err
	}
	return q.FindBranch(ctx, tx, models.NewBranchFilter().WithBranchIDs(request.ID))
}

func (q *branchRepository) DeleteBranch(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, deletedAt time.Time) error {
	ctxt := "BranchRepository-DeleteBranch"
	if _, err := execute(ctx, tx, ctxt, "UPDATE users SET deleted_at = $2 WHERE branch_id = $1 AND deleted_at IS NULL", branchID, deletedAt); err != nil {
		return err
	}
	_, err := execute(ctx, tx, ctxt, "UPDATE branches SET deleted_at = $2, updated_at = $2 WHERE id = $1 AND deleted_at IS NULL", branchID, deletedAt)
	return err
}

func (q *branchRepository) UpdateBranchLogin(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, email, passwordHash string) error {
	ctxt := "BranchRepository-UpdateBranchLogin"
	// the login of a branch is the administrator account created with it
	_, err := execute(ctx, tx, ctxt, `UPDATE users SET
		email = COALESCE(NULLIF(LOWER($2), ''), email)
		, password_hash = COALESCE(NULLIF($3, ''), password_hash)
	WHERE branch_id = $1
		AND role = 'administrator'
		AND name LIKE 'Branch %'
		AND (LOWER($2) NOT IN ('', email) OR $3 <> '')`, branchID, email, passwordHash)
	if IsUniqueViolation(err) {
		return models.ErrEmailTaken
	}
	return err
}

func (q *branchRepository) CountBranches(ctx context.Context, storeID uuid.UUID, branchIDs []uuid.UUID) (int64, error) {
	ctxt := "BranchRepository-CountBranches"
	return queryValue[int64](ctx, q.dbRead, ctxt, `SELECT COUNT(1)
	FROM branches t
	WHERE t.id = ANY($1)
		AND t.store_id = $2
		AND t.deleted_at IS NULL`, branchIDs, storeID)
}

func (q *branchRepository) FindBranchBillings(ctx context.Context, storeID uuid.UUID, at time.Time, limit, page int64) ([]*models.BranchBilling, int64, error) {
	ctxt := "BranchRepository-FindBranchBillings"
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1) FROM branches t WHERE t.store_id = $1 AND t.deleted_at IS NULL", storeID)
	if err != nil || total == 0 {
		return []*models.BranchBilling{}, 0, err
	}
	response, err := queryRows[models.BranchBilling](ctx, q.dbRead, ctxt, `SELECT t.id AS branch_id
		, t.id
		, t.name AS branch_name
		, s.plan
		, t.subscription_expires_at AS expires_at
		, CASE
			WHEN t.subscription_expires_at IS NULL OR t.subscription_expires_at < $2 THEN 'expired'
			WHEN t.subscription_expires_at < $2::timestamptz + INTERVAL '7 days' THEN 'expiring'
			ELSE 'active'
		END AS status
		, $3::numeric AS renewal_price
	FROM branches t
	JOIN stores s ON s.id = t.store_id
	WHERE t.store_id = $1
		AND t.deleted_at IS NULL
	ORDER BY t.is_head_office DESC
		, t.name`+paginate(limit, page), storeID, at, models.BranchRenewalMonthly)
	return response, total, err
}

func (q *branchRepository) CopyProducts(ctx context.Context, tx pgx.Tx, request *models.ProductCopy) (int64, error) {
	ctxt := "BranchRepository-CopyProducts"
	categoryIDs := request.CategoryIDs
	if categoryIDs == nil {
		categoryIDs = []uuid.UUID{}
	}
	if _, err := execute(ctx, tx, ctxt, `INSERT INTO categories (
		branch_id
		, name
		, color
		, sort_order
		, description
		, created_at
		, updated_at
	)
	SELECT $2
		, c.name
		, c.color
		, c.sort_order
		, c.description
		, $4
		, $4
	FROM categories c
	WHERE c.branch_id = $1
		AND (CARDINALITY($3::uuid[]) = 0 OR c.id = ANY($3))
		AND NOT EXISTS (SELECT 1 FROM categories x WHERE x.branch_id = $2 AND LOWER(x.name) = LOWER(c.name))`,
		request.HeadOfficeID, request.BranchID, categoryIDs, request.At); err != nil {
		return 0, err
	}
	copied, err := execute(ctx, tx, ctxt, `INSERT INTO products (
		branch_id
		, source_product_id
		, name
		, item_type
		, sku
		, barcode
		, category_id
		, unit
		, selling_price
		, purchase_price
		, price_type
		, discount_type
		, discount_value
		, wholesale_prices
		, customer_type_prices
		, track_stock
		, stock
		, minimum_stock
		, shelf_location
		, weight_grams
		, track_imei
		, units
		, variants
		, ingredients
		, points_earned
		, points_to_redeem
		, show_in_online_store
		, description
		, online_description
		, image_url
		, created_at
		, updated_at
	)
	SELECT $2
		, p.id
		, p.name
		, p.item_type
		, p.sku
		, p.barcode
		, (
			SELECT x.id
			FROM categories x
			JOIN categories c ON LOWER(c.name) = LOWER(x.name) AND c.id = p.category_id
			WHERE x.branch_id = $2
			LIMIT 1
		)
		, p.unit
		, p.selling_price
		, p.purchase_price
		, p.price_type
		, p.discount_type
		, p.discount_value
		, p.wholesale_prices
		, '[]'
		, p.track_stock
		, CASE WHEN $4 THEN p.stock ELSE 0 END
		, p.minimum_stock
		, p.shelf_location
		, p.weight_grams
		, p.track_imei
		, p.units
		, p.variants
		, '[]'
		, p.points_earned
		, p.points_to_redeem
		, p.show_in_online_store
		, p.description
		, p.online_description
		, p.image_url
		, $5
		, $5
	FROM products p
	WHERE p.branch_id = $1
		AND p.deleted_at IS NULL
		AND (CARDINALITY($3::uuid[]) = 0 OR p.category_id = ANY($3))
		AND NOT EXISTS (
			SELECT 1 FROM products x WHERE x.branch_id = $2 AND LOWER(x.sku) = LOWER(p.sku) AND x.deleted_at IS NULL
		)`, request.HeadOfficeID, request.BranchID, categoryIDs, request.IncludeStock, request.At)
	if err != nil || !request.IncludeStock {
		return copied, err
	}
	// the copied stock is the opening stock of the new products
	_, err = execute(ctx, tx, ctxt, `INSERT INTO stock_movements (
		branch_id
		, product_id
		, type
		, reference
		, quantity_change
		, stock_after
		, actor_name
		, note
		, created_at
	)
	SELECT p.branch_id
		, p.id
		, 'adjustment'
		, 'Copied from head office'
		, p.stock
		, p.stock
		, 'system'
		, 'Opening stock'
		, $2
	FROM products p
	WHERE p.branch_id = $1
		AND p.stock <> 0
		AND p.track_stock
		AND NOT EXISTS (SELECT 1 FROM stock_movements m WHERE m.product_id = p.id)`, request.BranchID, request.At)
	return copied, err
}
