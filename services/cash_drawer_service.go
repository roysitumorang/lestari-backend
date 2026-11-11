package services

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type CashDrawerService interface {
	FindCashDrawers(
		ctx context.Context,
		request *models.CashDrawerFilter,
	) ([]*models.CashDrawer, int64, error)
	FindCashDrawer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CashDrawerFilter,
	) (*models.CashDrawer, error)
	CreateCashDrawer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CashDrawer,
	) (*models.CashDrawer, error)
	UpdateCashDrawer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CashDrawer,
	) (*models.CashDrawer, error)
	DeleteCashDrawers(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CashDrawerFilter,
	) error
}

type cashDrawerService struct {
	cashDrawerRepository repositories.CashDrawerRepository
}

func NewCashDrawerService(
	cashDrawerRepository repositories.CashDrawerRepository,
) CashDrawerService {
	return &cashDrawerService{
		cashDrawerRepository: cashDrawerRepository,
	}
}

func (q *cashDrawerService) FindCashDrawers(
	ctx context.Context,
	request *models.CashDrawerFilter,
) ([]*models.CashDrawer, int64, error) {
	ctxt := "CashDrawerService-FindCashDrawers"
	response, total, err := q.cashDrawerRepository.FindCashDrawers(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCashDrawers")
	}
	return response, total, err
}

func (q *cashDrawerService) FindCashDrawer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CashDrawerFilter,
) (*models.CashDrawer, error) {
	ctxt := "CashDrawerService-FindCashDrawer"
	response, err := q.cashDrawerRepository.FindCashDrawer(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrCashDrawerNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCashDrawer")
	}
	return response, err
}

func (q *cashDrawerService) CreateCashDrawer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CashDrawer,
) (*models.CashDrawer, error) {
	ctxt := "CashDrawerService-CreateCashDrawer"
	response, err := q.cashDrawerRepository.CreateCashDrawer(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateCashDrawer")
	}
	return response, err
}

func (q *cashDrawerService) UpdateCashDrawer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CashDrawer,
) (*models.CashDrawer, error) {
	ctxt := "CashDrawerService-UpdateCashDrawer"
	response, err := q.cashDrawerRepository.UpdateCashDrawer(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateCashDrawer")
	}
	return response, err
}

// DeleteCashDrawers deletes every cash drawer of the filter or none: a missing id is ErrCashDrawerNotFound.
func (q *cashDrawerService) DeleteCashDrawers(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CashDrawerFilter,
) error {
	ctxt := "CashDrawerService-DeleteCashDrawers"
	deleted, err := q.cashDrawerRepository.DeleteCashDrawers(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCashDrawers")
		return err
	}
	if deleted != int64(len(request.CashDrawerIDs)) {
		return models.ErrCashDrawerNotFound
	}
	return nil
}
