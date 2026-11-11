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

type FeeService interface {
	FindFees(
		ctx context.Context,
		request *models.FeeFilter,
	) ([]*models.Fee, int64, error)
	FindFee(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FeeFilter,
	) (*models.Fee, error)
	CreateFee(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Fee,
	) (*models.Fee, error)
	UpdateFee(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Fee,
	) (*models.Fee, error)
	DeleteFees(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FeeFilter,
	) error
}

type feeService struct {
	feeRepository repositories.FeeRepository
}

func NewFeeService(
	feeRepository repositories.FeeRepository,
) FeeService {
	return &feeService{
		feeRepository: feeRepository,
	}
}

func (q *feeService) FindFees(
	ctx context.Context,
	request *models.FeeFilter,
) ([]*models.Fee, int64, error) {
	ctxt := "FeeService-FindFees"
	response, total, err := q.feeRepository.FindFees(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFees")
	}
	return response, total, err
}

func (q *feeService) FindFee(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FeeFilter,
) (*models.Fee, error) {
	ctxt := "FeeService-FindFee"
	response, err := q.feeRepository.FindFee(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrFeeNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFee")
	}
	return response, err
}

func (q *feeService) CreateFee(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Fee,
) (*models.Fee, error) {
	ctxt := "FeeService-CreateFee"
	if err := q.validate(ctx, tx, request, true); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.feeRepository.CreateFee(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateFee")
	}
	return response, err
}

func (q *feeService) UpdateFee(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Fee,
) (*models.Fee, error) {
	ctxt := "FeeService-UpdateFee"
	if err := q.validate(ctx, tx, request, false); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.feeRepository.UpdateFee(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateFee")
	}
	return response, err
}

// DeleteFees deletes every fee of the filter or none: a missing id is ErrFeeNotFound.
func (q *feeService) DeleteFees(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FeeFilter,
) error {
	ctxt := "FeeService-DeleteFees"
	deleted, err := q.feeRepository.DeleteFees(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteFees")
		return err
	}
	if deleted != int64(len(request.FeeIDs)) {
		return models.ErrFeeNotFound
	}
	return nil
}
