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

type ReturnRepository interface {
	FindReturns(ctx context.Context, request *models.ReturnFilter) ([]*models.Return, int64, error)
	// FindReturn returns a return with its items.
	FindReturn(ctx context.Context, tx pgx.Tx, branchID, returnID uuid.UUID, kind string) (*models.Return, error)
	// FindDocumentNumber is the receipt number of the sale or purchase a return is made against.
	FindDocumentNumber(ctx context.Context, tx pgx.Tx, branchID, documentID uuid.UUID, kind string) (string, error)
	NextReturnNumber(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, kind string, at time.Time) (string, error)
	// CreateReturn stores the header of a return and returns its id.
	CreateReturn(ctx context.Context, tx pgx.Tx, request *models.ReturnCreate, returnNumber string) (uuid.UUID, error)
	CreateReturnItem(ctx context.Context, tx pgx.Tx, returnID uuid.UUID, request *models.ReturnItem) error
	SetRefundAmount(ctx context.Context, tx pgx.Tx, returnID uuid.UUID, amount float64) error
	// FindDocumentLine totals a product on a sale or purchase: its quantity and average price after discounts.
	// A product that is not on the document is pgx.ErrNoRows.
	FindDocumentLine(ctx context.Context, tx pgx.Tx, documentID, productID uuid.UUID, kind string) (*models.SoldQuantity, error)
	// FindReturnedQuantity is how much of a product was already returned to the supplier of a purchase.
	FindReturnedQuantity(ctx context.Context, tx pgx.Tx, purchaseID, productID, exceptReturnID uuid.UUID) (float64, error)
}

type returnRepository struct {
	base
}

func NewReturnRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) ReturnRepository {
	return &returnRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const (
	returnColumns = `t.id
		, t.created_at
		, t.return_number
		, t.refund_method
		, t.refund_amount
		, t.reason
		, t.cashier_name
		, (SELECT COUNT(1) FROM return_items ri WHERE ri.return_id = t.id) AS item_count
		, t.sale_id
		, sa.receipt_number
		, CASE WHEN c.id IS NULL THEN NULL ELSE JSON_BUILD_OBJECT('id', c.id, 'name', c.name) END AS customer
		, t.purchase_id
		, pu.receipt_number AS purchase_number
		, CASE WHEN s.id IS NULL THEN NULL ELSE JSON_BUILD_OBJECT('id', s.id, 'name', s.name) END AS supplier`
	returnFrom = `
	FROM returns t
	LEFT JOIN sales sa ON sa.id = t.sale_id
	LEFT JOIN customers c ON c.id = sa.customer_id
	LEFT JOIN purchases pu ON pu.id = t.purchase_id
	LEFT JOIN suppliers s ON s.id = pu.supplier_id `
)

var mapReturnSortExpressions = map[string]string{
	"created_at": "t.created_at", "return_number": "t.return_number", "refund_method": "t.refund_method", "refund_amount": "t.refund_amount",
	"cashier_name": "t.cashier_name", "item_count": "(SELECT COUNT(1) FROM return_items ri WHERE ri.return_id = t.id)",
}

func (q *returnRepository) FindReturns(ctx context.Context, request *models.ReturnFilter) ([]*models.Return, int64, error) {
	ctxt := "ReturnRepository-FindReturns"
	var cond conditions
	cond.add("t.branch_id = $%d", request.BranchID)
	cond.add("t.kind = $%d", request.Kind)
	if request.Keyword != "" {
		cond.add("(t.return_number ILIKE $%d OR sa.receipt_number ILIKE $%d OR pu.receipt_number ILIKE $%d)", "%"+request.Keyword+"%")
	}
	if request.StartDate != "" {
		cond.add("t.created_at::date >= $%d::date", request.StartDate)
	}
	if request.EndDate != "" {
		cond.add("t.created_at::date <= $%d::date", request.EndDate)
	}
	where := cond.where()
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1)"+returnFrom+where, cond.params...)
	if err != nil || total == 0 {
		return []*models.Return{}, 0, err
	}
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString("SELECT ")
	_, _ = sb.WriteString(returnColumns)
	_, _ = sb.WriteString(returnFrom)
	_, _ = sb.WriteString(where)
	_, _ = sb.WriteString(orderBy(request.Sortings, mapReturnSortExpressions, "t.created_at DESC, t.id DESC"))
	_, _ = sb.WriteString(paginate(request.Limit, request.Page))
	response, err := queryRows[models.Return](ctx, q.dbRead, ctxt, sb.String(), cond.params...)
	return response, total, err
}

func (q *returnRepository) FindReturn(ctx context.Context, tx pgx.Tx, branchID, returnID uuid.UUID, kind string) (*models.Return, error) {
	ctxt := "ReturnRepository-FindReturn"
	response, err := queryRow[models.Return](ctx, q.reader(tx), ctxt, "SELECT "+returnColumns+`
		, COALESCE((
			SELECT JSON_AGG(JSON_BUILD_OBJECT(
				'product_id', i.product_id
				, 'product_name', i.product_name
				, 'quantity', i.quantity
				, 'unit_price', i.unit_price
				, 'unit_cost', i.unit_price
				, 'subtotal', i.subtotal
			) ORDER BY i.id)
			FROM return_items i
			WHERE i.return_id = t.id
		), '[]') AS items`+returnFrom+`
	WHERE t.id = $1
		AND t.branch_id = $2
		AND t.kind = $3`, returnID, branchID, kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrReturnNotFound
	}
	return response, err
}

func (q *returnRepository) FindDocumentNumber(ctx context.Context, tx pgx.Tx, branchID, documentID uuid.UUID, kind string) (string, error) {
	ctxt := "ReturnRepository-FindDocumentNumber"
	table, notFound := "sales", models.ErrSaleNotFound
	if kind == models.ReturnKindPurchase {
		table, notFound = "purchases", models.ErrPurchaseNotFound
	}
	response, err := queryValue[string](ctx, q.reader(tx), ctxt, sprintf("SELECT t.receipt_number FROM %s t WHERE t.id = $1 AND t.branch_id = $2", table), documentID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", notFound
	}
	return response, err
}

func (q *returnRepository) NextReturnNumber(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, kind string, at time.Time) (string, error) {
	ctxt := "ReturnRepository-NextReturnNumber"
	prefix := "RJ"
	if kind == models.ReturnKindPurchase {
		prefix = "RB"
	}
	return nextNumber(ctx, tx, ctxt, prefix, at.In(helperLocation()).Format("20060102"), "returns", "return_number", "branch_id", branchID)
}

func (q *returnRepository) CreateReturn(ctx context.Context, tx pgx.Tx, request *models.ReturnCreate, returnNumber string) (uuid.UUID, error) {
	ctxt := "ReturnRepository-CreateReturn"
	var saleID, purchaseID *uuid.UUID
	if request.Kind == models.ReturnKindPurchase {
		purchaseID = &request.DocumentID
	} else {
		saleID = &request.DocumentID
	}
	return queryValue[uuid.UUID](ctx, tx, ctxt, `INSERT INTO returns (
		branch_id
		, kind
		, return_number
		, sale_id
		, purchase_id
		, refund_method
		, reason
		, cashier_name
		, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9)
	RETURNING id`,
		request.BranchID, request.Kind, returnNumber, saleID, purchaseID, request.RefundMethod, request.Reason, request.Actor.Name, request.At)
}

func (q *returnRepository) CreateReturnItem(ctx context.Context, tx pgx.Tx, returnID uuid.UUID, request *models.ReturnItem) error {
	ctxt := "ReturnRepository-CreateReturnItem"
	_, err := execute(ctx, tx, ctxt, `INSERT INTO return_items (
		return_id
		, product_id
		, product_name
		, quantity
		, unit_price
		, subtotal
	) VALUES ($1, $2, $3, $4, $5, $6)`, returnID, request.ProductID, request.ProductName, request.Quantity, request.UnitPrice, request.Subtotal)
	return err
}

func (q *returnRepository) SetRefundAmount(ctx context.Context, tx pgx.Tx, returnID uuid.UUID, amount float64) error {
	ctxt := "ReturnRepository-SetRefundAmount"
	_, err := execute(ctx, tx, ctxt, "UPDATE returns SET refund_amount = $2 WHERE id = $1", returnID, amount)
	return err
}

func (q *returnRepository) FindDocumentLine(ctx context.Context, tx pgx.Tx, documentID, productID uuid.UUID, kind string) (*models.SoldQuantity, error) {
	ctxt := "ReturnRepository-FindDocumentLine"
	table, column := "sale_items", "sale_id"
	if kind == models.ReturnKindPurchase {
		table, column = "purchase_items", "purchase_id"
	}
	return queryRow[models.SoldQuantity](ctx, tx, ctxt, sprintf(`SELECT t.product_id
		, t.product_name
		, SUM(t.quantity) AS quantity
		, COALESCE(ROUND(SUM(t.subtotal) / NULLIF(SUM(t.quantity), 0), 2), 0) AS unit_price
	FROM %s t
	WHERE t.%s = $1
		AND t.product_id = $2
	GROUP BY t.product_id, t.product_name
	LIMIT 1`, table, column), documentID, productID)
}

func (q *returnRepository) FindReturnedQuantity(ctx context.Context, tx pgx.Tx, purchaseID, productID, exceptReturnID uuid.UUID) (float64, error) {
	ctxt := "ReturnRepository-FindReturnedQuantity"
	return queryValue[float64](ctx, tx, ctxt, `SELECT COALESCE(SUM(ri.quantity), 0)::float8
	FROM return_items ri
	JOIN returns r ON r.id = ri.return_id
	WHERE r.purchase_id = $1
		AND ri.product_id = $2
		AND r.id <> $3`, purchaseID, productID, exceptReturnID)
}
