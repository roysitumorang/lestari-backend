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

type OrderRepository interface {
	FindOrders(ctx context.Context, request *models.OrderFilter) ([]*models.Order, int64, error)
	// FindOrder returns an order with its items.
	FindOrder(ctx context.Context, tx pgx.Tx, branchID, orderID uuid.UUID) (*models.Order, error)
	// LockOrder locks an order and returns its number and status.
	LockOrder(ctx context.Context, tx pgx.Tx, branchID, orderID uuid.UUID) (*models.Order, error)
	NextOrderNumber(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, at time.Time) (string, error)
	// CreateOrder stores the header of an order and returns its id.
	CreateOrder(ctx context.Context, tx pgx.Tx, request *models.OrderCreate, orderNumber, status string) (uuid.UUID, error)
	// FindOnlineProduct is a product sold online; any other is ErrProductNotFound.
	FindOnlineProduct(ctx context.Context, tx pgx.Tx, branchID, productID uuid.UUID) (*models.OnlineProduct, error)
	CreateOrderItem(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, request *models.OrderItem) error
	SetOrderSubtotal(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, subtotal float64) error
	FindOrderedQuantities(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]*models.SoldQuantity, error)
	UpdateOrderStatus(ctx context.Context, tx pgx.Tx, request *models.OrderStatusChange) error
}

type orderRepository struct {
	base
}

func NewOrderRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) OrderRepository {
	return &orderRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const orderColumns = `t.id
		, t.created_at
		, t.updated_at
		, t.order_number
		, t.customer_name
		, t.customer_phone
		, t.shipping_address
		, t.shipping_method
		, t.courier
		, t.tracking_number
		, t.payment_method
		, t.payment_proof_url
		, t.subtotal
		, t.shipping_fee
		, t.grand_total
		, t.customer_note
		, t.status
		, t.status_reason`

var mapOrderSortExpressions = map[string]string{
	"created_at": "t.created_at", "order_number": "t.order_number", "customer_name": "t.customer_name", "payment_method": "t.payment_method",
	"subtotal": "t.subtotal", "shipping_fee": "t.shipping_fee", "grand_total": "t.grand_total", "status": "t.status",
}

func (q *orderRepository) FindOrders(ctx context.Context, request *models.OrderFilter) ([]*models.Order, int64, error) {
	ctxt := "OrderRepository-FindOrders"
	var cond conditions
	cond.add("t.branch_id = $%d", request.BranchID)
	if request.Keyword != "" {
		cond.add("(t.order_number ILIKE $%d OR t.customer_name ILIKE $%d)", "%"+request.Keyword+"%")
	}
	if request.PaymentMethod != "" {
		cond.add("t.payment_method = $%d", request.PaymentMethod)
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
	where := " FROM catalog_orders t " + cond.where()
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1)"+where, cond.params...)
	if err != nil || total == 0 {
		return []*models.Order{}, 0, err
	}
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString("SELECT ")
	_, _ = sb.WriteString(orderColumns)
	_, _ = sb.WriteString(where)
	_, _ = sb.WriteString(orderBy(request.Sortings, mapOrderSortExpressions, "t.created_at DESC, t.id DESC"))
	_, _ = sb.WriteString(paginate(request.Limit, request.Page))
	response, err := queryRows[models.Order](ctx, q.dbRead, ctxt, sb.String(), cond.params...)
	return response, total, err
}

func (q *orderRepository) FindOrder(ctx context.Context, tx pgx.Tx, branchID, orderID uuid.UUID) (*models.Order, error) {
	ctxt := "OrderRepository-FindOrder"
	response, err := queryRow[models.Order](ctx, q.reader(tx), ctxt, "SELECT "+orderColumns+`
		, COALESCE((
			SELECT JSON_AGG(JSON_BUILD_OBJECT(
				'product_id', i.product_id
				, 'product_name', i.product_name
				, 'quantity', i.quantity
				, 'unit_price', i.unit_price
				, 'subtotal', i.subtotal
			) ORDER BY i.id)
			FROM catalog_order_items i
			WHERE i.order_id = t.id
		), '[]') AS items
	FROM catalog_orders t
	WHERE t.id = $1
		AND t.branch_id = $2`, orderID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrOrderNotFound
	}
	return response, err
}

func (q *orderRepository) LockOrder(ctx context.Context, tx pgx.Tx, branchID, orderID uuid.UUID) (*models.Order, error) {
	ctxt := "OrderRepository-LockOrder"
	response, err := queryRow[models.Order](ctx, tx, ctxt, `SELECT t.id
		, t.order_number
		, t.status
	FROM catalog_orders t
	WHERE t.id = $1
		AND t.branch_id = $2
	FOR UPDATE`, orderID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrOrderNotFound
	}
	return response, err
}

func (q *orderRepository) NextOrderNumber(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, at time.Time) (string, error) {
	ctxt := "OrderRepository-NextOrderNumber"
	return nextNumber(ctx, tx, ctxt, "ORD", at.In(helperLocation()).Format("20060102"), "catalog_orders", "order_number", "branch_id", branchID)
}

func (q *orderRepository) CreateOrder(ctx context.Context, tx pgx.Tx, request *models.OrderCreate, orderNumber, status string) (uuid.UUID, error) {
	ctxt := "OrderRepository-CreateOrder"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO catalog_orders (
		branch_id
		, order_number
		, customer_name
		, customer_phone
		, shipping_address
		, shipping_method
		, courier
		, payment_method
		, payment_proof_url
		, shipping_fee
		, customer_note
		, status
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8, NULLIF($9, ''), $10, NULLIF($11, ''), $12, $13, $13)
	RETURNING id`)
	return queryValue[uuid.UUID](ctx, tx, ctxt, sb.String(),
		request.BranchID, orderNumber, request.CustomerName, request.CustomerPhone, request.ShippingAddress, request.ShippingMethod,
		request.Courier, request.PaymentMethod, request.PaymentProofURL, request.ShippingFee, request.CustomerNote, status, request.At)
}

func (q *orderRepository) FindOnlineProduct(ctx context.Context, tx pgx.Tx, branchID, productID uuid.UUID) (*models.OnlineProduct, error) {
	ctxt := "OrderRepository-FindOnlineProduct"
	response, err := queryRow[models.OnlineProduct](ctx, q.reader(tx), ctxt, `SELECT t.name
		, t.selling_price
	FROM products t
	WHERE t.id = $1
		AND t.branch_id = $2
		AND t.deleted_at IS NULL
		AND t.show_in_online_store`, productID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrProductNotFound
	}
	return response, err
}

func (q *orderRepository) CreateOrderItem(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, request *models.OrderItem) error {
	ctxt := "OrderRepository-CreateOrderItem"
	_, err := execute(ctx, tx, ctxt, `INSERT INTO catalog_order_items (
		order_id
		, product_id
		, product_name
		, quantity
		, unit_price
		, subtotal
	) VALUES ($1, $2, $3, $4, $5, $6)`, orderID, request.ProductID, request.ProductName, request.Quantity, request.UnitPrice, request.Subtotal)
	return err
}

func (q *orderRepository) SetOrderSubtotal(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, subtotal float64) error {
	ctxt := "OrderRepository-SetOrderSubtotal"
	_, err := execute(ctx, tx, ctxt, `UPDATE catalog_orders SET
		subtotal = $2
		, grand_total = $2 + shipping_fee
	WHERE id = $1`, orderID, subtotal)
	return err
}

func (q *orderRepository) FindOrderedQuantities(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]*models.SoldQuantity, error) {
	ctxt := "OrderRepository-FindOrderedQuantities"
	return queryRows[models.SoldQuantity](ctx, q.reader(tx), ctxt, `SELECT t.product_id
		, t.product_name
		, t.quantity
	FROM catalog_order_items t
	WHERE t.order_id = $1
		AND t.product_id IS NOT NULL
	ORDER BY t.id`, orderID)
}

func (q *orderRepository) UpdateOrderStatus(ctx context.Context, tx pgx.Tx, request *models.OrderStatusChange) error {
	ctxt := "OrderRepository-UpdateOrderStatus"
	_, err := execute(ctx, tx, ctxt, `UPDATE catalog_orders SET
		status = $2
		, status_reason = NULLIF($3, '')
		, courier = COALESCE(NULLIF($4, ''), courier)
		, tracking_number = COALESCE(NULLIF($5, ''), tracking_number)
		, updated_at = $6
	WHERE id = $1`, request.OrderID, request.Status, request.Reason, request.Courier, request.TrackingNumber, request.At)
	return err
}
