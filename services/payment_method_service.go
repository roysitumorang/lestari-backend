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

type PaymentMethodService interface {
	FindPaymentMethods(
		ctx context.Context,
		request *models.PaymentMethodFilter,
	) ([]*models.PaymentMethod, int64, error)
	FindPaymentMethod(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentMethodFilter,
	) (*models.PaymentMethod, error)
	CreatePaymentMethod(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentMethod,
	) (*models.PaymentMethod, error)
	UpdatePaymentMethod(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentMethod,
	) (*models.PaymentMethod, error)
	DeletePaymentMethods(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PaymentMethodFilter,
	) error
}

type paymentMethodService struct {
	paymentMethodRepository repositories.PaymentMethodRepository
}

func NewPaymentMethodService(
	paymentMethodRepository repositories.PaymentMethodRepository,
) PaymentMethodService {
	return &paymentMethodService{
		paymentMethodRepository: paymentMethodRepository,
	}
}

func (q *paymentMethodService) FindPaymentMethods(
	ctx context.Context,
	request *models.PaymentMethodFilter,
) ([]*models.PaymentMethod, int64, error) {
	ctxt := "PaymentMethodService-FindPaymentMethods"
	response, total, err := q.paymentMethodRepository.FindPaymentMethods(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPaymentMethods")
	}
	return response, total, err
}

func (q *paymentMethodService) FindPaymentMethod(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentMethodFilter,
) (*models.PaymentMethod, error) {
	ctxt := "PaymentMethodService-FindPaymentMethod"
	response, err := q.paymentMethodRepository.FindPaymentMethod(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrPaymentMethodNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPaymentMethod")
	}
	return response, err
}

func (q *paymentMethodService) CreatePaymentMethod(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentMethod,
) (*models.PaymentMethod, error) {
	ctxt := "PaymentMethodService-CreatePaymentMethod"
	if err := q.validate(ctx, tx, request, true); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.paymentMethodRepository.CreatePaymentMethod(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePaymentMethod")
	}
	return response, err
}

func (q *paymentMethodService) UpdatePaymentMethod(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentMethod,
) (*models.PaymentMethod, error) {
	ctxt := "PaymentMethodService-UpdatePaymentMethod"
	if err := q.validate(ctx, tx, request, false); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.paymentMethodRepository.UpdatePaymentMethod(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePaymentMethod")
	}
	return response, err
}

// DeletePaymentMethods deletes every payment method of the filter or none: a missing id is ErrPaymentMethodNotFound.
func (q *paymentMethodService) DeletePaymentMethods(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PaymentMethodFilter,
) error {
	ctxt := "PaymentMethodService-DeletePaymentMethods"
	deleted, err := q.paymentMethodRepository.DeletePaymentMethods(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeletePaymentMethods")
		return err
	}
	if deleted != int64(len(request.PaymentMethodIDs)) {
		return models.ErrPaymentMethodNotFound
	}
	return nil
}
