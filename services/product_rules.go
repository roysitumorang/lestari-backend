package services

import (
	"context"
	"log/slog"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

const openingStock = "Opening stock"

// validate applies the rules of a product that need more than one attribute: a new product without a SKU gets
// the next automatic one, and a service never tracks stock.
func (q *productService) validate(ctx context.Context, tx pgx.Tx, request *models.Product, create bool) error {
	ctxt := "ProductService-validate"
	if create && request.SKU == "" {
		sku, err := q.productRepository.NextSKU(ctx, tx, request.BranchID)
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextSKU")
			return err
		}
		request.SKU = sku
	}
	if request.ItemType == models.ProductItemTypeService {
		request.TrackStock = false
	}
	return nil
}

// afterCreate records the opening stock of a new product as its first stock movement.
func (q *productService) afterCreate(ctx context.Context, tx pgx.Tx, request, response *models.Product) error {
	ctxt := "ProductService-afterCreate"
	if !response.TrackStock || response.Stock == 0 {
		return nil
	}
	note := openingStock
	err := q.stockRepository.CreateStockMovement(ctx, tx, &models.StockMovement{
		BranchID:       response.BranchID,
		ProductID:      response.ID,
		Actor:          request.Actor,
		CreatedAt:      request.CreatedAt,
		Type:           models.StockMovementTypeAdjustment,
		Reference:      openingStock,
		QuantityChange: response.Stock,
		StockAfter:     response.Stock,
		UnitCost:       &response.PurchasePrice,
		ActorName:      request.Actor.Name,
		Note:           &note,
	})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStockMovement")
	}
	return err
}

func (q *productService) NextSKU(
	ctx context.Context,
	branchID uuid.UUID,
) (string, error) {
	ctxt := "ProductService-NextSKU"
	response, err := q.productRepository.NextSKU(ctx, nil, branchID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextSKU")
	}
	return response, err
}

func (q *productService) SKUAvailable(
	ctx context.Context,
	branchID uuid.UUID,
	sku string,
	exceptProductID *uuid.UUID,
) (bool, error) {
	ctxt := "ProductService-SKUAvailable"
	exists, err := q.productRepository.SKUExists(ctx, branchID, sku, exceptProductID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSKUExists")
	}
	return !exists, err
}
