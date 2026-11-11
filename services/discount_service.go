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

type DiscountService interface {
	FindDiscounts(
		ctx context.Context,
		request *models.DiscountFilter,
	) ([]*models.Discount, int64, error)
	FindDiscount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.DiscountFilter,
	) (*models.Discount, error)
	CreateDiscount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Discount,
	) (*models.Discount, error)
	UpdateDiscount(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Discount,
	) (*models.Discount, error)
	DeleteDiscounts(
		ctx context.Context,
		tx pgx.Tx,
		request *models.DiscountFilter,
	) error
}

type discountService struct {
	discountRepository repositories.DiscountRepository
}

func NewDiscountService(
	discountRepository repositories.DiscountRepository,
) DiscountService {
	return &discountService{
		discountRepository: discountRepository,
	}
}

func (q *discountService) FindDiscounts(
	ctx context.Context,
	request *models.DiscountFilter,
) ([]*models.Discount, int64, error) {
	ctxt := "DiscountService-FindDiscounts"
	response, total, err := q.discountRepository.FindDiscounts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDiscounts")
	}
	return response, total, err
}

func (q *discountService) FindDiscount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.DiscountFilter,
) (*models.Discount, error) {
	ctxt := "DiscountService-FindDiscount"
	response, err := q.discountRepository.FindDiscount(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrDiscountNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDiscount")
	}
	return response, err
}

func (q *discountService) CreateDiscount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Discount,
) (*models.Discount, error) {
	ctxt := "DiscountService-CreateDiscount"
	if err := q.validate(ctx, tx, request, true); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.discountRepository.CreateDiscount(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateDiscount")
	}
	return response, err
}

func (q *discountService) UpdateDiscount(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Discount,
) (*models.Discount, error) {
	ctxt := "DiscountService-UpdateDiscount"
	if err := q.validate(ctx, tx, request, false); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.discountRepository.UpdateDiscount(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateDiscount")
	}
	return response, err
}

// DeleteDiscounts deletes every discount of the filter or none: a missing id is ErrDiscountNotFound.
func (q *discountService) DeleteDiscounts(
	ctx context.Context,
	tx pgx.Tx,
	request *models.DiscountFilter,
) error {
	ctxt := "DiscountService-DeleteDiscounts"
	deleted, err := q.discountRepository.DeleteDiscounts(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteDiscounts")
		return err
	}
	if deleted != int64(len(request.DiscountIDs)) {
		return models.ErrDiscountNotFound
	}
	return nil
}
