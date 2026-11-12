package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// invoiceLifetime is how long a pending invoice can be paid.
const invoiceLifetime = 24 * time.Hour

type BillingService interface {
	// FindInvoices lists the invoices of a store after expiring the pending ones whose time ran out.
	FindInvoices(ctx context.Context, request *models.InvoiceFilter, at time.Time) ([]*models.Invoice, int64, error)
	FindInvoice(ctx context.Context, tx pgx.Tx, storeID, invoiceID uuid.UUID) (*models.Invoice, error)
	FindBillingSummary(ctx context.Context, storeID uuid.UUID) (*models.BillingSummary, error)
	FindStoreName(ctx context.Context, storeID uuid.UUID) (string, error)
	// CreateInvoice bills an order: coins pay at once, any other method yields payment instructions.
	CreateInvoice(ctx context.Context, tx pgx.Tx, request *models.InvoiceOrder) (*models.Invoice, error)
	// MarkInvoicePaid settles the pending invoice with this number and tells whether there was one.
	MarkInvoicePaid(ctx context.Context, tx pgx.Tx, invoiceNumber string, at time.Time) (bool, error)
	AttachPaymentProof(ctx context.Context, tx pgx.Tx, invoice *models.Invoice, proofURL, senderAccountName string, at time.Time) (*models.Invoice, error)
	CancelInvoice(ctx context.Context, tx pgx.Tx, storeID, invoiceID uuid.UUID, at time.Time) (*models.Invoice, error)
	RedeemVoucher(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, at time.Time) (*models.VoucherRedemption, error)
	// FindPlugins lists the plugin catalog with the subscriptions of the store, in a language.
	FindPlugins(ctx context.Context, storeID uuid.UUID, lang i18n.Lang, at time.Time) ([]*models.Plugin, error)
	StartPluginTrial(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, at time.Time) (*models.StorePlugin, error)
	FindStorage(ctx context.Context, storeID uuid.UUID) (*models.Storage, error)
	// BuyStorage pays a storage package with premium coins.
	BuyStorage(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, packageCode string, at time.Time) (*models.StoragePurchase, error)
	// SpendCoins deducts premium coins or answers 402.
	SpendCoins(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, amount float64) error
}

type billingService struct {
	billingRepository      repositories.BillingRepository
	storeRepository        repositories.StoreRepository
	notificationRepository repositories.NotificationRepository
	payments               Payments
}

func NewBillingService(
	billingRepository repositories.BillingRepository,
	storeRepository repositories.StoreRepository,
	notificationRepository repositories.NotificationRepository,
	payments Payments,
) BillingService {
	return &billingService{
		billingRepository:      billingRepository,
		storeRepository:        storeRepository,
		notificationRepository: notificationRepository,
		payments:               payments,
	}
}

func (q *billingService) FindInvoices(ctx context.Context, request *models.InvoiceFilter, at time.Time) ([]*models.Invoice, int64, error) {
	ctxt := "BillingService-FindInvoices"
	if err := q.billingRepository.ExpireInvoices(ctx, request.StoreID, at); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExpireInvoices")
		return nil, 0, err
	}
	response, total, err := q.billingRepository.FindInvoices(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindInvoices")
	}
	return response, total, err
}

func (q *billingService) FindInvoice(ctx context.Context, tx pgx.Tx, storeID, invoiceID uuid.UUID) (*models.Invoice, error) {
	ctxt := "BillingService-FindInvoice"
	response, err := q.billingRepository.FindInvoice(ctx, tx, storeID, invoiceID)
	if err != nil && !errors.Is(err, models.ErrInvoiceNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindInvoice")
	}
	return response, err
}

func (q *billingService) FindBillingSummary(ctx context.Context, storeID uuid.UUID) (*models.BillingSummary, error) {
	ctxt := "BillingService-FindBillingSummary"
	response, err := q.billingRepository.FindBillingSummary(ctx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBillingSummary")
	}
	return response, err
}

func (q *billingService) FindStoreName(ctx context.Context, storeID uuid.UUID) (string, error) {
	ctxt := "BillingService-FindStoreName"
	response, err := q.storeRepository.FindStoreName(ctx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStoreName")
	}
	return response, err
}

func (q *billingService) SpendCoins(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, amount float64) error {
	ctxt := "BillingService-SpendCoins"
	err := q.storeRepository.SpendCoins(ctx, tx, storeID, amount)
	if errors.Is(err, models.ErrInsufficientCoins) {
		return helper.PaymentRequired("insufficient_balance", "Your premium coin balance is too low. Top up first.")
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSpendCoins")
	}
	return err
}

func (q *billingService) CreateInvoice(ctx context.Context, tx pgx.Tx, request *models.InvoiceOrder) (*models.Invoice, error) {
	ctxt := "BillingService-CreateInvoice"
	amount := request.Amount
	if request.PromoCode != "" {
		percent, err := q.billingRepository.UseDiscountVoucher(ctx, tx, request.PromoCode, request.At)
		if errors.Is(err, models.ErrVoucherInvalid) {
			return nil, helper.Invalid("promo_code", "This promo code is not valid.")
		}
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUseDiscountVoucher")
			return nil, err
		}
		amount = round2(amount * (100 - percent) / 100)
	}
	invoiceNumber, err := q.billingRepository.NextInvoiceNumber(ctx, tx, request.StoreID, request.At.In(helper.Location).Format("20060102"))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextInvoiceNumber")
		return nil, err
	}
	meta, err := json.Marshal(request.Meta)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarshal")
		return nil, err
	}
	invoice := &models.Invoice{
		StoreID:       request.StoreID,
		InvoiceNumber: invoiceNumber,
		Kind:          request.Kind,
		Description:   request.Description,
		PaymentMethod: request.PaymentMethod,
		Amount:        amount,
		Status:        models.InvoiceStatusPending,
		Meta:          meta,
		CreatedAt:     request.At,
	}
	if request.PaymentMethod == models.PaymentMethodCoin {
		if request.Kind == models.InvoiceKindCoinTopUp {
			return nil, helper.Invalid("payment_method", "Coins cannot be bought with coins.")
		}
		if err = q.SpendCoins(ctx, tx, request.StoreID, amount); err != nil {
			return nil, err
		}
		invoice.Status = models.InvoiceStatusPaid
		invoice.PaidAt = &request.At
	} else {
		switch request.PaymentMethod {
		case models.PaymentMethodCreditCard:
			invoice.AdminFee = round2(amount * 0.029)
		case models.PaymentMethodManualTransfer:
		default:
			invoice.AdminFee = 4000
		}
		url, instructions, err := q.payments.Create(ctx, invoiceNumber, amount+invoice.AdminFee, request.PaymentMethod, request.Lang)
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePayment")
			return nil, err
		}
		invoice.PaymentURL = optional(url)
		invoice.PaymentInstructions = &instructions
		invoice.ExpiresAt = new(request.At.Add(invoiceLifetime))
	}
	if invoice.ID, err = q.billingRepository.CreateInvoice(ctx, tx, invoice); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateInvoice")
		return nil, err
	}
	if invoice.Status == models.InvoiceStatusPaid {
		if err = q.apply(ctx, tx, invoice, request.At); err != nil {
			return nil, err
		}
	}
	response, err := q.billingRepository.FindInvoice(ctx, tx, request.StoreID, invoice.ID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindInvoice")
		return nil, err
	}
	response.InvoiceID = &response.ID
	return response, nil
}

// apply delivers what a paid invoice bought.
func (q *billingService) apply(ctx context.Context, tx pgx.Tx, invoice *models.Invoice, at time.Time) error {
	ctxt := "BillingService-apply"
	var meta models.InvoiceMeta
	_ = json.Unmarshal(invoice.Meta, &meta)
	var err error
	switch invoice.Kind {
	case models.InvoiceKindCoinTopUp:
		err = q.storeRepository.AddCoins(ctx, tx, invoice.StoreID, invoice.Amount)
	case models.InvoiceKindSubscription:
		err = q.storeRepository.ExtendPlan(ctx, tx, invoice.StoreID, meta.Plan, meta.Months, 0, at)
	case models.InvoiceKindPlugin:
		err = q.storeRepository.ActivatePlugin(ctx, tx, invoice.StoreID, meta.Code, meta.Months, 0, at)
	case models.InvoiceKindBranchRenewal:
		err = q.storeRepository.ExtendBranches(ctx, tx, invoice.StoreID, meta.BranchIDs, meta.Months, at)
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrApply")
	}
	return err
}

func (q *billingService) MarkInvoicePaid(ctx context.Context, tx pgx.Tx, invoiceNumber string, at time.Time) (bool, error) {
	ctxt := "BillingService-MarkInvoicePaid"
	invoice, err := q.billingRepository.PayInvoice(ctx, tx, invoiceNumber, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPayInvoice")
		return false, err
	}
	if invoice == nil {
		return false, nil
	}
	if err = q.apply(ctx, tx, invoice, at); err != nil {
		return false, err
	}
	if err = q.notificationRepository.CreateNotification(ctx, tx, models.NewNotification(invoice.StoreID, nil,
		i18n.Tr("Payment received"), i18n.Tr("{item} has been paid.", "item", invoice.Description), "billing", at)); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateNotification")
		return false, err
	}
	return true, nil
}

func (q *billingService) AttachPaymentProof(ctx context.Context, tx pgx.Tx, invoice *models.Invoice, proofURL, senderAccountName string, at time.Time) (*models.Invoice, error) {
	ctxt := "BillingService-AttachPaymentProof"
	if err := q.billingRepository.UpdatePaymentProof(ctx, tx, invoice.ID, proofURL, senderAccountName, at); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePaymentProof")
		return nil, err
	}
	if err := q.notificationRepository.CreateNotification(ctx, tx, models.NewNotification(invoice.StoreID, nil,
		i18n.Tr("Payment proof received"), i18n.Tr("We are checking your transfer for {number}.", "number", invoice.InvoiceNumber), "billing", at)); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateNotification")
		return nil, err
	}
	return q.FindInvoice(ctx, tx, invoice.StoreID, invoice.ID)
}

func (q *billingService) CancelInvoice(ctx context.Context, tx pgx.Tx, storeID, invoiceID uuid.UUID, at time.Time) (*models.Invoice, error) {
	ctxt := "BillingService-CancelInvoice"
	cancelled, err := q.billingRepository.CancelInvoice(ctx, tx, storeID, invoiceID, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCancelInvoice")
		return nil, err
	}
	if !cancelled {
		return nil, helper.Conflict("Only a pending invoice can be cancelled.")
	}
	return q.FindInvoice(ctx, tx, storeID, invoiceID)
}

func (q *billingService) RedeemVoucher(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, at time.Time) (*models.VoucherRedemption, error) {
	ctxt := "BillingService-RedeemVoucher"
	response, err := q.billingRepository.UseRewardVoucher(ctx, tx, strings.TrimSpace(code), at)
	if errors.Is(err, models.ErrVoucherInvalid) {
		return nil, helper.Invalid("code", "This voucher is not valid or has been used up.")
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUseRewardVoucher")
		return nil, err
	}
	if err = q.billingRepository.CreateVoucherRedemption(ctx, tx, response.Code, storeID, at); err != nil {
		if errors.Is(err, models.ErrVoucherUsed) {
			return nil, helper.Invalid("code", "You have already used this voucher.")
		}
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateVoucherRedemption")
		return nil, err
	}
	switch response.RewardType {
	case models.VoucherRewardCoin:
		err = q.storeRepository.AddCoins(ctx, tx, storeID, response.RewardAmount)
	case models.VoucherRewardSubscriptionDays:
		err = q.storeRepository.ExtendPlan(ctx, tx, storeID, "pro", 0, int(response.RewardAmount), at)
	case models.VoucherRewardPlugin:
		err = q.storeRepository.ActivatePlugin(ctx, tx, storeID, models.PluginBusiness, 0, int(response.RewardAmount), at)
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrReward")
		return nil, err
	}
	if response.CoinBalance, err = q.storeRepository.FindCoinBalance(ctx, tx, storeID); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCoinBalance")
		return nil, err
	}
	return response, nil
}

func (q *billingService) FindPlugins(ctx context.Context, storeID uuid.UUID, lang i18n.Lang, at time.Time) ([]*models.Plugin, error) {
	ctxt := "BillingService-FindPlugins"
	subscribed, err := q.storeRepository.FindStorePlugins(ctx, nil, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStorePlugins")
		return nil, err
	}
	state := map[string]*models.StorePlugin{}
	for _, plugin := range subscribed {
		state[plugin.Code] = plugin
	}
	response := make([]*models.Plugin, len(models.PluginCatalog))
	for i, info := range models.PluginCatalog {
		plugin := &models.Plugin{
			Code:           info.Code,
			Name:           lang.T(info.Name),
			Description:    lang.T(info.Description),
			Features:       make([]string, len(info.Features)),
			PriceMonthly:   info.Monthly,
			PriceYearly:    info.Monthly * 10,
			Status:         models.PluginStatusInactive,
			TrialAvailable: true,
		}
		for j, feature := range info.Features {
			plugin.Features[j] = lang.T(feature)
		}
		if current, ok := state[info.Code]; ok {
			plugin.Status = current.Status
			plugin.TrialAvailable = !current.TrialUsed
			plugin.ExpiresAt = current.ExpiresAt
			if current.ExpiresAt != nil && current.ExpiresAt.Before(at) {
				plugin.Status = models.PluginStatusExpired
			}
		}
		response[i] = plugin
	}
	return response, nil
}

func (q *billingService) StartPluginTrial(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, code string, at time.Time) (*models.StorePlugin, error) {
	ctxt := "BillingService-StartPluginTrial"
	subscribed, err := q.storeRepository.FindStorePlugins(ctx, tx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStorePlugins")
		return nil, err
	}
	for _, plugin := range subscribed {
		if plugin.Code == code && (plugin.TrialUsed || plugin.Status == "active") {
			return nil, helper.Conflict("The free trial for this plugin was already used.")
		}
	}
	response, err := q.storeRepository.StartPluginTrial(ctx, tx, storeID, code, at.AddDate(0, 0, models.PluginTrialDays))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrStartPluginTrial")
	}
	return response, err
}

func (q *billingService) FindStorage(ctx context.Context, storeID uuid.UUID) (*models.Storage, error) {
	ctxt := "BillingService-FindStorage"
	response, err := q.storeRepository.FindStorage(ctx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStorage")
	}
	return response, err
}

func (q *billingService) BuyStorage(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, packageCode string, at time.Time) (*models.StoragePurchase, error) {
	ctxt := "BillingService-BuyStorage"
	storagePackage := models.StoragePackages[packageCode]
	if err := q.SpendCoins(ctx, tx, storeID, storagePackage.Price); err != nil {
		return nil, err
	}
	response, err := q.storeRepository.ExtendStorage(ctx, tx, storeID, storagePackage.MB, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExtendStorage")
		return nil, err
	}
	invoiceNumber, err := q.billingRepository.NextInvoiceNumber(ctx, tx, storeID, at.In(helper.Location).Format("20060102"))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextInvoiceNumber")
		return nil, err
	}
	if _, err = q.billingRepository.CreateInvoice(ctx, tx, &models.Invoice{
		StoreID:       storeID,
		InvoiceNumber: invoiceNumber,
		Kind:          models.InvoiceKindStorage,
		Description:   "Extra storage " + packageCode,
		PaymentMethod: models.PaymentMethodCoin,
		Amount:        storagePackage.Price,
		Status:        models.InvoiceStatusPaid,
		Meta:          json.RawMessage("{}"),
		PaidAt:        &at,
		CreatedAt:     at,
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateInvoice")
		return nil, err
	}
	return response, nil
}
