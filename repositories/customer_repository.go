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

type CustomerRepository interface {
	FindCustomers(
		ctx context.Context,
		request *models.CustomerFilter,
	) ([]*models.Customer, int64, error)
	FindCustomer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerFilter,
	) (*models.Customer, error)
	CreateCustomer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Customer,
	) (*models.Customer, error)
	UpdateCustomer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Customer,
	) (*models.Customer, error)
	DeleteCustomers(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerFilter,
	) (int64, error)
}

type customerRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewCustomerRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) CustomerRepository {
	return &customerRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// customerColumns is the SELECT list of customers (alias t), in the order scanCustomer reads it.
const customerColumns = `t.id
	, t.branch_id
	, t.name
	, t.phone
	, t.email
	, t.code
	, t.customer_type_id
	, (SELECT r.name FROM customer_types r WHERE r.id = t.customer_type_id)
	, TO_CHAR(t.birth_date, 'YYYY-MM-DD')
	, t.points::bigint
	, (SELECT COALESCE(sum(d.total_amount - d.paid_amount), 0) FROM debts d WHERE d.customer_id = t.id AND d.kind = 'receivable' AND d.status <> 'paid')::float8
	, t.address
	, t.note
	, t.created_at
	, t.updated_at`

// customerSortings maps the sortable fields to their SQL expressions.
var customerSortings = map[string]string{
	"id":                 "t.id",
	"created_at":         "t.created_at",
	"updated_at":         "t.updated_at",
	"name":               "t.name",
	"phone":              "t.phone",
	"email":              "t.email",
	"code":               "t.code",
	"customer_type_id":   "t.customer_type_id",
	"birth_date":         "t.birth_date",
	"points":             "t.points",
	"receivable_balance": "(SELECT COALESCE(sum(d.total_amount - d.paid_amount), 0) FROM debts d WHERE d.customer_id = t.id AND d.kind = 'receivable' AND d.status <> 'paid')",
	"address":            "t.address",
	"note":               "t.note",
	"customer_type.name": "(SELECT r.name FROM customer_types r WHERE r.id = t.customer_type_id)",
}

func scanCustomer(row pgx.Row) (*models.Customer, error) {
	var response models.Customer
	var customerTypeName *string
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.Name,
		&response.Phone,
		&response.Email,
		&response.Code,
		&response.CustomerTypeID,
		&customerTypeName,
		&response.BirthDate,
		&response.Points,
		&response.ReceivableBalance,
		&response.Address,
		&response.Note,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	response.CustomerType = models.NewReference(response.CustomerTypeID, customerTypeName)
	return &response, err
}

func (q *customerRepository) FindCustomers(
	ctx context.Context,
	request *models.CustomerFilter,
) ([]*models.Customer, int64, error) {
	ctxt := "CustomerRepository-FindCustomers"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM customers t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.Customer{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM customers t %s ORDER BY ", customerColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from customerSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", customerSortings[item.Field], item.Direction, item.Direction)
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
		customer, err := scanCustomer(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, customer)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindCustomer returns the first customer of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *customerRepository) FindCustomer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerFilter,
) (*models.Customer, error) {
	ctxt := "CustomerRepository-FindCustomer"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM customers t %s LIMIT 1", customerColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanCustomer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCustomerNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *customerRepository) CreateCustomer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Customer,
) (*models.Customer, error) {
	ctxt := "CustomerRepository-CreateCustomer"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO customers (
		branch_id
		, name
		, phone
		, email
		, code
		, customer_type_id
		, birth_date
		, address
		, note
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8, $9, $10, $11)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.Name,
		request.Phone,
		request.Email,
		request.Code,
		request.CustomerTypeID,
		request.BirthDate,
		request.Address,
		request.Note,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrCustomerConflict); !errors.Is(err, models.ErrCustomerConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindCustomer(ctx, tx, models.NewCustomerFilter().WithCustomerIDs(id))
}

// UpdateCustomer locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *customerRepository) UpdateCustomer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Customer,
) (*models.Customer, error) {
	ctxt := "CustomerRepository-UpdateCustomer"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.name
		, t.phone
		, t.email
		, t.code
		, t.customer_type_id
		, TO_CHAR(t.birth_date, 'YYYY-MM-DD')
		, t.address
		, t.note
	FROM customers t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`)
	var current models.Customer
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.Name,
		&current.Phone,
		&current.Email,
		&current.Code,
		&current.CustomerTypeID,
		&current.BirthDate,
		&current.Address,
		&current.Note,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCustomerNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE customers SET")
	if current.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
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
	if !equalPtr(current.CustomerTypeID, request.CustomerTypeID) {
		params = append(params, request.CustomerTypeID)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " customer_type_id = $%d", n)
	}
	if !equalPtr(current.BirthDate, request.BirthDate) {
		params = append(params, request.BirthDate)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " birth_date = $%d::date", n)
	}
	if !equalPtr(current.Address, request.Address) {
		params = append(params, request.Address)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " address = $%d", n)
	}
	if !equalPtr(current.Note, request.Note) {
		params = append(params, request.Note)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " note = $%d", n)
	}
	if len(params) > 0 {
		params = append(params, request.UpdatedAt)
		_, _ = fmt.Fprintf(sb, ", updated_at = $%d", len(params))
		params = append(params, request.ID)
		_, _ = fmt.Fprintf(sb, " WHERE id = $%d", len(params))
		if _, err = tx.Exec(ctx, sb.String(), params...); err != nil {
			if err = writeError(err, models.ErrCustomerConflict); !errors.Is(err, models.ErrCustomerConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindCustomer(ctx, tx, models.NewCustomerFilter().WithCustomerIDs(request.ID))
}

// DeleteCustomers deletes the customers of the filter and returns how many went.
func (q *customerRepository) DeleteCustomers(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerFilter,
) (int64, error) {
	ctxt := "CustomerRepository-DeleteCustomers"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if condition == "" {
		return 0, errors.New("customer: refusing to delete without a condition")
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "DELETE FROM customers t %s", condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrCustomerInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *customerRepository) buildCondition(request *models.CustomerFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.CustomerIDs) > 0 {
		params = append(params, request.CustomerIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.CustomerTypeID != nil {
		params = append(params, *request.CustomerTypeID)
		conds = append(conds, helper.Sprintf("t.customer_type_id = $%d", len(params)))
	}
	if request.Code != "" {
		params = append(params, request.Code)
		conds = append(conds, helper.Sprintf("LOWER(t.code) = LOWER($%d)", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		n := len(params)
		conds = append(conds, helper.Sprintf("((t.name)::text ILIKE $%d OR (t.phone)::text ILIKE $%d OR (t.email)::text ILIKE $%d OR (t.code)::text ILIKE $%d)", n, n, n, n))
	}
	if len(conds) > 0 {
		sb.Reset()
		_, _ = sb.WriteString("WHERE ")
		_, _ = sb.WriteString(strings.Join(conds, " AND "))
		condition = sb.String()
	}
	return
}
