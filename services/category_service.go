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

type CategoryService interface {
	FindCategories(
		ctx context.Context,
		request *models.CategoryFilter,
	) ([]*models.Category, int64, error)
	FindCategory(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CategoryFilter,
	) (*models.Category, error)
	CreateCategory(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Category,
	) (*models.Category, error)
	UpdateCategory(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Category,
	) (*models.Category, error)
	DeleteCategories(
		ctx context.Context,
		tx pgx.Tx,
		request *models.CategoryFilter,
	) error
}

type categoryService struct {
	categoryRepository repositories.CategoryRepository
}

func NewCategoryService(
	categoryRepository repositories.CategoryRepository,
) CategoryService {
	return &categoryService{
		categoryRepository: categoryRepository,
	}
}

func (q *categoryService) FindCategories(
	ctx context.Context,
	request *models.CategoryFilter,
) ([]*models.Category, int64, error) {
	ctxt := "CategoryService-FindCategories"
	response, total, err := q.categoryRepository.FindCategories(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCategories")
	}
	return response, total, err
}

func (q *categoryService) FindCategory(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CategoryFilter,
) (*models.Category, error) {
	ctxt := "CategoryService-FindCategory"
	response, err := q.categoryRepository.FindCategory(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrCategoryNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCategory")
	}
	return response, err
}

func (q *categoryService) CreateCategory(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Category,
) (*models.Category, error) {
	ctxt := "CategoryService-CreateCategory"
	response, err := q.categoryRepository.CreateCategory(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateCategory")
	}
	return response, err
}

func (q *categoryService) UpdateCategory(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Category,
) (*models.Category, error) {
	ctxt := "CategoryService-UpdateCategory"
	response, err := q.categoryRepository.UpdateCategory(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateCategory")
	}
	return response, err
}

// DeleteCategories deletes every category of the filter or none: a missing id is ErrCategoryNotFound.
func (q *categoryService) DeleteCategories(
	ctx context.Context,
	tx pgx.Tx,
	request *models.CategoryFilter,
) error {
	ctxt := "CategoryService-DeleteCategories"
	deleted, err := q.categoryRepository.DeleteCategories(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteCategories")
		return err
	}
	if deleted != int64(len(request.CategoryIDs)) {
		return models.ErrCategoryNotFound
	}
	return nil
}
