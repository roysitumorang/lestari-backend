package repositories

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

type DebtRepository interface {
	FindDebts(ctx context.Context, request *models.DebtFilter) ([]*models.Debt, int64, error)
	// FindDebt returns a debt with its payments.
	FindDebt(ctx context.Context, branchID, debtID uuid.UUID, kind string) (*models.Debt, error)
	// LockDebt locks a debt and returns its amounts and the document it came from.
	LockDebt(ctx context.Context, tx pgx.Tx, branchID, debtID uuid.UUID, kind string) (*models.Debt, error)
	CreateDebt(ctx context.Context, tx pgx.Tx, request *models.DebtRecord) error
	// CreateDebtPayment stores a payment and returns it.
	CreateDebtPayment(ctx context.Context, tx pgx.Tx, request *models.DebtPaymentCreate) (*models.DebtPayment, error)
	UpdateDebtPaid(ctx context.Context, tx pgx.Tx, debtID uuid.UUID, paidAmount float64, status string, at time.Time) error
	// CompleteDocument marks the sale or purchase a debt came from as completed.
	CompleteDocument(ctx context.Context, tx pgx.Tx, debt *models.Debt, at time.Time) error
	// UpdatePurchaseDebt refreshes the payable of a purchase and tells whether there was one.
	UpdatePurchaseDebt(ctx context.Context, tx pgx.Tx, request *models.DebtRecord) (bool, error)
	// DeleteUnpaidPurchaseDebt removes the payable of a purchase unless payments were recorded against it.
	DeleteUnpaidPurchaseDebt(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID) error
	// ReducePayable lowers the payable of a purchase by amount, never below what was already paid.
	ReducePayable(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID, amount float64, at time.Time) error
}

type debtRepository struct {
	base
}

func NewDebtRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) DebtRepository {
	return &debtRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const (
	debtColumns = `t.id
		, t.created_at
		, t.updated_at
		, t.reference_number
		, t.supplier_id
		, s.name AS supplier_name
		, CASE WHEN s.id IS NULL THEN NULL ELSE JSON_BUILD_OBJECT('id', s.id, 'name', s.name) END AS supplier
		, t.customer_id
		, c.name AS customer_name
		, CASE WHEN c.id IS NULL THEN NULL ELSE JSON_BUILD_OBJECT('id', c.id, 'name', c.name) END AS customer
		, t.transaction_date::text AS transaction_date
		, t.due_date::text AS due_date
		, t.total_amount
		, t.paid_amount
		, GREATEST(t.total_amount - t.paid_amount, 0) AS remaining_amount
		, t.status`
	debtFrom = `
	FROM debts t
	LEFT JOIN suppliers s ON s.id = t.supplier_id
	LEFT JOIN customers c ON c.id = t.customer_id `
)

var mapDebtSortExpressions = map[string]string{
	"reference_number": "t.reference_number", "party_name": "COALESCE(s.name, c.name)", "supplier_name": "s.name", "customer_name": "c.name",
	"transaction_date": "t.transaction_date", "due_date": "t.due_date", "total_amount": "t.total_amount", "paid_amount": "t.paid_amount",
	"remaining_amount": "GREATEST(t.total_amount - t.paid_amount, 0)", "status": "t.status",
}

func (q *debtRepository) FindDebts(ctx context.Context, request *models.DebtFilter) ([]*models.Debt, int64, error) {
	ctxt := "DebtRepository-FindDebts"
	var cond conditions
	cond.add("t.branch_id = $%d", request.BranchID)
	cond.add("t.kind = $%d", request.Kind)
	if request.Keyword != "" {
		cond.add("(t.reference_number ILIKE $%d OR s.name ILIKE $%d OR c.name ILIKE $%d)", "%"+request.Keyword+"%")
	}
	if request.PartyID != nil {
		cond.add("COALESCE(t.supplier_id, t.customer_id) = $%d", *request.PartyID)
	}
	if request.Status != "" {
		cond.add("t.status = $%d", request.Status)
	}
	if request.Overdue {
		cond.add("t.due_date < $%d::date AND t.status <> 'paid'", request.Today)
	}
	where := cond.where()
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1)"+debtFrom+where, cond.params...)
	if err != nil || total == 0 {
		return []*models.Debt{}, 0, err
	}
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString("SELECT ")
	_, _ = sb.WriteString(debtColumns)
	_, _ = sb.WriteString(debtFrom)
	_, _ = sb.WriteString(where)
	_, _ = sb.WriteString(orderBy(request.Sortings, mapDebtSortExpressions, "t.status DESC, t.due_date NULLS LAST, t.id DESC"))
	_, _ = sb.WriteString(paginate(request.Limit, request.Page))
	response, err := queryRows[models.Debt](ctx, q.dbRead, ctxt, sb.String(), cond.params...)
	return response, total, err
}

func (q *debtRepository) FindDebt(ctx context.Context, branchID, debtID uuid.UUID, kind string) (*models.Debt, error) {
	ctxt := "DebtRepository-FindDebt"
	response, err := queryRow[models.Debt](ctx, q.dbRead, ctxt, "SELECT "+debtColumns+`
		, COALESCE((
			SELECT JSON_AGG(JSON_BUILD_OBJECT(
				'id', p.id
				, 'payment_date', p.payment_date
				, 'amount', p.amount
				, 'payment_method', p.payment_method
				, 'note', p.note
			) ORDER BY p.payment_date, p.id)
			FROM debt_payments p
			WHERE p.debt_id = t.id
		), '[]') AS payments`+debtFrom+`
	WHERE t.id = $1
		AND t.branch_id = $2
		AND t.kind = $3`, debtID, branchID, kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrDebtNotFound
	}
	return response, err
}

func (q *debtRepository) LockDebt(ctx context.Context, tx pgx.Tx, branchID, debtID uuid.UUID, kind string) (*models.Debt, error) {
	ctxt := "DebtRepository-LockDebt"
	response, err := queryRow[models.Debt](ctx, tx, ctxt, `SELECT t.id
		, t.total_amount
		, t.paid_amount
		, t.sale_id
		, t.purchase_id
	FROM debts t
	WHERE t.id = $1
		AND t.branch_id = $2
		AND t.kind = $3
	FOR UPDATE`, debtID, branchID, kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrDebtNotFound
	}
	return response, err
}

func (q *debtRepository) CreateDebt(ctx context.Context, tx pgx.Tx, request *models.DebtRecord) error {
	ctxt := "DebtRepository-CreateDebt"
	_, err := execute(ctx, tx, ctxt, `INSERT INTO debts (
		branch_id
		, kind
		, supplier_id
		, customer_id
		, purchase_id
		, sale_id
		, reference_number
		, transaction_date
		, due_date
		, total_amount
		, paid_amount
		, status
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9::date, $10, $11, $12, $13, $13)`,
		request.BranchID, request.Kind, request.SupplierID, request.CustomerID, request.PurchaseID, request.SaleID, request.ReferenceNumber,
		request.TransactionDate, request.DueDate, request.TotalAmount, request.PaidAmount, request.Status, request.At)
	return err
}

func (q *debtRepository) CreateDebtPayment(ctx context.Context, tx pgx.Tx, request *models.DebtPaymentCreate) (*models.DebtPayment, error) {
	ctxt := "DebtRepository-CreateDebtPayment"
	return queryRow[models.DebtPayment](ctx, tx, ctxt, `INSERT INTO debt_payments (
		debt_id
		, payment_date
		, amount
		, payment_method
		, note
		, created_at
	) VALUES ($1, $2::date, $3, $4, NULLIF($5, ''), $6)
	RETURNING id
		, payment_date::text AS payment_date
		, amount
		, payment_method
		, note`, request.DebtID, request.PaymentDate, request.Amount, request.PaymentMethod, request.Note, request.At)
}

func (q *debtRepository) UpdateDebtPaid(ctx context.Context, tx pgx.Tx, debtID uuid.UUID, paidAmount float64, status string, at time.Time) error {
	ctxt := "DebtRepository-UpdateDebtPaid"
	_, err := execute(ctx, tx, ctxt, `UPDATE debts SET
		paid_amount = $2
		, status = $3
		, updated_at = $4
	WHERE id = $1`, debtID, paidAmount, status, at)
	return err
}

func (q *debtRepository) CompleteDocument(ctx context.Context, tx pgx.Tx, debt *models.Debt, at time.Time) (err error) {
	ctxt := "DebtRepository-CompleteDocument"
	if debt.SaleID != nil {
		if _, err = execute(ctx, tx, ctxt, `UPDATE sales SET
			status = 'completed'
			, updated_at = $2
		WHERE id = $1
			AND status <> 'completed'`, *debt.SaleID, at); err != nil {
			return
		}
	}
	if debt.PurchaseID != nil {
		_, err = execute(ctx, tx, ctxt, `UPDATE purchases SET
			status = 'completed'
			, updated_at = $2
		WHERE id = $1
			AND status <> 'completed'`, *debt.PurchaseID, at)
	}
	return
}

func (q *debtRepository) UpdatePurchaseDebt(ctx context.Context, tx pgx.Tx, request *models.DebtRecord) (bool, error) {
	ctxt := "DebtRepository-UpdatePurchaseDebt"
	exists, err := queryValue[bool](ctx, tx, ctxt, "SELECT EXISTS(SELECT 1 FROM debts t WHERE t.purchase_id = $1)", request.PurchaseID)
	if err != nil || !exists {
		return false, err
	}
	_, err = execute(ctx, tx, ctxt, `UPDATE debts SET
		supplier_id = $2
		, total_amount = $3
		, paid_amount = $4
		, due_date = $5::date
		, status = $6
		, transaction_date = $7::date
		, updated_at = $8
	WHERE purchase_id = $1
		AND (supplier_id, total_amount, paid_amount, due_date, status, transaction_date)
			IS DISTINCT FROM ($2::uuid, $3::numeric, $4::numeric, $5::date, $6, $7::date)`,
		request.PurchaseID, request.SupplierID, request.TotalAmount, request.PaidAmount, request.DueDate, request.Status,
		request.TransactionDate, request.At)
	return true, err
}

func (q *debtRepository) DeleteUnpaidPurchaseDebt(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID) error {
	ctxt := "DebtRepository-DeleteUnpaidPurchaseDebt"
	_, err := execute(ctx, tx, ctxt, `DELETE FROM debts
	WHERE purchase_id = $1
		AND NOT EXISTS (SELECT 1 FROM debt_payments dp WHERE dp.debt_id = debts.id)`, purchaseID)
	return err
}

func (q *debtRepository) ReducePayable(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID, amount float64, at time.Time) error {
	ctxt := "DebtRepository-ReducePayable"
	_, err := execute(ctx, tx, ctxt, `UPDATE debts SET
		total_amount = GREATEST(total_amount - $2, paid_amount)
		, status = CASE WHEN paid_amount >= total_amount - $2 THEN 'paid' ELSE status END
		, updated_at = $3
	WHERE purchase_id = $1
		AND kind = 'payable'`, purchaseID, amount, at)
	return err
}
