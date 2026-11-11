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

type SupplierService interface {
	FindSuppliers(
		ctx context.Context,
		request *models.SupplierFilter,
	) ([]*models.Supplier, int64, error)
	FindSupplier(
		ctx context.Context,
		tx pgx.Tx,
		request *models.SupplierFilter,
	) (*models.Supplier, error)
	CreateSupplier(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Supplier,
	) (*models.Supplier, error)
	UpdateSupplier(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Supplier,
	) (*models.Supplier, error)
	DeleteSuppliers(
		ctx context.Context,
		tx pgx.Tx,
		request *models.SupplierFilter,
	) error
}

type supplierService struct {
	supplierRepository repositories.SupplierRepository
}

func NewSupplierService(
	supplierRepository repositories.SupplierRepository,
) SupplierService {
	return &supplierService{
		supplierRepository: supplierRepository,
	}
}

func (q *supplierService) FindSuppliers(
	ctx context.Context,
	request *models.SupplierFilter,
) ([]*models.Supplier, int64, error) {
	ctxt := "SupplierService-FindSuppliers"
	response, total, err := q.supplierRepository.FindSuppliers(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSuppliers")
	}
	return response, total, err
}

func (q *supplierService) FindSupplier(
	ctx context.Context,
	tx pgx.Tx,
	request *models.SupplierFilter,
) (*models.Supplier, error) {
	ctxt := "SupplierService-FindSupplier"
	response, err := q.supplierRepository.FindSupplier(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrSupplierNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSupplier")
	}
	return response, err
}

func (q *supplierService) CreateSupplier(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Supplier,
) (*models.Supplier, error) {
	ctxt := "SupplierService-CreateSupplier"
	response, err := q.supplierRepository.CreateSupplier(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateSupplier")
	}
	return response, err
}

func (q *supplierService) UpdateSupplier(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Supplier,
) (*models.Supplier, error) {
	ctxt := "SupplierService-UpdateSupplier"
	response, err := q.supplierRepository.UpdateSupplier(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateSupplier")
	}
	return response, err
}

// DeleteSuppliers deletes every supplier of the filter or none: a missing id is ErrSupplierNotFound.
func (q *supplierService) DeleteSuppliers(
	ctx context.Context,
	tx pgx.Tx,
	request *models.SupplierFilter,
) error {
	ctxt := "SupplierService-DeleteSuppliers"
	deleted, err := q.supplierRepository.DeleteSuppliers(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteSuppliers")
		return err
	}
	if deleted != int64(len(request.SupplierIDs)) {
		return models.ErrSupplierNotFound
	}
	return nil
}
