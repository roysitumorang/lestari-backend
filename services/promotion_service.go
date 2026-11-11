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

type PromotionService interface {
	FindPromotions(
		ctx context.Context,
		request *models.PromotionFilter,
	) ([]*models.Promotion, int64, error)
	FindPromotion(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PromotionFilter,
	) (*models.Promotion, error)
	CreatePromotion(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Promotion,
	) (*models.Promotion, error)
	UpdatePromotion(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Promotion,
	) (*models.Promotion, error)
	DeletePromotions(
		ctx context.Context,
		tx pgx.Tx,
		request *models.PromotionFilter,
	) error
}

type promotionService struct {
	promotionRepository repositories.PromotionRepository
}

func NewPromotionService(
	promotionRepository repositories.PromotionRepository,
) PromotionService {
	return &promotionService{
		promotionRepository: promotionRepository,
	}
}

func (q *promotionService) FindPromotions(
	ctx context.Context,
	request *models.PromotionFilter,
) ([]*models.Promotion, int64, error) {
	ctxt := "PromotionService-FindPromotions"
	response, total, err := q.promotionRepository.FindPromotions(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPromotions")
	}
	return response, total, err
}

func (q *promotionService) FindPromotion(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PromotionFilter,
) (*models.Promotion, error) {
	ctxt := "PromotionService-FindPromotion"
	response, err := q.promotionRepository.FindPromotion(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrPromotionNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPromotion")
	}
	return response, err
}

func (q *promotionService) CreatePromotion(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Promotion,
) (*models.Promotion, error) {
	ctxt := "PromotionService-CreatePromotion"
	if err := q.validate(ctx, tx, request, true); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.promotionRepository.CreatePromotion(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreatePromotion")
	}
	return response, err
}

func (q *promotionService) UpdatePromotion(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Promotion,
) (*models.Promotion, error) {
	ctxt := "PromotionService-UpdatePromotion"
	if err := q.validate(ctx, tx, request, false); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidate")
		return nil, err
	}
	response, err := q.promotionRepository.UpdatePromotion(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePromotion")
	}
	return response, err
}

// DeletePromotions deletes every promotion of the filter or none: a missing id is ErrPromotionNotFound.
func (q *promotionService) DeletePromotions(
	ctx context.Context,
	tx pgx.Tx,
	request *models.PromotionFilter,
) error {
	ctxt := "PromotionService-DeletePromotions"
	deleted, err := q.promotionRepository.DeletePromotions(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeletePromotions")
		return err
	}
	if deleted != int64(len(request.PromotionIDs)) {
		return models.ErrPromotionNotFound
	}
	return nil
}
