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

type FinanceEntryRepository interface {
	FindFinanceEntries(
		ctx context.Context,
		request *models.FinanceEntryFilter,
	) ([]*models.FinanceEntry, int64, error)
	FindFinanceEntry(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FinanceEntryFilter,
	) (*models.FinanceEntry, error)
	CreateFinanceEntry(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FinanceEntry,
	) (*models.FinanceEntry, error)
	UpdateFinanceEntry(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FinanceEntry,
	) (*models.FinanceEntry, error)
	DeleteFinanceEntries(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FinanceEntryFilter,
	) (int64, error)
}

type financeEntryRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewFinanceEntryRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) FinanceEntryRepository {
	return &financeEntryRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// financeEntryColumns is the SELECT list of finance_entries (alias t), in the order scanFinanceEntry reads it.
const financeEntryColumns = `t.id
	, t.branch_id
	, TO_CHAR(t.entry_date, 'YYYY-MM-DD')
	, t.type
	, t.category
	, t.amount::float8
	, t.payment_method
	, t.cash_drawer_id
	, (SELECT r.name FROM cash_drawers r WHERE r.id = t.cash_drawer_id)
	, t.description
	, t.attachment_url
	, t.created_at
	, t.updated_at`

// financeEntrySortings maps the sortable fields to their SQL expressions.
var financeEntrySortings = map[string]string{
	"id":               "t.id",
	"created_at":       "t.created_at",
	"updated_at":       "t.updated_at",
	"entry_date":       "t.entry_date",
	"type":             "t.type",
	"category":         "t.category",
	"amount":           "t.amount",
	"payment_method":   "t.payment_method",
	"cash_drawer_id":   "t.cash_drawer_id",
	"description":      "t.description",
	"attachment_url":   "t.attachment_url",
	"cash_drawer.name": "(SELECT r.name FROM cash_drawers r WHERE r.id = t.cash_drawer_id)",
}

func scanFinanceEntry(row pgx.Row) (*models.FinanceEntry, error) {
	var response models.FinanceEntry
	var cashDrawerName *string
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.EntryDate,
		&response.Type,
		&response.Category,
		&response.Amount,
		&response.PaymentMethod,
		&response.CashDrawerID,
		&cashDrawerName,
		&response.Description,
		&response.AttachmentURL,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	response.CashDrawer = models.NewReference(response.CashDrawerID, cashDrawerName)
	return &response, err
}

func (q *financeEntryRepository) FindFinanceEntries(
	ctx context.Context,
	request *models.FinanceEntryFilter,
) ([]*models.FinanceEntry, int64, error) {
	ctxt := "FinanceEntryRepository-FindFinanceEntries"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM finance_entries t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.FinanceEntry{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM finance_entries t %s ORDER BY ", financeEntryColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.entry_date DESC, t.id DESC")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from financeEntrySortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", financeEntrySortings[item.Field], item.Direction, item.Direction)
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
		financeEntry, err := scanFinanceEntry(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, financeEntry)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindFinanceEntry returns the first entry of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *financeEntryRepository) FindFinanceEntry(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FinanceEntryFilter,
) (*models.FinanceEntry, error) {
	ctxt := "FinanceEntryRepository-FindFinanceEntry"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM finance_entries t %s LIMIT 1", financeEntryColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanFinanceEntry(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrFinanceEntryNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *financeEntryRepository) CreateFinanceEntry(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FinanceEntry,
) (*models.FinanceEntry, error) {
	ctxt := "FinanceEntryRepository-CreateFinanceEntry"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO finance_entries (
		branch_id
		, entry_date
		, type
		, category
		, amount
		, payment_method
		, cash_drawer_id
		, description
		, attachment_url
		, created_at
		, updated_at
	) VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.EntryDate,
		request.Type,
		request.Category,
		request.Amount,
		request.PaymentMethod,
		request.CashDrawerID,
		request.Description,
		request.AttachmentURL,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrFinanceEntryConflict); !errors.Is(err, models.ErrFinanceEntryConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindFinanceEntry(ctx, tx, models.NewFinanceEntryFilter().WithFinanceEntryIDs(id))
}

// UpdateFinanceEntry locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *financeEntryRepository) UpdateFinanceEntry(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FinanceEntry,
) (*models.FinanceEntry, error) {
	ctxt := "FinanceEntryRepository-UpdateFinanceEntry"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT TO_CHAR(t.entry_date, 'YYYY-MM-DD')
		, t.type
		, t.category
		, t.amount::float8
		, t.payment_method
		, t.cash_drawer_id
		, t.description
		, t.attachment_url
	FROM finance_entries t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.FinanceEntry
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.EntryDate,
		&current.Type,
		&current.Category,
		&current.Amount,
		&current.PaymentMethod,
		&current.CashDrawerID,
		&current.Description,
		&current.AttachmentURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrFinanceEntryNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE finance_entries SET")
	if current.EntryDate != request.EntryDate {
		params = append(params, request.EntryDate)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " entry_date = $%d::date", n)
	}
	if current.Type != request.Type {
		params = append(params, request.Type)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " type = $%d", n)
	}
	if current.Category != request.Category {
		params = append(params, request.Category)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " category = $%d", n)
	}
	if current.Amount != request.Amount {
		params = append(params, request.Amount)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " amount = $%d", n)
	}
	if !equalPtr(current.PaymentMethod, request.PaymentMethod) {
		params = append(params, request.PaymentMethod)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " payment_method = $%d", n)
	}
	if !equalPtr(current.CashDrawerID, request.CashDrawerID) {
		params = append(params, request.CashDrawerID)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " cash_drawer_id = $%d", n)
	}
	if !equalPtr(current.Description, request.Description) {
		params = append(params, request.Description)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " description = $%d", n)
	}
	if !equalPtr(current.AttachmentURL, request.AttachmentURL) {
		params = append(params, request.AttachmentURL)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " attachment_url = $%d", n)
	}
	if len(params) > 0 {
		params = append(params, request.UpdatedAt)
		_, _ = fmt.Fprintf(sb, ", updated_at = $%d", len(params))
		params = append(params, request.ID)
		_, _ = fmt.Fprintf(sb, " WHERE id = $%d", len(params))
		if _, err = tx.Exec(ctx, sb.String(), params...); err != nil {
			if err = writeError(err, models.ErrFinanceEntryConflict); !errors.Is(err, models.ErrFinanceEntryConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindFinanceEntry(ctx, tx, models.NewFinanceEntryFilter().WithFinanceEntryIDs(request.ID))
}

// DeleteFinanceEntries deletes the finance entries of the filter and returns how many went.
func (q *financeEntryRepository) DeleteFinanceEntries(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FinanceEntryFilter,
) (int64, error) {
	ctxt := "FinanceEntryRepository-DeleteFinanceEntries"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("entry: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM finance_entries t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrFinanceEntryInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *financeEntryRepository) buildCondition(request *models.FinanceEntryFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.FinanceEntryIDs) > 0 {
		params = append(params, request.FinanceEntryIDs)
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
	if request.StartDate != "" {
		params = append(params, request.StartDate)
		conds = append(conds, helper.Sprintf("t.entry_date >= $%d::date", len(params)))
	}
	if request.EndDate != "" {
		params = append(params, request.EndDate)
		conds = append(conds, helper.Sprintf("t.entry_date <= $%d::date", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		n := len(params)
		conds = append(conds, helper.Sprintf("((t.category)::text ILIKE $%d OR (t.description)::text ILIKE $%d)", n, n))
	}
	if len(conds) > 0 {
		sb.Reset()
		_, _ = sb.WriteString("WHERE ")
		_, _ = sb.WriteString(strings.Join(conds, " AND "))
		condition = sb.String()
	}
	return
}
