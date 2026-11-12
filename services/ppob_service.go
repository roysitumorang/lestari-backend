package services

import (
	"context"
	"errors"
	"log/slog"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type PPOBService interface {
	FindPPOBPrices(ctx context.Context, request *models.PPOBPriceFilter) ([]*models.PPOBPrice, int64, error)
	FindPPOBPrice(ctx context.Context, tx pgx.Tx, request *models.PPOBPriceFilter) (*models.PPOBPrice, error)
	// UpdatePPOBPrice saves a price; a prepaid product cannot be sold below its cost.
	UpdatePPOBPrice(ctx context.Context, tx pgx.Tx, request *models.PPOBPrice) (*models.PPOBPrice, error)
	// FindPPOBDeposits lists the deposits of a branch after expiring the pending ones whose time ran out.
	FindPPOBDeposits(ctx context.Context, request *models.PPOBDepositFilter, at time.Time) ([]*models.PPOBDeposit, int64, error)
	FindSummary(ctx context.Context, branchID uuid.UUID) (*models.PPOBSummary, error)
	FindPPOBDeposit(ctx context.Context, tx pgx.Tx, branchID, depositID uuid.UUID) (*models.PPOBDeposit, error)
	// CreatePPOBDeposit asks the payment provider how to pay a top-up of the PPOB balance.
	CreatePPOBDeposit(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, request *models.PPOBDepositRequest, lang i18n.Lang, at time.Time) (*models.PPOBDeposit, error)
	AttachDepositProof(ctx context.Context, tx pgx.Tx, deposit *models.PPOBDeposit, proofURL string, at time.Time) (*models.PPOBDeposit, error)
	CancelDeposit(ctx context.Context, tx pgx.Tx, branchID, depositID uuid.UUID, at time.Time) (*models.PPOBDeposit, error)
	// MarkDepositPaid completes the pending deposit with this reference and tells whether there was one.
	MarkDepositPaid(ctx context.Context, tx pgx.Tx, referenceID string, at time.Time) (bool, error)
	// Sell records a PPOB sale and charges its cost to the balance of the branch.
	Sell(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, request *models.PPOBSaleRequest, cashier models.Actor, at time.Time) (*models.PPOBTransaction, error)
}

type ppobService struct {
	ppobRepository repositories.PPOBRepository
	payments       Payments
}

func NewPPOBService(
	ppobRepository repositories.PPOBRepository,
	payments Payments,
) PPOBService {
	return &ppobService{
		ppobRepository: ppobRepository,
		payments:       payments,
	}
}

func (q *ppobService) FindPPOBPrices(ctx context.Context, request *models.PPOBPriceFilter) ([]*models.PPOBPrice, int64, error) {
	ctxt := "PPOBService-FindPPOBPrices"
	response, total, err := q.ppobRepository.FindPPOBPrices(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBPrices")
	}
	return response, total, err
}

func (q *ppobService) FindPPOBPrice(ctx context.Context, tx pgx.Tx, request *models.PPOBPriceFilter) (*models.PPOBPrice, error) {
	ctxt := "PPOBService-FindPPOBPrice"
	response, err := q.ppobRepository.FindPPOBPrice(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrPPOBPriceNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBPrice")
	}
	return response, err
}

func (q *ppobService) UpdatePPOBPrice(ctx context.Context, tx pgx.Tx, request *models.PPOBPrice) (*models.PPOBPrice, error) {
	ctxt := "PPOBService-UpdatePPOBPrice"
	if request.Kind == models.PPOBKindPrepaid && deref(request.SellingPrice) < deref(request.BasePrice) {
		return nil, helper.Invalid("selling_price", "The selling price cannot be below the cost of {cost}.", "cost", helper.Sprintf("%g", deref(request.BasePrice)))
	}
	response, err := q.ppobRepository.UpdatePPOBPrice(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrPPOBPriceNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePPOBPrice")
	}
	return response, err
}

func (q *ppobService) FindPPOBDeposits(ctx context.Context, request *models.PPOBDepositFilter, at time.Time) ([]*models.PPOBDeposit, int64, error) {
	ctxt := "PPOBService-FindPPOBDeposits"
	if err := q.ppobRepository.ExpireDeposits(ctx, request.BranchID, at); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExpireDeposits")
		return nil, 0, err
	}
	response, total, err := q.ppobRepository.FindPPOBDeposits(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBDeposits")
	}
	return response, total, err
}

func (q *ppobService) FindSummary(ctx context.Context, branchID uuid.UUID) (*models.PPOBSummary, error) {
	ctxt := "PPOBService-FindSummary"
	balance, err := q.ppobRepository.FindBalance(ctx, nil, branchID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBalance")
		return nil, err
	}
	transactionTotal, err := q.ppobRepository.FindTransactionTotal(ctx, branchID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindTransactionTotal")
		return nil, err
	}
	return &models.PPOBSummary{Balance: balance, TransactionTotal: transactionTotal}, nil
}

func (q *ppobService) FindPPOBDeposit(ctx context.Context, tx pgx.Tx, branchID, depositID uuid.UUID) (*models.PPOBDeposit, error) {
	ctxt := "PPOBService-FindPPOBDeposit"
	response, err := q.ppobRepository.FindPPOBDeposit(ctx, tx, branchID, depositID)
	if err != nil && !errors.Is(err, models.ErrPPOBDepositNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBDeposit")
	}
	return response, err
}

func (q *ppobService) CreatePPOBDeposit(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, request *models.PPOBDepositRequest, lang i18n.Lang, at time.Time) (*models.PPOBDeposit, error) {
	ctxt := "PPOBService-CreatePPOBDeposit"
	balance, err := q.ppobRepository.FindBalance(ctx, tx, branchID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBalance")
		return nil, err
	}
	if balance+request.Amount > models.PPOBMaximumBalance {
		return nil, helper.Invalid("amount", "The PPOB balance cannot exceed Rp 2,000,000.")
	}
	adminFee := 4000.0
	if request.PaymentMethod == models.PaymentMethodManualTransfer {
		adminFee = 0
	}
	if request.VoucherCode != "" {
		err = q.ppobRepository.UseFreeFeeVoucher(ctx, tx, request.VoucherCode, at)
		if errors.Is(err, models.ErrVoucherInvalid) {
			return nil, helper.Invalid("voucher_code", "This voucher is not valid.")
		}
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUseFreeFeeVoucher")
			return nil, err
		}
		adminFee = 0
	}
	referenceID := "PPOB-" + at.In(helper.Location).Format("060102") + "-" + middlewares.RandomDigits(6)
	url, instructions, err := q.payments.Create(ctx, referenceID, request.Amount+adminFee, request.PaymentMethod, lang)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePayment")
		return nil, err
	}
	response, err := q.ppobRepository.CreatePPOBDeposit(ctx, tx, &models.PPOBDeposit{
		BranchID:            branchID,
		ReferenceID:         referenceID,
		Type:                "top_up",
		PaymentMethod:       &request.PaymentMethod,
		Amount:              request.Amount,
		AdminFee:            adminFee,
		Status:              models.PPOBDepositStatusPending,
		PaymentURL:          optional(url),
		PaymentInstructions: &instructions,
		ExpiresAt:           new(at.Add(invoiceLifetime)),
		CreatedAt:           at,
	})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePPOBDeposit")
	}
	return response, err
}

func (q *ppobService) AttachDepositProof(ctx context.Context, tx pgx.Tx, deposit *models.PPOBDeposit, proofURL string, at time.Time) (*models.PPOBDeposit, error) {
	ctxt := "PPOBService-AttachDepositProof"
	if err := q.ppobRepository.UpdateDepositProof(ctx, tx, deposit.ID, proofURL, at); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateDepositProof")
		return nil, err
	}
	return q.FindPPOBDeposit(ctx, tx, deposit.BranchID, deposit.ID)
}

func (q *ppobService) CancelDeposit(ctx context.Context, tx pgx.Tx, branchID, depositID uuid.UUID, at time.Time) (*models.PPOBDeposit, error) {
	ctxt := "PPOBService-CancelDeposit"
	cancelled, err := q.ppobRepository.CancelDeposit(ctx, tx, branchID, depositID, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCancelDeposit")
		return nil, err
	}
	if !cancelled {
		return nil, helper.Conflict("Only a pending top-up can be cancelled.")
	}
	return q.FindPPOBDeposit(ctx, tx, branchID, depositID)
}

func (q *ppobService) MarkDepositPaid(ctx context.Context, tx pgx.Tx, referenceID string, at time.Time) (bool, error) {
	ctxt := "PPOBService-MarkDepositPaid"
	deposit, err := q.ppobRepository.LockPendingDeposit(ctx, tx, referenceID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockPendingDeposit")
		return false, err
	}
	if deposit == nil {
		return false, nil
	}
	balance, err := q.ppobRepository.FindBalance(ctx, tx, deposit.BranchID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBalance")
		return false, err
	}
	if err = q.ppobRepository.CompleteDeposit(ctx, tx, deposit.ID, balance+deposit.Amount, at); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCompleteDeposit")
		return false, err
	}
	return true, nil
}

func (q *ppobService) Sell(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, request *models.PPOBSaleRequest, cashier models.Actor, at time.Time) (*models.PPOBTransaction, error) {
	ctxt := "PPOBService-Sell"
	unavailable := helper.Invalid("price_id", "This PPOB product is not available.")
	priceID, err := uuid.Parse(request.PriceID)
	if err != nil {
		return nil, unavailable
	}
	price, err := q.ppobRepository.FindPPOBPrice(ctx, tx, models.NewPPOBPriceFilter().WithBranchIDs(branchID).WithPPOBPriceIDs(priceID))
	if errors.Is(err, models.ErrPPOBPriceNotFound) || err == nil && !price.IsActive {
		return nil, unavailable
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOBPrice")
		return nil, err
	}
	cost, sellingPrice := deref(price.BasePrice), deref(price.SellingPrice)
	if price.Kind == models.PPOBKindPostpaid {
		if request.BillAmount <= 0 {
			return nil, helper.Invalid("bill_amount", "Enter the bill amount.")
		}
		cost, sellingPrice = request.BillAmount+deref(price.BaseAdminFee), request.BillAmount+deref(price.AdminFee)
	}
	balance, err := q.ppobRepository.FindBalance(ctx, tx, branchID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBalance")
		return nil, err
	}
	if balance < cost {
		return nil, helper.PaymentRequired("insufficient_balance", "The PPOB balance is too low. Top up first.")
	}
	response, err := q.ppobRepository.CreatePPOBTransaction(ctx, tx, &models.PPOBTransaction{
		BranchID:       branchID,
		ReferenceID:    "TRX" + at.In(helper.Location).Format("060102150405") + middlewares.RandomDigits(3),
		ProductName:    price.ProductName,
		CustomerNumber: request.CustomerNumber,
		BasePrice:      cost,
		SellingPrice:   sellingPrice,
		Status:         "success",
		Cashier:        cashier,
		CreatedAt:      at,
	})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePPOBTransaction")
	}
	return response, err
}
