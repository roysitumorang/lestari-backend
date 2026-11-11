package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

const (
	stockReferenceAdjustment = "Manual adjustment"
	stockReferenceImport     = "Stock import"
)

type StockService interface {
	FindStocks(
		ctx context.Context,
		request *models.StockFilter,
	) ([]*models.Stock, int64, error)
	FindStock(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockFilter,
	) (*models.Stock, error)
	// ChangeStock adds to (or, with a negative change, takes from) the stock of a product and records the movement.
	ChangeStock(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockChange,
	) (float64, error)
	// AdjustStock sets, adds to or subtracts from the stock of one product.
	AdjustStock(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockAdjustment,
	) error
	// ImportStock makes the stock of the product with the row's SKU equal to the row's stock.
	ImportStock(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockImport,
	) error
	FindStockMovements(
		ctx context.Context,
		request *models.StockMovementFilter,
	) ([]*models.StockMovement, int64, error)
	// RecalculateStocks rebuilds the stock of a branch from its movements in a background job.
	RecalculateStocks(
		ctx context.Context,
		storeID,
		branchID uuid.UUID,
		at time.Time,
	) (*models.JobResponse, error)
}

type stockService struct {
	stockRepository   repositories.StockRepository
	productRepository repositories.ProductRepository
	jobService        JobService
}

func NewStockService(
	stockRepository repositories.StockRepository,
	productRepository repositories.ProductRepository,
	jobService JobService,
) StockService {
	return &stockService{
		stockRepository:   stockRepository,
		productRepository: productRepository,
		jobService:        jobService,
	}
}

func (q *stockService) FindStocks(
	ctx context.Context,
	request *models.StockFilter,
) ([]*models.Stock, int64, error) {
	ctxt := "StockService-FindStocks"
	response, total, err := q.stockRepository.FindStocks(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStocks")
	}
	return response, total, err
}

func (q *stockService) FindStock(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockFilter,
) (*models.Stock, error) {
	ctxt := "StockService-FindStock"
	response, err := q.stockRepository.FindStock(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrStockNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStock")
	}
	return response, err
}

func (q *stockService) ChangeStock(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockChange,
) (float64, error) {
	ctxt := "StockService-ChangeStock"
	response, err := q.stockRepository.ChangeStock(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrProductNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeStock")
	}
	return response, err
}

func (q *stockService) AdjustStock(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockAdjustment,
) error {
	ctxt := "StockService-AdjustStock"
	current, err := q.stockRepository.LockStock(ctx, tx, request.BranchID, request.ProductID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockStock")
		return err
	}
	change := request.Quantity
	switch request.Mode {
	case models.StockAdjustmentModeSet:
		change = request.Quantity - current
	case models.StockAdjustmentModeSubtract:
		change = -request.Quantity
	}
	if change == 0 {
		return nil
	}
	// only received units carry a cost
	var unitCost *float64
	if request.Mode == models.StockAdjustmentModeAdd {
		unitCost = request.UnitCost
	}
	note := strings.TrimSpace(request.Note)
	if request.Reason != "" {
		note = strings.TrimSpace(request.Reason + ": " + note)
	}
	if _, err = q.stockRepository.ChangeStock(ctx, tx, &models.StockChange{
		BranchID:  request.BranchID,
		ProductID: request.ProductID,
		Change:    change,
		Type:      models.StockMovementTypeAdjustment,
		Reference: stockReferenceAdjustment,
		UnitCost:  unitCost,
		Actor:     request.Actor,
		Note:      note,
		At:        request.At,
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeStock")
		return err
	}
	if unitCost == nil || change <= 0 {
		return nil
	}
	if err = q.stockRepository.AverageUnitCost(ctx, tx, request.ProductID, change, *unitCost); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrAverageUnitCost")
	}
	return err
}

func (q *stockService) ImportStock(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockImport,
) error {
	ctxt := "StockService-ImportStock"
	product, err := q.productRepository.FindProduct(
		ctx,
		tx,
		models.NewProductFilter().
			WithBranchIDs(request.BranchID).
			WithSKU(request.SKU),
	)
	if err != nil {
		if !errors.Is(err, models.ErrProductNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProduct")
		}
		return err
	}
	if request.UnitCost != nil {
		if err = q.stockRepository.SetUnitCost(ctx, tx, product.ID, *request.UnitCost); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSetUnitCost")
			return err
		}
	}
	if *request.Stock == product.Stock {
		return nil
	}
	if _, err = q.stockRepository.ChangeStock(ctx, tx, &models.StockChange{
		BranchID:  request.BranchID,
		ProductID: product.ID,
		Change:    *request.Stock - product.Stock,
		Type:      models.StockMovementTypeAdjustment,
		Reference: stockReferenceImport,
		UnitCost:  request.UnitCost,
		Actor:     request.Actor,
		Note:      request.Note,
		At:        request.At,
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeStock")
	}
	return err
}

func (q *stockService) FindStockMovements(
	ctx context.Context,
	request *models.StockMovementFilter,
) ([]*models.StockMovement, int64, error) {
	ctxt := "StockService-FindStockMovements"
	response, total, err := q.stockRepository.FindStockMovements(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockMovements")
	}
	return response, total, err
}

func (q *stockService) RecalculateStocks(
	ctx context.Context,
	storeID,
	branchID uuid.UUID,
	at time.Time,
) (*models.JobResponse, error) {
	ctxt := "StockService-RecalculateStocks"
	response, err := q.jobService.StartJob(
		ctx,
		&models.Job{
			StoreID:   storeID,
			Kind:      models.JobKindStockRecalculate,
			Payload:   []byte(helper.Sprintf(`{"branch_id":%q}`, branchID.String())),
			CreatedAt: at,
		},
		func(ctx context.Context) (any, error) {
			updated, err := q.stockRepository.RecalculateStocks(ctx, branchID, time.Now())
			return map[string]int64{"updated": updated}, err
		},
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrStartJob")
	}
	return response, err
}
