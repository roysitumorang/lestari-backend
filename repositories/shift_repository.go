package repositories

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
)

type ShiftRepository interface {
	FindShifts(ctx context.Context, request *models.ShiftFilter) ([]*models.Shift, int64, error)
	// FindShift returns a shift with its sales per payment method.
	FindShift(ctx context.Context, branchID, shiftID uuid.UUID) (*models.Shift, error)
	CashDrawerExists(ctx context.Context, branchID, cashDrawerID uuid.UUID) (bool, error)
	HasOpenShift(ctx context.Context, cashDrawerID uuid.UUID) (bool, error)
	CreateShift(ctx context.Context, branchID, cashierID uuid.UUID, request *models.ShiftOpenRequest, at time.Time) (*models.Shift, error)
	// CloseShift closes an open shift and tells whether there was one.
	CloseShift(ctx context.Context, branchID, shiftID uuid.UUID, request *models.ShiftCloseRequest, at time.Time) (bool, error)
}

type shiftRepository struct {
	base
}

func NewShiftRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) ShiftRepository {
	return &shiftRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const (
	shiftCashSales = "COALESCE((SELECT SUM(s.grand_total) FROM sales s WHERE s.shift_id = t.id AND s.payment_method = 'cash'), 0)"
	shiftColumns   = `t.id
		, t.opened_at
		, t.closed_at
		, d.name AS cash_drawer_name
		, u.name AS cashier_name
		, t.opening_cash
		, t.cash_in
		, t.cash_out
		, ` + shiftCashSales + ` AS cash_sales
		, t.opening_cash + t.cash_in - t.cash_out + ` + shiftCashSales + ` AS expected_cash
		, t.actual_cash
		, t.actual_cash - (t.opening_cash + t.cash_in - t.cash_out + ` + shiftCashSales + `) AS difference`
	shiftFrom = `
	FROM shifts t
	LEFT JOIN cash_drawers d ON d.id = t.cash_drawer_id
	LEFT JOIN users u ON u.id = t.cashier_id `
)

func (q *shiftRepository) FindShifts(ctx context.Context, request *models.ShiftFilter) ([]*models.Shift, int64, error) {
	ctxt := "ShiftRepository-FindShifts"
	var cond conditions
	cond.add("t.branch_id = $%d", request.BranchID)
	cond.add("t.opened_at::date >= $%d::date", request.StartDate)
	cond.add("t.opened_at::date <= $%d::date", request.EndDate)
	if request.CashDrawerID != nil {
		cond.add("t.cash_drawer_id = $%d", *request.CashDrawerID)
	}
	if request.CashierID != nil {
		cond.add("t.cashier_id = $%d", *request.CashierID)
	}
	where := cond.where()
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1)"+shiftFrom+where, cond.params...)
	if err != nil || total == 0 {
		return []*models.Shift{}, 0, err
	}
	response, err := queryRows[models.Shift](ctx, q.dbRead, ctxt, "SELECT "+shiftColumns+shiftFrom+where+" ORDER BY t.opened_at DESC"+paginate(request.Limit, request.Page), cond.params...)
	return response, total, err
}

func (q *shiftRepository) FindShift(ctx context.Context, branchID, shiftID uuid.UUID) (*models.Shift, error) {
	ctxt := "ShiftRepository-FindShift"
	// read on the write pool: a shift is read back right after it is closed
	response, err := queryRow[models.Shift](ctx, q.dbWrite, ctxt, "SELECT "+shiftColumns+`
		, COALESCE((
			SELECT JSON_AGG(x)
			FROM (
				SELECT s.payment_method
					, COUNT(1) AS transaction_count
					, SUM(s.grand_total) AS amount
				FROM sales s
				WHERE s.shift_id = t.id
				GROUP BY 1
				ORDER BY 1
			) x
		), '[]') AS payments`+shiftFrom+`
	WHERE t.id = $1
		AND t.branch_id = $2`, shiftID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrShiftNotFound
	}
	return response, err
}

func (q *shiftRepository) CashDrawerExists(ctx context.Context, branchID, cashDrawerID uuid.UUID) (bool, error) {
	ctxt := "ShiftRepository-CashDrawerExists"
	return queryValue[bool](ctx, q.dbRead, ctxt, "SELECT EXISTS(SELECT 1 FROM cash_drawers t WHERE t.id = $1 AND t.branch_id = $2)", cashDrawerID, branchID)
}

func (q *shiftRepository) HasOpenShift(ctx context.Context, cashDrawerID uuid.UUID) (bool, error) {
	ctxt := "ShiftRepository-HasOpenShift"
	return queryValue[bool](ctx, q.dbWrite, ctxt, "SELECT EXISTS(SELECT 1 FROM shifts t WHERE t.cash_drawer_id = $1 AND t.closed_at IS NULL)", cashDrawerID)
}

func (q *shiftRepository) CreateShift(ctx context.Context, branchID, cashierID uuid.UUID, request *models.ShiftOpenRequest, at time.Time) (*models.Shift, error) {
	ctxt := "ShiftRepository-CreateShift"
	return queryRow[models.Shift](ctx, q.dbWrite, ctxt, `INSERT INTO shifts (
		branch_id
		, cash_drawer_id
		, cashier_id
		, opening_cash
		, opened_at
	) VALUES ($1, $2, $3, $4, $5)
	RETURNING id
		, cash_drawer_id
		, cashier_id
		, opened_at
		, opening_cash`, branchID, request.CashDrawerID, nullID(cashierID), request.OpeningCash, at)
}

func (q *shiftRepository) CloseShift(ctx context.Context, branchID, shiftID uuid.UUID, request *models.ShiftCloseRequest, at time.Time) (bool, error) {
	ctxt := "ShiftRepository-CloseShift"
	rowsAffected, err := execute(ctx, q.dbWrite, ctxt, `UPDATE shifts SET
		closed_at = $3
		, actual_cash = $4
		, cash_in = $5
		, cash_out = $6
	WHERE id = $1
		AND branch_id = $2
		AND closed_at IS NULL`, shiftID, branchID, at, request.ActualCash, request.CashIn, request.CashOut)
	return rowsAffected > 0, err
}
