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

type FeeRepository interface {
	FindFees(
		ctx context.Context,
		request *models.FeeFilter,
	) ([]*models.Fee, int64, error)
	FindFee(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FeeFilter,
	) (*models.Fee, error)
	CreateFee(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Fee,
	) (*models.Fee, error)
	UpdateFee(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Fee,
	) (*models.Fee, error)
	DeleteFees(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FeeFilter,
	) (int64, error)
}

type feeRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewFeeRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) FeeRepository {
	return &feeRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// feeColumns is the SELECT list of fees (alias t), in the order scanFee reads it.
const feeColumns = `t.id
	, t.branch_id
	, t.name
	, t.amount_type
	, t.amount::float8
	, t.applies_to
	, t.created_at
	, t.updated_at`

// feeSortings maps the sortable fields to their SQL expressions.
var feeSortings = map[string]string{
	"id":          "t.id",
	"created_at":  "t.created_at",
	"updated_at":  "t.updated_at",
	"name":        "t.name",
	"amount_type": "t.amount_type",
	"amount":      "t.amount",
	"applies_to":  "t.applies_to",
}

func scanFee(row pgx.Row) (*models.Fee, error) {
	var response models.Fee
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.AmountType,
		&response.Amount,
		&response.AppliesTo,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *feeRepository) FindFees(
	ctx context.Context,
	request *models.FeeFilter,
) ([]*models.Fee, int64, error) {
	ctxt := "FeeRepository-FindFees"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM fees t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.Fee{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM fees t %s ORDER BY ", feeColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from feeSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", feeSortings[item.Field], item.Direction, item.Direction)
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
		fee, err := scanFee(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, fee)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindFee returns the first fee of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *feeRepository) FindFee(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FeeFilter,
) (*models.Fee, error) {
	ctxt := "FeeRepository-FindFee"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM fees t %s LIMIT 1", feeColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanFee(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrFeeNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *feeRepository) CreateFee(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Fee,
) (*models.Fee, error) {
	ctxt := "FeeRepository-CreateFee"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO fees (
		branch_id
		, name
		, amount_type
		, amount
		, applies_to
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.Name,
		request.AmountType,
		request.Amount,
		request.AppliesTo,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrFeeConflict); !errors.Is(err, models.ErrFeeConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindFee(ctx, tx, models.NewFeeFilter().WithFeeIDs(id))
}

// UpdateFee locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *feeRepository) UpdateFee(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Fee,
) (*models.Fee, error) {
	ctxt := "FeeRepository-UpdateFee"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.amount_type
		, t.amount::float8
		, t.applies_to
	FROM fees t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.Fee
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.AmountType,
		&current.Amount,
		&current.AppliesTo,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrFeeNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE fees SET")
	if current.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
	}
	if current.AmountType != request.AmountType {
		params = append(params, request.AmountType)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " amount_type = $%d", n)
	}
	if current.Amount != request.Amount {
		params = append(params, request.Amount)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " amount = $%d", n)
	}
	if current.AppliesTo != request.AppliesTo {
		params = append(params, request.AppliesTo)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " applies_to = $%d", n)
	}
	if len(params) > 0 {
		params = append(params, request.UpdatedAt)
		_, _ = fmt.Fprintf(sb, ", updated_at = $%d", len(params))
		params = append(params, request.ID)
		_, _ = fmt.Fprintf(sb, " WHERE id = $%d", len(params))
		if _, err = tx.Exec(ctx, sb.String(), params...); err != nil {
			if err = writeError(err, models.ErrFeeConflict); !errors.Is(err, models.ErrFeeConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindFee(ctx, tx, models.NewFeeFilter().WithFeeIDs(request.ID))
}

// DeleteFees deletes the fees of the filter and returns how many went.
func (q *feeRepository) DeleteFees(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FeeFilter,
) (int64, error) {
	ctxt := "FeeRepository-DeleteFees"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("fee: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM fees t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrFeeInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *feeRepository) buildCondition(request *models.FeeFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.FeeIDs) > 0 {
		params = append(params, request.FeeIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.AppliesTo != nil {
		params = append(params, *request.AppliesTo)
		conds = append(conds, helper.Sprintf("t.applies_to = $%d", len(params)))
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
