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

type SupplierRepository interface {
	FindSuppliers(
		ctx context.Context,
		request *models.SupplierFilter,
	) ([]*models.Supplier, int64, error)
	FindSupplier(
		ctx context.Context,
		tx pgx.Tx,
		request *models.SupplierFilter,
	) (*models.Supplier, error)
	CreateSupplier(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Supplier,
	) (*models.Supplier, error)
	UpdateSupplier(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Supplier,
	) (*models.Supplier, error)
	DeleteSuppliers(
		ctx context.Context,
		tx pgx.Tx,
		request *models.SupplierFilter,
	) (int64, error)
}

type supplierRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewSupplierRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) SupplierRepository {
	return &supplierRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// supplierColumns is the SELECT list of suppliers (alias t), in the order scanSupplier reads it.
const supplierColumns = `t.id
	, t.branch_id
	, t.name
	, t.contact_person
	, t.phone
	, t.email
	, t.code
	, (SELECT COALESCE(sum(d.total_amount - d.paid_amount), 0) FROM debts d WHERE d.supplier_id = t.id AND d.kind = 'payable' AND d.status <> 'paid')::float8
	, t.address
	, t.created_at
	, t.updated_at`

// supplierSortings maps the sortable fields to their SQL expressions.
var supplierSortings = map[string]string{
	"id":              "t.id",
	"created_at":      "t.created_at",
	"updated_at":      "t.updated_at",
	"name":            "t.name",
	"contact_person":  "t.contact_person",
	"phone":           "t.phone",
	"email":           "t.email",
	"code":            "t.code",
	"payable_balance": "(SELECT COALESCE(sum(d.total_amount - d.paid_amount), 0) FROM debts d WHERE d.supplier_id = t.id AND d.kind = 'payable' AND d.status <> 'paid')",
	"address":         "t.address",
}

func scanSupplier(row pgx.Row) (*models.Supplier, error) {
	var response models.Supplier
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.ContactPerson,
		&response.Phone,
		&response.Email,
		&response.Code,
		&response.PayableBalance,
		&response.Address,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	return &response, err
}

func (q *supplierRepository) FindSuppliers(
	ctx context.Context,
	request *models.SupplierFilter,
) ([]*models.Supplier, int64, error) {
	ctxt := "SupplierRepository-FindSuppliers"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM suppliers t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.Supplier{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM suppliers t %s ORDER BY ", supplierColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from supplierSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", supplierSortings[item.Field], item.Direction, item.Direction)
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
		supplier, err := scanSupplier(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, supplier)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindSupplier returns the first supplier of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *supplierRepository) FindSupplier(
	ctx context.Context,
	tx pgx.Tx,
	request *models.SupplierFilter,
) (*models.Supplier, error) {
	ctxt := "SupplierRepository-FindSupplier"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM suppliers t %s LIMIT 1", supplierColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanSupplier(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrSupplierNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *supplierRepository) CreateSupplier(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Supplier,
) (*models.Supplier, error) {
	ctxt := "SupplierRepository-CreateSupplier"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO suppliers (
		branch_id
		, name
		, contact_person
		, phone
		, email
		, code
		, address
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.Name,
		request.ContactPerson,
		request.Phone,
		request.Email,
		request.Code,
		request.Address,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrSupplierConflict); !errors.Is(err, models.ErrSupplierConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindSupplier(ctx, tx, models.NewSupplierFilter().WithSupplierIDs(id))
}

// UpdateSupplier locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *supplierRepository) UpdateSupplier(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Supplier,
) (*models.Supplier, error) {
	ctxt := "SupplierRepository-UpdateSupplier"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.contact_person
		, t.phone
		, t.email
		, t.code
		, t.address
	FROM suppliers t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.Supplier
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.ContactPerson,
		&current.Phone,
		&current.Email,
		&current.Code,
		&current.Address,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrSupplierNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE suppliers SET")
	if current.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
	}
	if !equalPtr(current.ContactPerson, request.ContactPerson) {
		params = append(params, request.ContactPerson)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " contact_person = $%d", n)
	}
	if !equalPtr(current.Phone, request.Phone) {
		params = append(params, request.Phone)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " phone = $%d", n)
	}
	if !equalPtr(current.Email, request.Email) {
		params = append(params, request.Email)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " email = $%d", n)
	}
	if !equalPtr(current.Code, request.Code) {
		params = append(params, request.Code)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " code = $%d", n)
	}
	if !equalPtr(current.Address, request.Address) {
		params = append(params, request.Address)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " address = $%d", n)
	}
	if len(params) > 0 {
		params = append(params, request.UpdatedAt)
		_, _ = fmt.Fprintf(sb, ", updated_at = $%d", len(params))
		params = append(params, request.ID)
		_, _ = fmt.Fprintf(sb, " WHERE id = $%d", len(params))
		if _, err = tx.Exec(ctx, sb.String(), params...); err != nil {
			if err = writeError(err, models.ErrSupplierConflict); !errors.Is(err, models.ErrSupplierConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindSupplier(ctx, tx, models.NewSupplierFilter().WithSupplierIDs(request.ID))
}

// DeleteSuppliers deletes the suppliers of the filter and returns how many went.
func (q *supplierRepository) DeleteSuppliers(
	ctx context.Context,
	tx pgx.Tx,
	request *models.SupplierFilter,
) (int64, error) {
	ctxt := "SupplierRepository-DeleteSuppliers"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("supplier: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM suppliers t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrSupplierInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *supplierRepository) buildCondition(request *models.SupplierFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.SupplierIDs) > 0 {
		params = append(params, request.SupplierIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.Code != "" {
		params = append(params, request.Code)
		conds = append(conds, helper.Sprintf("LOWER(t.code) = LOWER($%d)", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		n := len(params)
		conds = append(conds, helper.Sprintf("((t.name)::text ILIKE $%d OR (t.contact_person)::text ILIKE $%d OR (t.phone)::text ILIKE $%d OR (t.email)::text ILIKE $%d OR (t.code)::text ILIKE $%d)", n, n, n, n, n))
	}
	if len(conds) > 0 {
		sb.Reset()
		_, _ = sb.WriteString("WHERE ")
		_, _ = sb.WriteString(strings.Join(conds, " AND "))
		condition = sb.String()
	}
	return
}
