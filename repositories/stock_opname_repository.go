package repositories

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

type StockOpnameRepository interface {
	FindStockOpnames(
		ctx context.Context,
		request *models.StockOpnameFilter,
	) ([]*models.StockOpname, int64, error)
	FindStockOpname(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockOpnameFilter,
	) (*models.StockOpname, error)
	CreateStockOpname(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockOpname,
	) (*models.StockOpname, error)
	// NextStockOpnameCode is the next document number of the branch for a day (YYYYMMDD): SO/20261108/0001.
	NextStockOpnameCode(
		ctx context.Context,
		tx pgx.Tx,
		branchID uuid.UUID,
		day string,
	) (string, error)
	CreateStockOpnameItems(
		ctx context.Context,
		tx pgx.Tx,
		stockOpnameID uuid.UUID,
		request []*models.StockOpnameItem,
	) error
	FindStockOpnameItems(
		ctx context.Context,
		tx pgx.Tx,
		stockOpnameID uuid.UUID,
	) ([]*models.StockOpnameItem, error)
	// LockStockOpname locks the row and returns its code and status.
	LockStockOpname(
		ctx context.Context,
		tx pgx.Tx,
		branchID,
		stockOpnameID uuid.UUID,
	) (*models.StockOpname, error)
	// DecideStockOpname stores the approval or rejection.
	DecideStockOpname(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockOpnameDecision,
	) error
	DeleteStockOpnames(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockOpnameFilter,
	) (int64, error)
}

type stockOpnameRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewStockOpnameRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) StockOpnameRepository {
	return &stockOpnameRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// stockOpnameColumns is the SELECT list of stock_opnames (alias t), in the order scanStockOpname reads it.
const stockOpnameColumns = `t.id
	, t.branch_id
	, t.code
	, TO_CHAR(t.opname_date, 'YYYY-MM-DD')
	, t.performed_by_name
	, t.performed_by_role
	, t.status
	, t.note
	, t.reject_reason
	, (SELECT count(*) FROM stock_opname_items i WHERE i.opname_id = t.id)::bigint
	, (SELECT COALESCE(sum((i.physical_stock - i.system_stock) * i.unit_cost), 0) FROM stock_opname_items i WHERE i.opname_id = t.id)::float8
	, t.created_at
	, t.updated_at`

// stockOpnameSortings maps the sortable fields to their SQL expressions.
var stockOpnameSortings = map[string]string{
	"id":                "t.id",
	"created_at":        "t.created_at",
	"updated_at":        "t.updated_at",
	"code":              "t.code",
	"opname_date":       "t.opname_date",
	"performed_by_name": "t.performed_by_name",
	"performed_by_role": "t.performed_by_role",
	"status":            "t.status",
	"note":              "t.note",
	"reject_reason":     "t.reject_reason",
	"item_count":        "(SELECT count(*) FROM stock_opname_items i WHERE i.opname_id = t.id)",
	"difference_value":  "(SELECT COALESCE(sum((i.physical_stock - i.system_stock) * i.unit_cost), 0) FROM stock_opname_items i WHERE i.opname_id = t.id)",
}

func scanStockOpname(row pgx.Row) (*models.StockOpname, error) {
	var response models.StockOpname
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Code,
		&response.OpnameDate,
		&response.PerformedByName,
		&response.PerformedByRole,
		&response.Status,
		&response.Note,
		&response.RejectReason,
		&response.ItemCount,
		&response.DifferenceValue,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *stockOpnameRepository) FindStockOpnames(
	ctx context.Context,
	request *models.StockOpnameFilter,
) ([]*models.StockOpname, int64, error) {
	ctxt := "StockOpnameRepository-FindStockOpnames"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM stock_opnames t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.StockOpname{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM stock_opnames t %s ORDER BY ", stockOpnameColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.opname_date DESC, t.id DESC")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from stockOpnameSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", stockOpnameSortings[item.Field], item.Direction, item.Direction)
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
		stockOpname, err := scanStockOpname(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, stockOpname)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindStockOpname returns the first stock opname of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *stockOpnameRepository) FindStockOpname(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockOpnameFilter,
) (*models.StockOpname, error) {
	ctxt := "StockOpnameRepository-FindStockOpname"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM stock_opnames t %s LIMIT 1", stockOpnameColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanStockOpname(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrStockOpnameNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *stockOpnameRepository) CreateStockOpname(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockOpname,
) (*models.StockOpname, error) {
	ctxt := "StockOpnameRepository-CreateStockOpname"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO stock_opnames (
		branch_id
		, code
		, opname_date
		, performed_by_name
		, performed_by_role
		, status
		, note
		, created_at
		, updated_at
	) VALUES ($1, $2, $3::date, $4, $5, $6, $7, $8, $9)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.Code,
		request.OpnameDate,
		request.PerformedByName,
		request.PerformedByRole,
		request.Status,
		request.Note,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrStockOpnameConflict); !errors.Is(err, models.ErrStockOpnameConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindStockOpname(ctx, tx, models.NewStockOpnameFilter().WithStockOpnameIDs(id))
}

// DeleteStockOpnames deletes the stock opnames of the filter and returns how many went.
func (q *stockOpnameRepository) DeleteStockOpnames(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockOpnameFilter,
) (int64, error) {
	ctxt := "StockOpnameRepository-DeleteStockOpnames"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("stock opname: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM stock_opnames t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrStockOpnameInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *stockOpnameRepository) buildCondition(request *models.StockOpnameFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.StockOpnameIDs) > 0 {
		params = append(params, request.StockOpnameIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.Status != nil {
		params = append(params, *request.Status)
		conds = append(conds, helper.Sprintf("t.status = $%d", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		n := len(params)
		conds = append(conds, helper.Sprintf("((t.code)::text ILIKE $%d OR (t.performed_by_name)::text ILIKE $%d)", n, n))
	}
	if len(conds) > 0 {
		sb.Reset()
		_, _ = sb.WriteString("WHERE ")
		_, _ = sb.WriteString(strings.Join(conds, " AND "))
		condition = sb.String()
	}
	return
}

func (q *stockOpnameRepository) NextStockOpnameCode(
	ctx context.Context,
	tx pgx.Tx,
	branchID uuid.UUID,
	day string,
) (string, error) {
	ctxt := "StockOpnameRepository-NextStockOpnameCode"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT COUNT(1)
	FROM stock_opnames t
	WHERE t.branch_id = $1
		AND t.code LIKE $2`)
	prefix := helper.Sprintf("SO/%s/", day)
	var count int64
	if err := tx.QueryRow(ctx, sb.String(), branchID, prefix+"%").Scan(&count); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return "", err
	}
	return helper.Sprintf("%s%04d", prefix, count+1), nil
}

func (q *stockOpnameRepository) CreateStockOpnameItems(
	ctx context.Context,
	tx pgx.Tx,
	stockOpnameID uuid.UUID,
	request []*models.StockOpnameItem,
) error {
	ctxt := "StockOpnameRepository-CreateStockOpnameItems"
	if len(request) == 0 {
		return nil
	}
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO stock_opname_items (
		opname_id
		, product_id
		, product_name
		, system_stock
		, physical_stock
		, unit_cost
		, note
	) VALUES `)
	params := make([]any, 0, 1+len(request)*6)
	params = append(params, stockOpnameID)
	for i, item := range request {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		params = append(params, item.ProductID, item.ProductName, item.SystemStock, item.PhysicalStock, item.UnitCost, item.Note)
		n := len(params)
		_, _ = fmt.Fprintf(sb, "($1, $%d, $%d, $%d, $%d, $%d, $%d)", n-5, n-4, n-3, n-2, n-1, n)
	}
	if _, err := tx.Exec(ctx, sb.String(), params...); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return err
	}
	return nil
}

func (q *stockOpnameRepository) FindStockOpnameItems(
	ctx context.Context,
	tx pgx.Tx,
	stockOpnameID uuid.UUID,
) ([]*models.StockOpnameItem, error) {
	ctxt := "StockOpnameRepository-FindStockOpnameItems"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT i.product_id
		, i.product_name
		, p.sku
		, i.system_stock::float8
		, i.physical_stock::float8
		, (i.physical_stock - i.system_stock)::float8
		, i.unit_cost::float8
		, i.note
		, COALESCE(p.stock, 0)::float8
	FROM stock_opname_items i
	LEFT JOIN products p ON p.id = i.product_id
	WHERE i.opname_id = $1
	ORDER BY i.id`)
	var (
		rows pgx.Rows
		err  error
	)
	if tx != nil {
		rows, err = tx.Query(ctx, sb.String(), stockOpnameID)
	} else {
		rows, err = q.dbRead.Query(ctx, sb.String(), stockOpnameID)
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrQuery")
		return nil, err
	}
	defer rows.Close()
	response := []*models.StockOpnameItem{}
	for rows.Next() {
		var item models.StockOpnameItem
		if err = rows.Scan(
			&item.ProductID,
			&item.ProductName,
			&item.SKU,
			&item.SystemStock,
			&item.PhysicalStock,
			&item.Difference,
			&item.UnitCost,
			&item.Note,
			&item.CurrentStock,
		); err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, err
		}
		response = append(response, &item)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, err
	}
	return response, nil
}

func (q *stockOpnameRepository) LockStockOpname(
	ctx context.Context,
	tx pgx.Tx,
	branchID,
	stockOpnameID uuid.UUID,
) (*models.StockOpname, error) {
	ctxt := "StockOpnameRepository-LockStockOpname"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.code
		, t.status
	FROM stock_opnames t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	response := &models.StockOpname{ID: stockOpnameID, BranchID: branchID}
	err := tx.QueryRow(ctx, sb.String(), stockOpnameID, branchID).Scan(&response.Code, &response.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrStockOpnameNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *stockOpnameRepository) DecideStockOpname(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockOpnameDecision,
) error {
	ctxt := "StockOpnameRepository-DecideStockOpname"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`UPDATE stock_opnames SET
		status = $2
		, reject_reason = NULLIF($3, '')
		, decided_at = $4
		, updated_at = $4
	WHERE id = $1`)
	_, err := tx.Exec(ctx, sb.String(), request.StockOpnameID, request.Status, request.Reason, request.At)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
	}
	return err
}
