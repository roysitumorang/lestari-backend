package services

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type OrderService interface {
	FindOrders(ctx context.Context, request *models.OrderFilter) ([]*models.Order, int64, error)
	FindOrder(ctx context.Context, tx pgx.Tx, branchID, orderID uuid.UUID) (*models.Order, error)
	// CreateOrder records an order at the online prices of its products and notifies the branch.
	CreateOrder(ctx context.Context, tx pgx.Tx, request *models.OrderCreate) (*models.Order, error)
	// ChangeOrderStatus moves an order along its allowed transitions; the stock is taken when the order starts
	// being processed and given back when it is cancelled or returned.
	ChangeOrderStatus(ctx context.Context, tx pgx.Tx, request *models.OrderStatusChange) (*models.Order, error)
}

type orderService struct {
	orderRepository        repositories.OrderRepository
	saleRepository         repositories.SaleRepository
	stockRepository        repositories.StockRepository
	notificationRepository repositories.NotificationRepository
	settingService         SettingService
}

func NewOrderService(
	orderRepository repositories.OrderRepository,
	saleRepository repositories.SaleRepository,
	stockRepository repositories.StockRepository,
	notificationRepository repositories.NotificationRepository,
	settingService SettingService,
) OrderService {
	return &orderService{
		orderRepository:        orderRepository,
		saleRepository:         saleRepository,
		stockRepository:        stockRepository,
		notificationRepository: notificationRepository,
		settingService:         settingService,
	}
}

func (q *orderService) FindOrders(ctx context.Context, request *models.OrderFilter) ([]*models.Order, int64, error) {
	ctxt := "OrderService-FindOrders"
	response, total, err := q.orderRepository.FindOrders(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindOrders")
	}
	return response, total, err
}

func (q *orderService) FindOrder(ctx context.Context, tx pgx.Tx, branchID, orderID uuid.UUID) (*models.Order, error) {
	ctxt := "OrderService-FindOrder"
	response, err := q.orderRepository.FindOrder(ctx, tx, branchID, orderID)
	if err != nil && !errors.Is(err, models.ErrOrderNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindOrder")
	}
	return response, err
}

func (q *orderService) CreateOrder(ctx context.Context, tx pgx.Tx, request *models.OrderCreate) (*models.Order, error) {
	ctxt := "OrderService-CreateOrder"
	orderNumber, err := q.orderRepository.NextOrderNumber(ctx, tx, request.BranchID, request.At)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextOrderNumber")
		return nil, err
	}
	status := models.OrderStatusUnpaid
	if request.PaymentProofURL != "" {
		status = models.OrderStatusAwaitingConfirmation
	}
	orderID, err := q.orderRepository.CreateOrder(ctx, tx, request, orderNumber, status)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateOrder")
		return nil, err
	}
	var subtotal float64
	for i, item := range request.Items {
		if item == nil || item.ProductID == uuid.Nil() || item.Quantity <= 0 {
			return nil, helper.Invalid("items", "Row {row} needs a product and a quantity above 0.", "row", i+1)
		}
		product, err := q.orderRepository.FindOnlineProduct(ctx, tx, request.BranchID, item.ProductID)
		if errors.Is(err, models.ErrProductNotFound) {
			return nil, helper.Invalid("items", "Row {row}: product is not sold online.", "row", i+1)
		}
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindOnlineProduct")
			return nil, err
		}
		line := &models.OrderItem{
			ProductID:   &item.ProductID,
			ProductName: product.Name,
			Quantity:    item.Quantity,
			UnitPrice:   product.SellingPrice,
			Subtotal:    round2(product.SellingPrice * item.Quantity),
		}
		if err = q.orderRepository.CreateOrderItem(ctx, tx, orderID, line); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateOrderItem")
			return nil, err
		}
		subtotal += line.Subtotal
	}
	if len(request.Items) == 0 {
		return nil, helper.Invalid("items", "Add at least one item.")
	}
	if err = q.orderRepository.SetOrderSubtotal(ctx, tx, orderID, subtotal); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSetOrderSubtotal")
		return nil, err
	}
	notification := models.NewNotification(request.StoreID, &request.BranchID, i18n.Tr("New online order"), i18n.Tr("You have a new order."), "catalog-orders", request.At)
	notification.Source = models.NotificationSourceOnlineStore
	if err = q.notificationRepository.CreateNotification(ctx, tx, notification); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateNotification")
		return nil, err
	}
	return q.FindOrder(ctx, tx, request.BranchID, orderID)
}

func (q *orderService) ChangeOrderStatus(ctx context.Context, tx pgx.Tx, request *models.OrderStatusChange) (*models.Order, error) {
	ctxt := "OrderService-ChangeOrderStatus"
	order, err := q.orderRepository.LockOrder(ctx, tx, request.BranchID, request.OrderID)
	if err != nil {
		if !errors.Is(err, models.ErrOrderNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockOrder")
		}
		return nil, err
	}
	if !slices.Contains(models.OrderTransitions[order.Status], request.Status) {
		return nil, helper.Conflict("An order that is {from} cannot become {to}.", "from", i18n.Tr(order.Status), "to", i18n.Tr(request.Status))
	}
	var onlineStore models.OnlineStoreSetting
	if err = q.settingService.FindSetting(ctx, tx, request.BranchID, models.SettingOnlineStore, &onlineStore); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSetting")
		return nil, err
	}
	takes := models.OrderHoldsStock(request.Status)
	if onlineStore.UsePhysicalStock && takes != models.OrderHoldsStock(order.Status) {
		items, err := q.orderRepository.FindOrderedQuantities(ctx, tx, request.OrderID)
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindOrderedQuantities")
			return nil, err
		}
		for _, item := range items {
			product, err := q.saleRepository.LockSaleProduct(ctx, tx, request.BranchID, item.ProductID)
			if errors.Is(err, models.ErrProductNotFound) {
				continue
			}
			if err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockSaleProduct")
				return nil, err
			}
			change := &models.StockChange{
				BranchID:  request.BranchID,
				Change:    item.Quantity,
				Type:      models.StockMovementTypeOnlineOrder,
				Reference: order.OrderNumber,
				Actor:     request.Actor,
				At:        request.At,
			}
			if takes {
				change.Change = -item.Quantity
			}
			if err = moveProductStock(ctx, tx, q.stockRepository, product, change); err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMoveProductStock")
				return nil, err
			}
		}
	}
	if err = q.orderRepository.UpdateOrderStatus(ctx, tx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateOrderStatus")
		return nil, err
	}
	return q.FindOrder(ctx, tx, request.BranchID, request.OrderID)
}
