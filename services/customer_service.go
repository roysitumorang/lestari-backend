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

type CustomerService interface {
	FindCustomers(
		ctx context.Context,
		request *models.CustomerFilter,
	) ([]*models.Customer, int64, error)
	FindCustomer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerFilter,
	) (*models.Customer, error)
	CreateCustomer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Customer,
	) (*models.Customer, error)
	UpdateCustomer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Customer,
	) (*models.Customer, error)
	DeleteCustomers(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CustomerFilter,
	) error
}

type customerService struct {
	customerRepository repositories.CustomerRepository
}

func NewCustomerService(
	customerRepository repositories.CustomerRepository,
) CustomerService {
	return &customerService{
		customerRepository: customerRepository,
	}
}

func (q *customerService) FindCustomers(
	ctx context.Context,
	request *models.CustomerFilter,
) ([]*models.Customer, int64, error) {
	ctxt := "CustomerService-FindCustomers"
	response, total, err := q.customerRepository.FindCustomers(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomers")
	}
	return response, total, err
}

func (q *customerService) FindCustomer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerFilter,
) (*models.Customer, error) {
	ctxt := "CustomerService-FindCustomer"
	response, err := q.customerRepository.FindCustomer(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrCustomerNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomer")
	}
	return response, err
}

// checkReferences makes sure every related row belongs to the same branch.
func (q *customerService) checkReferences(ctx context.Context, tx pgx.Tx, request *models.Customer) error {
	v := &helper.Validation{}
	if request.CustomerTypeID != nil {
		exists, err := repositories.ReferenceExists(ctx, tx, "customer_types", "branch_id", *request.CustomerTypeID, request.BranchID)
		if err != nil {
			return err
		}
		if !exists {
			v.Add("customer_type_id", "Not found.")
		}
	}
	return v.Err()
}

func (q *customerService) CreateCustomer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Customer,
) (*models.Customer, error) {
	ctxt := "CustomerService-CreateCustomer"
	if err := q.checkReferences(ctx, tx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCheckReferences")
		return nil, err
	}
	response, err := q.customerRepository.CreateCustomer(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateCustomer")
	}
	return response, err
}

func (q *customerService) UpdateCustomer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Customer,
) (*models.Customer, error) {
	ctxt := "CustomerService-UpdateCustomer"
	if err := q.checkReferences(ctx, tx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCheckReferences")
		return nil, err
	}
	response, err := q.customerRepository.UpdateCustomer(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateCustomer")
	}
	return response, err
}

// DeleteCustomers deletes every customer of the filter or none: a missing id is ErrCustomerNotFound.
func (q *customerService) DeleteCustomers(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CustomerFilter,
) error {
	ctxt := "CustomerService-DeleteCustomers"
	deleted, err := q.customerRepository.DeleteCustomers(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCustomers")
		return err
	}
	if deleted != int64(len(request.CustomerIDs)) {
		return models.ErrCustomerNotFound
	}
	return nil
}
