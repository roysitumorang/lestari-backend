package services

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type PurchaseService interface {
	FindPurchases(ctx context.Context, request *models.PurchaseFilter) ([]*models.Purchase, int64, error)
	FindPurchase(ctx context.Context, tx pgx.Tx, branchID, purchaseID uuid.UUID) (*models.Purchase, error)
	// CreatePurchase records a purchase: the stock comes in at its cost, the purchase price of each product
	// becomes the weighted average and a credit purchase gets a payable.
	CreatePurchase(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite) (*models.Purchase, error)
	// UpdatePurchase changes what was sent; new items replace the old ones and their stock.
	UpdatePurchase(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite) (*models.Purchase, error)
	// DeletePurchase takes the stock back and removes the purchase.
	DeletePurchase(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite) error
}

type purchaseService struct {
	purchaseRepository repositories.PurchaseRepository
	stockRepository    repositories.StockRepository
	debtRepository     repositories.DebtRepository
}

func NewPurchaseService(
	purchaseRepository repositories.PurchaseRepository,
	stockRepository repositories.StockRepository,
	debtRepository repositories.DebtRepository,
) PurchaseService {
	return &purchaseService{
		purchaseRepository: purchaseRepository,
		stockRepository:    stockRepository,
		debtRepository:     debtRepository,
	}
}

func (q *purchaseService) FindPurchases(ctx context.Context, request *models.PurchaseFilter) ([]*models.Purchase, int64, error) {
	ctxt := "PurchaseService-FindPurchases"
	response, total, err := q.purchaseRepository.FindPurchases(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchases")
	}
	return response, total, err
}

func (q *purchaseService) FindPurchase(ctx context.Context, tx pgx.Tx, branchID, purchaseID uuid.UUID) (*models.Purchase, error) {
	ctxt := "PurchaseService-FindPurchase"
	response, err := q.purchaseRepository.FindPurchase(ctx, tx, branchID, purchaseID)
	if err != nil && !errors.Is(err, models.ErrPurchaseNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchase")
	}
	return response, err
}

func (q *purchaseService) CreatePurchase(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite) (*models.Purchase, error) {
	ctxt := "PurchaseService-CreatePurchase"
	receiptNumber, err := q.purchaseRepository.NextReceiptNumber(ctx, tx, request.BranchID, request.At)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextReceiptNumber")
		return nil, err
	}
	if request.ID, err = q.purchaseRepository.CreatePurchase(ctx, tx, request, receiptNumber); err != nil {
		if errors.Is(err, models.ErrReferenceNotFound) {
			return nil, helper.Invalid("supplier_id", "Supplier not found.")
		}
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePurchase")
		return nil, err
	}
	if err = q.addItems(ctx, tx, request, receiptNumber); err != nil {
		return nil, err
	}
	return q.finish(ctx, tx, request)
}

func (q *purchaseService) UpdatePurchase(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite) (*models.Purchase, error) {
	ctxt := "PurchaseService-UpdatePurchase"
	current, err := q.purchaseRepository.FindPurchase(ctx, tx, request.BranchID, request.ID)
	if err != nil {
		if !errors.Is(err, models.ErrPurchaseNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchase")
		}
		return nil, err
	}
	if err = q.purchaseRepository.UpdatePurchase(ctx, tx, current, request); err != nil {
		if errors.Is(err, models.ErrReferenceNotFound) {
			return nil, helper.Invalid("supplier_id", "Supplier not found.")
		}
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePurchase")
		return nil, err
	}
	if request.HasItems {
		if err = q.reverseItems(ctx, tx, request, current.ReceiptNumber, "Purchase edited"); err != nil {
			return nil, err
		}
		if err = q.addItems(ctx, tx, request, current.ReceiptNumber); err != nil {
			return nil, err
		}
	}
	return q.finish(ctx, tx, request)
}

func (q *purchaseService) DeletePurchase(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite) error {
	ctxt := "PurchaseService-DeletePurchase"
	current, err := q.purchaseRepository.FindPurchase(ctx, tx, request.BranchID, request.ID)
	if err != nil {
		if !errors.Is(err, models.ErrPurchaseNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchase")
		}
		return err
	}
	if err = q.reverseItems(ctx, tx, request, current.ReceiptNumber, "Purchase deleted"); err != nil {
		return err
	}
	if err = q.purchaseRepository.DeletePurchase(ctx, tx, request.ID); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeletePurchase")
	}
	return err
}

// finish refreshes the totals and the payable, then reads the purchase back.
func (q *purchaseService) finish(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite) (*models.Purchase, error) {
	ctxt := "PurchaseService-finish"
	if err := q.recalculate(ctx, tx, request); err != nil {
		return nil, err
	}
	response, err := q.purchaseRepository.FindPurchase(ctx, tx, request.BranchID, request.ID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchase")
	}
	return response, err
}

// addItems stores the lines of a purchase and receives their stock.
func (q *purchaseService) addItems(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite, receiptNumber string) error {
	ctxt := "PurchaseService-addItems"
	v := &helper.Validation{}
	added := 0
	for i, item := range request.Items {
		if item == nil || item.ProductID == uuid.Nil() && item.Quantity == 0 {
			continue
		}
		if item.ProductID == uuid.Nil() || item.Quantity <= 0 || item.UnitCost < 0 {
			v.Add("items", "Row {row} needs a product, a quantity above 0 and a unit cost.", "row", i+1)
			continue
		}
		name, err := q.purchaseRepository.FindProductName(ctx, tx, request.BranchID, item.ProductID)
		if errors.Is(err, models.ErrProductNotFound) {
			v.Add("items", "Row {row}: product not found.", "row", i+1)
			continue
		}
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProductName")
			return err
		}
		line := &models.PurchaseItem{
			ProductID:      &item.ProductID,
			ProductName:    name,
			Quantity:       item.Quantity,
			Unit:           optional(item.Unit),
			UnitCost:       item.UnitCost,
			DiscountAmount: item.DiscountAmount,
			Subtotal:       round2(item.Quantity*item.UnitCost - item.DiscountAmount),
		}
		if err = q.purchaseRepository.CreatePurchaseItem(ctx, tx, request.ID, line); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePurchaseItem")
			return err
		}
		unitCost := round2(line.Subtotal / item.Quantity)
		if _, err = q.stockRepository.ChangeStock(ctx, tx, &models.StockChange{
			BranchID:  request.BranchID,
			ProductID: item.ProductID,
			Change:    item.Quantity,
			Type:      models.StockMovementTypePurchase,
			Reference: receiptNumber,
			UnitCost:  &unitCost,
			Actor:     request.Actor,
			At:        request.At,
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeStock")
			return err
		}
		if err = q.stockRepository.AverageUnitCost(ctx, tx, item.ProductID, item.Quantity, unitCost); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrAverageUnitCost")
			return err
		}
		added++
	}
	if added == 0 && len(v.Fields) == 0 {
		v.Add("items", "Add at least one item.")
	}
	return v.Err()
}

// reverseItems takes the stock of the lines back and removes them.
func (q *purchaseService) reverseItems(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite, receiptNumber, note string) error {
	ctxt := "PurchaseService-reverseItems"
	items, err := q.purchaseRepository.FindPurchasedQuantities(ctx, tx, request.ID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchasedQuantities")
		return err
	}
	for _, item := range items {
		if _, err = q.stockRepository.ChangeStock(ctx, tx, &models.StockChange{
			BranchID:  request.BranchID,
			ProductID: item.ProductID,
			Change:    -item.Quantity,
			Type:      models.StockMovementTypePurchase,
			Reference: receiptNumber,
			Actor:     request.Actor,
			Note:      note,
			At:        request.At,
		}); err != nil && !errors.Is(err, models.ErrProductNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangeStock")
			return err
		}
	}
	if err = q.purchaseRepository.DeletePurchaseItems(ctx, tx, request.ID); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeletePurchaseItems")
	}
	return err
}

// recalculate refreshes the totals, the status and the payable of a purchase.
func (q *purchaseService) recalculate(ctx context.Context, tx pgx.Tx, request *models.PurchaseWrite) error {
	ctxt := "PurchaseService-recalculate"
	totals, err := q.purchaseRepository.FindPurchaseTotals(ctx, tx, request.ID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchaseTotals")
		return err
	}
	credit := totals.PaymentType == models.PaymentTypeCredit
	total := round2(math.Max(totals.Subtotal-totals.DiscountTotal, 0) + totals.TaxTotal + totals.FeeTotal)
	paid := total
	if credit {
		if totals.SupplierID == nil {
			return helper.Invalid("supplier_id", "A credit purchase needs a supplier.")
		}
		paid = 0
		if totals.PaidAmount != nil {
			paid = math.Min(*totals.PaidAmount, total)
		}
	}
	// payments already recorded against the payable count as paid
	settled := paid+totals.DebtPaid >= total
	status := models.SaleStatusCompleted
	if credit && !settled {
		status = models.SaleStatusUnpaid
	}
	if err = q.purchaseRepository.UpdatePurchaseTotals(ctx, tx, request.ID, totals.Subtotal, total, paid, status, request.At); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePurchaseTotals")
		return err
	}
	if !credit || paid >= total {
		if err = q.debtRepository.DeleteUnpaidPurchaseDebt(ctx, tx, request.ID); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteUnpaidPurchaseDebt")
		}
		return err
	}
	debt := &models.DebtRecord{
		BranchID:        request.BranchID,
		Kind:            models.DebtKindPayable,
		SupplierID:      totals.SupplierID,
		PurchaseID:      &request.ID,
		ReferenceNumber: totals.ReceiptNumber,
		TransactionDate: totals.PurchaseDate,
		DueDate:         totals.DueDate,
		TotalAmount:     total,
		PaidAmount:      paid + totals.DebtPaid,
		Status:          models.DebtStatusUnpaid,
		At:              request.At,
	}
	if settled {
		debt.Status = models.DebtStatusPaid
	}
	exists, err := q.debtRepository.UpdatePurchaseDebt(ctx, tx, debt)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePurchaseDebt")
		return err
	}
	if exists {
		return nil
	}
	debt.PaidAmount = paid
	if err = q.debtRepository.CreateDebt(ctx, tx, debt); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateDebt")
	}
	return err
}

// DebtService lists payables and receivables and records their payments.
type DebtService interface {
	FindDebts(ctx context.Context, request *models.DebtFilter) ([]*models.Debt, int64, error)
	FindDebt(ctx context.Context, branchID, debtID uuid.UUID, kind string) (*models.Debt, error)
	// PayDebt records a payment; a debt paid in full completes the sale or purchase it came from.
	PayDebt(ctx context.Context, tx pgx.Tx, request *models.DebtPaymentCreate) (*models.DebtPaymentResponse, error)
}

type debtService struct {
	debtRepository repositories.DebtRepository
}

func NewDebtService(
	debtRepository repositories.DebtRepository,
) DebtService {
	return &debtService{
		debtRepository: debtRepository,
	}
}

func (q *debtService) FindDebts(ctx context.Context, request *models.DebtFilter) ([]*models.Debt, int64, error) {
	ctxt := "DebtService-FindDebts"
	response, total, err := q.debtRepository.FindDebts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDebts")
	}
	return response, total, err
}

func (q *debtService) FindDebt(ctx context.Context, branchID, debtID uuid.UUID, kind string) (*models.Debt, error) {
	ctxt := "DebtService-FindDebt"
	response, err := q.debtRepository.FindDebt(ctx, branchID, debtID, kind)
	if err != nil && !errors.Is(err, models.ErrDebtNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDebt")
	}
	return response, err
}

// debtTolerance absorbs the rounding of amounts typed with fractions.
const debtTolerance = 0.004

func (q *debtService) PayDebt(ctx context.Context, tx pgx.Tx, request *models.DebtPaymentCreate) (*models.DebtPaymentResponse, error) {
	ctxt := "DebtService-PayDebt"
	debt, err := q.debtRepository.LockDebt(ctx, tx, request.BranchID, request.DebtID, request.Kind)
	if err != nil {
		if !errors.Is(err, models.ErrDebtNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockDebt")
		}
		return nil, err
	}
	remaining := round2(debt.TotalAmount - debt.PaidAmount)
	if remaining <= 0 {
		return nil, models.ErrDebtPaid
	}
	if request.Amount > remaining+debtTolerance {
		return nil, helper.Invalid("amount", "The amount is more than the remaining {amount}.", "amount", trimNumber(remaining))
	}
	response := &models.DebtPaymentResponse{ID: debt.ID, Status: models.DebtStatusUnpaid}
	if response.Payment, err = q.debtRepository.CreateDebtPayment(ctx, tx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateDebtPayment")
		return nil, err
	}
	response.PaidAmount = round2(debt.PaidAmount + request.Amount)
	response.RemainingAmount = round2(math.Max(debt.TotalAmount-response.PaidAmount, 0))
	if response.PaidAmount >= debt.TotalAmount-debtTolerance {
		response.Status = models.DebtStatusPaid
	}
	if err = q.debtRepository.UpdateDebtPaid(ctx, tx, debt.ID, response.PaidAmount, response.Status, request.At); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateDebtPaid")
		return nil, err
	}
	if response.Status == models.DebtStatusPaid {
		if err = q.debtRepository.CompleteDocument(ctx, tx, debt, request.At); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCompleteDocument")
			return nil, err
		}
	}
	return response, nil
}

// ShiftService opens and closes cashier shifts.
type ShiftService interface {
	FindShifts(ctx context.Context, request *models.ShiftFilter) ([]*models.Shift, int64, error)
	FindShift(ctx context.Context, branchID, shiftID uuid.UUID) (*models.Shift, error)
	OpenShift(ctx context.Context, branchID, cashierID uuid.UUID, request *models.ShiftOpenRequest, at time.Time) (*models.Shift, error)
	// CloseShift closes an open shift and returns it with its totals.
	CloseShift(ctx context.Context, branchID, shiftID uuid.UUID, request *models.ShiftCloseRequest, at time.Time) (*models.Shift, error)
}

type shiftService struct {
	shiftRepository repositories.ShiftRepository
}

func NewShiftService(
	shiftRepository repositories.ShiftRepository,
) ShiftService {
	return &shiftService{
		shiftRepository: shiftRepository,
	}
}

func (q *shiftService) FindShifts(ctx context.Context, request *models.ShiftFilter) ([]*models.Shift, int64, error) {
	ctxt := "ShiftService-FindShifts"
	response, total, err := q.shiftRepository.FindShifts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindShifts")
	}
	return response, total, err
}

func (q *shiftService) FindShift(ctx context.Context, branchID, shiftID uuid.UUID) (*models.Shift, error) {
	ctxt := "ShiftService-FindShift"
	response, err := q.shiftRepository.FindShift(ctx, branchID, shiftID)
	if err != nil && !errors.Is(err, models.ErrShiftNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindShift")
	}
	return response, err
}

func (q *shiftService) OpenShift(ctx context.Context, branchID, cashierID uuid.UUID, request *models.ShiftOpenRequest, at time.Time) (*models.Shift, error) {
	ctxt := "ShiftService-OpenShift"
	exists, err := q.shiftRepository.CashDrawerExists(ctx, branchID, request.CashDrawerID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCashDrawerExists")
		return nil, err
	}
	if !exists {
		return nil, helper.Invalid("cash_drawer_id", "Cash drawer not found.")
	}
	open, err := q.shiftRepository.HasOpenShift(ctx, request.CashDrawerID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrHasOpenShift")
		return nil, err
	}
	if open {
		return nil, helper.Conflict("This cash drawer already has an open shift.")
	}
	response, err := q.shiftRepository.CreateShift(ctx, branchID, cashierID, request, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateShift")
	}
	return response, err
}

func (q *shiftService) CloseShift(ctx context.Context, branchID, shiftID uuid.UUID, request *models.ShiftCloseRequest, at time.Time) (*models.Shift, error) {
	ctxt := "ShiftService-CloseShift"
	closed, err := q.shiftRepository.CloseShift(ctx, branchID, shiftID, request, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCloseShift")
		return nil, err
	}
	if !closed {
		return nil, helper.Conflict("The shift is already closed or does not exist.")
	}
	return q.FindShift(ctx, branchID, shiftID)
}
