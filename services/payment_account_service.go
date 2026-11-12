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

type PaymentAccountService interface {
	FindPaymentAccounts(
		ctx context.Context,
		request *models.PaymentAccountFilter,
	) ([]*models.PaymentAccount, int64, error)
	FindPaymentAccount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentAccountFilter,
	) (*models.PaymentAccount, error)
	CreatePaymentAccount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentAccount,
	) (*models.PaymentAccount, error)
	UpdatePaymentAccount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentAccount,
	) (*models.PaymentAccount, error)
	DeletePaymentAccounts(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentAccountFilter,
	) error
}

type paymentAccountService struct {
	paymentAccountRepository repositories.PaymentAccountRepository
}

func NewPaymentAccountService(
	paymentAccountRepository repositories.PaymentAccountRepository,
) PaymentAccountService {
	return &paymentAccountService{
		paymentAccountRepository: paymentAccountRepository,
	}
}

func (q *paymentAccountService) FindPaymentAccounts(
	ctx context.Context,
	request *models.PaymentAccountFilter,
) ([]*models.PaymentAccount, int64, error) {
	ctxt := "PaymentAccountService-FindPaymentAccounts"
	response, total, err := q.paymentAccountRepository.FindPaymentAccounts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPaymentAccounts")
	}
	return response, total, err
}

func (q *paymentAccountService) FindPaymentAccount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentAccountFilter,
) (*models.PaymentAccount, error) {
	ctxt := "PaymentAccountService-FindPaymentAccount"
	response, err := q.paymentAccountRepository.FindPaymentAccount(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrPaymentAccountNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPaymentAccount")
	}
	return response, err
}

func (q *paymentAccountService) CreatePaymentAccount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentAccount,
) (*models.PaymentAccount, error) {
	ctxt := "PaymentAccountService-CreatePaymentAccount"
	if err := q.validate(ctx, tx, request, true); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.paymentAccountRepository.CreatePaymentAccount(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePaymentAccount")
	}
	return response, err
}

func (q *paymentAccountService) UpdatePaymentAccount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentAccount,
) (*models.PaymentAccount, error) {
	ctxt := "PaymentAccountService-UpdatePaymentAccount"
	if err := q.validate(ctx, tx, request, false); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.paymentAccountRepository.UpdatePaymentAccount(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePaymentAccount")
	}
	return response, err
}

// DeletePaymentAccounts deletes every account of the filter or none: a missing id is ErrPaymentAccountNotFound.
func (q *paymentAccountService) DeletePaymentAccounts(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentAccountFilter,
) error {
	ctxt := "PaymentAccountService-DeletePaymentAccounts"
	deleted, err := q.paymentAccountRepository.DeletePaymentAccounts(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeletePaymentAccounts")
		return err
	}
	if deleted != int64(len(request.PaymentAccountIDs)) {
		return models.ErrPaymentAccountNotFound
	}
	return nil
}
