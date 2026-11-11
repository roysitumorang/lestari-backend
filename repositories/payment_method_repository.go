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

type PaymentMethodRepository interface {
	FindPaymentMethods(
		ctx context.Context,
		request *models.PaymentMethodFilter,
	) ([]*models.PaymentMethod, int64, error)
	FindPaymentMethod(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentMethodFilter,
	) (*models.PaymentMethod, error)
	CreatePaymentMethod(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentMethod,
	) (*models.PaymentMethod, error)
	UpdatePaymentMethod(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentMethod,
	) (*models.PaymentMethod, error)
	DeletePaymentMethods(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentMethodFilter,
	) (int64, error)
}

type paymentMethodRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewPaymentMethodRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) PaymentMethodRepository {
	return &paymentMethodRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// paymentMethodColumns is the SELECT list of payment_methods (alias t), in the order scanPaymentMethod reads it.
const paymentMethodColumns = `t.id
	, t.branch_id
	, t.name
	, t.type
	, t.code
	, t.mdr_type
	, t.mdr_value::float8
	, t.charge_fee_to_customer
	, t.edc_card_rates
	, t.is_active
	, t.created_at
	, t.updated_at`

// paymentMethodSortings maps the sortable fields to their SQL expressions.
var paymentMethodSortings = map[string]string{
	"id":                     "t.id",
	"created_at":             "t.created_at",
	"updated_at":             "t.updated_at",
	"name":                   "t.name",
	"type":                   "t.type",
	"code":                   "t.code",
	"mdr_type":               "t.mdr_type",
	"mdr_value":              "t.mdr_value",
	"charge_fee_to_customer": "t.charge_fee_to_customer",
	"edc_card_rates":         "t.edc_card_rates",
	"is_active":              "t.is_active",
}

func scanPaymentMethod(row pgx.Row) (*models.PaymentMethod, error) {
	var response models.PaymentMethod
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.Type,
		&response.Code,
		&response.MdrType,
		&response.MdrValue,
		&response.ChargeFeeToCustomer,
		&response.EdcCardRates,
		&response.IsActive,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *paymentMethodRepository) FindPaymentMethods(
	ctx context.Context,
	request *models.PaymentMethodFilter,
) ([]*models.PaymentMethod, int64, error) {
	ctxt := "PaymentMethodRepository-FindPaymentMethods"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM payment_methods t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.PaymentMethod{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM payment_methods t %s ORDER BY ", paymentMethodColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.id")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from paymentMethodSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", paymentMethodSortings[item.Field], item.Direction, item.Direction)
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
		paymentMethod, err := scanPaymentMethod(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, paymentMethod)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindPaymentMethod returns the first payment method of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *paymentMethodRepository) FindPaymentMethod(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentMethodFilter,
) (*models.PaymentMethod, error) {
	ctxt := "PaymentMethodRepository-FindPaymentMethod"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM payment_methods t %s LIMIT 1", paymentMethodColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanPaymentMethod(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPaymentMethodNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *paymentMethodRepository) CreatePaymentMethod(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentMethod,
) (*models.PaymentMethod, error) {
	ctxt := "PaymentMethodRepository-CreatePaymentMethod"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO payment_methods (
		branch_id
		, name
		, type
		, code
		, mdr_type
		, mdr_value
		, charge_fee_to_customer
		, edc_card_rates
		, is_active
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.Name,
		request.Type,
		request.Code,
		request.MdrType,
		request.MdrValue,
		request.ChargeFeeToCustomer,
		request.EdcCardRates,
		request.IsActive,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrPaymentMethodConflict); !errors.Is(err, models.ErrPaymentMethodConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindPaymentMethod(ctx, tx, models.NewPaymentMethodFilter().WithPaymentMethodIDs(id))
}

// UpdatePaymentMethod locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *paymentMethodRepository) UpdatePaymentMethod(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentMethod,
) (*models.PaymentMethod, error) {
	ctxt := "PaymentMethodRepository-UpdatePaymentMethod"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.type
		, t.code
		, t.mdr_type
		, t.mdr_value::float8
		, t.charge_fee_to_customer
		, t.edc_card_rates
		, t.is_active
	FROM payment_methods t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.PaymentMethod
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.Type,
		&current.Code,
		&current.MdrType,
		&current.MdrValue,
		&current.ChargeFeeToCustomer,
		&current.EdcCardRates,
		&current.IsActive,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPaymentMethodNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE payment_methods SET")
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
	if !equalPtr(current.Code, request.Code) {
		params = append(params, request.Code)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " code = $%d", n)
	}
	if !equalPtr(current.MdrType, request.MdrType) {
		params = append(params, request.MdrType)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " mdr_type = $%d", n)
	}
	if !equalPtr(current.MdrValue, request.MdrValue) {
		params = append(params, request.MdrValue)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " mdr_value = $%d", n)
	}
	if current.ChargeFeeToCustomer != request.ChargeFeeToCustomer {
		params = append(params, request.ChargeFeeToCustomer)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " charge_fee_to_customer = $%d", n)
	}
	if !equalJSON(current.EdcCardRates, request.EdcCardRates) {
		params = append(params, request.EdcCardRates)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " edc_card_rates = $%d::jsonb", n)
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
			if err = writeError(err, models.ErrPaymentMethodConflict); !errors.Is(err, models.ErrPaymentMethodConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindPaymentMethod(ctx, tx, models.NewPaymentMethodFilter().WithPaymentMethodIDs(request.ID))
}

// DeletePaymentMethods deletes the payment methods of the filter and returns how many went.
func (q *paymentMethodRepository) DeletePaymentMethods(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentMethodFilter,
) (int64, error) {
	ctxt := "PaymentMethodRepository-DeletePaymentMethods"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("payment method: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM payment_methods t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrPaymentMethodInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *paymentMethodRepository) buildCondition(request *models.PaymentMethodFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.PaymentMethodIDs) > 0 {
		params = append(params, request.PaymentMethodIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.Type != nil {
		params = append(params, *request.Type)
		conds = append(conds, helper.Sprintf("t.type = $%d", len(params)))
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
