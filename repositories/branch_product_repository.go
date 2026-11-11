package repositories

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

type BranchProductRepository interface {
	FindBranchProducts(
		ctx context.Context,
		request *models.BranchProductFilter,
	) ([]*models.BranchProduct, int64, error)
	FindBranchProduct(
		ctx context.Context,
		tx pgx.Tx,
		request *models.BranchProductFilter,
	) (*models.BranchProduct, error)
	UpdateBranchProduct(
		ctx context.Context,
		tx pgx.Tx,
		request *models.BranchProduct,
	) (*models.BranchProduct, error)
	// SyncPrices copies the selling prices of the head office to the matching products of the branches and
	// returns how many products changed.
	SyncPrices(
		ctx context.Context,
		tx pgx.Tx,
		request *models.BranchPriceSync,
	) (int64, error)
}

type branchProductRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewBranchProductRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) BranchProductRepository {
	return &branchProductRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// branchProductColumns is the SELECT list of a branch product (products, alias t), in the order
// scanBranchProduct reads it.
const branchProductColumns = `t.id
	, t.branch_id
	, COALESCE((SELECT r.name FROM branches r WHERE r.id = t.branch_id), '')
	, t.name
	, t.sku
	, t.category_id
	, t.stock::float8
	, t.selling_price::float8
	, t.purchase_price::float8
	, t.is_available
	, t.created_at
	, t.updated_at`

// branchProductSortings maps the sortable fields to their SQL expressions.
var branchProductSortings = map[string]string{
	"id":             "t.id",
	"created_at":     "t.created_at",
	"updated_at":     "t.updated_at",
	"branch_id":      "t.branch_id",
	"branch_name":    "(SELECT r.name FROM branches r WHERE r.id = t.branch_id)",
	"product_name":   "t.name",
	"sku":            "t.sku",
	"category_id":    "t.category_id",
	"stock":          "t.stock",
	"selling_price":  "t.selling_price",
	"purchase_price": "t.purchase_price",
	"is_available":   "t.is_available",
}

func scanBranchProduct(row pgx.Row) (*models.BranchProduct, error) {
	var response models.BranchProduct
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.BranchName,
		&response.ProductName,
		&response.SKU,
		&response.CategoryID,
		&response.Stock,
		&response.SellingPrice,
		&response.PurchasePrice,
		&response.IsAvailable,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *branchProductRepository) FindBranchProducts(
	ctx context.Context,
	request *models.BranchProductFilter,
) ([]*models.BranchProduct, int64, error) {
	ctxt := "BranchProductRepository-FindBranchProducts"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM products t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.BranchProduct{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM products t %s ORDER BY ", branchProductColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.branch_id, t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from branchProductSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", branchProductSortings[item.Field], item.Direction, item.Direction)
	}
	if request.Limit > 0 {
		_, _ = fmt.Fprintf(sb, " LIMIT %d OFFSET %d", request.Limit, max(request.Page-1, 0)*request.Limit)
	}
	rows, err := q.dbRead.Query(ctx, sb.String(), params...)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrQuery")
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		branchProduct, err := scanBranchProduct(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, branchProduct)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindBranchProduct returns the first branch product of the filter. Inside a transaction pass tx so the read
// sees its writes; with a nil tx the read pool is used.
func (q *branchProductRepository) FindBranchProduct(
	ctx context.Context,
	tx pgx.Tx,
	request *models.BranchProductFilter,
) (*models.BranchProduct, error) {
	ctxt := "BranchProductRepository-FindBranchProduct"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM products t %s LIMIT 1", branchProductColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanBranchProduct(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrBranchProductNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

// UpdateBranchProduct locks the row and writes only the attributes whose value differs from the stored one. When
// nothing changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *branchProductRepository) UpdateBranchProduct(
	ctx context.Context,
	tx pgx.Tx,
	request *models.BranchProduct,
) (*models.BranchProduct, error) {
	ctxt := "BranchProductRepository-UpdateBranchProduct"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.selling_price::float8
		, t.purchase_price::float8
		, t.is_available
	FROM products t
	WHERE t.id = $1
		AND t.branch_id = $2
		AND t.deleted_at IS NULL
	FOR NO KEY UPDATE`)
	var current models.BranchProduct
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.SellingPrice,
		&current.PurchasePrice,
		&current.IsAvailable,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrBranchProductNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE products SET")
	if current.SellingPrice != request.SellingPrice {
		params = append(params, request.SellingPrice)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " selling_price = $%d", n)
	}
	if current.PurchasePrice != request.PurchasePrice {
		params = append(params, request.PurchasePrice)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " purchase_price = $%d", n)
	}
	if current.IsAvailable != request.IsAvailable {
		params = append(params, request.IsAvailable)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " is_available = $%d", n)
	}
	if len(params) > 0 {
		params = append(params, request.UpdatedAt)
		_, _ = fmt.Fprintf(sb, ", updated_at = $%d", len(params))
		params = append(params, request.ID)
		_, _ = fmt.Fprintf(sb, " WHERE id = $%d", len(params))
		if _, err = tx.Exec(ctx, sb.String(), params...); err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			return nil, err
		}
	}
	return q.FindBranchProduct(ctx, tx, models.NewBranchProductFilter().WithBranchProductIDs(request.ID))
}

func (q *branchProductRepository) SyncPrices(
	ctx context.Context,
	tx pgx.Tx,
	request *models.BranchPriceSync,
) (int64, error) {
	ctxt := "BranchProductRepository-SyncPrices"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	params := []any{request.HeadOfficeID, request.BranchIDs, request.StoreID, request.At}
	_, _ = sb.WriteString(`UPDATE products t SET
		selling_price = hq.selling_price
		, updated_at = $4
	FROM products hq
	WHERE hq.branch_id = $1
		AND hq.deleted_at IS NULL
		AND (t.source_product_id = hq.id OR LOWER(t.sku) = LOWER(hq.sku))
		AND t.branch_id = ANY($2)
		AND t.branch_id IN (SELECT r.id FROM branches r WHERE r.store_id = $3)
		AND t.branch_id <> $1
		AND t.selling_price <> hq.selling_price`)
	if len(request.ProductIDs) > 0 {
		params = append(params, request.ProductIDs)
		n := len(params)
		_, _ = fmt.Fprintf(sb, " AND (hq.id = ANY($%d) OR t.id = ANY($%d))", n, n)
	}
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *branchProductRepository) buildCondition(request *models.BranchProductFilter, sb *strings.Builder) (condition string, params []any) {
	conds := []string{"t.deleted_at IS NULL"}
	if len(request.BranchProductIDs) > 0 {
		params = append(params, request.BranchProductIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.StoreIDs) > 0 {
		params = append(params, request.StoreIDs)
		conds = append(conds, helper.Sprintf("t.branch_id IN (SELECT r.id FROM branches r WHERE r.store_id = ANY($%d) AND r.deleted_at IS NULL)", len(params)))
	}
	if request.BranchID != nil {
		params = append(params, *request.BranchID)
		conds = append(conds, helper.Sprintf("t.branch_id = $%d", len(params)))
	}
	if request.CategoryID != nil {
		params = append(params, *request.CategoryID)
		conds = append(conds, helper.Sprintf("t.category_id = $%d", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		n := len(params)
		conds = append(conds, helper.Sprintf("(t.name ILIKE $%d OR t.sku ILIKE $%d)", n, n))
	}
	sb.Reset()
	_, _ = sb.WriteString("WHERE ")
	_, _ = sb.WriteString(strings.Join(conds, " AND "))
	return sb.String(), params
}
