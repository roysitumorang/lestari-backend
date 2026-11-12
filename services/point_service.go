package services

import (
	"context"
	"errors"
	"log/slog"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type PointService interface {
	// ChangePoints changes the point balance of a customer and returns the new balance.
	ChangePoints(ctx context.Context, tx pgx.Tx, request *models.PointChange) (int64, error)
	FindPointEntries(ctx context.Context, branchID, customerID uuid.UUID, limit, page int64) ([]*models.PointEntry, int64, error)
	FindCustomerCodes(ctx context.Context, branchID uuid.UUID, customerIDs []uuid.UUID) ([]*models.CustomerCode, error)
	// ImportPoints applies one imported row to the customer with its code.
	ImportPoints(ctx context.Context, tx pgx.Tx, request *models.PointImport) error
}

type pointService struct {
	pointRepository    repositories.PointRepository
	customerRepository repositories.CustomerRepository
}

func NewPointService(
	pointRepository repositories.PointRepository,
	customerRepository repositories.CustomerRepository,
) PointService {
	return &pointService{
		pointRepository:    pointRepository,
		customerRepository: customerRepository,
	}
}

func (q *pointService) ChangePoints(ctx context.Context, tx pgx.Tx, request *models.PointChange) (int64, error) {
	ctxt := "PointService-ChangePoints"
	response, err := q.pointRepository.ChangePoints(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrCustomerNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangePoints")
	}
	return response, err
}

func (q *pointService) FindPointEntries(ctx context.Context, branchID, customerID uuid.UUID, limit, page int64) ([]*models.PointEntry, int64, error) {
	ctxt := "PointService-FindPointEntries"
	response, total, err := q.pointRepository.FindPointEntries(ctx, branchID, customerID, limit, page)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPointEntries")
	}
	return response, total, err
}

func (q *pointService) FindCustomerCodes(ctx context.Context, branchID uuid.UUID, customerIDs []uuid.UUID) ([]*models.CustomerCode, error) {
	ctxt := "PointService-FindCustomerCodes"
	response, err := q.pointRepository.FindCustomerCodes(ctx, branchID, customerIDs)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomerCodes")
	}
	return response, err
}

func (q *pointService) ImportPoints(ctx context.Context, tx pgx.Tx, request *models.PointImport) error {
	ctxt := "PointService-ImportPoints"
	customer, err := q.customerRepository.FindCustomer(ctx, tx, models.NewCustomerFilter().WithBranchIDs(request.BranchID).WithCode(request.Code))
	if err != nil {
		if !errors.Is(err, models.ErrCustomerNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomer")
		}
		return err
	}
	points := request.Points
	if request.Set {
		points -= customer.Points
	}
	if _, err = q.pointRepository.ChangePoints(ctx, tx, &models.PointChange{
		BranchID:   request.BranchID,
		CustomerID: customer.ID,
		Points:     points,
		Type:       models.PointTypeAdjusted,
		Reference:  "import",
		At:         request.At,
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangePoints")
	}
	return err
}
