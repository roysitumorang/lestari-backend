package services

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type StockOpnameService interface {
	FindStockOpnames(
		ctx context.Context,
		request *models.StockOpnameFilter,
	) ([]*models.StockOpname, int64, error)
	// FindStockOpname returns one stock opname with its items.
	FindStockOpname(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockOpnameFilter,
	) (*models.StockOpname, error)
	CreateStockOpname(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockOpname,
	) (*models.StockOpname, error)
	// DecideStockOpname approves (stock becomes the counted stock) or rejects a pending stock opname.
	DecideStockOpname(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockOpnameDecision,
	) error
	DeleteStockOpnames(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockOpnameFilter,
	) error
}

type stockOpnameService struct {
	stockOpnameRepository repositories.StockOpnameRepository
	productRepository     repositories.ProductRepository
	stockRepository       repositories.StockRepository
}

func NewStockOpnameService(
	stockOpnameRepository repositories.StockOpnameRepository,
	productRepository repositories.ProductRepository,
	stockRepository repositories.StockRepository,
) StockOpnameService {
	return &stockOpnameService{
		stockOpnameRepository: stockOpnameRepository,
		productRepository:     productRepository,
		stockRepository:       stockRepository,
	}
}

func (q *stockOpnameService) FindStockOpnames(
	ctx context.Context,
	request *models.StockOpnameFilter,
) ([]*models.StockOpname, int64, error) {
	ctxt := "StockOpnameService-FindStockOpnames"
	response, total, err := q.stockOpnameRepository.FindStockOpnames(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockOpnames")
	}
	return response, total, err
}

func (q *stockOpnameService) FindStockOpname(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockOpnameFilter,
) (*models.StockOpname, error) {
	ctxt := "StockOpnameService-FindStockOpname"
	response, err := q.stockOpnameRepository.FindStockOpname(ctx, tx, request)
	if err != nil {
		if !errors.Is(err, models.ErrStockOpnameNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockOpname")
		}
		return nil, err
	}
	if response.Items, err = q.stockOpnameRepository.FindStockOpnameItems(ctx, tx, response.ID); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockOpnameItems")
		return nil, err
	}
	return response, nil
}

// CreateStockOpname numbers the opname and stores its items with the name, stock and cost each product has now.
func (q *stockOpnameService) CreateStockOpname(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockOpname,
) (*models.StockOpname, error) {
	ctxt := "StockOpnameService-CreateStockOpname"
	v := &helper.Validation{}
	for _, item := range request.Items {
		product, err := q.productRepository.FindProduct(
			ctx,
			tx,
			models.NewProductFilter().
				WithBranchIDs(request.BranchID).
				WithProductIDs(*item.ProductID),
		)
		if errors.Is(err, models.ErrProductNotFound) {
			v.Add("items", "Row {row}: product not found.", "row", item.Row)
			continue
		}
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProduct")
			return nil, err
		}
		item.ProductName = product.Name
		item.SystemStock = product.Stock
		item.UnitCost = product.PurchasePrice
	}
	if err := v.Err(); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	var err error
	if request.Code, err = q.stockOpnameRepository.NextStockOpnameCode(
		ctx,
		tx,
		request.BranchID,
		request.CreatedAt.In(helper.Location).Format("20060102"),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextStockOpnameCode")
		return nil, err
	}
	response, err := q.stockOpnameRepository.CreateStockOpname(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStockOpname")
		return nil, err
	}
	if err = q.stockOpnameRepository.CreateStockOpnameItems(ctx, tx, response.ID, request.Items); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStockOpnameItems")
		return nil, err
	}
	return q.FindStockOpname(ctx, tx, models.NewStockOpnameFilter().WithStockOpnameIDs(response.ID))
}

func (q *stockOpnameService) DecideStockOpname(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockOpnameDecision,
) error {
	ctxt := "StockOpnameService-DecideStockOpname"
	stockOpname, err := q.stockOpnameRepository.LockStockOpname(ctx, tx, request.BranchID, request.StockOpnameID)
	if err != nil {
		if !errors.Is(err, models.ErrStockOpnameNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockStockOpname")
		}
		return err
	}
	if stockOpname.Status != models.StockOpnameStatusPending {
		if request.Status == models.StockOpnameStatusRejected {
			return helper.Conflict("Only a pending stock opname can be rejected.")
		}
		return helper.Conflict("This stock opname was already {status}.", "status", i18n.Tr(stockOpname.Status))
	}
	if request.Status == models.StockOpnameStatusApproved {
		items, err := q.stockOpnameRepository.FindStockOpnameItems(ctx, tx, request.StockOpnameID)
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockOpnameItems")
			return err
		}
		for _, item := range items {
			// a product deleted since the count has nothing to correct
			if item.ProductID == nil || item.PhysicalStock == item.CurrentStock {
				continue
			}
			if _, err = q.stockRepository.ChangeStock(ctx, tx, &models.StockChange{
				BranchID:  request.BranchID,
				ProductID: *item.ProductID,
				Change:    item.PhysicalStock - item.CurrentStock,
				Type:      models.StockMovementTypeOpname,
				Reference: stockOpname.Code,
				Actor:     request.Actor,
				Note:      "Stock opname",
				At:        request.At,
			}); err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeStock")
				return err
			}
		}
	}
	if err = q.stockOpnameRepository.DecideStockOpname(ctx, tx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecideStockOpname")
	}
	return err
}

// DeleteStockOpnames deletes every stock opname of the filter or none: a missing id is ErrStockOpnameNotFound and
// an approved one, which has already changed stock, is ErrStockOpnameApproved.
func (q *stockOpnameService) DeleteStockOpnames(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockOpnameFilter,
) error {
	ctxt := "StockOpnameService-DeleteStockOpnames"
	for _, stockOpnameID := range request.StockOpnameIDs {
		for _, branchID := range request.BranchIDs {
			stockOpname, err := q.stockOpnameRepository.LockStockOpname(ctx, tx, branchID, stockOpnameID)
			if err != nil {
				if !errors.Is(err, models.ErrStockOpnameNotFound) {
					helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockStockOpname")
				}
				return err
			}
			if stockOpname.Status == models.StockOpnameStatusApproved {
				return models.ErrStockOpnameApproved
			}
		}
	}
	deleted, err := q.stockOpnameRepository.DeleteStockOpnames(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteStockOpnames")
		return err
	}
	if deleted != int64(len(request.StockOpnameIDs)) {
		return models.ErrStockOpnameNotFound
	}
	return nil
}
