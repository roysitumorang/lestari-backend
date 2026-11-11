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

type DiscountRepository interface {
	FindDiscounts(
		ctx context.Context,
		request *models.DiscountFilter,
	) ([]*models.Discount, int64, error)
	FindDiscount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.DiscountFilter,
	) (*models.Discount, error)
	CreateDiscount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Discount,
	) (*models.Discount, error)
	UpdateDiscount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Discount,
	) (*models.Discount, error)
	DeleteDiscounts(
		ctx context.Context,
		tx pgx.Tx,
		request *models.DiscountFilter,
	) (int64, error)
}

type discountRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewDiscountRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) DiscountRepository {
	return &discountRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// discountColumns is the SELECT list of discounts (alias t), in the order scanDiscount reads it.
const discountColumns = `t.id
	, t.branch_id
	, t.name
	, t.type
	, t.value::float8
	, t.is_active
	, t.created_at
	, t.updated_at`

// discountSortings maps the sortable fields to their SQL expressions.
var discountSortings = map[string]string{
	"id":         "t.id",
	"created_at": "t.created_at",
	"updated_at": "t.updated_at",
	"name":       "t.name",
	"type":       "t.type",
	"value":      "t.value",
	"is_active":  "t.is_active",
}

func scanDiscount(row pgx.Row) (*models.Discount, error) {
	var response models.Discount
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.Type,
		&response.Value,
		&response.IsActive,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *discountRepository) FindDiscounts(
	ctx context.Context,
	request *models.DiscountFilter,
) ([]*models.Discount, int64, error) {
	ctxt := "DiscountRepository-FindDiscounts"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM discounts t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.Discount{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM discounts t %s ORDER BY ", discountColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from discountSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", discountSortings[item.Field], item.Direction, item.Direction)
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
		discount, err := scanDiscount(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, discount)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindDiscount returns the first discount of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *discountRepository) FindDiscount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.DiscountFilter,
) (*models.Discount, error) {
	ctxt := "DiscountRepository-FindDiscount"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM discounts t %s LIMIT 1", discountColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanDiscount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrDiscountNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *discountRepository) CreateDiscount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Discount,
) (*models.Discount, error) {
	ctxt := "DiscountRepository-CreateDiscount"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO discounts (
		branch_id
		, name
		, type
		, value
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
		request.Type,
		request.Value,
		request.IsActive,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrDiscountConflict); !errors.Is(err, models.ErrDiscountConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindDiscount(ctx, tx, models.NewDiscountFilter().WithDiscountIDs(id))
}

// UpdateDiscount locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *discountRepository) UpdateDiscount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Discount,
) (*models.Discount, error) {
	ctxt := "DiscountRepository-UpdateDiscount"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.type
		, t.value::float8
		, t.is_active
	FROM discounts t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.Discount
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.Type,
		&current.Value,
		&current.IsActive,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrDiscountNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE discounts SET")
	if current.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
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
			if err = writeError(err, models.ErrDiscountConflict); !errors.Is(err, models.ErrDiscountConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindDiscount(ctx, tx, models.NewDiscountFilter().WithDiscountIDs(request.ID))
}

// DeleteDiscounts deletes the discounts of the filter and returns how many went.
func (q *discountRepository) DeleteDiscounts(
	ctx context.Context,
	tx pgx.Tx,
	request *models.DiscountFilter,
) (int64, error) {
	ctxt := "DiscountRepository-DeleteDiscounts"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("discount: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM discounts t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrDiscountInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *discountRepository) buildCondition(request *models.DiscountFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.DiscountIDs) > 0 {
		params = append(params, request.DiscountIDs)
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
