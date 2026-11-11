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

type TaxRepository interface {
	FindTaxes(
		ctx context.Context,
		request *models.TaxFilter,
	) ([]*models.Tax, int64, error)
	FindTax(
		ctx context.Context,
		tx pgx.Tx,
		request *models.TaxFilter,
	) (*models.Tax, error)
	CreateTax(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Tax,
	) (*models.Tax, error)
	UpdateTax(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Tax,
	) (*models.Tax, error)
	DeleteTaxes(
		ctx context.Context,
		tx pgx.Tx,
		request *models.TaxFilter,
	) (int64, error)
}

type taxRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewTaxRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) TaxRepository {
	return &taxRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// taxColumns is the SELECT list of taxes (alias t), in the order scanTax reads it.
const taxColumns = `t.id
	, t.branch_id
	, t.name
	, t.rate::float8
	, t.is_inclusive
	, t.is_active
	, t.created_at
	, t.updated_at`

// taxSortings maps the sortable fields to their SQL expressions.
var taxSortings = map[string]string{
	"id":           "t.id",
	"created_at":   "t.created_at",
	"updated_at":   "t.updated_at",
	"name":         "t.name",
	"rate":         "t.rate",
	"is_inclusive": "t.is_inclusive",
	"is_active":    "t.is_active",
}

func scanTax(row pgx.Row) (*models.Tax, error) {
	var response models.Tax
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.Rate,
		&response.IsInclusive,
		&response.IsActive,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *taxRepository) FindTaxes(
	ctx context.Context,
	request *models.TaxFilter,
) ([]*models.Tax, int64, error) {
	ctxt := "TaxRepository-FindTaxes"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM taxes t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.Tax{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM taxes t %s ORDER BY ", taxColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from taxSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", taxSortings[item.Field], item.Direction, item.Direction)
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
		tax, err := scanTax(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, tax)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindTax returns the first tax of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *taxRepository) FindTax(
	ctx context.Context,
	tx pgx.Tx,
	request *models.TaxFilter,
) (*models.Tax, error) {
	ctxt := "TaxRepository-FindTax"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM taxes t %s LIMIT 1", taxColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanTax(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrTaxNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *taxRepository) CreateTax(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Tax,
) (*models.Tax, error) {
	ctxt := "TaxRepository-CreateTax"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO taxes (
		branch_id
		, name
		, rate
		, is_inclusive
		, is_active
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
		request.Rate,
		request.IsInclusive,
		request.IsActive,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrTaxConflict); !errors.Is(err, models.ErrTaxConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindTax(ctx, tx, models.NewTaxFilter().WithTaxIDs(id))
}

// UpdateTax locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *taxRepository) UpdateTax(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Tax,
) (*models.Tax, error) {
	ctxt := "TaxRepository-UpdateTax"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.rate::float8
		, t.is_inclusive
		, t.is_active
	FROM taxes t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.Tax
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.Rate,
		&current.IsInclusive,
		&current.IsActive,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrTaxNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE taxes SET")
	if current.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
	}
	if current.Rate != request.Rate {
		params = append(params, request.Rate)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " rate = $%d", n)
	}
	if current.IsInclusive != request.IsInclusive {
		params = append(params, request.IsInclusive)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " is_inclusive = $%d", n)
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
			if err = writeError(err, models.ErrTaxConflict); !errors.Is(err, models.ErrTaxConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindTax(ctx, tx, models.NewTaxFilter().WithTaxIDs(request.ID))
}

// DeleteTaxes deletes the taxes of the filter and returns how many went.
func (q *taxRepository) DeleteTaxes(
	ctx context.Context,
	tx pgx.Tx,
	request *models.TaxFilter,
) (int64, error) {
	ctxt := "TaxRepository-DeleteTaxes"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("tax: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM taxes t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrTaxInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *taxRepository) buildCondition(request *models.TaxFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.TaxIDs) > 0 {
		params = append(params, request.TaxIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.IsActive != nil {
		params = append(params, *request.IsActive)
		conds = append(conds, helper.Sprintf("t.is_active = $%d", len(params)))
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
