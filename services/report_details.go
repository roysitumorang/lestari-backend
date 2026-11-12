package services

import (
	"context"
	"errors"
	"log/slog"
	"uuid"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// reportDetails are the detail lists of the reports and the dashboard.
type reportDetails interface {
	FindSalesOfDay(ctx context.Context, branchID uuid.UUID, day string) ([]*models.ReportSale, error)
	FindSalesOf(ctx context.Context, f *models.ReportFilter, kind string, id uuid.UUID) ([]*models.ReportSale, error)
	FindStoreRating(ctx context.Context, branchID, ratingID uuid.UUID) (*models.StoreRatingRow, error)
	// FindDashboard gathers the day of the dashboard; the alerts are added by the caller, who knows the language.
	FindDashboard(ctx context.Context, f *models.DashboardFilter) (*models.Dashboard, error)
	FindStoreUsage(ctx context.Context, storeID uuid.UUID) (*models.StoreUsage, error)
}

func (q *reportService) FindSalesOfDay(ctx context.Context, branchID uuid.UUID, day string) ([]*models.ReportSale, error) {
	ctxt := "ReportService-FindSalesOfDay"
	response, err := q.reportRepository.FindSalesOfDay(ctx, branchID, day)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSalesOfDay")
	}
	return response, err
}

func (q *reportService) FindSalesOf(ctx context.Context, f *models.ReportFilter, kind string, id uuid.UUID) ([]*models.ReportSale, error) {
	ctxt := "ReportService-FindSalesOf"
	response, err := q.reportRepository.FindSalesOf(ctx, f, kind, id)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSalesOf")
	}
	return response, err
}

func (q *reportService) FindStoreRating(ctx context.Context, branchID, ratingID uuid.UUID) (*models.StoreRatingRow, error) {
	ctxt := "ReportService-FindStoreRating"
	response, err := q.reportRepository.FindStoreRating(ctx, branchID, ratingID)
	if err != nil && !errors.Is(err, models.ErrStoreRatingNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStoreRating")
	}
	return response, err
}

func (q *reportService) FindStoreUsage(ctx context.Context, storeID uuid.UUID) (*models.StoreUsage, error) {
	ctxt := "ReportService-FindStoreUsage"
	response, err := q.reportRepository.FindStoreUsage(ctx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStoreUsage")
	}
	return response, err
}

// percentChange compares a value with the one before; nil when there is no baseline.
func percentChange(current, previous float64) *float64 {
	if previous == 0 {
		if current == 0 {
			return new(0.0)
		}
		return nil
	}
	return new(round2((current - previous) / previous * 100))
}

func (q *reportService) FindDashboard(ctx context.Context, f *models.DashboardFilter) (*models.Dashboard, error) {
	ctxt := "ReportService-FindDashboard"
	current, err := q.reportRepository.FindDashboardTotals(ctx, f, f.Date)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDashboardTotals")
		return nil, err
	}
	previous, err := q.reportRepository.FindDashboardTotals(ctx, f, f.PreviousDate)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDashboardTotals")
		return nil, err
	}
	response := &models.Dashboard{
		Date: f.Date,
		Summary: &models.DashboardSummary{
			DashboardTotals: *current,
			ComparedToPrevious: models.DashboardChange{
				TransactionCount: percentChange(float64(current.TransactionCount), float64(previous.TransactionCount)),
				Revenue:          percentChange(current.Revenue, previous.Revenue),
				Profit:           percentChange(current.Profit, previous.Profit),
			},
		},
		Alerts: []*models.DashboardAlert{},
	}
	if response.Hourly, err = q.reportRepository.FindDashboardHours(ctx, f); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDashboardHours")
		return nil, err
	}
	if response.TopProducts, err = q.reportRepository.FindTopProducts(ctx, f); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindTopProducts")
		return nil, err
	}
	if response.LowStock, err = q.reportRepository.FindLowStockProducts(ctx, f.BranchID); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindLowStockProducts")
		return nil, err
	}
	return response, nil
}
