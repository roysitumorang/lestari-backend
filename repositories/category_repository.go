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

type CategoryRepository interface {
	FindCategories(
		ctx context.Context,
		request *models.CategoryFilter,
	) ([]*models.Category, int64, error)
	FindCategory(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CategoryFilter,
	) (*models.Category, error)
	CreateCategory(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Category,
	) (*models.Category, error)
	UpdateCategory(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Category,
	) (*models.Category, error)
	DeleteCategories(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CategoryFilter,
	) (int64, error)
}

type categoryRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewCategoryRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) CategoryRepository {
	return &categoryRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// categoryColumns is the SELECT list of categories (alias t), in the order scanCategory reads it.
const categoryColumns = `t.id
	, t.branch_id
	, t.name
	, t.color
	, t.sort_order::bigint
	, (SELECT count(*) FROM products p WHERE p.category_id = t.id AND p.deleted_at IS NULL)::bigint
	, t.description
	, t.created_at
	, t.updated_at`

// categorySortings maps the sortable fields to their SQL expressions.
var categorySortings = map[string]string{
	"id":            "t.id",
	"created_at":    "t.created_at",
	"updated_at":    "t.updated_at",
	"name":          "t.name",
	"color":         "t.color",
	"sort_order":    "t.sort_order",
	"product_count": "(SELECT count(*) FROM products p WHERE p.category_id = t.id AND p.deleted_at IS NULL)",
	"description":   "t.description",
}

func scanCategory(row pgx.Row) (*models.Category, error) {
	var response models.Category
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.Color,
		&response.SortOrder,
		&response.ProductCount,
		&response.Description,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *categoryRepository) FindCategories(
	ctx context.Context,
	request *models.CategoryFilter,
) ([]*models.Category, int64, error) {
	ctxt := "CategoryRepository-FindCategories"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM categories t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.Category{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM categories t %s ORDER BY ", categoryColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.sort_order NULLS LAST, t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from categorySortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", categorySortings[item.Field], item.Direction, item.Direction)
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
		category, err := scanCategory(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, category)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindCategory returns the first category of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *categoryRepository) FindCategory(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CategoryFilter,
) (*models.Category, error) {
	ctxt := "CategoryRepository-FindCategory"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM categories t %s LIMIT 1", categoryColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanCategory(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCategoryNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *categoryRepository) CreateCategory(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Category,
) (*models.Category, error) {
	ctxt := "CategoryRepository-CreateCategory"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO categories (
		branch_id
		, name
		, color
		, sort_order
		, description
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
		request.Color,
		request.SortOrder,
		request.Description,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrCategoryConflict); !errors.Is(err, models.ErrCategoryConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindCategory(ctx, tx, models.NewCategoryFilter().WithCategoryIDs(id))
}

// UpdateCategory locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *categoryRepository) UpdateCategory(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Category,
) (*models.Category, error) {
	ctxt := "CategoryRepository-UpdateCategory"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.color
		, t.sort_order::bigint
		, t.description
	FROM categories t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.Category
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.Color,
		&current.SortOrder,
		&current.Description,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCategoryNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE categories SET")
	if current.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
	}
	if !equalPtr(current.Color, request.Color) {
		params = append(params, request.Color)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " color = $%d", n)
	}
	if !equalPtr(current.SortOrder, request.SortOrder) {
		params = append(params, request.SortOrder)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " sort_order = $%d", n)
	}
	if !equalPtr(current.Description, request.Description) {
		params = append(params, request.Description)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " description = $%d", n)
	}
	if len(params) > 0 {
		params = append(params, request.UpdatedAt)
		_, _ = fmt.Fprintf(sb, ", updated_at = $%d", len(params))
		params = append(params, request.ID)
		_, _ = fmt.Fprintf(sb, " WHERE id = $%d", len(params))
		if _, err = tx.Exec(ctx, sb.String(), params...); err != nil {
			if err = writeError(err, models.ErrCategoryConflict); !errors.Is(err, models.ErrCategoryConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindCategory(ctx, tx, models.NewCategoryFilter().WithCategoryIDs(request.ID))
}

// DeleteCategories deletes the categories of the filter and returns how many went.
func (q *categoryRepository) DeleteCategories(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CategoryFilter,
) (int64, error) {
	ctxt := "CategoryRepository-DeleteCategories"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("category: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM categories t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrCategoryInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *categoryRepository) buildCondition(request *models.CategoryFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.CategoryIDs) > 0 {
		params = append(params, request.CategoryIDs)
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
