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

type TaxService interface {
	FindTaxes(
		ctx context.Context,
		request *models.TaxFilter,
	) ([]*models.Tax, int64, error)
	FindTax(
		ctx context.Context,
		tx pgx.Tx,
		request *models.TaxFilter,
	) (*models.Tax, error)
	CreateTax(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Tax,
	) (*models.Tax, error)
	UpdateTax(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Tax,
	) (*models.Tax, error)
	DeleteTaxes(
		ctx context.Context,
		tx pgx.Tx,
		request *models.TaxFilter,
	) error
}

type taxService struct {
	taxRepository repositories.TaxRepository
}

func NewTaxService(
	taxRepository repositories.TaxRepository,
) TaxService {
	return &taxService{
		taxRepository: taxRepository,
	}
}

func (q *taxService) FindTaxes(
	ctx context.Context,
	request *models.TaxFilter,
) ([]*models.Tax, int64, error) {
	ctxt := "TaxService-FindTaxes"
	response, total, err := q.taxRepository.FindTaxes(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindTaxes")
	}
	return response, total, err
}

func (q *taxService) FindTax(
	ctx context.Context,
	tx pgx.Tx,
	request *models.TaxFilter,
) (*models.Tax, error) {
	ctxt := "TaxService-FindTax"
	response, err := q.taxRepository.FindTax(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrTaxNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindTax")
	}
	return response, err
}

func (q *taxService) CreateTax(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Tax,
) (*models.Tax, error) {
	ctxt := "TaxService-CreateTax"
	response, err := q.taxRepository.CreateTax(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateTax")
	}
	return response, err
}

func (q *taxService) UpdateTax(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Tax,
) (*models.Tax, error) {
	ctxt := "TaxService-UpdateTax"
	response, err := q.taxRepository.UpdateTax(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateTax")
	}
	return response, err
}

// DeleteTaxes deletes every tax of the filter or none: a missing id is ErrTaxNotFound.
func (q *taxService) DeleteTaxes(
	ctx context.Context,
	tx pgx.Tx,
	request *models.TaxFilter,
) error {
	ctxt := "TaxService-DeleteTaxes"
	deleted, err := q.taxRepository.DeleteTaxes(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteTaxes")
		return err
	}
	if deleted != int64(len(request.TaxIDs)) {
		return models.ErrTaxNotFound
	}
	return nil
}
