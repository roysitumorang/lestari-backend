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

type CommissionService interface {
	FindCommissions(
		ctx context.Context,
		request *models.CommissionFilter,
	) ([]*models.Commission, int64, error)
	FindCommission(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CommissionFilter,
	) (*models.Commission, error)
	CreateCommission(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Commission,
	) (*models.Commission, error)
	UpdateCommission(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Commission,
	) (*models.Commission, error)
	DeleteCommissions(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CommissionFilter,
	) error
}

type commissionService struct {
	commissionRepository repositories.CommissionRepository
}

func NewCommissionService(
	commissionRepository repositories.CommissionRepository,
) CommissionService {
	return &commissionService{
		commissionRepository: commissionRepository,
	}
}

func (q *commissionService) FindCommissions(
	ctx context.Context,
	request *models.CommissionFilter,
) ([]*models.Commission, int64, error) {
	ctxt := "CommissionService-FindCommissions"
	response, total, err := q.commissionRepository.FindCommissions(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCommissions")
	}
	return response, total, err
}

func (q *commissionService) FindCommission(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CommissionFilter,
) (*models.Commission, error) {
	ctxt := "CommissionService-FindCommission"
	response, err := q.commissionRepository.FindCommission(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrCommissionNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCommission")
	}
	return response, err
}

func (q *commissionService) CreateCommission(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Commission,
) (*models.Commission, error) {
	ctxt := "CommissionService-CreateCommission"
	if err := q.validate(ctx, tx, request, true); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.commissionRepository.CreateCommission(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateCommission")
	}
	return response, err
}

func (q *commissionService) UpdateCommission(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Commission,
) (*models.Commission, error) {
	ctxt := "CommissionService-UpdateCommission"
	if err := q.validate(ctx, tx, request, false); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.commissionRepository.UpdateCommission(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateCommission")
	}
	return response, err
}

// DeleteCommissions deletes every commission rule of the filter or none: a missing id is ErrCommissionNotFound.
func (q *commissionService) DeleteCommissions(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CommissionFilter,
) error {
	ctxt := "CommissionService-DeleteCommissions"
	deleted, err := q.commissionRepository.DeleteCommissions(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCommissions")
		return err
	}
	if deleted != int64(len(request.CommissionIDs)) {
		return models.ErrCommissionNotFound
	}
	return nil
}
