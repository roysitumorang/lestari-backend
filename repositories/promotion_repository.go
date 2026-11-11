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

type PromotionRepository interface {
	FindPromotions(
		ctx context.Context,
		request *models.PromotionFilter,
	) ([]*models.Promotion, int64, error)
	FindPromotion(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PromotionFilter,
	) (*models.Promotion, error)
	CreatePromotion(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Promotion,
	) (*models.Promotion, error)
	UpdatePromotion(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Promotion,
	) (*models.Promotion, error)
	DeletePromotions(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PromotionFilter,
	) (int64, error)
}

type promotionRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewPromotionRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) PromotionRepository {
	return &promotionRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// promotionColumns is the SELECT list of promotions (alias t), in the order scanPromotion reads it.
const promotionColumns = `t.id
	, t.branch_id
	, t.name
	, t.type
	, TO_CHAR(t.start_date, 'YYYY-MM-DD')
	, TO_CHAR(t.end_date, 'YYYY-MM-DD')
	, t.start_time
	, t.end_time
	, t.days
	, t.customer_type_ids
	, t.discount_type
	, t.discount_value::float8
	, t.minimum_purchase::float8
	, t.buy_quantity::float8
	, t.get_quantity::float8
	, t.products
	, t.is_active
	, t.description
	, t.created_at
	, t.updated_at`

// promotionSortings maps the sortable fields to their SQL expressions.
var promotionSortings = map[string]string{
	"id":                "t.id",
	"created_at":        "t.created_at",
	"updated_at":        "t.updated_at",
	"name":              "t.name",
	"type":              "t.type",
	"start_date":        "t.start_date",
	"end_date":          "t.end_date",
	"start_time":        "t.start_time",
	"end_time":          "t.end_time",
	"days":              "t.days",
	"customer_type_ids": "t.customer_type_ids",
	"discount_type":     "t.discount_type",
	"discount_value":    "t.discount_value",
	"minimum_purchase":  "t.minimum_purchase",
	"buy_quantity":      "t.buy_quantity",
	"get_quantity":      "t.get_quantity",
	"products":          "t.products",
	"is_active":         "t.is_active",
	"description":       "t.description",
}

func scanPromotion(row pgx.Row) (*models.Promotion, error) {
	var response models.Promotion
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.Type,
		&response.StartDate,
		&response.EndDate,
		&response.StartTime,
		&response.EndTime,
		&response.Days,
		&response.CustomerTypeIds,
		&response.DiscountType,
		&response.DiscountValue,
		&response.MinimumPurchase,
		&response.BuyQuantity,
		&response.GetQuantity,
		&response.Products,
		&response.IsActive,
		&response.Description,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *promotionRepository) FindPromotions(
	ctx context.Context,
	request *models.PromotionFilter,
) ([]*models.Promotion, int64, error) {
	ctxt := "PromotionRepository-FindPromotions"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM promotions t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.Promotion{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM promotions t %s ORDER BY ", promotionColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.start_date DESC, t.id DESC")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from promotionSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", promotionSortings[item.Field], item.Direction, item.Direction)
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
		promotion, err := scanPromotion(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, promotion)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindPromotion returns the first promotion of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *promotionRepository) FindPromotion(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PromotionFilter,
) (*models.Promotion, error) {
	ctxt := "PromotionRepository-FindPromotion"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM promotions t %s LIMIT 1", promotionColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanPromotion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPromotionNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *promotionRepository) CreatePromotion(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Promotion,
) (*models.Promotion, error) {
	ctxt := "PromotionRepository-CreatePromotion"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO promotions (
		branch_id
		, name
		, type
		, start_date
		, end_date
		, start_time
		, end_time
		, days
		, customer_type_ids
		, discount_type
		, discount_value
		, minimum_purchase
		, buy_quantity
		, get_quantity
		, products
		, is_active
		, description
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4::date, $5::date, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16, $17, $18, $19)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.Name,
		request.Type,
		request.StartDate,
		request.EndDate,
		request.StartTime,
		request.EndTime,
		request.Days,
		request.CustomerTypeIds,
		request.DiscountType,
		request.DiscountValue,
		request.MinimumPurchase,
		request.BuyQuantity,
		request.GetQuantity,
		request.Products,
		request.IsActive,
		request.Description,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrPromotionConflict); !errors.Is(err, models.ErrPromotionConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindPromotion(ctx, tx, models.NewPromotionFilter().WithPromotionIDs(id))
}

// UpdatePromotion locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *promotionRepository) UpdatePromotion(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Promotion,
) (*models.Promotion, error) {
	ctxt := "PromotionRepository-UpdatePromotion"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.type
		, TO_CHAR(t.start_date, 'YYYY-MM-DD')
		, TO_CHAR(t.end_date, 'YYYY-MM-DD')
		, t.start_time
		, t.end_time
		, t.days
		, t.customer_type_ids
		, t.discount_type
		, t.discount_value::float8
		, t.minimum_purchase::float8
		, t.buy_quantity::float8
		, t.get_quantity::float8
		, t.products
		, t.is_active
		, t.description
	FROM promotions t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.Promotion
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.Type,
		&current.StartDate,
		&current.EndDate,
		&current.StartTime,
		&current.EndTime,
		&current.Days,
		&current.CustomerTypeIds,
		&current.DiscountType,
		&current.DiscountValue,
		&current.MinimumPurchase,
		&current.BuyQuantity,
		&current.GetQuantity,
		&current.Products,
		&current.IsActive,
		&current.Description,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPromotionNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE promotions SET")
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
	if current.StartDate != request.StartDate {
		params = append(params, request.StartDate)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " start_date = $%d::date", n)
	}
	if current.EndDate != request.EndDate {
		params = append(params, request.EndDate)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " end_date = $%d::date", n)
	}
	if !equalPtr(current.StartTime, request.StartTime) {
		params = append(params, request.StartTime)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " start_time = $%d", n)
	}
	if !equalPtr(current.EndTime, request.EndTime) {
		params = append(params, request.EndTime)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " end_time = $%d", n)
	}
	if !slices.Equal(current.Days, request.Days) {
		params = append(params, request.Days)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " days = $%d", n)
	}
	if !slices.Equal(current.CustomerTypeIds, request.CustomerTypeIds) {
		params = append(params, request.CustomerTypeIds)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " customer_type_ids = $%d", n)
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
	if !equalPtr(current.MinimumPurchase, request.MinimumPurchase) {
		params = append(params, request.MinimumPurchase)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " minimum_purchase = $%d", n)
	}
	if !equalPtr(current.BuyQuantity, request.BuyQuantity) {
		params = append(params, request.BuyQuantity)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " buy_quantity = $%d", n)
	}
	if !equalPtr(current.GetQuantity, request.GetQuantity) {
		params = append(params, request.GetQuantity)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " get_quantity = $%d", n)
	}
	if !equalJSON(current.Products, request.Products) {
		params = append(params, request.Products)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " products = $%d::jsonb", n)
	}
	if current.IsActive != request.IsActive {
		params = append(params, request.IsActive)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " is_active = $%d", n)
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
			if err = writeError(err, models.ErrPromotionConflict); !errors.Is(err, models.ErrPromotionConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindPromotion(ctx, tx, models.NewPromotionFilter().WithPromotionIDs(request.ID))
}

// DeletePromotions deletes the promotions of the filter and returns how many went.
func (q *promotionRepository) DeletePromotions(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PromotionFilter,
) (int64, error) {
	ctxt := "PromotionRepository-DeletePromotions"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("promotion: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM promotions t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrPromotionInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *promotionRepository) buildCondition(request *models.PromotionFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.PromotionIDs) > 0 {
		params = append(params, request.PromotionIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.Status != "" {
		params = append(params, request.Status)
		conds = append(conds, helper.Sprintf("(CASE $%d WHEN 'active' THEN t.is_active AND t.start_date <= current_date AND t.end_date >= current_date WHEN 'scheduled' THEN t.is_active AND t.start_date > current_date WHEN 'ended' THEN t.end_date < current_date OR NOT t.is_active ELSE true END)", len(params)))
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
