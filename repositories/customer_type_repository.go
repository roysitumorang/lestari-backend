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

type CustomerTypeRepository interface {
	FindCustomerTypes(
		ctx context.Context,
		request *models.CustomerTypeFilter,
	) ([]*models.CustomerType, int64, error)
	FindCustomerType(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerTypeFilter,
	) (*models.CustomerType, error)
	CreateCustomerType(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerType,
	) (*models.CustomerType, error)
	UpdateCustomerType(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerType,
	) (*models.CustomerType, error)
	DeleteCustomerTypes(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerTypeFilter,
	) (int64, error)
}

type customerTypeRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewCustomerTypeRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) CustomerTypeRepository {
	return &customerTypeRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// customerTypeColumns is the SELECT list of customer_types (alias t), in the order scanCustomerType reads it.
const customerTypeColumns = `t.id
	, t.branch_id
	, t.name
	, t.discount_type
	, t.discount_value::float8
	, t.point_multiplier::float8
	, (SELECT count(*) FROM customers c WHERE c.customer_type_id = t.id)::bigint
	, t.description
	, t.created_at
	, t.updated_at`

// customerTypeSortings maps the sortable fields to their SQL expressions.
var customerTypeSortings = map[string]string{
	"id":               "t.id",
	"created_at":       "t.created_at",
	"updated_at":       "t.updated_at",
	"name":             "t.name",
	"discount_type":    "t.discount_type",
	"discount_value":   "t.discount_value",
	"point_multiplier": "t.point_multiplier",
	"customer_count":   "(SELECT count(*) FROM customers c WHERE c.customer_type_id = t.id)",
	"description":      "t.description",
}

func scanCustomerType(row pgx.Row) (*models.CustomerType, error) {
	var response models.CustomerType
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.DiscountType,
		&response.DiscountValue,
		&response.PointMultiplier,
		&response.CustomerCount,
		&response.Description,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *customerTypeRepository) FindCustomerTypes(
	ctx context.Context,
	request *models.CustomerTypeFilter,
) ([]*models.CustomerType, int64, error) {
	ctxt := "CustomerTypeRepository-FindCustomerTypes"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM customer_types t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.CustomerType{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM customer_types t %s ORDER BY ", customerTypeColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from customerTypeSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", customerTypeSortings[item.Field], item.Direction, item.Direction)
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
		customerType, err := scanCustomerType(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, customerType)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindCustomerType returns the first customer type of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *customerTypeRepository) FindCustomerType(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerTypeFilter,
) (*models.CustomerType, error) {
	ctxt := "CustomerTypeRepository-FindCustomerType"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM customer_types t %s LIMIT 1", customerTypeColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanCustomerType(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCustomerTypeNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *customerTypeRepository) CreateCustomerType(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerType,
) (*models.CustomerType, error) {
	ctxt := "CustomerTypeRepository-CreateCustomerType"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO customer_types (
		branch_id
		, name
		, discount_type
		, discount_value
		, point_multiplier
		, description
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.Name,
		request.DiscountType,
		request.DiscountValue,
		request.PointMultiplier,
		request.Description,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrCustomerTypeConflict); !errors.Is(err, models.ErrCustomerTypeConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindCustomerType(ctx, tx, models.NewCustomerTypeFilter().WithCustomerTypeIDs(id))
}

// UpdateCustomerType locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *customerTypeRepository) UpdateCustomerType(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerType,
) (*models.CustomerType, error) {
	ctxt := "CustomerTypeRepository-UpdateCustomerType"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.discount_type
		, t.discount_value::float8
		, t.point_multiplier::float8
		, t.description
	FROM customer_types t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.CustomerType
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.DiscountType,
		&current.DiscountValue,
		&current.PointMultiplier,
		&current.Description,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCustomerTypeNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE customer_types SET")
	if current.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
	}
	if !equalPtr(current.DiscountType, request.DiscountType) {
		params = append(params, request.DiscountType)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " discount_type = $%d", n)
	}
	if !equalPtr(current.DiscountValue, request.DiscountValue) {
		params = append(params, request.DiscountValue)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " discount_value = $%d", n)
	}
	if !equalPtr(current.PointMultiplier, request.PointMultiplier) {
		params = append(params, request.PointMultiplier)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " point_multiplier = $%d", n)
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
			if err = writeError(err, models.ErrCustomerTypeConflict); !errors.Is(err, models.ErrCustomerTypeConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindCustomerType(ctx, tx, models.NewCustomerTypeFilter().WithCustomerTypeIDs(request.ID))
}

// DeleteCustomerTypes deletes the customer types of the filter and returns how many went.
func (q *customerTypeRepository) DeleteCustomerTypes(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerTypeFilter,
) (int64, error) {
	ctxt := "CustomerTypeRepository-DeleteCustomerTypes"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("customer type: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM customer_types t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrCustomerTypeInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *customerTypeRepository) buildCondition(request *models.CustomerTypeFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.CustomerTypeIDs) > 0 {
		params = append(params, request.CustomerTypeIDs)
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
