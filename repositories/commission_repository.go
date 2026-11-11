package repositories

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

type CommissionRepository interface {
	FindCommissions(
		ctx context.Context,
		request *models.CommissionFilter,
	) ([]*models.Commission, int64, error)
	FindCommission(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CommissionFilter,
	) (*models.Commission, error)
	CreateCommission(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Commission,
	) (*models.Commission, error)
	UpdateCommission(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Commission,
	) (*models.Commission, error)
	DeleteCommissions(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CommissionFilter,
	) (int64, error)
}

type commissionRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewCommissionRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) CommissionRepository {
	return &commissionRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// commissionColumns is the SELECT list of commissions (alias t), in the order scanCommission reads it.
const commissionColumns = `t.id
	, t.branch_id
	, t.name
	, t.basis
	, t.type
	, t.value::float8
	, t.staff_ids
	, t.product_ids
	, t.is_active
	, t.created_at
	, t.updated_at`

// commissionSortings maps the sortable fields to their SQL expressions.
var commissionSortings = map[string]string{
	"id":          "t.id",
	"created_at":  "t.created_at",
	"updated_at":  "t.updated_at",
	"name":        "t.name",
	"basis":       "t.basis",
	"type":        "t.type",
	"value":       "t.value",
	"staff_ids":   "t.staff_ids",
	"product_ids": "t.product_ids",
	"is_active":   "t.is_active",
}

func scanCommission(row pgx.Row) (*models.Commission, error) {
	var response models.Commission
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.Basis,
		&response.Type,
		&response.Value,
		&response.StaffIds,
		&response.ProductIds,
		&response.IsActive,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *commissionRepository) FindCommissions(
	ctx context.Context,
	request *models.CommissionFilter,
) ([]*models.Commission, int64, error) {
	ctxt := "CommissionRepository-FindCommissions"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM commissions t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.Commission{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM commissions t %s ORDER BY ", commissionColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from commissionSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", commissionSortings[item.Field], item.Direction, item.Direction)
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
		commission, err := scanCommission(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, commission)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindCommission returns the first commission rule of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *commissionRepository) FindCommission(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CommissionFilter,
) (*models.Commission, error) {
	ctxt := "CommissionRepository-FindCommission"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM commissions t %s LIMIT 1", commissionColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanCommission(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCommissionNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *commissionRepository) CreateCommission(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Commission,
) (*models.Commission, error) {
	ctxt := "CommissionRepository-CreateCommission"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO commissions (
		branch_id
		, name
		, basis
		, type
		, value
		, staff_ids
		, product_ids
		, is_active
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.Name,
		request.Basis,
		request.Type,
		request.Value,
		request.StaffIds,
		request.ProductIds,
		request.IsActive,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrCommissionConflict); !errors.Is(err, models.ErrCommissionConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindCommission(ctx, tx, models.NewCommissionFilter().WithCommissionIDs(id))
}

// UpdateCommission locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *commissionRepository) UpdateCommission(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Commission,
) (*models.Commission, error) {
	ctxt := "CommissionRepository-UpdateCommission"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.basis
		, t.type
		, t.value::float8
		, t.staff_ids
		, t.product_ids
		, t.is_active
	FROM commissions t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.Commission
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.Basis,
		&current.Type,
		&current.Value,
		&current.StaffIds,
		&current.ProductIds,
		&current.IsActive,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCommissionNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE commissions SET")
	if current.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
	}
	if current.Basis != request.Basis {
		params = append(params, request.Basis)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " basis = $%d", n)
	}
	if current.Type != request.Type {
		params = append(params, request.Type)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " type = $%d", n)
	}
	if current.Value != request.Value {
		params = append(params, request.Value)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " value = $%d", n)
	}
	if !slices.Equal(current.StaffIds, request.StaffIds) {
		params = append(params, request.StaffIds)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " staff_ids = $%d", n)
	}
	if !slices.Equal(current.ProductIds, request.ProductIds) {
		params = append(params, request.ProductIds)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " product_ids = $%d", n)
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
			if err = writeError(err, models.ErrCommissionConflict); !errors.Is(err, models.ErrCommissionConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindCommission(ctx, tx, models.NewCommissionFilter().WithCommissionIDs(request.ID))
}

// DeleteCommissions deletes the commissions of the filter and returns how many went.
func (q *commissionRepository) DeleteCommissions(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CommissionFilter,
) (int64, error) {
	ctxt := "CommissionRepository-DeleteCommissions"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("commission rule: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM commissions t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrCommissionInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *commissionRepository) buildCondition(request *models.CommissionFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.CommissionIDs) > 0 {
		params = append(params, request.CommissionIDs)
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
