package repositories

import (
	"context"
	"errors"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

type PPOBRepository interface {
	FindPPOBPrices(ctx context.Context, request *models.PPOBPriceFilter) ([]*models.PPOBPrice, int64, error)
	FindPPOBPrice(ctx context.Context, tx pgx.Tx, request *models.PPOBPriceFilter) (*models.PPOBPrice, error)
	// UpdatePPOBPrice writes only the attributes that differ from the stored ones.
	UpdatePPOBPrice(ctx context.Context, tx pgx.Tx, request *models.PPOBPrice) (*models.PPOBPrice, error)
	// FindBalance is what the branch can still spend: deposits that arrived minus the cost of its PPOB sales.
	FindBalance(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) (float64, error)
	FindTransactionTotal(ctx context.Context, branchID uuid.UUID) (float64, error)
	ExpireDeposits(ctx context.Context, branchID uuid.UUID, at time.Time) error
	FindPPOBDeposits(ctx context.Context, request *models.PPOBDepositFilter) ([]*models.PPOBDeposit, int64, error)
	FindPPOBDeposit(ctx context.Context, tx pgx.Tx, branchID, depositID uuid.UUID) (*models.PPOBDeposit, error)
	CreatePPOBDeposit(ctx context.Context, tx pgx.Tx, request *models.PPOBDeposit) (*models.PPOBDeposit, error)
	// UseFreeFeeVoucher consumes one use of a voucher that waives the admin fee, or returns ErrVoucherInvalid.
	UseFreeFeeVoucher(ctx context.Context, tx pgx.Tx, code string, at time.Time) error
	UpdateDepositProof(ctx context.Context, tx pgx.Tx, depositID uuid.UUID, proofURL string, updatedAt time.Time) error
	// CancelDeposit cancels a pending deposit and tells whether there was one.
	CancelDeposit(ctx context.Context, tx pgx.Tx, branchID, depositID uuid.UUID, updatedAt time.Time) (bool, error)
	// LockPendingDeposit finds a pending deposit by its reference and locks it; nil when there is none.
	LockPendingDeposit(ctx context.Context, tx pgx.Tx, referenceID string) (*models.PPOBDeposit, error)
	CompleteDeposit(ctx context.Context, tx pgx.Tx, depositID uuid.UUID, balanceAfter float64, updatedAt time.Time) error
	CreatePPOBTransaction(ctx context.Context, tx pgx.Tx, request *models.PPOBTransaction) (*models.PPOBTransaction, error)
}

type ppobRepository struct {
	base
}

func NewPPOBRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) PPOBRepository {
	return &ppobRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const ppobPriceColumns = `t.id
	, t.branch_id
	, t.kind
	, t.category
	, t.provider
	, t.product_code
	, t.product_name
	, t.is_active
	, t.base_price
	, t.selling_price
	, t.base_admin_fee
	, t.admin_fee
	, t.created_at
	, t.updated_at`

const ppobDepositColumns = `t.id
	, t.branch_id
	, t.reference_id
	, t.type
	, t.payment_method
	, t.amount
	, t.admin_fee
	, t.balance_after
	, t.status
	, t.payment_url
	, t.payment_instructions
	, t.payment_proof_url
	, t.expires_at
	, t.created_at`

func (q *ppobRepository) FindPPOBPrices(ctx context.Context, request *models.PPOBPriceFilter) ([]*models.PPOBPrice, int64, error) {
	ctxt := "PPOBRepository-FindPPOBPrices"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1) FROM ppob_prices t "+condition, params...)
	if err != nil || total == 0 {
		return []*models.PPOBPrice{}, 0, err
	}
	sb.Reset()
	_, _ = sb.WriteString("SELECT " + ppobPriceColumns + " FROM ppob_prices t " + condition + " ORDER BY ")
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.category, t.product_name")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the field is a column name checked against MapPPOBPriceSortings, never request text
		_, _ = sb.WriteString(sprintf("t.%s %s NULLS LAST, t.id %s", item.Field, item.Direction, item.Direction))
	}
	_, _ = sb.WriteString(paginate(request.Limit, request.Page))
	response, err := queryRows[models.PPOBPrice](ctx, q.dbRead, ctxt, sb.String(), params...)
	return response, total, err
}

func (q *ppobRepository) FindPPOBPrice(ctx context.Context, tx pgx.Tx, request *models.PPOBPriceFilter) (*models.PPOBPrice, error) {
	ctxt := "PPOBRepository-FindPPOBPrice"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	response, err := queryRow[models.PPOBPrice](ctx, q.reader(tx), ctxt, "SELECT "+ppobPriceColumns+" FROM ppob_prices t "+condition+" LIMIT 1", params...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPPOBPriceNotFound
	}
	return response, err
}

func (q *ppobRepository) UpdatePPOBPrice(ctx context.Context, tx pgx.Tx, request *models.PPOBPrice) (*models.PPOBPrice, error) {
	ctxt := "PPOBRepository-UpdatePPOBPrice"
	current, err := queryRow[models.PPOBPrice](ctx, tx, ctxt, `SELECT t.is_active
		, t.selling_price
		, t.admin_fee
	FROM ppob_prices t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR NO KEY UPDATE`, request.ID, request.BranchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPPOBPriceNotFound
	}
	if err != nil {
		return nil, err
	}
	var update changes
	update.set(current.IsActive != request.IsActive, "is_active", request.IsActive)
	update.set(!equalPtr(current.SellingPrice, request.SellingPrice), "selling_price", request.SellingPrice)
	update.set(!equalPtr(current.AdminFee, request.AdminFee), "admin_fee", request.AdminFee)
	if err = update.apply(ctx, tx, ctxt, "ppob_prices", request.ID, request.UpdatedAt); err != nil {
		return nil, err
	}
	return q.FindPPOBPrice(ctx, tx, models.NewPPOBPriceFilter().WithPPOBPriceIDs(request.ID))
}

func (q *ppobRepository) buildCondition(request *models.PPOBPriceFilter, sb *strings.Builder) (condition string, params []any) {
	var conds []string
	if len(request.PPOBPriceIDs) > 0 {
		params = append(params, request.PPOBPriceIDs)
		conds = append(conds, sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.Kind != "" {
		params = append(params, request.Kind)
		conds = append(conds, sprintf("t.kind = $%d", len(params)))
	}
	if request.Category != "" {
		params = append(params, request.Category)
		conds = append(conds, sprintf("t.category = $%d", len(params)))
	}
	if request.ProductCode != "" {
		params = append(params, request.ProductCode)
		conds = append(conds, sprintf("LOWER(t.product_code) = LOWER($%d)", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		n := len(params)
		conds = append(conds, sprintf("(t.provider ILIKE $%d OR t.product_code ILIKE $%d OR t.product_name ILIKE $%d)", n, n, n))
	}
	if len(conds) > 0 {
		sb.Reset()
		_, _ = sb.WriteString("WHERE ")
		_, _ = sb.WriteString(strings.Join(conds, " AND "))
		condition = sb.String()
	}
	return
}

func (q *ppobRepository) FindBalance(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) (float64, error) {
	ctxt := "PPOBRepository-FindBalance"
	return queryValue[float64](ctx, q.writer(tx), ctxt, `SELECT COALESCE((
			SELECT SUM(d.amount) FROM ppob_deposits d WHERE d.branch_id = $1 AND d.status = 'success'
		), 0) - COALESCE((
			SELECT SUM(t.base_price) FROM ppob_transactions t WHERE t.branch_id = $1 AND t.status IN ('success', 'pending')
		), 0)`, branchID)
}

func (q *ppobRepository) FindTransactionTotal(ctx context.Context, branchID uuid.UUID) (float64, error) {
	ctxt := "PPOBRepository-FindTransactionTotal"
	return queryValue[float64](ctx, q.dbRead, ctxt, `SELECT COALESCE(SUM(t.selling_price), 0)
	FROM ppob_transactions t
	WHERE t.branch_id = $1
		AND t.status = 'success'`, branchID)
}

func (q *ppobRepository) ExpireDeposits(ctx context.Context, branchID uuid.UUID, at time.Time) error {
	ctxt := "PPOBRepository-ExpireDeposits"
	_, err := execute(ctx, q.dbWrite, ctxt, `UPDATE ppob_deposits SET
		status = 'expired'
		, updated_at = $2
	WHERE branch_id = $1
		AND status = 'pending'
		AND expires_at < $2`, branchID, at)
	return err
}

func (q *ppobRepository) FindPPOBDeposits(ctx context.Context, request *models.PPOBDepositFilter) ([]*models.PPOBDeposit, int64, error) {
	ctxt := "PPOBRepository-FindPPOBDeposits"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	params := []any{request.BranchID, request.StartDate, request.EndDate}
	_, _ = sb.WriteString("WHERE t.branch_id = $1 AND t.created_at::date BETWEEN $2::date AND $3::date")
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		_, _ = sb.WriteString(" AND (t.reference_id ILIKE $4 OR COALESCE(t.payment_method, '') ILIKE $4)")
	}
	condition := sb.String()
	total, err := queryValue[int64](ctx, q.dbWrite, ctxt, "SELECT COUNT(1) FROM ppob_deposits t "+condition, params...)
	if err != nil || total == 0 {
		return []*models.PPOBDeposit{}, 0, err
	}
	response, err := queryRows[models.PPOBDeposit](ctx, q.dbWrite, ctxt,
		"SELECT "+ppobDepositColumns+" FROM ppob_deposits t "+condition+" ORDER BY t.created_at DESC"+paginate(request.Limit, request.Page), params...)
	return response, total, err
}

func (q *ppobRepository) FindPPOBDeposit(ctx context.Context, tx pgx.Tx, branchID, depositID uuid.UUID) (*models.PPOBDeposit, error) {
	ctxt := "PPOBRepository-FindPPOBDeposit"
	response, err := queryRow[models.PPOBDeposit](ctx, q.writer(tx), ctxt,
		"SELECT "+ppobDepositColumns+" FROM ppob_deposits t WHERE t.id = $1 AND t.branch_id = $2", depositID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPPOBDepositNotFound
	}
	return response, err
}

func (q *ppobRepository) CreatePPOBDeposit(ctx context.Context, tx pgx.Tx, request *models.PPOBDeposit) (*models.PPOBDeposit, error) {
	ctxt := "PPOBRepository-CreatePPOBDeposit"
	return queryRow[models.PPOBDeposit](ctx, tx, ctxt, `INSERT INTO ppob_deposits AS t (
		branch_id
		, reference_id
		, type
		, payment_method
		, amount
		, admin_fee
		, status
		, payment_url
		, payment_instructions
		, expires_at
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
	RETURNING `+ppobDepositColumns,
		request.BranchID,
		request.ReferenceID,
		request.Type,
		request.PaymentMethod,
		request.Amount,
		request.AdminFee,
		request.Status,
		request.PaymentURL,
		request.PaymentInstructions,
		request.ExpiresAt,
		request.CreatedAt,
	)
}

func (q *ppobRepository) UseFreeFeeVoucher(ctx context.Context, tx pgx.Tx, code string, at time.Time) error {
	ctxt := "PPOBRepository-UseFreeFeeVoucher"
	used, err := execute(ctx, tx, ctxt, `UPDATE vouchers SET
		used_count = used_count + 1
	WHERE UPPER(code) = UPPER($1)
		AND reward_type = 'ppob_free_fee'
		AND used_count < max_uses
		AND (expires_at IS NULL OR expires_at > $2)`, code, at)
	if err == nil && used == 0 {
		err = models.ErrVoucherInvalid
	}
	return err
}

func (q *ppobRepository) UpdateDepositProof(ctx context.Context, tx pgx.Tx, depositID uuid.UUID, proofURL string, updatedAt time.Time) error {
	ctxt := "PPOBRepository-UpdateDepositProof"
	_, err := execute(ctx, tx, ctxt, "UPDATE ppob_deposits SET payment_proof_url = $2, updated_at = $3 WHERE id = $1", depositID, proofURL, updatedAt)
	return err
}

func (q *ppobRepository) CancelDeposit(ctx context.Context, tx pgx.Tx, branchID, depositID uuid.UUID, updatedAt time.Time) (bool, error) {
	ctxt := "PPOBRepository-CancelDeposit"
	cancelled, err := execute(ctx, tx, ctxt, `UPDATE ppob_deposits SET
		status = 'cancelled'
		, updated_at = $3
	WHERE id = $1
		AND branch_id = $2
		AND status = 'pending'`, depositID, branchID, updatedAt)
	return cancelled > 0, err
}

func (q *ppobRepository) LockPendingDeposit(ctx context.Context, tx pgx.Tx, referenceID string) (*models.PPOBDeposit, error) {
	ctxt := "PPOBRepository-LockPendingDeposit"
	response, err := queryRow[models.PPOBDeposit](ctx, tx, ctxt,
		"SELECT "+ppobDepositColumns+" FROM ppob_deposits t WHERE t.reference_id = $1 AND t.status = 'pending' FOR NO KEY UPDATE", referenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return response, err
}

func (q *ppobRepository) CompleteDeposit(ctx context.Context, tx pgx.Tx, depositID uuid.UUID, balanceAfter float64, updatedAt time.Time) error {
	ctxt := "PPOBRepository-CompleteDeposit"
	_, err := execute(ctx, tx, ctxt, `UPDATE ppob_deposits SET
		status = 'success'
		, balance_after = $2
		, updated_at = $3
	WHERE id = $1`, depositID, balanceAfter, updatedAt)
	return err
}

func (q *ppobRepository) CreatePPOBTransaction(ctx context.Context, tx pgx.Tx, request *models.PPOBTransaction) (*models.PPOBTransaction, error) {
	ctxt := "PPOBRepository-CreatePPOBTransaction"
	return queryRow[models.PPOBTransaction](ctx, tx, ctxt, `INSERT INTO ppob_transactions (
		branch_id
		, reference_id
		, product_name
		, customer_number
		, base_price
		, selling_price
		, cashier_id
		, cashier_name
		, status
		, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	RETURNING id
		, reference_id
		, product_name
		, customer_number
		, base_price
		, selling_price
		, selling_price - base_price AS profit
		, status
		, created_at`,
		request.BranchID,
		request.ReferenceID,
		request.ProductName,
		request.CustomerNumber,
		request.BasePrice,
		request.SellingPrice,
		nullID(request.Cashier.ID),
		request.Cashier.Name,
		request.Status,
		request.CreatedAt,
	)
}
