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

type PaymentAccountRepository interface {
	FindPaymentAccounts(
		ctx context.Context,
		request *models.PaymentAccountFilter,
	) ([]*models.PaymentAccount, int64, error)
	FindPaymentAccount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentAccountFilter,
	) (*models.PaymentAccount, error)
	CreatePaymentAccount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentAccount,
	) (*models.PaymentAccount, error)
	UpdatePaymentAccount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentAccount,
	) (*models.PaymentAccount, error)
	DeletePaymentAccounts(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentAccountFilter,
	) (int64, error)
}

type paymentAccountRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewPaymentAccountRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) PaymentAccountRepository {
	return &paymentAccountRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// paymentAccountColumns is the SELECT list of catalog_payment_accounts (alias t), in the order scanPaymentAccount reads it.
const paymentAccountColumns = `t.id
	, t.branch_id
	, t.type
	, t.provider
	, t.account_name
	, t.account_number
	, t.qris_image_url
	, t.is_active
	, t.created_at
	, t.updated_at`

// paymentAccountSortings maps the sortable fields to their SQL expressions.
var paymentAccountSortings = map[string]string{
	"id":             "t.id",
	"created_at":     "t.created_at",
	"updated_at":     "t.updated_at",
	"type":           "t.type",
	"provider":       "t.provider",
	"account_name":   "t.account_name",
	"account_number": "t.account_number",
	"qris_image_url": "t.qris_image_url",
	"is_active":      "t.is_active",
}

func scanPaymentAccount(row pgx.Row) (*models.PaymentAccount, error) {
	var response models.PaymentAccount
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Type,
		&response.Provider,
		&response.AccountName,
		&response.AccountNumber,
		&response.QRISImageURL,
		&response.IsActive,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *paymentAccountRepository) FindPaymentAccounts(
	ctx context.Context,
	request *models.PaymentAccountFilter,
) ([]*models.PaymentAccount, int64, error) {
	ctxt := "PaymentAccountRepository-FindPaymentAccounts"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM catalog_payment_accounts t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.PaymentAccount{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM catalog_payment_accounts t %s ORDER BY ", paymentAccountColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.type, t.provider")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from paymentAccountSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", paymentAccountSortings[item.Field], item.Direction, item.Direction)
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
		paymentAccount, err := scanPaymentAccount(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, paymentAccount)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindPaymentAccount returns the first account of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *paymentAccountRepository) FindPaymentAccount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentAccountFilter,
) (*models.PaymentAccount, error) {
	ctxt := "PaymentAccountRepository-FindPaymentAccount"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM catalog_payment_accounts t %s LIMIT 1", paymentAccountColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanPaymentAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPaymentAccountNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *paymentAccountRepository) CreatePaymentAccount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentAccount,
) (*models.PaymentAccount, error) {
	ctxt := "PaymentAccountRepository-CreatePaymentAccount"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO catalog_payment_accounts (
		branch_id
		, type
		, provider
		, account_name
		, account_number
		, qris_image_url
		, is_active
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.Type,
		request.Provider,
		request.AccountName,
		request.AccountNumber,
		request.QRISImageURL,
		request.IsActive,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrPaymentAccountConflict); !errors.Is(err, models.ErrPaymentAccountConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindPaymentAccount(ctx, tx, models.NewPaymentAccountFilter().WithPaymentAccountIDs(id))
}

// UpdatePaymentAccount locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *paymentAccountRepository) UpdatePaymentAccount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentAccount,
) (*models.PaymentAccount, error) {
	ctxt := "PaymentAccountRepository-UpdatePaymentAccount"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.type
		, t.provider
		, t.account_name
		, t.account_number
		, t.qris_image_url
		, t.is_active
	FROM catalog_payment_accounts t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.PaymentAccount
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Type,
		&current.Provider,
		&current.AccountName,
		&current.AccountNumber,
		&current.QRISImageURL,
		&current.IsActive,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPaymentAccountNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE catalog_payment_accounts SET")
	if current.Type != request.Type {
		params = append(params, request.Type)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " type = $%d", n)
	}
	if current.Provider != request.Provider {
		params = append(params, request.Provider)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " provider = $%d", n)
	}
	if current.AccountName != request.AccountName {
		params = append(params, request.AccountName)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " account_name = $%d", n)
	}
	if !equalPtr(current.AccountNumber, request.AccountNumber) {
		params = append(params, request.AccountNumber)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " account_number = $%d", n)
	}
	if !equalPtr(current.QRISImageURL, request.QRISImageURL) {
		params = append(params, request.QRISImageURL)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " qris_image_url = $%d", n)
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
			if err = writeError(err, models.ErrPaymentAccountConflict); !errors.Is(err, models.ErrPaymentAccountConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindPaymentAccount(ctx, tx, models.NewPaymentAccountFilter().WithPaymentAccountIDs(request.ID))
}

// DeletePaymentAccounts deletes the payout accounts of the filter and returns how many went.
func (q *paymentAccountRepository) DeletePaymentAccounts(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentAccountFilter,
) (int64, error) {
	ctxt := "PaymentAccountRepository-DeletePaymentAccounts"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("account: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM catalog_payment_accounts t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrPaymentAccountInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *paymentAccountRepository) buildCondition(request *models.PaymentAccountFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.PaymentAccountIDs) > 0 {
		params = append(params, request.PaymentAccountIDs)
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
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		n := len(params)
		conds = append(conds, helper.Sprintf("((t.provider)::text ILIKE $%d OR (t.account_name)::text ILIKE $%d)", n, n))
	}
	if len(conds) > 0 {
		sb.Reset()
		_, _ = sb.WriteString("WHERE ")
		_, _ = sb.WriteString(strings.Join(conds, " AND "))
		condition = sb.String()
	}
	return
}
