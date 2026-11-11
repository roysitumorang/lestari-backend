package services

import (
	"context"
	"errors"
	"log/slog"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type ProductService interface {
	FindProducts(
		ctx context.Context,
		request *models.ProductFilter,
	) ([]*models.Product, int64, error)
	FindProduct(
		ctx context.Context,
		tx pgx.Tx,
		request *models.ProductFilter,
	) (*models.Product, error)
	CreateProduct(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Product,
	) (*models.Product, error)
	UpdateProduct(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Product,
	) (*models.Product, error)
	DeleteProducts(
		ctx context.Context,
		tx pgx.Tx,
		request *models.ProductFilter,
		deletedAt time.Time,
	) error
	// NextSKU is the SKU a new product gets when none is sent.
	NextSKU(
		ctx context.Context,
		branchID uuid.UUID,
	) (string, error)
	// SKUAvailable tells whether no other product of the branch (other than exceptProductID) uses the SKU.
	SKUAvailable(
		ctx context.Context,
		branchID uuid.UUID,
		sku string,
		exceptProductID *uuid.UUID,
	) (bool, error)
}

type productService struct {
	productRepository repositories.ProductRepository
	stockRepository   repositories.StockRepository
}

func NewProductService(
	productRepository repositories.ProductRepository,
	stockRepository repositories.StockRepository,
) ProductService {
	return &productService{
		productRepository: productRepository,
		stockRepository:   stockRepository,
	}
}

func (q *productService) FindProducts(
	ctx context.Context,
	request *models.ProductFilter,
) ([]*models.Product, int64, error) {
	ctxt := "ProductService-FindProducts"
	response, total, err := q.productRepository.FindProducts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProducts")
	}
	return response, total, err
}

func (q *productService) FindProduct(
	ctx context.Context,
	tx pgx.Tx,
	request *models.ProductFilter,
) (*models.Product, error) {
	ctxt := "ProductService-FindProduct"
	response, err := q.productRepository.FindProduct(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrProductNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProduct")
	}
	return response, err
}

// checkReferences makes sure every related row belongs to the same branch.
func (q *productService) checkReferences(ctx context.Context, tx pgx.Tx, request *models.Product) error {
	v := &helper.Validation{}
	if request.CategoryID != nil {
		exists, err := repositories.ReferenceExists(ctx, tx, "categories", "branch_id", *request.CategoryID, request.BranchID)
		if err != nil {
			return err
		}
		if !exists {
			v.Add("category_id", "Not found.")
		}
	}
	if request.TaxID != nil {
		exists, err := repositories.ReferenceExists(ctx, tx, "taxes", "branch_id", *request.TaxID, request.BranchID)
		if err != nil {
			return err
		}
		if !exists {
			v.Add("tax_id", "Not found.")
		}
	}
	return v.Err()
}

func (q *productService) CreateProduct(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Product,
) (*models.Product, error) {
	ctxt := "ProductService-CreateProduct"
	if err := q.checkReferences(ctx, tx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCheckReferences")
		return nil, err
	}
	if err := q.validate(ctx, tx, request, true); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.productRepository.CreateProduct(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateProduct")
		return nil, err
	}
	if err = q.afterCreate(ctx, tx, request, response); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrAfterCreate")
		return nil, err
	}
	return response, err
}

func (q *productService) UpdateProduct(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Product,
) (*models.Product, error) {
	ctxt := "ProductService-UpdateProduct"
	if err := q.checkReferences(ctx, tx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCheckReferences")
		return nil, err
	}
	if err := q.validate(ctx, tx, request, false); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.productRepository.UpdateProduct(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateProduct")
	}
	return response, err
}

// DeleteProducts deletes every product of the filter or none: a missing id is ErrProductNotFound.
func (q *productService) DeleteProducts(
	ctx context.Context,
	tx pgx.Tx,
	request *models.ProductFilter,
	deletedAt time.Time,
) error {
	ctxt := "ProductService-DeleteProducts"
	deleted, err := q.productRepository.DeleteProducts(ctx, tx, request, deletedAt)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteProducts")
		return err
	}
	if deleted != int64(len(request.ProductIDs)) {
		return models.ErrProductNotFound
	}
	return nil
}
