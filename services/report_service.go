package services

import (
	"context"
	"log/slog"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type ReportService interface {
	FindProfitLoss(ctx context.Context, f *models.ReportFilter) ([]*models.ProfitLossRow, *models.ProfitLossSummary, int64, error)
	FindCashFlow(ctx context.Context, f *models.ReportFilter) ([]*models.CashFlowRow, *models.CashFlowSummary, int64, error)
	FindDailySales(ctx context.Context, f *models.ReportFilter) ([]*models.DailySalesRow, *models.DailySalesSummary, int64, error)
	FindSalesTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.ReportSale, *models.SalesTransactionsSummary, int64, error)
	FindSalesByProduct(ctx context.Context, f *models.ReportFilter) ([]*models.SalesByProductRow, *models.SalesByProductSummary, int64, error)
	FindSalesByCategory(ctx context.Context, f *models.ReportFilter) ([]*models.SalesByCategoryRow, *models.SalesByCategorySummary, int64, error)
	FindDailyPurchases(ctx context.Context, f *models.ReportFilter) ([]*models.DailyPurchasesRow, *models.DailyPurchasesSummary, int64, error)
	FindPurchaseTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.PurchaseTransactionsRow, *models.PurchaseTransactionsSummary, int64, error)
	FindCustomerPoints(ctx context.Context, f *models.ReportFilter) ([]*models.CustomerPointsRow, *models.CustomerPointsSummary, int64, error)
	FindPointRedemptions(ctx context.Context, f *models.ReportFilter) ([]*models.PointRedemptionsRow, *models.PointRedemptionsSummary, int64, error)
	FindPointEarnings(ctx context.Context, f *models.ReportFilter) ([]*models.PointEarningsRow, *models.PointEarningsSummary, int64, error)
	FindCapital(ctx context.Context, f *models.ReportFilter) ([]*models.CapitalRow, *models.CapitalSummary, int64, error)
	FindSaleFees(ctx context.Context, f *models.ReportFilter) ([]*models.FeeRow, *models.FeeSummary, int64, error)
	FindPurchaseFees(ctx context.Context, f *models.ReportFilter) ([]*models.FeeRow, *models.FeeSummary, int64, error)
	FindMDRFees(ctx context.Context, f *models.ReportFilter) ([]*models.MDRFeeRow, *models.FeeSummary, int64, error)
	FindTaxes(ctx context.Context, f *models.ReportFilter) ([]*models.TaxReportRow, *models.TaxReportSummary, int64, error)
	FindPPOB(ctx context.Context, f *models.ReportFilter) ([]*models.PPOBReportRow, *models.PPOBReportSummary, int64, error)
	FindVisitors(ctx context.Context, f *models.ReportFilter) ([]*models.VisitorsRow, *models.VisitorsSummary, int64, error)
	FindPromotionTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.PromotionTransactionsRow, *models.PromotionTransactionsSummary, int64, error)
	FindPromotionProducts(ctx context.Context, f *models.ReportFilter) ([]*models.PromotionProductsRow, *models.PromotionProductsSummary, int64, error)
	FindStoreRatings(ctx context.Context, f *models.ReportFilter) ([]*models.StoreRatingRow, *models.StoreRatingSummary, int64, error)
	FindBranches(ctx context.Context, f *models.ReportFilter) ([]*models.BranchReportRow, *models.BranchReportSummary, int64, error)
	FindStockTransfers(ctx context.Context, f *models.ReportFilter) ([]*models.StockTransferReportRow, *models.ReportNoSummary, int64, error)
	FindStockTransferConfirmations(ctx context.Context, f *models.ReportFilter) ([]*models.StockTransferReportRow, *models.ReportNoSummary, int64, error)
	reportDetails
}

type reportService struct {
	reportRepository repositories.ReportRepository
}

func NewReportService(
	reportRepository repositories.ReportRepository,
) ReportService {
	return &reportService{
		reportRepository: reportRepository,
	}
}

func (q *reportService) FindProfitLoss(ctx context.Context, f *models.ReportFilter) ([]*models.ProfitLossRow, *models.ProfitLossSummary, int64, error) {
	ctxt := "ReportService-FindProfitLoss"
	response, summary, total, err := q.reportRepository.FindProfitLoss(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProfitLoss")
	}
	return response, summary, total, err
}

func (q *reportService) FindCashFlow(ctx context.Context, f *models.ReportFilter) ([]*models.CashFlowRow, *models.CashFlowSummary, int64, error) {
	ctxt := "ReportService-FindCashFlow"
	response, summary, total, err := q.reportRepository.FindCashFlow(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCashFlow")
	}
	return response, summary, total, err
}

func (q *reportService) FindDailySales(ctx context.Context, f *models.ReportFilter) ([]*models.DailySalesRow, *models.DailySalesSummary, int64, error) {
	ctxt := "ReportService-FindDailySales"
	response, summary, total, err := q.reportRepository.FindDailySales(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDailySales")
	}
	return response, summary, total, err
}

func (q *reportService) FindSalesTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.ReportSale, *models.SalesTransactionsSummary, int64, error) {
	ctxt := "ReportService-FindSalesTransactions"
	response, summary, total, err := q.reportRepository.FindSalesTransactions(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSalesTransactions")
	}
	return response, summary, total, err
}

func (q *reportService) FindSalesByProduct(ctx context.Context, f *models.ReportFilter) ([]*models.SalesByProductRow, *models.SalesByProductSummary, int64, error) {
	ctxt := "ReportService-FindSalesByProduct"
	response, summary, total, err := q.reportRepository.FindSalesByProduct(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSalesByProduct")
	}
	return response, summary, total, err
}

func (q *reportService) FindSalesByCategory(ctx context.Context, f *models.ReportFilter) ([]*models.SalesByCategoryRow, *models.SalesByCategorySummary, int64, error) {
	ctxt := "ReportService-FindSalesByCategory"
	response, summary, total, err := q.reportRepository.FindSalesByCategory(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSalesByCategory")
	}
	return response, summary, total, err
}

func (q *reportService) FindDailyPurchases(ctx context.Context, f *models.ReportFilter) ([]*models.DailyPurchasesRow, *models.DailyPurchasesSummary, int64, error) {
	ctxt := "ReportService-FindDailyPurchases"
	response, summary, total, err := q.reportRepository.FindDailyPurchases(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDailyPurchases")
	}
	return response, summary, total, err
}

func (q *reportService) FindPurchaseTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.PurchaseTransactionsRow, *models.PurchaseTransactionsSummary, int64, error) {
	ctxt := "ReportService-FindPurchaseTransactions"
	response, summary, total, err := q.reportRepository.FindPurchaseTransactions(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchaseTransactions")
	}
	return response, summary, total, err
}

func (q *reportService) FindCustomerPoints(ctx context.Context, f *models.ReportFilter) ([]*models.CustomerPointsRow, *models.CustomerPointsSummary, int64, error) {
	ctxt := "ReportService-FindCustomerPoints"
	response, summary, total, err := q.reportRepository.FindCustomerPoints(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomerPoints")
	}
	return response, summary, total, err
}

func (q *reportService) FindPointRedemptions(ctx context.Context, f *models.ReportFilter) ([]*models.PointRedemptionsRow, *models.PointRedemptionsSummary, int64, error) {
	ctxt := "ReportService-FindPointRedemptions"
	response, summary, total, err := q.reportRepository.FindPointRedemptions(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPointRedemptions")
	}
	return response, summary, total, err
}

func (q *reportService) FindPointEarnings(ctx context.Context, f *models.ReportFilter) ([]*models.PointEarningsRow, *models.PointEarningsSummary, int64, error) {
	ctxt := "ReportService-FindPointEarnings"
	response, summary, total, err := q.reportRepository.FindPointEarnings(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPointEarnings")
	}
	return response, summary, total, err
}

func (q *reportService) FindCapital(ctx context.Context, f *models.ReportFilter) ([]*models.CapitalRow, *models.CapitalSummary, int64, error) {
	ctxt := "ReportService-FindCapital"
	response, summary, total, err := q.reportRepository.FindCapital(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCapital")
	}
	return response, summary, total, err
}

func (q *reportService) FindSaleFees(ctx context.Context, f *models.ReportFilter) ([]*models.FeeRow, *models.FeeSummary, int64, error) {
	ctxt := "ReportService-FindSaleFees"
	response, summary, total, err := q.reportRepository.FindSaleFees(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSaleFees")
	}
	return response, summary, total, err
}

func (q *reportService) FindPurchaseFees(ctx context.Context, f *models.ReportFilter) ([]*models.FeeRow, *models.FeeSummary, int64, error) {
	ctxt := "ReportService-FindPurchaseFees"
	response, summary, total, err := q.reportRepository.FindPurchaseFees(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPurchaseFees")
	}
	return response, summary, total, err
}

func (q *reportService) FindMDRFees(ctx context.Context, f *models.ReportFilter) ([]*models.MDRFeeRow, *models.FeeSummary, int64, error) {
	ctxt := "ReportService-FindMDRFees"
	response, summary, total, err := q.reportRepository.FindMDRFees(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindMDRFees")
	}
	return response, summary, total, err
}

func (q *reportService) FindTaxes(ctx context.Context, f *models.ReportFilter) ([]*models.TaxReportRow, *models.TaxReportSummary, int64, error) {
	ctxt := "ReportService-FindTaxes"
	response, summary, total, err := q.reportRepository.FindTaxes(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindTaxes")
	}
	return response, summary, total, err
}

func (q *reportService) FindPPOB(ctx context.Context, f *models.ReportFilter) ([]*models.PPOBReportRow, *models.PPOBReportSummary, int64, error) {
	ctxt := "ReportService-FindPPOB"
	response, summary, total, err := q.reportRepository.FindPPOB(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPPOB")
	}
	return response, summary, total, err
}

func (q *reportService) FindVisitors(ctx context.Context, f *models.ReportFilter) ([]*models.VisitorsRow, *models.VisitorsSummary, int64, error) {
	ctxt := "ReportService-FindVisitors"
	response, summary, total, err := q.reportRepository.FindVisitors(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindVisitors")
	}
	return response, summary, total, err
}

func (q *reportService) FindPromotionTransactions(ctx context.Context, f *models.ReportFilter) ([]*models.PromotionTransactionsRow, *models.PromotionTransactionsSummary, int64, error) {
	ctxt := "ReportService-FindPromotionTransactions"
	response, summary, total, err := q.reportRepository.FindPromotionTransactions(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPromotionTransactions")
	}
	return response, summary, total, err
}

func (q *reportService) FindPromotionProducts(ctx context.Context, f *models.ReportFilter) ([]*models.PromotionProductsRow, *models.PromotionProductsSummary, int64, error) {
	ctxt := "ReportService-FindPromotionProducts"
	response, summary, total, err := q.reportRepository.FindPromotionProducts(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPromotionProducts")
	}
	return response, summary, total, err
}

func (q *reportService) FindStoreRatings(ctx context.Context, f *models.ReportFilter) ([]*models.StoreRatingRow, *models.StoreRatingSummary, int64, error) {
	ctxt := "ReportService-FindStoreRatings"
	response, summary, total, err := q.reportRepository.FindStoreRatings(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStoreRatings")
	}
	return response, summary, total, err
}

func (q *reportService) FindBranches(ctx context.Context, f *models.ReportFilter) ([]*models.BranchReportRow, *models.BranchReportSummary, int64, error) {
	ctxt := "ReportService-FindBranches"
	response, summary, total, err := q.reportRepository.FindBranches(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranches")
	}
	return response, summary, total, err
}

func (q *reportService) FindStockTransfers(ctx context.Context, f *models.ReportFilter) ([]*models.StockTransferReportRow, *models.ReportNoSummary, int64, error) {
	ctxt := "ReportService-FindStockTransfers"
	response, summary, total, err := q.reportRepository.FindStockTransfers(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransfers")
	}
	return response, summary, total, err
}

func (q *reportService) FindStockTransferConfirmations(ctx context.Context, f *models.ReportFilter) ([]*models.StockTransferReportRow, *models.ReportNoSummary, int64, error) {
	ctxt := "ReportService-FindStockTransferConfirmations"
	response, summary, total, err := q.reportRepository.FindStockTransferConfirmations(ctx, f)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransferConfirmations")
	}
	return response, summary, total, err
}
