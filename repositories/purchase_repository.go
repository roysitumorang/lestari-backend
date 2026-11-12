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

type PurchaseRepository interface {
	FindPurchases(ctx context.Context, request *models.PurchaseFilter) ([]*models.Purchase, int64, error)
	// FindPurchase returns a purchase with its items.
	FindPurchase(ctx context.Context, tx pgx.Tx, branchID, purchaseID uuid.UUID) (*models.Purchase, error)
	NextReceiptNumber(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, at time.Time) (string, error)
	// CreatePurchase stores the header of a purchase and returns its id; the totals follow from the items.
	CreatePurchase(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite, receiptNumber string) (uuid.UUID, error)
	// UpdatePurchase writes the attributes that differ from current.
	UpdatePurchase(ctx context.Context, tx pgx.Tx, current *models.Purchase, request *models.PurchaseWrite) error
	DeletePurchase(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID) error
	// FindProductName is the name of a product of the branch; an unknown product is ErrProductNotFound.
	FindProductName(ctx context.Context, tx pgx.Tx, branchID, productID uuid.UUID) (string, error)
	CreatePurchaseItem(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID, request *models.PurchaseItem) error
	FindPurchasedQuantities(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID) ([]*models.SoldQuantity, error)
	DeletePurchaseItems(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID) error
	FindPurchaseTotals(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID) (*models.PurchaseTotals, error)
	// UpdatePurchaseTotals writes the totals and status when any of them changed.
	UpdatePurchaseTotals(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID, subtotal, grandTotal, paidAmount float64, status string, at time.Time) error
}

type purchaseRepository struct {
	base
}

func NewPurchaseRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) PurchaseRepository {
	return &purchaseRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const (
	purchaseColumns = `t.id
		, t.created_at
		, t.updated_at
		, t.receipt_number
		, t.supplier_id
		, s.name AS supplier_name
		, CASE WHEN s.id IS NULL THEN NULL ELSE JSON_BUILD_OBJECT('id', s.id, 'name', s.name) END AS supplier
		, t.purchase_date::text AS purchase_date
		, t.invoice_number
		, t.payment_type
		, t.payment_method
		, t.due_date::text AS due_date
		, t.paid_amount
		, t.subtotal
		, t.discount_total
		, t.tax_total
		, t.fee_total
		, t.grand_total
		, t.staff_name
		, t.status
		, t.note`
	purchaseFrom = `
	FROM purchases t
	LEFT JOIN suppliers s ON s.id = t.supplier_id `
)

var mapPurchaseSortExpressions = map[string]string{
	"purchase_date": "t.purchase_date", "receipt_number": "t.receipt_number", "supplier_name": "s.name", "invoice_number": "t.invoice_number",
	"payment_type": "t.payment_type", "due_date": "t.due_date", "subtotal": "t.subtotal", "grand_total": "t.grand_total",
	"paid_amount": "t.paid_amount", "staff_name": "t.staff_name", "status": "t.status", "created_at": "t.created_at",
}

func (q *purchaseRepository) FindPurchases(ctx context.Context, request *models.PurchaseFilter) ([]*models.Purchase, int64, error) {
	ctxt := "PurchaseRepository-FindPurchases"
	var cond conditions
	cond.add("t.branch_id = $%d", request.BranchID)
	if request.Keyword != "" {
		cond.add("(t.receipt_number ILIKE $%d OR s.name ILIKE $%d OR t.invoice_number ILIKE $%d)", "%"+request.Keyword+"%")
	}
	if request.SupplierID != nil {
		cond.add("t.supplier_id = $%d", *request.SupplierID)
	}
	if request.PaymentType != "" {
		cond.add("t.payment_type = $%d", request.PaymentType)
	}
	if request.StartDate != "" {
		cond.add("t.purchase_date >= $%d::date", request.StartDate)
	}
	if request.EndDate != "" {
		cond.add("t.purchase_date <= $%d::date", request.EndDate)
	}
	where := cond.where()
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1)"+purchaseFrom+where, cond.params...)
	if err != nil || total == 0 {
		return []*models.Purchase{}, 0, err
	}
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString("SELECT ")
	_, _ = sb.WriteString(purchaseColumns)
	_, _ = sb.WriteString(purchaseFrom)
	_, _ = sb.WriteString(where)
	_, _ = sb.WriteString(orderBy(request.Sortings, mapPurchaseSortExpressions, "t.purchase_date DESC, t.id DESC"))
	_, _ = sb.WriteString(paginate(request.Limit, request.Page))
	response, err := queryRows[models.Purchase](ctx, q.dbRead, ctxt, sb.String(), cond.params...)
	return response, total, err
}

func (q *purchaseRepository) FindPurchase(ctx context.Context, tx pgx.Tx, branchID, purchaseID uuid.UUID) (*models.Purchase, error) {
	ctxt := "PurchaseRepository-FindPurchase"
	response, err := queryRow[models.Purchase](ctx, q.reader(tx), ctxt, "SELECT "+purchaseColumns+`
		, COALESCE((
			SELECT JSON_AGG(JSON_BUILD_OBJECT(
				'product_id', i.product_id
				, 'product_name', i.product_name
				, 'quantity', i.quantity
				, 'unit', i.unit
				, 'unit_cost', i.unit_cost
				, 'discount_amount', i.discount_amount
				, 'subtotal', i.subtotal
			) ORDER BY i.id)
			FROM purchase_items i
			WHERE i.purchase_id = t.id
		), '[]') AS items`+purchaseFrom+`
	WHERE t.id = $1
		AND t.branch_id = $2`, purchaseID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPurchaseNotFound
	}
	return response, err
}

func (q *purchaseRepository) NextReceiptNumber(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, at time.Time) (string, error) {
	ctxt := "PurchaseRepository-NextReceiptNumber"
	return nextNumber(ctx, tx, ctxt, "PO", at.In(helperLocation()).Format("20060102"), "purchases", "receipt_number", "branch_id", branchID)
}

func (q *purchaseRepository) CreatePurchase(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite, receiptNumber string) (uuid.UUID, error) {
	ctxt := "PurchaseRepository-CreatePurchase"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO purchases (
		branch_id
		, receipt_number
		, supplier_id
		, staff_id
		, staff_name
		, purchase_date
		, invoice_number
		, payment_type
		, payment_method
		, due_date
		, paid_amount
		, discount_total
		, tax_total
		, fee_total
		, note
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6::date, $7, $8, $9, $10::date, COALESCE($11, 0), COALESCE($12, 0), COALESCE($13, 0), COALESCE($14, 0), $15, $16, $16)
	RETURNING id`)
	response, err := queryValue[uuid.UUID](ctx, tx, ctxt, sb.String(),
		request.BranchID, receiptNumber, request.SupplierID, nullID(request.Actor.ID), request.Actor.Name, request.PurchaseDate,
		request.InvoiceNumber, request.PaymentType, request.PaymentMethod, request.DueDate, request.PaidAmount, request.DiscountTotal,
		request.TaxTotal, request.FeeTotal, request.Note, request.At)
	if IsForeignKeyViolation(err) {
		return response, models.ErrReferenceNotFound
	}
	return response, err
}

func (q *purchaseRepository) UpdatePurchase(ctx context.Context, tx pgx.Tx, current *models.Purchase, request *models.PurchaseWrite) error {
	ctxt := "PurchaseRepository-UpdatePurchase"
	var set changes
	if request.SupplierID != nil || request.IsNull("supplier_id") {
		set.set(!equalPtr(current.SupplierID, request.SupplierID), "supplier_id", request.SupplierID)
	}
	if request.PurchaseDate != nil {
		set.set(current.PurchaseDate != *request.PurchaseDate, "purchase_date", *request.PurchaseDate, "::date")
	}
	if request.InvoiceNumber != nil || request.IsNull("invoice_number") {
		set.set(!equalPtr(current.InvoiceNumber, request.InvoiceNumber), "invoice_number", request.InvoiceNumber)
	}
	if request.PaymentType != nil {
		set.set(current.PaymentType != *request.PaymentType, "payment_type", *request.PaymentType)
	}
	if request.PaymentMethod != nil || request.IsNull("payment_method") {
		set.set(!equalPtr(current.PaymentMethod, request.PaymentMethod), "payment_method", request.PaymentMethod)
	}
	if request.DueDate != nil || request.IsNull("due_date") {
		set.set(!equalPtr(current.DueDate, request.DueDate), "due_date", request.DueDate, "::date")
	}
	if request.PaidAmount != nil {
		set.set(current.PaidAmount != *request.PaidAmount, "paid_amount", *request.PaidAmount)
	}
	if request.DiscountTotal != nil {
		set.set(current.DiscountTotal != *request.DiscountTotal, "discount_total", *request.DiscountTotal)
	}
	if request.TaxTotal != nil {
		set.set(current.TaxTotal != *request.TaxTotal, "tax_total", *request.TaxTotal)
	}
	if request.FeeTotal != nil {
		set.set(current.FeeTotal != *request.FeeTotal, "fee_total", *request.FeeTotal)
	}
	if request.Note != nil || request.IsNull("note") {
		set.set(!equalPtr(current.Note, request.Note), "note", request.Note)
	}
	err := set.apply(ctx, tx, ctxt, "purchases", current.ID, request.At)
	if IsForeignKeyViolation(err) {
		return models.ErrReferenceNotFound
	}
	return err
}

func (q *purchaseRepository) DeletePurchase(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID) error {
	ctxt := "PurchaseRepository-DeletePurchase"
	_, err := execute(ctx, tx, ctxt, "DELETE FROM purchases WHERE id = $1", purchaseID)
	return err
}

func (q *purchaseRepository) FindProductName(ctx context.Context, tx pgx.Tx, branchID, productID uuid.UUID) (string, error) {
	ctxt := "PurchaseRepository-FindProductName"
	response, err := queryValue[string](ctx, q.reader(tx), ctxt, `SELECT t.name
	FROM products t
	WHERE t.id = $1
		AND t.branch_id = $2
		AND t.deleted_at IS NULL`, productID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", models.ErrProductNotFound
	}
	return response, err
}

func (q *purchaseRepository) CreatePurchaseItem(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID, request *models.PurchaseItem) error {
	ctxt := "PurchaseRepository-CreatePurchaseItem"
	_, err := execute(ctx, tx, ctxt, `INSERT INTO purchase_items (
		purchase_id
		, product_id
		, product_name
		, quantity
		, unit
		, unit_cost
		, discount_amount
		, subtotal
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		purchaseID, request.ProductID, request.ProductName, request.Quantity, request.Unit, request.UnitCost, request.DiscountAmount,
		request.Subtotal)
	return err
}

func (q *purchaseRepository) FindPurchasedQuantities(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID) ([]*models.SoldQuantity, error) {
	ctxt := "PurchaseRepository-FindPurchasedQuantities"
	return queryRows[models.SoldQuantity](ctx, q.reader(tx), ctxt, `SELECT t.product_id
		, t.product_name
		, t.quantity
	FROM purchase_items t
	WHERE t.purchase_id = $1
		AND t.product_id IS NOT NULL
	ORDER BY t.id`, purchaseID)
}

func (q *purchaseRepository) DeletePurchaseItems(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID) error {
	ctxt := "PurchaseRepository-DeletePurchaseItems"
	_, err := execute(ctx, tx, ctxt, "DELETE FROM purchase_items WHERE purchase_id = $1", purchaseID)
	return err
}

func (q *purchaseRepository) FindPurchaseTotals(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID) (*models.PurchaseTotals, error) {
	ctxt := "PurchaseRepository-FindPurchaseTotals"
	response, err := queryRow[models.PurchaseTotals](ctx, q.reader(tx), ctxt, `SELECT t.payment_type
		, t.receipt_number
		, t.supplier_id
		, t.paid_amount
		, t.due_date::text AS due_date
		, t.discount_total
		, t.tax_total
		, t.fee_total
		, t.purchase_date::text AS purchase_date
		, COALESCE((SELECT SUM(i.subtotal) FROM purchase_items i WHERE i.purchase_id = t.id), 0) AS subtotal
		, COALESCE((
			SELECT SUM(dp.amount)
			FROM debt_payments dp
			JOIN debts d ON d.id = dp.debt_id
			WHERE d.purchase_id = t.id
		), 0) AS debt_paid
	FROM purchases t
	WHERE t.id = $1`, purchaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrPurchaseNotFound
	}
	return response, err
}

func (q *purchaseRepository) UpdatePurchaseTotals(ctx context.Context, tx pgx.Tx, purchaseID uuid.UUID, subtotal, grandTotal, paidAmount float64, status string, at time.Time) error {
	ctxt := "PurchaseRepository-UpdatePurchaseTotals"
	_, err := execute(ctx, tx, ctxt, `UPDATE purchases SET
		subtotal = $2
		, grand_total = $3
		, paid_amount = $4
		, status = $5
		, updated_at = $6
	WHERE id = $1
		AND (subtotal, grand_total, paid_amount, status) IS DISTINCT FROM ($2::numeric, $3::numeric, $4::numeric, $5)`,
		purchaseID, subtotal, grandTotal, paidAmount, status, at)
	return err
}
