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

type CustomerTypeService interface {
	FindCustomerTypes(
		ctx context.Context,
		request *models.CustomerTypeFilter,
	) ([]*models.CustomerType, int64, error)
	FindCustomerType(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerTypeFilter,
	) (*models.CustomerType, error)
	CreateCustomerType(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerType,
	) (*models.CustomerType, error)
	UpdateCustomerType(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerType,
	) (*models.CustomerType, error)
	DeleteCustomerTypes(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerTypeFilter,
	) error
}

type customerTypeService struct {
	customerTypeRepository repositories.CustomerTypeRepository
}

func NewCustomerTypeService(
	customerTypeRepository repositories.CustomerTypeRepository,
) CustomerTypeService {
	return &customerTypeService{
		customerTypeRepository: customerTypeRepository,
	}
}

func (q *customerTypeService) FindCustomerTypes(
	ctx context.Context,
	request *models.CustomerTypeFilter,
) ([]*models.CustomerType, int64, error) {
	ctxt := "CustomerTypeService-FindCustomerTypes"
	response, total, err := q.customerTypeRepository.FindCustomerTypes(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomerTypes")
	}
	return response, total, err
}

func (q *customerTypeService) FindCustomerType(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerTypeFilter,
) (*models.CustomerType, error) {
	ctxt := "CustomerTypeService-FindCustomerType"
	response, err := q.customerTypeRepository.FindCustomerType(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrCustomerTypeNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomerType")
	}
	return response, err
}

func (q *customerTypeService) CreateCustomerType(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerType,
) (*models.CustomerType, error) {
	ctxt := "CustomerTypeService-CreateCustomerType"
	response, err := q.customerTypeRepository.CreateCustomerType(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateCustomerType")
	}
	return response, err
}

func (q *customerTypeService) UpdateCustomerType(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerType,
) (*models.CustomerType, error) {
	ctxt := "CustomerTypeService-UpdateCustomerType"
	response, err := q.customerTypeRepository.UpdateCustomerType(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateCustomerType")
	}
	return response, err
}

// DeleteCustomerTypes deletes every customer type of the filter or none: a missing id is ErrCustomerTypeNotFound.
func (q *customerTypeService) DeleteCustomerTypes(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerTypeFilter,
) error {
	ctxt := "CustomerTypeService-DeleteCustomerTypes"
	deleted, err := q.customerTypeRepository.DeleteCustomerTypes(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCustomerTypes")
		return err
	}
	if deleted != int64(len(request.CustomerTypeIDs)) {
		return models.ErrCustomerTypeNotFound
	}
	return nil
}
