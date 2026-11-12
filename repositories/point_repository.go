package repositories

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
)

// PointRepository is the single place the point balance of a customer changes.
type PointRepository interface {
	// ChangePoints returns the balance after the change; an unknown customer is ErrCustomerNotFound.
	ChangePoints(ctx context.Context, tx pgx.Tx, request *models.PointChange) (int64, error)
	FindPointBalance(ctx context.Context, tx pgx.Tx, customerID uuid.UUID) (int64, error)
	FindPointEntries(ctx context.Context, branchID, customerID uuid.UUID, limit, page int64) ([]*models.PointEntry, int64, error)
	// FindCustomerCodes lists the customers to print cards for (all of the branch when customerIDs is empty); a
	// customer without a code gets one derived from their id.
	FindCustomerCodes(ctx context.Context, branchID uuid.UUID, customerIDs []uuid.UUID) ([]*models.CustomerCode, error)
}

type pointRepository struct {
	base
}

func NewPointRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) PointRepository {
	return &pointRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

func (q *pointRepository) ChangePoints(ctx context.Context, tx pgx.Tx, request *models.PointChange) (int64, error) {
	ctxt := "PointRepository-ChangePoints"
	balance, err := queryValue[int64](ctx, tx, ctxt, `UPDATE customers SET
		points = GREATEST(points + $3, 0)
		, updated_at = $4
	WHERE id = $1
		AND branch_id = $2
	RETURNING points`, request.CustomerID, request.BranchID, request.Points, request.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, models.ErrCustomerNotFound
	}
	if err != nil {
		return 0, err
	}
	_, err = execute(ctx, tx, ctxt, `INSERT INTO point_ledger (
		customer_id
		, type
		, points
		, balance_after
		, reference
		, note
		, created_at
	) VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7)`,
		request.CustomerID, request.Type, request.Points, balance, request.Reference, request.Note, request.At)
	return balance, err
}

func (q *pointRepository) FindPointBalance(ctx context.Context, tx pgx.Tx, customerID uuid.UUID) (int64, error) {
	ctxt := "PointRepository-FindPointBalance"
	response, err := queryValue[int64](ctx, q.reader(tx), ctxt, "SELECT t.points FROM customers t WHERE t.id = $1", customerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, models.ErrCustomerNotFound
	}
	return response, err
}

func (q *pointRepository) FindPointEntries(ctx context.Context, branchID, customerID uuid.UUID, limit, page int64) ([]*models.PointEntry, int64, error) {
	ctxt := "PointRepository-FindPointEntries"
	const condition = `FROM point_ledger t
	JOIN customers c ON c.id = t.customer_id
	WHERE t.customer_id = $1
		AND c.branch_id = $2`
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1) "+condition, customerID, branchID)
	if err != nil || total == 0 {
		return []*models.PointEntry{}, 0, err
	}
	response, err := queryRows[models.PointEntry](ctx, q.dbRead, ctxt, `SELECT t.id
		, t.created_at
		, t.type
		, t.points
		, t.balance_after
		, t.reference
		, t.note
	`+condition+` ORDER BY t.id DESC`+paginate(limit, page), customerID, branchID)
	return response, total, err
}

func (q *pointRepository) FindCustomerCodes(ctx context.Context, branchID uuid.UUID, customerIDs []uuid.UUID) ([]*models.CustomerCode, error) {
	ctxt := "PointRepository-FindCustomerCodes"
	if customerIDs == nil {
		customerIDs = []uuid.UUID{}
	}
	return queryRows[models.CustomerCode](ctx, q.dbRead, ctxt, `SELECT t.id
		, t.name
		, COALESCE(NULLIF(t.code, ''), 'C' || UPPER(RIGHT(REPLACE(t.id::text, '-', ''), 8))) AS code
		, t.phone
	FROM customers t
	WHERE t.branch_id = $1
		AND (CARDINALITY($2::uuid[]) = 0 OR t.id = ANY($2))
	ORDER BY t.name
	LIMIT 2000`, branchID, customerIDs)
}
