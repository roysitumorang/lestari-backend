package services

import (
	"context"
	"errors"
	"log/slog"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type StockTransferService interface {
	FindStockTransfers(
		ctx context.Context,
		request *models.StockTransferFilter,
	) ([]*models.StockTransfer, int64, error)
	// FindStockTransfer returns one stock transfer with its items.
	FindStockTransfer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockTransferFilter,
	) (*models.StockTransfer, error)
	// CreateStockTransfer takes the stock out of the sending branch and, unless the transfer requires
	// confirmation, puts it into the receiving branch at once.
	CreateStockTransfer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockTransfer,
	) (*models.StockTransfer, error)
	// AcceptStockTransfer receives a pending transfer; what did not arrive goes back to the sender.
	AcceptStockTransfer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockTransferDecision,
	) error
	// RejectStockTransfer refuses a pending transfer; its stock goes back to the sender.
	RejectStockTransfer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockTransferDecision,
	) error
	// DeleteStockTransfer cancels a pending transfer of the sending branch and gives it its stock back.
	DeleteStockTransfer(
		ctx context.Context,
		tx pgx.Tx,
		request *models.StockTransferDecision,
	) error
}

type stockTransferService struct {
	stockTransferRepository repositories.StockTransferRepository
	productRepository       repositories.ProductRepository
	stockRepository         repositories.StockRepository
	notificationRepository  repositories.NotificationRepository
}

func NewStockTransferService(
	stockTransferRepository repositories.StockTransferRepository,
	productRepository repositories.ProductRepository,
	stockRepository repositories.StockRepository,
	notificationRepository repositories.NotificationRepository,
) StockTransferService {
	return &stockTransferService{
		stockTransferRepository: stockTransferRepository,
		productRepository:       productRepository,
		stockRepository:         stockRepository,
		notificationRepository:  notificationRepository,
	}
}

func (q *stockTransferService) FindStockTransfers(
	ctx context.Context,
	request *models.StockTransferFilter,
) ([]*models.StockTransfer, int64, error) {
	ctxt := "StockTransferService-FindStockTransfers"
	response, total, err := q.stockTransferRepository.FindStockTransfers(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransfers")
	}
	return response, total, err
}

func (q *stockTransferService) FindStockTransfer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockTransferFilter,
) (*models.StockTransfer, error) {
	ctxt := "StockTransferService-FindStockTransfer"
	response, err := q.stockTransferRepository.FindStockTransfer(ctx, tx, request)
	if err != nil {
		if !errors.Is(err, models.ErrStockTransferNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransfer")
		}
		return nil, err
	}
	if response.Items, err = q.stockTransferRepository.FindStockTransferItems(ctx, tx, response.ID); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransferItems")
		return nil, err
	}
	return response, nil
}

func (q *stockTransferService) CreateStockTransfer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockTransfer,
) (*models.StockTransfer, error) {
	ctxt := "StockTransferService-CreateStockTransfer"
	if request.ToBranchID == request.FromBranchID {
		return nil, helper.Invalid("to_branch_id", "Choose a different branch.")
	}
	exists, err := repositories.ReferenceExists(ctx, tx, "branches", "store_id", request.ToBranchID, request.StoreID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrReferenceExists")
		return nil, err
	}
	if !exists {
		return nil, helper.Invalid("to_branch_id", "Not found.")
	}
	if request.TransferNumber, err = q.stockTransferRepository.NextStockTransferNumber(
		ctx,
		tx,
		request.StoreID,
		request.CreatedAt.In(helper.Location).Format("20060102"),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextStockTransferNumber")
		return nil, err
	}
	v := &helper.Validation{}
	for _, item := range request.Items {
		product, err := q.productRepository.FindProduct(
			ctx,
			tx,
			models.NewProductFilter().
				WithBranchIDs(request.FromBranchID).
				WithProductIDs(*item.ProductID),
		)
		if errors.Is(err, models.ErrProductNotFound) {
			v.Add("items", "Row {row}: product not found in this branch.", "row", item.Row)
			continue
		}
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProduct")
			return nil, err
		}
		// the lock makes the stock check hold until the stock is taken out below
		stock, err := q.stockRepository.LockStock(ctx, tx, request.FromBranchID, product.ID)
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockStock")
			return nil, err
		}
		if stock < item.Quantity {
			v.Add("items", "Row {row}: only {qty} of {product} in stock.", "row", item.Row, "qty", helper.Sprintf("%g", stock), "product", product.Name)
			continue
		}
		item.ProductName = product.Name
		item.SKU = &product.SKU
		if !request.RequiresConfirmation {
			item.ReceivedQuantity = &item.Quantity
		}
		if _, err = q.stockRepository.ChangeStock(ctx, tx, &models.StockChange{
			BranchID:  request.FromBranchID,
			ProductID: product.ID,
			Change:    -item.Quantity,
			Type:      models.StockMovementTypeTransferOut,
			Reference: request.TransferNumber,
			Actor:     request.Actor,
			At:        request.CreatedAt,
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeStock")
			return nil, err
		}
		if request.RequiresConfirmation {
			continue
		}
		if err = q.receive(ctx, tx, &receipt{
			branchID:  request.ToBranchID,
			productID: product.ID,
			sku:       product.SKU,
			quantity:  item.Quantity,
			reference: request.TransferNumber,
			decision:  &models.StockTransferDecision{Actor: request.Actor, At: request.CreatedAt},
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrReceive")
			return nil, err
		}
	}
	if err = v.Err(); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.stockTransferRepository.CreateStockTransfer(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStockTransfer")
		return nil, err
	}
	if err = q.stockTransferRepository.CreateStockTransferItems(ctx, tx, response.ID, request.Items); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStockTransferItems")
		return nil, err
	}
	if request.RequiresConfirmation {
		if err = q.notificationRepository.CreateNotification(ctx, tx, models.NewNotification(request.StoreID, &request.ToBranchID, i18n.Tr("Incoming stock transfer"), i18n.Tr("Transfer {number} is waiting for your confirmation.", "number", request.TransferNumber), "report-transfer-confirmations", request.CreatedAt)); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateNotification")
			return nil, err
		}
	}
	return q.FindStockTransfer(ctx, tx, models.NewStockTransferFilter().WithStockTransferIDs(response.ID))
}

// receipt is stock arriving in a branch from a product of another branch.
type receipt struct {
	branchID,
	productID uuid.UUID
	sku       string
	quantity  float64
	reference string
	decision  *models.StockTransferDecision
}

// receive adds stock to the product the receiving branch keeps for the sent one (its copy, or the product with
// the same SKU), creating the product there when the branch does not have it yet.
func (q *stockTransferService) receive(ctx context.Context, tx pgx.Tx, request *receipt) error {
	ctxt := "StockTransferService-receive"
	productID, err := q.productRepository.FindBranchCopy(ctx, tx, request.branchID, request.productID, request.sku)
	if errors.Is(err, models.ErrProductNotFound) {
		productID, err = q.productRepository.CopyProductToBranch(ctx, tx, request.branchID, request.productID, request.decision.At)
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranchCopy")
		return err
	}
	if _, err = q.stockRepository.ChangeStock(ctx, tx, &models.StockChange{
		BranchID:  request.branchID,
		ProductID: productID,
		Change:    request.quantity,
		Type:      models.StockMovementTypeTransferIn,
		Reference: request.reference,
		Actor:     request.decision.Actor,
		At:        request.decision.At,
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeStock")
	}
	return err
}

// giveBack returns stock of a transfer to the product it was taken from.
func (q *stockTransferService) giveBack(ctx context.Context, tx pgx.Tx, branchID, productID uuid.UUID, quantity float64, reference string, request *models.StockTransferDecision) error {
	ctxt := "StockTransferService-giveBack"
	_, err := q.stockRepository.ChangeStock(ctx, tx, &models.StockChange{
		BranchID:  branchID,
		ProductID: productID,
		Change:    quantity,
		Type:      models.StockMovementTypeTransferIn,
		Reference: reference,
		Actor:     request.Actor,
		At:        request.At,
	})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeStock")
	}
	return err
}

// pending locks the transfer of a decision and checks that the active branch is its receiver and that it still
// waits for an answer; refusal is the message when another branch tries.
func (q *stockTransferService) pending(ctx context.Context, tx pgx.Tx, request *models.StockTransferDecision, refusal string) (*models.StockTransfer, []*models.StockTransferItem, error) {
	ctxt := "StockTransferService-pending"
	stockTransfer, err := q.stockTransferRepository.LockStockTransfer(ctx, tx, request.StoreID, request.StockTransferID)
	if err != nil {
		if !errors.Is(err, models.ErrStockTransferNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockStockTransfer")
		}
		return nil, nil, err
	}
	if stockTransfer.ToBranchID != request.BranchID {
		return nil, nil, helper.Forbidden(refusal)
	}
	if stockTransfer.Status != models.StockTransferStatusPending {
		return nil, nil, helper.Conflict("This transfer was already {status}.", "status", i18n.Tr(stockTransfer.Status))
	}
	items, err := q.stockTransferRepository.FindStockTransferItems(ctx, tx, request.StockTransferID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransferItems")
		return nil, nil, err
	}
	return stockTransfer, items, nil
}

func (q *stockTransferService) AcceptStockTransfer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockTransferDecision,
) error {
	ctxt := "StockTransferService-AcceptStockTransfer"
	stockTransfer, items, err := q.pending(ctx, tx, request, "Only the receiving branch can accept this transfer.")
	if err != nil {
		return err
	}
	for _, item := range items {
		// a product deleted from the sending branch since then can be neither received nor given back
		if item.ProductID == nil {
			continue
		}
		quantity := item.Quantity
		if received, ok := request.Received[*item.ProductID]; ok {
			if received < 0 || received > item.Quantity {
				return helper.Invalid("items", "Received quantity for {product} must be between 0 and {qty}.", "product", item.ProductName, "qty", helper.Sprintf("%g", item.Quantity))
			}
			quantity = received
		}
		if err = q.stockTransferRepository.UpdateReceivedQuantity(ctx, tx, stockTransfer.ID, *item.ProductID, quantity); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateReceivedQuantity")
			return err
		}
		if quantity > 0 {
			var sku string
			if item.SKU != nil {
				sku = *item.SKU
			}
			if err = q.receive(ctx, tx, &receipt{
				branchID:  stockTransfer.ToBranchID,
				productID: *item.ProductID,
				sku:       sku,
				quantity:  quantity,
				reference: stockTransfer.TransferNumber,
				decision:  request,
			}); err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrReceive")
				return err
			}
		}
		if short := item.Quantity - quantity; short > 0 {
			if err = q.giveBack(ctx, tx, stockTransfer.FromBranchID, *item.ProductID, short, stockTransfer.TransferNumber+" (not received)", request); err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrGiveBack")
				return err
			}
		}
	}
	if err = q.stockTransferRepository.UpdateStockTransferStatus(ctx, tx, stockTransfer.ID, models.StockTransferStatusAccepted, request.Note, "", request.At); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateStockTransferStatus")
		return err
	}
	if err = q.notificationRepository.CreateNotification(ctx, tx, models.NewNotification(stockTransfer.StoreID, &stockTransfer.FromBranchID, i18n.Tr("Transfer accepted"), i18n.Tr("Transfer {number} was received.", "number", stockTransfer.TransferNumber), "report-transfers", request.At)); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateNotification")
	}
	return err
}

func (q *stockTransferService) RejectStockTransfer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockTransferDecision,
) error {
	ctxt := "StockTransferService-RejectStockTransfer"
	stockTransfer, items, err := q.pending(ctx, tx, request, "Only the receiving branch can reject this transfer.")
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ProductID == nil {
			continue
		}
		if err = q.giveBack(ctx, tx, stockTransfer.FromBranchID, *item.ProductID, item.Quantity, stockTransfer.TransferNumber+" (rejected)", request); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrGiveBack")
			return err
		}
	}
	if err = q.stockTransferRepository.UpdateStockTransferStatus(ctx, tx, stockTransfer.ID, models.StockTransferStatusRejected, "", request.Reason, request.At); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateStockTransferStatus")
		return err
	}
	if err = q.notificationRepository.CreateNotification(ctx, tx, models.NewNotification(stockTransfer.StoreID, &stockTransfer.FromBranchID, i18n.Tr("Transfer rejected"), i18n.Tr("Transfer {number} was rejected: {reason}", "number", stockTransfer.TransferNumber, "reason", request.Reason), "report-transfers", request.At)); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateNotification")
	}
	return err
}

func (q *stockTransferService) DeleteStockTransfer(
	ctx context.Context,
	tx pgx.Tx,
	request *models.StockTransferDecision,
) error {
	ctxt := "StockTransferService-DeleteStockTransfer"
	stockTransfer, err := q.stockTransferRepository.LockStockTransfer(ctx, tx, request.StoreID, request.StockTransferID)
	if err != nil {
		if !errors.Is(err, models.ErrStockTransferNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockStockTransfer")
		}
		return err
	}
	if stockTransfer.FromBranchID != request.BranchID {
		// a branch that takes no part in the transfer does not learn that it exists
		if stockTransfer.ToBranchID != request.BranchID {
			return models.ErrStockTransferNotFound
		}
		return helper.Forbidden("Only the sending branch can delete a transfer.")
	}
	if stockTransfer.Status != models.StockTransferStatusPending {
		return helper.Conflict("Only a transfer that is still pending can be deleted.")
	}
	items, err := q.stockTransferRepository.FindStockTransferItems(ctx, tx, stockTransfer.ID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransferItems")
		return err
	}
	for _, item := range items {
		if item.ProductID == nil {
			continue
		}
		if err = q.giveBack(ctx, tx, stockTransfer.FromBranchID, *item.ProductID, item.Quantity, "Transfer cancelled", request); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrGiveBack")
			return err
		}
	}
	if _, err = q.stockTransferRepository.DeleteStockTransfers(
		ctx,
		tx,
		models.NewStockTransferFilter().
			WithStoreIDs(request.StoreID).
			WithStockTransferIDs(stockTransfer.ID),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteStockTransfers")
	}
	return err
}
