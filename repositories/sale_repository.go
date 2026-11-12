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

type SaleRepository interface {
	FindSales(ctx context.Context, request *models.SaleFilter) ([]*models.Sale, int64, error)
	// FindSale returns a sale with its items and fees.
	FindSale(ctx context.Context, tx pgx.Tx, branchID, saleID uuid.UUID) (*models.Sale, error)
	// LockSale locks a sale and returns its receipt number, customer and points.
	LockSale(ctx context.Context, tx pgx.Tx, branchID, saleID uuid.UUID) (*models.Sale, error)
	NextReceiptNumber(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, at time.Time) (string, error)
	FindSaleCustomer(ctx context.Context, tx pgx.Tx, branchID, customerID uuid.UUID) (*models.SaleCustomer, error)
	// LockSaleProduct locks a product for the transaction; an unknown product is ErrProductNotFound.
	LockSaleProduct(ctx context.Context, tx pgx.Tx, branchID, productID uuid.UUID) (*models.SaleProduct, error)
	// FindUnitCost is the purchase price of a product, 0 when it does not exist.
	FindUnitCost(ctx context.Context, tx pgx.Tx, branchID, productID uuid.UUID) (float64, error)
	FindActivePromotions(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, at time.Time, customerTypeID *uuid.UUID) ([]*models.ActivePromotion, error)
	FindSaleTax(ctx context.Context, tx pgx.Tx, branchID, taxID uuid.UUID) (*models.SaleTax, error)
	FindSaleFee(ctx context.Context, tx pgx.Tx, branchID, feeID uuid.UUID) (*models.SaleFeeRule, error)
	// FindMDR is the merchant discount rate of the active payment method of a type, nil without one.
	FindMDR(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, paymentMethod string) (*models.MDR, error)
	FindCommissionRules(ctx context.Context, tx pgx.Tx, branchID, staffID uuid.UUID) ([]*models.CommissionRule, error)
	// CreateSale stores the sale and fills its id.
	CreateSale(ctx context.Context, tx pgx.Tx, request *models.SaleRecord) error
	CreateSaleItem(ctx context.Context, tx pgx.Tx, request *models.SaleItemRecord) error
	CreateSaleFee(ctx context.Context, tx pgx.Tx, saleID uuid.UUID, request *models.SaleFee) error
	// FindSoldQuantities lists the products and quantities of a sale.
	FindSoldQuantities(ctx context.Context, tx pgx.Tx, saleID uuid.UUID) ([]*models.SoldQuantity, error)
	DeleteSale(ctx context.Context, tx pgx.Tx, saleID uuid.UUID) error
}

type saleRepository struct {
	base
}

func NewSaleRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) SaleRepository {
	return &saleRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const (
	saleColumns = `t.id
		, t.created_at
		, t.updated_at
		, t.receipt_number
		, t.cashier_id
		, t.cashier_name
		, t.customer_id
		, c.name AS customer_name
		, CASE WHEN c.id IS NULL THEN NULL ELSE JSON_BUILD_OBJECT('id', c.id, 'name', c.name) END AS customer
		, t.payment_method
		, t.payment_type
		, t.subtotal
		, t.discount_total
		, t.tax_name
		, t.tax_total
		, t.service_charge
		, t.fee_total
		, t.rounding
		, t.grand_total
		, t.paid_amount
		, t.change_amount
		, t.profit
		, t.points_earned
		, t.points_redeemed
		, t.label
		, t.customer_type
		, t.note
		, t.status
		, t.due_date::text AS due_date`
	saleFrom = `
	FROM sales t
	LEFT JOIN customers c ON c.id = t.customer_id `
)

var mapSaleSortExpressions = map[string]string{
	"created_at": "t.created_at", "receipt_number": "t.receipt_number", "cashier_name": "t.cashier_name", "customer_name": "c.name",
	"payment_method": "t.payment_method", "payment_type": "t.payment_type", "subtotal": "t.subtotal", "discount_total": "t.discount_total",
	"tax_total": "t.tax_total", "grand_total": "t.grand_total", "paid_amount": "t.paid_amount", "profit": "t.profit", "status": "t.status",
	"due_date": "t.due_date",
}

func (q *saleRepository) FindSales(ctx context.Context, request *models.SaleFilter) ([]*models.Sale, int64, error) {
	ctxt := "SaleRepository-FindSales"
	var cond conditions
	cond.add("t.branch_id = $%d", request.BranchID)
	if request.Keyword != "" {
		cond.add("(t.receipt_number ILIKE $%d OR c.name ILIKE $%d OR t.label ILIKE $%d)", "%"+request.Keyword+"%")
	}
	if request.CashierID != nil {
		cond.add("t.cashier_id = $%d", *request.CashierID)
	}
	if request.CustomerID != nil {
		cond.add("t.customer_id = $%d", *request.CustomerID)
	}
	if request.PaymentMethod != "" {
		cond.add("t.payment_method = $%d", request.PaymentMethod)
	}
	if request.PaymentType != "" {
		cond.add("t.payment_type = $%d", request.PaymentType)
	}
	if request.Status != "" {
		cond.add("t.status = $%d", request.Status)
	}
	if request.StartDate != "" {
		cond.add("t.created_at::date >= $%d::date", request.StartDate)
	}
	if request.EndDate != "" {
		cond.add("t.created_at::date <= $%d::date", request.EndDate)
	}
	where := cond.where()
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1)"+saleFrom+where, cond.params...)
	if err != nil || total == 0 {
		return []*models.Sale{}, 0, err
	}
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString("SELECT ")
	_, _ = sb.WriteString(saleColumns)
	_, _ = sb.WriteString(saleFrom)
	_, _ = sb.WriteString(where)
	_, _ = sb.WriteString(orderBy(request.Sortings, mapSaleSortExpressions, "t.created_at DESC, t.id DESC"))
	_, _ = sb.WriteString(paginate(request.Limit, request.Page))
	response, err := queryRows[models.Sale](ctx, q.dbRead, ctxt, sb.String(), cond.params...)
	return response, total, err
}

func (q *saleRepository) FindSale(ctx context.Context, tx pgx.Tx, branchID, saleID uuid.UUID) (*models.Sale, error) {
	ctxt := "SaleRepository-FindSale"
	response, err := queryRow[models.Sale](ctx, q.reader(tx), ctxt, "SELECT "+saleColumns+`
		, COALESCE((
			SELECT JSON_AGG(JSON_BUILD_OBJECT(
				'id', i.id
				, 'product_id', i.product_id
				, 'product_name', i.product_name
				, 'variant_name', i.variant_name
				, 'quantity', i.quantity
				, 'unit_price', i.unit_price
				, 'discount_amount', i.discount_amount + i.promotion_discount
				, 'subtotal', i.subtotal
				, 'points_earned', i.points_earned
				, 'points_redeemed', i.points_redeemed
			) ORDER BY i.id)
			FROM sale_items i
			WHERE i.sale_id = t.id
		), '[]') AS items
		, COALESCE((
			SELECT JSON_AGG(JSON_BUILD_OBJECT('fee_name', f.fee_name, 'amount', f.amount) ORDER BY f.id)
			FROM sale_fees f
			WHERE f.sale_id = t.id
		), '[]') AS fees`+saleFrom+`
	WHERE t.id = $1
		AND t.branch_id = $2`, saleID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrSaleNotFound
	}
	return response, err
}

func (q *saleRepository) LockSale(ctx context.Context, tx pgx.Tx, branchID, saleID uuid.UUID) (*models.Sale, error) {
	ctxt := "SaleRepository-LockSale"
	response, err := queryRow[models.Sale](ctx, tx, ctxt, `SELECT t.id
		, t.receipt_number
		, t.customer_id
		, t.points_earned
		, t.points_redeemed
	FROM sales t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR UPDATE`, saleID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrSaleNotFound
	}
	return response, err
}

func (q *saleRepository) NextReceiptNumber(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, at time.Time) (string, error) {
	ctxt := "SaleRepository-NextReceiptNumber"
	return nextNumber(ctx, tx, ctxt, "S", at.In(helperLocation()).Format("20060102"), "sales", "receipt_number", "branch_id", branchID)
}

func (q *saleRepository) FindSaleCustomer(ctx context.Context, tx pgx.Tx, branchID, customerID uuid.UUID) (*models.SaleCustomer, error) {
	ctxt := "SaleRepository-FindSaleCustomer"
	response, err := queryRow[models.SaleCustomer](ctx, q.reader(tx), ctxt, `SELECT ct.id AS customer_type_id
		, ct.name AS customer_type_name
		, ct.discount_type
		, ct.discount_value
		, COALESCE(ct.point_multiplier, 1) AS point_multiplier
		, t.points
	FROM customers t
	LEFT JOIN customer_types ct ON ct.id = t.customer_type_id
	WHERE t.id = $1
		AND t.branch_id = $2`, customerID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrCustomerNotFound
	}
	return response, err
}

func (q *saleRepository) LockSaleProduct(ctx context.Context, tx pgx.Tx, branchID, productID uuid.UUID) (*models.SaleProduct, error) {
	ctxt := "SaleRepository-LockSaleProduct"
	response, err := queryRow[models.SaleProduct](ctx, tx, ctxt, `SELECT t.id
		, t.name
		, t.category_id
		, t.selling_price
		, t.purchase_price
		, t.price_type
		, t.discount_type
		, t.discount_value
		, t.wholesale_prices
		, t.customer_type_prices
		, t.ingredients
		, t.variants
		, t.track_stock
		, t.stock
		, t.points_earned
	FROM products t
	WHERE t.id = $1
		AND t.branch_id = $2
		AND t.deleted_at IS NULL
	FOR UPDATE`, productID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrProductNotFound
	}
	return response, err
}

func (q *saleRepository) FindUnitCost(ctx context.Context, tx pgx.Tx, branchID, productID uuid.UUID) (float64, error) {
	ctxt := "SaleRepository-FindUnitCost"
	response, err := queryValue[float64](ctx, q.reader(tx), ctxt, `SELECT t.purchase_price::float8
	FROM products t
	WHERE t.id = $1
		AND t.branch_id = $2`, productID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return response, err
}

func (q *saleRepository) FindActivePromotions(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, at time.Time, customerTypeID *uuid.UUID) ([]*models.ActivePromotion, error) {
	ctxt := "SaleRepository-FindActivePromotions"
	at = at.In(helperLocation())
	return queryRows[models.ActivePromotion](ctx, q.reader(tx), ctxt, `SELECT t.id
		, t.type
		, t.discount_type
		, t.discount_value
		, COALESCE(t.minimum_purchase, 0) AS minimum_purchase
		, COALESCE(t.buy_quantity, 0) AS buy_quantity
		, COALESCE(t.get_quantity, 0) AS get_quantity
		, t.products
	FROM promotions t
	WHERE t.branch_id = $1
		AND t.is_active
		AND t.start_date <= $2::date
		AND t.end_date >= $2::date
		AND (CARDINALITY(t.days) = 0 OR $3 = ANY(t.days))
		AND (t.start_time IS NULL OR t.start_time <= $4)
		AND (t.end_time IS NULL OR t.end_time >= $4)
		AND (CARDINALITY(t.customer_type_ids) = 0 OR $5::uuid = ANY(t.customer_type_ids))
	ORDER BY t.id`, branchID, at.Format(time.DateOnly), strings.ToLower(at.Weekday().String()), at.Format("15:04"), customerTypeID)
}

func (q *saleRepository) FindSaleTax(ctx context.Context, tx pgx.Tx, branchID, taxID uuid.UUID) (*models.SaleTax, error) {
	ctxt := "SaleRepository-FindSaleTax"
	response, err := queryRow[models.SaleTax](ctx, q.reader(tx), ctxt, `SELECT t.name
		, t.rate
		, t.is_inclusive
	FROM taxes t
	WHERE t.id = $1
		AND t.branch_id = $2`, taxID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrTaxNotFound
	}
	return response, err
}

func (q *saleRepository) FindSaleFee(ctx context.Context, tx pgx.Tx, branchID, feeID uuid.UUID) (*models.SaleFeeRule, error) {
	ctxt := "SaleRepository-FindSaleFee"
	response, err := queryRow[models.SaleFeeRule](ctx, q.reader(tx), ctxt, `SELECT t.name
		, t.amount_type
		, t.amount
	FROM fees t
	WHERE t.id = $1
		AND t.branch_id = $2
		AND t.applies_to IN ('sale', 'both')`, feeID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrFeeNotFound
	}
	return response, err
}

func (q *saleRepository) FindMDR(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, paymentMethod string) (*models.MDR, error) {
	ctxt := "SaleRepository-FindMDR"
	response, err := queryRow[models.MDR](ctx, q.reader(tx), ctxt, `SELECT t.mdr_type
		, t.mdr_value
	FROM payment_methods t
	WHERE t.branch_id = $1
		AND t.type = $2
		AND t.is_active
	ORDER BY t.id
	LIMIT 1`, branchID, paymentMethod)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return response, err
}

func (q *saleRepository) FindCommissionRules(ctx context.Context, tx pgx.Tx, branchID, staffID uuid.UUID) ([]*models.CommissionRule, error) {
	ctxt := "SaleRepository-FindCommissionRules"
	return queryRows[models.CommissionRule](ctx, q.reader(tx), ctxt, `SELECT t.basis
		, t.type
		, t.value
		, t.product_ids
	FROM commissions t
	WHERE t.branch_id = $1
		AND t.is_active
		AND (CARDINALITY(t.staff_ids) = 0 OR $2 = ANY(t.staff_ids))`, branchID, staffID)
}

func (q *saleRepository) CreateSale(ctx context.Context, tx pgx.Tx, request *models.SaleRecord) (err error) {
	ctxt := "SaleRepository-CreateSale"
	request.ID, err = queryValue[uuid.UUID](ctx, tx, ctxt, `INSERT INTO sales (
		branch_id
		, receipt_number
		, cashier_id
		, cashier_name
		, customer_id
		, shift_id
		, payment_method
		, payment_type
		, subtotal
		, discount_total
		, tax_id
		, tax_name
		, tax_rate
		, tax_total
		, service_charge
		, fee_total
		, rounding
		, grand_total
		, paid_amount
		, change_amount
		, cost_total
		, profit
		, mdr_rate
		, mdr_amount
		, commission_total
		, points_earned
		, points_redeemed
		, promotion_id
		, label
		, customer_type
		, note
		, status
		, due_date
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27,
		$28, $29, $30, $31, $32, $33::date, $34, $34)
	RETURNING id`,
		request.BranchID, request.ReceiptNumber, request.CashierID, request.CashierName, request.CustomerID, request.ShiftID,
		request.PaymentMethod, request.PaymentType, request.Subtotal, request.DiscountTotal, request.TaxID, request.TaxName, request.TaxRate,
		request.TaxTotal, request.ServiceCharge, request.FeeTotal, request.Rounding, request.GrandTotal, request.PaidAmount,
		request.ChangeAmount, request.CostTotal, request.Profit, request.MDRRate, request.MDRAmount, request.CommissionTotal,
		request.PointsEarned, request.PointsRedeemed, request.PromotionID, request.Label, request.CustomerType, request.Note, request.Status,
		request.DueDate, request.CreatedAt)
	if IsUniqueViolation(err) {
		return models.ErrSaleConflict
	}
	if IsForeignKeyViolation(err) {
		return models.ErrReferenceNotFound
	}
	return err
}

func (q *saleRepository) CreateSaleItem(ctx context.Context, tx pgx.Tx, request *models.SaleItemRecord) error {
	ctxt := "SaleRepository-CreateSaleItem"
	_, err := execute(ctx, tx, ctxt, `INSERT INTO sale_items (
		sale_id
		, product_id
		, product_name
		, variant_name
		, category_id
		, quantity
		, unit_price
		, unit_cost
		, discount_amount
		, subtotal
		, points_earned
		, points_redeemed
		, promotion_id
		, promotion_discount
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		request.SaleID, request.ProductID, request.ProductName, request.VariantName, request.CategoryID, request.Quantity, request.UnitPrice,
		request.UnitCost, request.DiscountAmount, request.Subtotal, request.PointsEarned, request.PointsRedeemed, request.PromotionID,
		request.PromotionDiscount)
	return err
}

func (q *saleRepository) CreateSaleFee(ctx context.Context, tx pgx.Tx, saleID uuid.UUID, request *models.SaleFee) error {
	ctxt := "SaleRepository-CreateSaleFee"
	_, err := execute(ctx, tx, ctxt, "INSERT INTO sale_fees (sale_id, fee_name, amount) VALUES ($1, $2, $3)", saleID, request.FeeName, request.Amount)
	return err
}

func (q *saleRepository) FindSoldQuantities(ctx context.Context, tx pgx.Tx, saleID uuid.UUID) ([]*models.SoldQuantity, error) {
	ctxt := "SaleRepository-FindSoldQuantities"
	return queryRows[models.SoldQuantity](ctx, q.reader(tx), ctxt, `SELECT t.product_id
		, t.product_name
		, t.quantity
	FROM sale_items t
	WHERE t.sale_id = $1
		AND t.product_id IS NOT NULL
	ORDER BY t.id`, saleID)
}

func (q *saleRepository) DeleteSale(ctx context.Context, tx pgx.Tx, saleID uuid.UUID) error {
	ctxt := "SaleRepository-DeleteSale"
	_, err := execute(ctx, tx, ctxt, "DELETE FROM sales WHERE id = $1", saleID)
	return err
}
