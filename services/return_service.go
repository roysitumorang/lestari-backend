package services

import (
	"context"
	"errors"
	"log/slog"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type ReturnService interface {
	FindReturns(ctx context.Context, request *models.ReturnFilter) ([]*models.Return, int64, error)
	FindReturn(ctx context.Context, tx pgx.Tx, branchID, returnID uuid.UUID, kind string) (*models.Return, error)
	// CreateReturn records goods coming back from a customer (the stock goes up) or going back to a supplier
	// (the stock goes down, and the payable too when asked).
	CreateReturn(ctx context.Context, tx pgx.Tx, request *models.ReturnCreate) (*models.Return, error)
}

type returnService struct {
	returnRepository repositories.ReturnRepository
	saleRepository   repositories.SaleRepository
	stockRepository  repositories.StockRepository
	debtRepository   repositories.DebtRepository
}

func NewReturnService(
	returnRepository repositories.ReturnRepository,
	saleRepository repositories.SaleRepository,
	stockRepository repositories.StockRepository,
	debtRepository repositories.DebtRepository,
) ReturnService {
	return &returnService{
		returnRepository: returnRepository,
		saleRepository:   saleRepository,
		stockRepository:  stockRepository,
		debtRepository:   debtRepository,
	}
}

func (q *returnService) FindReturns(ctx context.Context, request *models.ReturnFilter) ([]*models.Return, int64, error) {
	ctxt := "ReturnService-FindReturns"
	response, total, err := q.returnRepository.FindReturns(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindReturns")
	}
	return response, total, err
}

func (q *returnService) FindReturn(ctx context.Context, tx pgx.Tx, branchID, returnID uuid.UUID, kind string) (*models.Return, error) {
	ctxt := "ReturnService-FindReturn"
	response, err := q.returnRepository.FindReturn(ctx, tx, branchID, returnID, kind)
	if err != nil && !errors.Is(err, models.ErrReturnNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindReturn")
	}
	return response, err
}

func (q *returnService) CreateReturn(ctx context.Context, tx pgx.Tx, request *models.ReturnCreate) (*models.Return, error) {
	ctxt := "ReturnService-CreateReturn"
	purchase := request.Kind == models.ReturnKindPurchase
	if _, err := q.returnRepository.FindDocumentNumber(ctx, tx, request.BranchID, request.DocumentID, request.Kind); err != nil {
		if !errors.Is(err, models.ErrSaleNotFound) && !errors.Is(err, models.ErrPurchaseNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDocumentNumber")
		}
		return nil, err
	}
	returnNumber, err := q.returnRepository.NextReturnNumber(ctx, tx, request.BranchID, request.Kind, request.At)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextReturnNumber")
		return nil, err
	}
	returnID, err := q.returnRepository.CreateReturn(ctx, tx, request, returnNumber)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateReturn")
		return nil, err
	}
	var (
		total float64
		added int
	)
	for i, item := range request.Items {
		if item == nil || purchase && item.ProductID == uuid.Nil() && item.Quantity == 0 {
			continue
		}
		line, err := q.returnRepository.FindDocumentLine(ctx, tx, request.DocumentID, item.ProductID, request.Kind)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDocumentLine")
			return nil, err
		}
		if err != nil || item.Quantity <= 0 || !purchase && item.Quantity > line.Quantity {
			message := "Row {row}: the product is not on this sale or the quantity is invalid."
			if purchase {
				message = "Row {row}: the product is not on this purchase or the quantity is invalid."
			}
			return nil, helper.Invalid("items", message, "row", i+1)
		}
		if purchase {
			returned, err := q.returnRepository.FindReturnedQuantity(ctx, tx, request.DocumentID, item.ProductID, returnID)
			if err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindReturnedQuantity")
				return nil, err
			}
			if item.Quantity > line.Quantity-returned {
				return nil, helper.Invalid("items", "Row {row}: only {qty} can still be returned.", "row", i+1, "qty", trimNumber(line.Quantity-returned))
			}
		}
		subtotal := round2(item.Quantity * line.UnitPrice)
		if err = q.returnRepository.CreateReturnItem(ctx, tx, returnID, &models.ReturnItem{
			ProductID:   &item.ProductID,
			ProductName: line.ProductName,
			Quantity:    item.Quantity,
			UnitPrice:   line.UnitPrice,
			Subtotal:    subtotal,
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateReturnItem")
			return nil, err
		}
		change := &models.StockChange{
			BranchID:  request.BranchID,
			ProductID: item.ProductID,
			Change:    item.Quantity,
			Type:      models.StockMovementTypeReturn,
			Reference: returnNumber,
			Actor:     request.Actor,
			At:        request.At,
		}
		if purchase {
			change.Change, change.UnitCost, change.Note = -item.Quantity, &line.UnitPrice, "Returned to supplier"
			_, err = q.stockRepository.ChangeStock(ctx, tx, change)
			if errors.Is(err, models.ErrProductNotFound) {
				err = helper.Invalid("product_id", "Product {id} not found.", "id", item.ProductID.String())
			}
		} else if product, findErr := q.saleRepository.LockSaleProduct(ctx, tx, request.BranchID, item.ProductID); findErr == nil {
			err = moveProductStock(ctx, tx, q.stockRepository, product, change)
		} else if !errors.Is(findErr, models.ErrProductNotFound) {
			err = findErr
		}
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeStock")
			return nil, err
		}
		total += subtotal
		added++
	}
	if added == 0 {
		return nil, helper.Invalid("items", "Add at least one item to return.")
	}
	if err = q.returnRepository.SetRefundAmount(ctx, tx, returnID, total); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSetRefundAmount")
		return nil, err
	}
	if purchase && request.RefundMethod == models.RefundMethodReducePayable {
		if err = q.debtRepository.ReducePayable(ctx, tx, request.DocumentID, total, request.At); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrReducePayable")
			return nil, err
		}
	}
	response, err := q.returnRepository.FindReturn(ctx, tx, request.BranchID, returnID, request.Kind)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindReturn")
	}
	return response, err
}
