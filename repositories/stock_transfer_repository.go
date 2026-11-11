package repositories

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

type StockTransferRepository interface {
	FindStockTransfers(
		ctx context.Context,
		request *models.StockTransferFilter,
	) ([]*models.StockTransfer, int64, error)
	FindStockTransfer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockTransferFilter,
	) (*models.StockTransfer, error)
	// NextStockTransferNumber is the next document number of the store for a day (YYYYMMDD): TRF/20261108/0001.
	NextStockTransferNumber(
		ctx context.Context,
		tx pgx.Tx,
		storeID uuid.UUID,
		day string,
	) (string, error)
	CreateStockTransfer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockTransfer,
	) (*models.StockTransfer, error)
	CreateStockTransferItems(
		ctx context.Context,
		tx pgx.Tx,
		stockTransferID uuid.UUID,
		request []*models.StockTransferItem,
	) error
	FindStockTransferItems(
		ctx context.Context,
		tx pgx.Tx,
		stockTransferID uuid.UUID,
	) ([]*models.StockTransferItem, error)
	// LockStockTransfer locks the row and returns its number, status and branches.
	LockStockTransfer(
		ctx context.Context,
		tx pgx.Tx,
		storeID,
		stockTransferID uuid.UUID,
	) (*models.StockTransfer, error)
	UpdateReceivedQuantity(
		ctx context.Context,
		tx pgx.Tx,
		stockTransferID,
		productID uuid.UUID,
		receivedQuantity float64,
	) error
	// UpdateStockTransferStatus stores the outcome; an empty note or reason keeps what is stored.
	UpdateStockTransferStatus(
		ctx context.Context,
		tx pgx.Tx,
		stockTransferID uuid.UUID,
		status,
		note,
		rejectReason string,
		updatedAt time.Time,
	) error
	DeleteStockTransfers(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockTransferFilter,
	) (int64, error)
}

type stockTransferRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewStockTransferRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) StockTransferRepository {
	return &stockTransferRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// stockTransferColumns is the SELECT list of stock_transfers (alias t), in the order scanStockTransfer reads it.
const stockTransferColumns = `t.id
	, t.store_id
	, t.transfer_number
	, t.from_branch_id
	, (SELECT r.name FROM branches r WHERE r.id = t.from_branch_id)
	, t.to_branch_id
	, (SELECT r.name FROM branches r WHERE r.id = t.to_branch_id)
	, TO_CHAR(t.transfer_date, 'YYYY-MM-DD')
	, t.requires_confirmation
	, t.status
	, t.note
	, t.reject_reason
	, (SELECT COALESCE(SUM(i.quantity), 0) FROM stock_transfer_items i WHERE i.transfer_id = t.id)::float8
	, (SELECT COUNT(1) FROM stock_transfer_items i WHERE i.transfer_id = t.id)::bigint
	, t.created_at
	, t.updated_at`

// stockTransferSortings maps the sortable fields to their SQL expressions.
var stockTransferSortings = map[string]string{
	"id":                    "t.id",
	"created_at":            "t.created_at",
	"updated_at":            "t.updated_at",
	"transfer_number":       "t.transfer_number",
	"from_branch_id":        "t.from_branch_id",
	"to_branch_id":          "t.to_branch_id",
	"transfer_date":         "t.transfer_date",
	"requires_confirmation": "t.requires_confirmation",
	"status":                "t.status",
	"note":                  "t.note",
	"reject_reason":         "t.reject_reason",
	"total_quantity":        "(SELECT COALESCE(SUM(i.quantity), 0) FROM stock_transfer_items i WHERE i.transfer_id = t.id)",
	"item_count":            "(SELECT COUNT(1) FROM stock_transfer_items i WHERE i.transfer_id = t.id)",
	"from_branch.name":      "(SELECT r.name FROM branches r WHERE r.id = t.from_branch_id)",
	"to_branch.name":        "(SELECT r.name FROM branches r WHERE r.id = t.to_branch_id)",
}

func scanStockTransfer(row pgx.Row) (*models.StockTransfer, error) {
	var (
		response models.StockTransfer
		fromBranchName,
		toBranchName *string
	)
	err := row.Scan(
		&response.ID,
		&response.StoreID,
		&response.TransferNumber,
		&response.FromBranchID,
		&fromBranchName,
		&response.ToBranchID,
		&toBranchName,
		&response.TransferDate,
		&response.RequiresConfirmation,
		&response.Status,
		&response.Note,
		&response.RejectReason,
		&response.TotalQuantity,
		&response.ItemCount,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	response.FromBranch = models.NewReference(&response.FromBranchID, fromBranchName)
	response.ToBranch = models.NewReference(&response.ToBranchID, toBranchName)
	return &response, err
}

func (q *stockTransferRepository) FindStockTransfers(
	ctx context.Context,
	request *models.StockTransferFilter,
) ([]*models.StockTransfer, int64, error) {
	ctxt := "StockTransferRepository-FindStockTransfers"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM stock_transfers t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.StockTransfer{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM stock_transfers t %s ORDER BY ", stockTransferColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.transfer_date DESC, t.id DESC")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from stockTransferSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", stockTransferSortings[item.Field], item.Direction, item.Direction)
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
		stockTransfer, err := scanStockTransfer(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, stockTransfer)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindStockTransfer returns the first stock transfer of the filter. Inside a transaction pass tx so the read sees
// its writes; with a nil tx the read pool is used.
func (q *stockTransferRepository) FindStockTransfer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockTransferFilter,
) (*models.StockTransfer, error) {
	ctxt := "StockTransferRepository-FindStockTransfer"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM stock_transfers t %s LIMIT 1", stockTransferColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanStockTransfer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrStockTransferNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *stockTransferRepository) NextStockTransferNumber(
	ctx context.Context,
	tx pgx.Tx,
	storeID uuid.UUID,
	day string,
) (string, error) {
	ctxt := "StockTransferRepository-NextStockTransferNumber"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT COUNT(1)
	FROM stock_transfers t
	WHERE t.store_id = $1
		AND t.transfer_number LIKE $2`)
	prefix := helper.Sprintf("TRF/%s/", day)
	var count int64
	if err := tx.QueryRow(ctx, sb.String(), storeID, prefix+"%").Scan(&count); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return "", err
	}
	return helper.Sprintf("%s%04d", prefix, count+1), nil
}

func (q *stockTransferRepository) CreateStockTransfer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockTransfer,
) (*models.StockTransfer, error) {
	ctxt := "StockTransferRepository-CreateStockTransfer"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO stock_transfers (
		store_id
		, transfer_number
		, from_branch_id
		, to_branch_id
		, transfer_date
		, requires_confirmation
		, status
		, note
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5::date, $6, $7, $8, $9, $10)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.StoreID,
		request.TransferNumber,
		request.FromBranchID,
		request.ToBranchID,
		request.TransferDate,
		request.RequiresConfirmation,
		request.Status,
		request.Note,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if IsForeignKeyViolation(err) {
			return nil, models.ErrReferenceNotFound
		}
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return q.FindStockTransfer(ctx, tx, models.NewStockTransferFilter().WithStockTransferIDs(id))
}

func (q *stockTransferRepository) CreateStockTransferItems(
	ctx context.Context,
	tx pgx.Tx,
	stockTransferID uuid.UUID,
	request []*models.StockTransferItem,
) error {
	ctxt := "StockTransferRepository-CreateStockTransferItems"
	if len(request) == 0 {
		return nil
	}
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO stock_transfer_items (
		transfer_id
		, product_id
		, product_name
		, sku
		, quantity
		, received_quantity
		, note
	) VALUES `)
	params := make([]any, 0, 1+len(request)*6)
	params = append(params, stockTransferID)
	for i, item := range request {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		params = append(params, item.ProductID, item.ProductName, item.SKU, item.Quantity, item.ReceivedQuantity, item.Note)
		n := len(params)
		_, _ = fmt.Fprintf(sb, "($1, $%d, $%d, $%d, $%d, $%d, $%d)", n-5, n-4, n-3, n-2, n-1, n)
	}
	if _, err := tx.Exec(ctx, sb.String(), params...); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return err
	}
	return nil
}

func (q *stockTransferRepository) FindStockTransferItems(
	ctx context.Context,
	tx pgx.Tx,
	stockTransferID uuid.UUID,
) ([]*models.StockTransferItem, error) {
	ctxt := "StockTransferRepository-FindStockTransferItems"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT i.product_id
		, i.product_name
		, i.sku
		, i.quantity::float8
		, i.received_quantity::float8
		, i.note
	FROM stock_transfer_items i
	WHERE i.transfer_id = $1
	ORDER BY i.id`)
	var (
		rows pgx.Rows
		err  error
	)
	if tx != nil {
		rows, err = tx.Query(ctx, sb.String(), stockTransferID)
	} else {
		rows, err = q.dbRead.Query(ctx, sb.String(), stockTransferID)
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrQuery")
		return nil, err
	}
	defer rows.Close()
	response := []*models.StockTransferItem{}
	for rows.Next() {
		var item models.StockTransferItem
		if err = rows.Scan(
			&item.ProductID,
			&item.ProductName,
			&item.SKU,
			&item.Quantity,
			&item.ReceivedQuantity,
			&item.Note,
		); err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, err
		}
		response = append(response, &item)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, err
	}
	return response, nil
}

func (q *stockTransferRepository) LockStockTransfer(
	ctx context.Context,
	tx pgx.Tx,
	storeID,
	stockTransferID uuid.UUID,
) (*models.StockTransfer, error) {
	ctxt := "StockTransferRepository-LockStockTransfer"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.transfer_number
		, t.status
		, t.from_branch_id
		, t.to_branch_id
	FROM stock_transfers t
	WHERE t.id = $1
		AND t.store_id = $2
	FOR NO KEY UPDATE`)
	response := &models.StockTransfer{ID: stockTransferID, StoreID: storeID}
	err := tx.QueryRow(ctx, sb.String(), stockTransferID, storeID).Scan(
		&response.TransferNumber,
		&response.Status,
		&response.FromBranchID,
		&response.ToBranchID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrStockTransferNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *stockTransferRepository) UpdateReceivedQuantity(
	ctx context.Context,
	tx pgx.Tx,
	stockTransferID,
	productID uuid.UUID,
	receivedQuantity float64,
) error {
	ctxt := "StockTransferRepository-UpdateReceivedQuantity"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`UPDATE stock_transfer_items SET
		received_quantity = $3
	WHERE transfer_id = $1
		AND product_id = $2`)
	_, err := tx.Exec(ctx, sb.String(), stockTransferID, productID, receivedQuantity)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
	}
	return err
}

func (q *stockTransferRepository) UpdateStockTransferStatus(
	ctx context.Context,
	tx pgx.Tx,
	stockTransferID uuid.UUID,
	status,
	note,
	rejectReason string,
	updatedAt time.Time,
) error {
	ctxt := "StockTransferRepository-UpdateStockTransferStatus"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	params := []any{stockTransferID, status, updatedAt}
	_, _ = sb.WriteString("UPDATE stock_transfers SET status = $2, updated_at = $3")
	if note != "" {
		params = append(params, note)
		_, _ = fmt.Fprintf(sb, ", note = $%d", len(params))
	}
	if rejectReason != "" {
		params = append(params, rejectReason)
		_, _ = fmt.Fprintf(sb, ", reject_reason = $%d", len(params))
	}
	_, _ = sb.WriteString(" WHERE id = $1")
	_, err := tx.Exec(ctx, sb.String(), params...)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
	}
	return err
}

// DeleteStockTransfers deletes the stock transfers of the filter and returns how many went.
func (q *stockTransferRepository) DeleteStockTransfers(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockTransferFilter,
) (int64, error) {
	ctxt := "StockTransferRepository-DeleteStockTransfers"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("stock transfer: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM stock_transfers t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *stockTransferRepository) buildCondition(request *models.StockTransferFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.StockTransferIDs) > 0 {
		params = append(params, request.StockTransferIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.StoreIDs) > 0 {
		params = append(params, request.StoreIDs)
		conds = append(conds, helper.Sprintf("t.store_id = ANY($%d)", len(params)))
	}
	if len(request.PartyBranchIDs) > 0 {
		params = append(params, request.PartyBranchIDs)
		n := len(params)
		conds = append(conds, helper.Sprintf("(t.from_branch_id = ANY($%d) OR t.to_branch_id = ANY($%d))", n, n))
	}
	if len(request.FromBranchIDs) > 0 {
		params = append(params, request.FromBranchIDs)
		conds = append(conds, helper.Sprintf("t.from_branch_id = ANY($%d)", len(params)))
	}
	if len(request.ToBranchIDs) > 0 {
		params = append(params, request.ToBranchIDs)
		conds = append(conds, helper.Sprintf("t.to_branch_id = ANY($%d)", len(params)))
	}
	switch request.Role {
	case models.StockTransferRoleHeadOffice:
		params = append(params, request.HeadOfficeID)
		n := len(params)
		conds = append(conds, helper.Sprintf("(t.from_branch_id = $%d OR t.to_branch_id = $%d)", n, n))
	case models.StockTransferRoleBranch:
		params = append(params, request.HeadOfficeID)
		n := len(params)
		conds = append(conds, helper.Sprintf("(t.from_branch_id <> $%d AND t.to_branch_id <> $%d)", n, n))
	}
	if request.Status != nil {
		params = append(params, *request.Status)
		conds = append(conds, helper.Sprintf("t.status = $%d", len(params)))
	}
	if request.StartDate != "" {
		params = append(params, request.StartDate)
		conds = append(conds, helper.Sprintf("t.transfer_date >= $%d::date", len(params)))
	}
	if request.EndDate != "" {
		params = append(params, request.EndDate)
		conds = append(conds, helper.Sprintf("t.transfer_date <= $%d::date", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		conds = append(conds, helper.Sprintf("t.transfer_number ILIKE $%d", len(params)))
	}
	if len(conds) > 0 {
		sb.Reset()
		_, _ = sb.WriteString("WHERE ")
		_, _ = sb.WriteString(strings.Join(conds, " AND "))
		condition = sb.String()
	}
	return
}
