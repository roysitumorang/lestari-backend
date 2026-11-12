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

// BillingRepository stores invoices and vouchers.
type BillingRepository interface {
	// ExpireInvoices marks the pending invoices of a store whose time ran out.
	ExpireInvoices(ctx context.Context, storeID uuid.UUID, at time.Time) error
	FindInvoices(ctx context.Context, request *models.InvoiceFilter) ([]*models.Invoice, int64, error)
	FindInvoice(ctx context.Context, tx pgx.Tx, storeID, invoiceID uuid.UUID) (*models.Invoice, error)
	FindBillingSummary(ctx context.Context, storeID uuid.UUID) (*models.BillingSummary, error)
	// NextInvoiceNumber is the next document number of the store for a day (YYYYMMDD): INV/20261108/0001.
	NextInvoiceNumber(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, day string) (string, error)
	CreateInvoice(ctx context.Context, tx pgx.Tx, request *models.Invoice) (uuid.UUID, error)
	// PayInvoice marks a pending invoice, found by its number, as paid; nil when there is none.
	PayInvoice(ctx context.Context, tx pgx.Tx, invoiceNumber string, paidAt time.Time) (*models.Invoice, error)
	UpdatePaymentProof(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID, proofURL, senderAccountName string, updatedAt time.Time) error
	// CancelInvoice cancels a pending invoice and tells whether there was one.
	CancelInvoice(ctx context.Context, tx pgx.Tx, storeID, invoiceID uuid.UUID, updatedAt time.Time) (bool, error)
	// UseDiscountVoucher consumes one use of a promo code and returns its percentage.
	UseDiscountVoucher(ctx context.Context, tx pgx.Tx, code string, at time.Time) (float64, error)
	// UseRewardVoucher consumes one use of a voucher that gives a reward.
	UseRewardVoucher(ctx context.Context, tx pgx.Tx, code string, at time.Time) (*models.VoucherRedemption, error)
	// CreateVoucherRedemption records that a store used a voucher; a second time is ErrVoucherUsed.
	CreateVoucherRedemption(ctx context.Context, tx pgx.Tx, code string, storeID uuid.UUID, at time.Time) error
}

type billingRepository struct {
	base
}

func NewBillingRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) BillingRepository {
	return &billingRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const invoiceColumns = `t.id
	, t.store_id
	, t.invoice_number
	, t.kind
	, t.description
	, t.payment_method
	, t.amount
	, t.admin_fee
	, t.status
	, t.payment_url
	, t.payment_instructions
	, t.payment_proof_url
	, t.meta
	, t.expires_at
	, t.paid_at
	, t.created_at`

func (q *billingRepository) ExpireInvoices(ctx context.Context, storeID uuid.UUID, at time.Time) error {
	ctxt := "BillingRepository-ExpireInvoices"
	_, err := execute(ctx, q.dbWrite, ctxt, `UPDATE invoices SET
		status = 'expired'
	WHERE store_id = $1
		AND status = 'pending'
		AND expires_at < $2`, storeID, at)
	return err
}

func (q *billingRepository) FindInvoices(ctx context.Context, request *models.InvoiceFilter) ([]*models.Invoice, int64, error) {
	ctxt := "BillingRepository-FindInvoices"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	params := []any{request.StoreID, request.StartDate, request.EndDate}
	_, _ = sb.WriteString("WHERE t.store_id = $1 AND t.created_at::date BETWEEN $2::date AND $3::date")
	if request.Status != "" {
		params = append(params, request.Status)
		_, _ = sb.WriteString(sprintf(" AND t.status = $%d", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		n := len(params)
		_, _ = sb.WriteString(sprintf(" AND (t.invoice_number ILIKE $%d OR t.description ILIKE $%d)", n, n))
	}
	condition := sb.String()
	total, err := queryValue[int64](ctx, q.dbWrite, ctxt, "SELECT COUNT(1) FROM invoices t "+condition, params...)
	if err != nil || total == 0 {
		return []*models.Invoice{}, 0, err
	}
	response, err := queryRows[models.Invoice](ctx, q.dbWrite, ctxt,
		"SELECT "+invoiceColumns+" FROM invoices t "+condition+" ORDER BY t.created_at DESC"+paginate(request.Limit, request.Page), params...)
	return response, total, err
}

func (q *billingRepository) FindInvoice(ctx context.Context, tx pgx.Tx, storeID, invoiceID uuid.UUID) (*models.Invoice, error) {
	ctxt := "BillingRepository-FindInvoice"
	response, err := queryRow[models.Invoice](ctx, q.writer(tx), ctxt,
		"SELECT "+invoiceColumns+" FROM invoices t WHERE t.id = $1 AND t.store_id = $2", invoiceID, storeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrInvoiceNotFound
	}
	return response, err
}

func (q *billingRepository) FindBillingSummary(ctx context.Context, storeID uuid.UUID) (*models.BillingSummary, error) {
	ctxt := "BillingRepository-FindBillingSummary"
	return queryRow[models.BillingSummary](ctx, q.dbWrite, ctxt, `SELECT CASE t.plan
			WHEN 'pro' THEN 'Pro'
			WHEN 'pro_trial' THEN 'Pro (trial)'
			ELSE 'Free'
		END AS plan_name
		, t.plan_expires_at
		, t.coin_balance
	FROM stores t
	WHERE t.id = $1`, storeID)
}

func (q *billingRepository) NextInvoiceNumber(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, day string) (string, error) {
	ctxt := "BillingRepository-NextInvoiceNumber"
	return nextNumber(ctx, tx, ctxt, "INV", day, "invoices", "invoice_number", "store_id", storeID)
}

func (q *billingRepository) CreateInvoice(ctx context.Context, tx pgx.Tx, request *models.Invoice) (uuid.UUID, error) {
	ctxt := "BillingRepository-CreateInvoice"
	return queryValue[uuid.UUID](ctx, tx, ctxt, `INSERT INTO invoices (
		store_id
		, invoice_number
		, kind
		, description
		, payment_method
		, amount
		, admin_fee
		, status
		, payment_url
		, payment_instructions
		, meta
		, expires_at
		, paid_at
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $14)
	RETURNING id`,
		request.StoreID,
		request.InvoiceNumber,
		request.Kind,
		request.Description,
		request.PaymentMethod,
		request.Amount,
		request.AdminFee,
		request.Status,
		request.PaymentURL,
		request.PaymentInstructions,
		request.Meta,
		request.ExpiresAt,
		request.PaidAt,
		request.CreatedAt,
	)
}

func (q *billingRepository) PayInvoice(ctx context.Context, tx pgx.Tx, invoiceNumber string, paidAt time.Time) (*models.Invoice, error) {
	ctxt := "BillingRepository-PayInvoice"
	response, err := queryRow[models.Invoice](ctx, tx, ctxt, `UPDATE invoices t SET
		status = 'paid'
		, paid_at = $2
		, updated_at = $2
	WHERE t.invoice_number = $1
		AND t.status = 'pending'
	RETURNING `+invoiceColumns, invoiceNumber, paidAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return response, err
}

func (q *billingRepository) UpdatePaymentProof(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID, proofURL, senderAccountName string, updatedAt time.Time) error {
	ctxt := "BillingRepository-UpdatePaymentProof"
	_, err := execute(ctx, q.writer(tx), ctxt, `UPDATE invoices SET
		payment_proof_url = $2
		, sender_account_name = NULLIF($3, '')
		, updated_at = $4
	WHERE id = $1`, invoiceID, proofURL, senderAccountName, updatedAt)
	return err
}

func (q *billingRepository) CancelInvoice(ctx context.Context, tx pgx.Tx, storeID, invoiceID uuid.UUID, updatedAt time.Time) (bool, error) {
	ctxt := "BillingRepository-CancelInvoice"
	cancelled, err := execute(ctx, q.writer(tx), ctxt, `UPDATE invoices SET
		status = 'cancelled'
		, updated_at = $3
	WHERE id = $1
		AND store_id = $2
		AND status = 'pending'`, invoiceID, storeID, updatedAt)
	return cancelled > 0, err
}

func (q *billingRepository) UseDiscountVoucher(ctx context.Context, tx pgx.Tx, code string, at time.Time) (float64, error) {
	ctxt := "BillingRepository-UseDiscountVoucher"
	response, err := queryValue[float64](ctx, tx, ctxt, `UPDATE vouchers SET
		used_count = used_count + 1
	WHERE UPPER(code) = UPPER($1)
		AND reward_type = 'discount_percent'
		AND used_count < max_uses
		AND (expires_at IS NULL OR expires_at > $2)
	RETURNING reward_amount`, code, at)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, models.ErrVoucherInvalid
	}
	return response, err
}

func (q *billingRepository) UseRewardVoucher(ctx context.Context, tx pgx.Tx, code string, at time.Time) (*models.VoucherRedemption, error) {
	ctxt := "BillingRepository-UseRewardVoucher"
	response, err := queryRow[models.VoucherRedemption](ctx, tx, ctxt, `UPDATE vouchers SET
		used_count = used_count + 1
	WHERE UPPER(code) = UPPER($1)
		AND reward_type <> 'discount_percent'
		AND used_count < max_uses
		AND (expires_at IS NULL OR expires_at > $2)
	RETURNING code, reward_type, reward_amount`, code, at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrVoucherInvalid
	}
	return response, err
}

func (q *billingRepository) CreateVoucherRedemption(ctx context.Context, tx pgx.Tx, code string, storeID uuid.UUID, at time.Time) error {
	ctxt := "BillingRepository-CreateVoucherRedemption"
	_, err := execute(ctx, tx, ctxt, "INSERT INTO voucher_redemptions (code, store_id, created_at) VALUES ($1, $2, $3)", code, storeID, at)
	if IsUniqueViolation(err) {
		return models.ErrVoucherUsed
	}
	return err
}
