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

type BranchProductService interface {
	FindBranchProducts(
		ctx context.Context,
		request *models.BranchProductFilter,
	) ([]*models.BranchProduct, int64, error)
	FindBranchProduct(
		ctx context.Context,
		tx pgx.Tx,
		request *models.BranchProductFilter,
	) (*models.BranchProduct, error)
	UpdateBranchProduct(
		ctx context.Context,
		tx pgx.Tx,
		request *models.BranchProduct,
	) (*models.BranchProduct, error)
	// SyncPrices copies the selling prices of the head office to branches and returns how many products changed.
	SyncPrices(
		ctx context.Context,
		tx pgx.Tx,
		request *models.BranchPriceSync,
	) (int64, error)
}

type branchProductService struct {
	branchProductRepository repositories.BranchProductRepository
}

func NewBranchProductService(
	branchProductRepository repositories.BranchProductRepository,
) BranchProductService {
	return &branchProductService{
		branchProductRepository: branchProductRepository,
	}
}

func (q *branchProductService) FindBranchProducts(
	ctx context.Context,
	request *models.BranchProductFilter,
) ([]*models.BranchProduct, int64, error) {
	ctxt := "BranchProductService-FindBranchProducts"
	response, total, err := q.branchProductRepository.FindBranchProducts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranchProducts")
	}
	return response, total, err
}

func (q *branchProductService) FindBranchProduct(
	ctx context.Context,
	tx pgx.Tx,
	request *models.BranchProductFilter,
) (*models.BranchProduct, error) {
	ctxt := "BranchProductService-FindBranchProduct"
	response, err := q.branchProductRepository.FindBranchProduct(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrBranchProductNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranchProduct")
	}
	return response, err
}

func (q *branchProductService) UpdateBranchProduct(
	ctx context.Context,
	tx pgx.Tx,
	request *models.BranchProduct,
) (*models.BranchProduct, error) {
	ctxt := "BranchProductService-UpdateBranchProduct"
	response, err := q.branchProductRepository.UpdateBranchProduct(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateBranchProduct")
	}
	return response, err
}

func (q *branchProductService) SyncPrices(
	ctx context.Context,
	tx pgx.Tx,
	request *models.BranchPriceSync,
) (int64, error) {
	ctxt := "BranchProductService-SyncPrices"
	response, err := q.branchProductRepository.SyncPrices(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSyncPrices")
	}
	return response, err
}
