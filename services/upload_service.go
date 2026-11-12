package services

import (
	"context"
	"log/slog"
	"uuid"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type UploadService interface {
	// CreateUpload records a file that was saved to the upload directory.
	CreateUpload(ctx context.Context, request *models.Upload) (*models.Upload, error)
}

type uploadService struct {
	uploadRepository repositories.UploadRepository
}

func NewUploadService(
	uploadRepository repositories.UploadRepository,
) UploadService {
	return &uploadService{
		uploadRepository: uploadRepository,
	}
}

func (q *uploadService) CreateUpload(ctx context.Context, request *models.Upload) (*models.Upload, error) {
	ctxt := "UploadService-CreateUpload"
	var err error
	if request.ID, err = q.uploadRepository.CreateUpload(ctx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateUpload")
		return nil, err
	}
	return request, nil
}

type RegionService interface {
	FindProvinces(ctx context.Context, countryCode, keyword string) ([]*models.Region, error)
	FindCities(ctx context.Context, provinceID *uuid.UUID, keyword string) ([]*models.Region, error)
}

type regionService struct {
	regionRepository repositories.RegionRepository
}

func NewRegionService(
	regionRepository repositories.RegionRepository,
) RegionService {
	return &regionService{
		regionRepository: regionRepository,
	}
}

func (q *regionService) FindProvinces(ctx context.Context, countryCode, keyword string) ([]*models.Region, error) {
	ctxt := "RegionService-FindProvinces"
	response, err := q.regionRepository.FindProvinces(ctx, countryCode, keyword)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProvinces")
	}
	return response, err
}

func (q *regionService) FindCities(ctx context.Context, provinceID *uuid.UUID, keyword string) ([]*models.Region, error) {
	ctxt := "RegionService-FindCities"
	response, err := q.regionRepository.FindCities(ctx, provinceID, keyword)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCities")
	}
	return response, err
}
