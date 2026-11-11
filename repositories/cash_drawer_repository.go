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

type CashDrawerRepository interface {
	FindCashDrawers(
		ctx context.Context,
		request *models.CashDrawerFilter,
	) ([]*models.CashDrawer, int64, error)
	FindCashDrawer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CashDrawerFilter,
	) (*models.CashDrawer, error)
	CreateCashDrawer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CashDrawer,
	) (*models.CashDrawer, error)
	UpdateCashDrawer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CashDrawer,
	) (*models.CashDrawer, error)
	DeleteCashDrawers(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CashDrawerFilter,
	) (int64, error)
}

type cashDrawerRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewCashDrawerRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) CashDrawerRepository {
	return &cashDrawerRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// cashDrawerColumns is the SELECT list of cash_drawers (alias t), in the order scanCashDrawer reads it.
const cashDrawerColumns = `t.id
	, t.branch_id
	, t.name
	, t.is_active
	, (CASE WHEN EXISTS(SELECT 1 FROM shifts s WHERE s.cash_drawer_id = t.id AND s.closed_at IS NULL) THEN 'open' ELSE 'closed' END)
	, t.created_at
	, t.updated_at`

// cashDrawerSortings maps the sortable fields to their SQL expressions.
var cashDrawerSortings = map[string]string{
	"id":                   "t.id",
	"created_at":           "t.created_at",
	"updated_at":           "t.updated_at",
	"name":                 "t.name",
	"is_active":            "t.is_active",
	"current_shift_status": "(CASE WHEN EXISTS(SELECT 1 FROM shifts s WHERE s.cash_drawer_id = t.id AND s.closed_at IS NULL) THEN 'open' ELSE 'closed' END)",
}

func scanCashDrawer(row pgx.Row) (*models.CashDrawer, error) {
	var response models.CashDrawer
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.IsActive,
		&response.CurrentShiftStatus,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *cashDrawerRepository) FindCashDrawers(
	ctx context.Context,
	request *models.CashDrawerFilter,
) ([]*models.CashDrawer, int64, error) {
	ctxt := "CashDrawerRepository-FindCashDrawers"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM cash_drawers t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.CashDrawer{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM cash_drawers t %s ORDER BY ", cashDrawerColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from cashDrawerSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", cashDrawerSortings[item.Field], item.Direction, item.Direction)
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
		cashDrawer, err := scanCashDrawer(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, cashDrawer)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindCashDrawer returns the first cash drawer of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *cashDrawerRepository) FindCashDrawer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CashDrawerFilter,
) (*models.CashDrawer, error) {
	ctxt := "CashDrawerRepository-FindCashDrawer"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM cash_drawers t %s LIMIT 1", cashDrawerColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanCashDrawer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCashDrawerNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *cashDrawerRepository) CreateCashDrawer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CashDrawer,
) (*models.CashDrawer, error) {
	ctxt := "CashDrawerRepository-CreateCashDrawer"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO cash_drawers (
		branch_id
		, name
		, is_active
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.Name,
		request.IsActive,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrCashDrawerConflict); !errors.Is(err, models.ErrCashDrawerConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindCashDrawer(ctx, tx, models.NewCashDrawerFilter().WithCashDrawerIDs(id))
}

// UpdateCashDrawer locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *cashDrawerRepository) UpdateCashDrawer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CashDrawer,
) (*models.CashDrawer, error) {
	ctxt := "CashDrawerRepository-UpdateCashDrawer"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.is_active
	FROM cash_drawers t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.CashDrawer
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.IsActive,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCashDrawerNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE cash_drawers SET")
	if current.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
	}
	if current.IsActive != request.IsActive {
		params = append(params, request.IsActive)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " is_active = $%d", n)
	}
	if len(params) > 0 {
		params = append(params, request.UpdatedAt)
		_, _ = fmt.Fprintf(sb, ", updated_at = $%d", len(params))
		params = append(params, request.ID)
		_, _ = fmt.Fprintf(sb, " WHERE id = $%d", len(params))
		if _, err = tx.Exec(ctx, sb.String(), params...); err != nil {
			if err = writeError(err, models.ErrCashDrawerConflict); !errors.Is(err, models.ErrCashDrawerConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindCashDrawer(ctx, tx, models.NewCashDrawerFilter().WithCashDrawerIDs(request.ID))
}

// DeleteCashDrawers deletes the cash drawers of the filter and returns how many went.
func (q *cashDrawerRepository) DeleteCashDrawers(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CashDrawerFilter,
) (int64, error) {
	ctxt := "CashDrawerRepository-DeleteCashDrawers"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("cash drawer: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM cash_drawers t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrCashDrawerInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *cashDrawerRepository) buildCondition(request *models.CashDrawerFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.CashDrawerIDs) > 0 {
		params = append(params, request.CashDrawerIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		n := len(params)
		conds = append(conds, helper.Sprintf("((t.name)::text ILIKE $%d)", n))
	}
	if len(conds) > 0 {
		sb.Reset()
		_, _ = sb.WriteString("WHERE ")
		_, _ = sb.WriteString(strings.Join(conds, " AND "))
		condition = sb.String()
	}
	return
}
