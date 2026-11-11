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

type TaxRepository interface {
	FindTaxes(
		ctx context.Context,
		request *models.TaxFilter,
	) ([]*models.Tax, int64, error)
	FindTax(
		ctx context.Context,
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

const taxColumns = `id
	, branch_id
	, name
	, rate::float8
	, is_inclusive
	, is_active
	, created_at
	, updated_at`

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
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM taxes %s", condition)
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
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM taxes %s ORDER BY", taxColumns, condition)
	sortBy := request.Sortings
	if len(sortBy) == 0 {
		sortBy = append(sortBy, models.DefaultTaxSortBy)
	}
	for i, item := range sortBy {
		if i > 0 {
			_, _ = sb.WriteString(",")
		}
		// the field comes from models.MapTaxSortings (see sanitizers.FindTaxes), never from the request text
		_, _ = fmt.Fprintf(sb, " %s %s", item.Field, item.Direction)
	}
	_, _ = fmt.Fprintf(sb, ", id %s", sortBy[len(sortBy)-1].Direction)
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

func (q *taxRepository) FindTax(
	ctx context.Context,
	request *models.TaxFilter,
) (*models.Tax, error) {
	ctxt := "TaxRepository-FindTax"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM taxes %s LIMIT 1", taxColumns, condition)
	response, err := scanTax(q.dbRead.QueryRow(ctx, sb.String(), params...))
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
	response, err := scanTax(tx.QueryRow(
		ctx,
		`INSERT INTO taxes (
			branch_id
			, name
			, rate
			, is_inclusive
			, is_active
			, created_at
			, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+taxColumns,
		request.BranchID,
		request.Name,
		request.Rate,
		request.IsInclusive,
		request.IsActive,
		request.CreatedAt,
		request.UpdatedAt,
	))
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

// UpdateTax locks the row and writes only the attributes whose value differs from the stored one. When
// nothing changed no UPDATE is executed and the stored row is returned as it is; otherwise updated_at is set
// to request.UpdatedAt (the caller's time.Now()).
func (q *taxRepository) UpdateTax(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Tax,
) (*models.Tax, error) {
	ctxt := "TaxRepository-UpdateTax"
	response, err := scanTax(tx.QueryRow(
		ctx,
		`SELECT `+taxColumns+`
		FROM taxes
		WHERE id = $1
			AND branch_id = $2
		FOR NO KEY UPDATE`,
		request.ID,
		request.BranchID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrTaxNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString("UPDATE taxes SET")
	if response.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
	}
	if response.Rate != request.Rate {
		params = append(params, request.Rate)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " rate = $%d", n)
	}
	if response.IsInclusive != request.IsInclusive {
		params = append(params, request.IsInclusive)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " is_inclusive = $%d", n)
	}
	if response.IsActive != request.IsActive {
		params = append(params, request.IsActive)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " is_active = $%d", n)
	}
	if len(params) == 0 {
		return response, nil
	}
	params = append(params, request.UpdatedAt)
	_, _ = fmt.Fprintf(sb, ", updated_at = $%d", len(params))
	params = append(params, request.ID)
	_, _ = fmt.Fprintf(sb, " WHERE id = $%d RETURNING %s", len(params), taxColumns)
	if response, err = scanTax(tx.QueryRow(ctx, sb.String(), params...)); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

// DeleteTaxes deletes the taxes matching the filter and returns how many went. Products and sale lines that
// used a deleted tax keep working without it (their tax_id becomes NULL).
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
	_, _ = fmt.Fprintf(sb, "DELETE FROM taxes %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
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
		conds = append(conds, helper.Sprintf("id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("branch_id = ANY($%d)", len(params)))
	}
	if request.IsActive != nil {
		params = append(params, *request.IsActive)
		conds = append(conds, helper.Sprintf("is_active = $%d", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+strings.ToLower(request.Keyword)+"%")
		conds = append(conds, helper.Sprintf("LOWER(name) LIKE $%d", len(params)))
	}
	if len(conds) > 0 {
		sb.Reset()
		_, _ = sb.WriteString("WHERE ")
		_, _ = sb.WriteString(strings.Join(conds, " AND "))
		condition = sb.String()
	}
	return
}
