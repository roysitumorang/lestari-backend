package repositories

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

// StockRepository reads stock levels and is the single place stock changes: every change goes through
// ChangeStock, which also writes the stock movement.
type StockRepository interface {
	FindStocks(
		ctx context.Context,
		request *models.StockFilter,
	) ([]*models.Stock, int64, error)
	FindStock(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockFilter,
	) (*models.Stock, error)
	// LockStock locks the product row and returns its current stock.
	LockStock(
		ctx context.Context,
		tx pgx.Tx,
		branchID,
		productID uuid.UUID,
	) (float64, error)
	// ChangeStock returns the stock after the change.
	ChangeStock(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockChange,
	) (float64, error)
	CreateStockMovement(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockMovement,
	) error
	// AverageUnitCost sets the purchase price to the weighted average after received units arrived at unitCost.
	AverageUnitCost(
		ctx context.Context,
		tx pgx.Tx,
		productID uuid.UUID,
		received,
		unitCost float64,
	) error
	SetUnitCost(
		ctx context.Context,
		tx pgx.Tx,
		productID uuid.UUID,
		unitCost float64,
	) error
	FindStockMovements(
		ctx context.Context,
		request *models.StockMovementFilter,
	) ([]*models.StockMovement, int64, error)
	// RecalculateStocks sets the stock of every tracked product of the branch to the sum of its movements and
	// returns how many products changed.
	RecalculateStocks(
		ctx context.Context,
		branchID uuid.UUID,
		updatedAt time.Time,
	) (int64, error)
}

type stockRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewStockRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) StockRepository {
	return &stockRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// stockColumns is the SELECT list of a stock level (products, alias t), in the order scanStock reads it.
const stockColumns = `t.id
	, t.branch_id
	, t.image_url
	, t.name
	, t.sku
	, t.category_id
	, (SELECT r.name FROM categories r WHERE r.id = t.category_id)
	, t.stock::float8
	, t.unit
	, t.minimum_stock::float8
	, t.purchase_price::float8
	, ROUND(GREATEST(t.stock, 0) * t.purchase_price, 2)::float8
	, t.created_at
	, t.updated_at`

// stockSortings maps the sortable fields to their SQL expressions.
var stockSortings = map[string]string{
	"product_id":    "t.id",
	"created_at":    "t.created_at",
	"updated_at":    "t.updated_at",
	"image_url":     "t.image_url",
	"name":          "t.name",
	"sku":           "t.sku",
	"category_id":   "t.category_id",
	"category_name": "(SELECT r.name FROM categories r WHERE r.id = t.category_id)",
	"stock":         "t.stock",
	"unit":          "t.unit",
	"minimum_stock": "t.minimum_stock",
	"unit_cost":     "t.purchase_price",
	"stock_value":   "ROUND(GREATEST(t.stock, 0) * t.purchase_price, 2)",
}

func scanStock(row pgx.Row) (*models.Stock, error) {
	var response models.Stock
	err := row.Scan(
		&response.ProductID,
		&response.BranchID,
		&response.ImageURL,
		&response.Name,
		&response.SKU,
		&response.CategoryID,
		&response.CategoryName,
		&response.Stock,
		&response.Unit,
		&response.MinimumStock,
		&response.UnitCost,
		&response.StockValue,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *stockRepository) FindStocks(
	ctx context.Context,
	request *models.StockFilter,
) ([]*models.Stock, int64, error) {
	ctxt := "StockRepository-FindStocks"
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
	response := []*models.Stock{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM products t %s ORDER BY ", stockColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from stockSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", stockSortings[item.Field], item.Direction, item.Direction)
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
		stock, err := scanStock(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, stock)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindStock returns the first stock level of the filter. Inside a transaction pass tx so the read sees its
// writes; with a nil tx the read pool is used.
func (q *stockRepository) FindStock(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockFilter,
) (*models.Stock, error) {
	ctxt := "StockRepository-FindStock"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM products t %s LIMIT 1", stockColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanStock(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrStockNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *stockRepository) LockStock(
	ctx context.Context,
	tx pgx.Tx,
	branchID,
	productID uuid.UUID,
) (float64, error) {
	ctxt := "StockRepository-LockStock"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.stock::float8
	FROM products t
	WHERE t.id = $1
		AND t.branch_id = $2
		AND t.deleted_at IS NULL
	FOR NO KEY UPDATE`)
	var response float64
	err := tx.QueryRow(ctx, sb.String(), productID, branchID).Scan(&response)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, models.ErrProductNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
	}
	return response, err
}

func (q *stockRepository) ChangeStock(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockChange,
) (float64, error) {
	ctxt := "StockRepository-ChangeStock"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`UPDATE products SET
		stock = stock + $3
		, updated_at = $4
	WHERE id = $1
		AND branch_id = $2
	RETURNING stock::float8
		, track_stock`)
	var (
		stockAfter float64
		trackStock bool
	)
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ProductID,
		request.BranchID,
		request.Change,
		request.At,
	).Scan(&stockAfter, &trackStock)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, models.ErrProductNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return 0, err
	}
	if !trackStock {
		return stockAfter, nil
	}
	movement := &models.StockMovement{
		BranchID:       request.BranchID,
		ProductID:      request.ProductID,
		Actor:          request.Actor,
		CreatedAt:      request.At,
		Type:           request.Type,
		Reference:      request.Reference,
		QuantityChange: request.Change,
		StockAfter:     stockAfter,
		UnitCost:       request.UnitCost,
		ActorName:      request.Actor.Name,
	}
	if request.Note != "" {
		movement.Note = &request.Note
	}
	return stockAfter, q.CreateStockMovement(ctx, tx, movement)
}

func (q *stockRepository) CreateStockMovement(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockMovement,
) error {
	ctxt := "StockRepository-CreateStockMovement"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO stock_movements (
		branch_id
		, product_id
		, type
		, reference
		, quantity_change
		, stock_after
		, unit_cost
		, actor_id
		, actor_name
		, note
		, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`)
	var actorID *uuid.UUID
	if request.Actor.ID != uuid.Nil() {
		actorID = &request.Actor.ID
	}
	_, err := tx.Exec(
		ctx,
		sb.String(),
		request.BranchID,
		request.ProductID,
		request.Type,
		request.Reference,
		request.QuantityChange,
		request.StockAfter,
		request.UnitCost,
		actorID,
		request.ActorName,
		request.Note,
		request.CreatedAt,
	)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
	}
	return err
}

func (q *stockRepository) AverageUnitCost(
	ctx context.Context,
	tx pgx.Tx,
	productID uuid.UUID,
	received,
	unitCost float64,
) error {
	ctxt := "StockRepository-AverageUnitCost"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`UPDATE products SET
		purchase_price = CASE
			WHEN stock > 0 THEN ROUND(((stock - $2::numeric) * purchase_price + $2::numeric * $3::numeric) / stock, 2)
			ELSE $3::numeric
		END
	WHERE id = $1`)
	_, err := tx.Exec(ctx, sb.String(), productID, received, unitCost)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
	}
	return err
}

func (q *stockRepository) SetUnitCost(
	ctx context.Context,
	tx pgx.Tx,
	productID uuid.UUID,
	unitCost float64,
) error {
	ctxt := "StockRepository-SetUnitCost"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString("UPDATE products SET purchase_price = $2 WHERE id = $1")
	_, err := tx.Exec(ctx, sb.String(), productID, unitCost)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
	}
	return err
}

func (q *stockRepository) FindStockMovements(
	ctx context.Context,
	request *models.StockMovementFilter,
) ([]*models.StockMovement, int64, error) {
	ctxt := "StockRepository-FindStockMovements"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	params := []any{request.ProductID, request.BranchID}
	_, _ = sb.WriteString("WHERE t.product_id = $1 AND t.branch_id = $2")
	if request.StartDate != "" {
		params = append(params, request.StartDate)
		_, _ = fmt.Fprintf(sb, " AND t.created_at::date >= $%d::date", len(params))
	}
	if request.EndDate != "" {
		params = append(params, request.EndDate)
		_, _ = fmt.Fprintf(sb, " AND t.created_at::date <= $%d::date", len(params))
	}
	condition := sb.String()
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM stock_movements t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.StockMovement{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, `SELECT t.id
		, t.created_at
		, t.type
		, t.reference
		, t.quantity_change::float8
		, t.stock_after::float8
		, t.unit_cost::float8
		, t.actor_name
		, t.note
	FROM stock_movements t %s
	ORDER BY t.id DESC`, condition)
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
		var movement models.StockMovement
		if err = rows.Scan(
			&movement.ID,
			&movement.CreatedAt,
			&movement.Type,
			&movement.Reference,
			&movement.QuantityChange,
			&movement.StockAfter,
			&movement.UnitCost,
			&movement.ActorName,
			&movement.Note,
		); err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, &movement)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

func (q *stockRepository) RecalculateStocks(
	ctx context.Context,
	branchID uuid.UUID,
	updatedAt time.Time,
) (int64, error) {
	ctxt := "StockRepository-RecalculateStocks"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`UPDATE products t SET
		stock = s.total
		, updated_at = $2
	FROM (
		SELECT m.product_id
			, SUM(m.quantity_change) AS total
		FROM stock_movements m
		WHERE m.branch_id = $1
		GROUP BY m.product_id
	) s
	WHERE t.id = s.product_id
		AND t.branch_id = $1
		AND t.track_stock
		AND t.stock <> s.total`)
	tag, err := q.dbWrite.Exec(ctx, sb.String(), branchID, updatedAt)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *stockRepository) buildCondition(request *models.StockFilter, sb *strings.Builder) (condition string, params []any) {
	conds := []string{"t.deleted_at IS NULL", "t.track_stock"}
	if len(request.ProductIDs) > 0 {
		params = append(params, request.ProductIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.CategoryID != nil {
		params = append(params, *request.CategoryID)
		conds = append(conds, helper.Sprintf("t.category_id = $%d", len(params)))
	}
	if request.StockStatus != "" {
		params = append(params, request.StockStatus)
		conds = append(conds, helper.Sprintf("(CASE $%d WHEN 'in_stock' THEN t.stock > 0 WHEN 'low' THEN t.minimum_stock IS NOT NULL AND t.stock <= t.minimum_stock AND t.stock > 0 WHEN 'out' THEN t.stock <= 0 ELSE true END)", len(params)))
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
