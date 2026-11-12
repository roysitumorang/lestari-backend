// Package router wires the application: database pools, repositories, services and controllers.
package router

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/config"
	"github.com/roysitumorang/lestari-backend/controllers"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/migrations"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/seeds"
	"github.com/roysitumorang/lestari-backend/services"
)

// Service is the composition root: everything a controller can ask for.
type Service struct {
	Config    config.Config
	DbRead    *pgxpool.Pool
	DbWrite   *pgxpool.Pool
	Migration *migrations.Migration
	// AuthService issues and checks the access tokens; LoginService signs users in and registers stores.
	AuthService           *middlewares.Service
	Uploader              *controllers.Uploader
	TaxService            services.TaxService
	CategoryService       services.CategoryService
	CashDrawerService     services.CashDrawerService
	SupplierService       services.SupplierService
	CustomerTypeService   services.CustomerTypeService
	CustomerService       services.CustomerService
	FinanceEntryService   services.FinanceEntryService
	DiscountService       services.DiscountService
	FeeService            services.FeeService
	PaymentMethodService  services.PaymentMethodService
	PromotionService      services.PromotionService
	CommissionService     services.CommissionService
	JobService            services.JobService
	ProductService        services.ProductService
	StockService          services.StockService
	StockOpnameService    services.StockOpnameService
	StockTransferService  services.StockTransferService
	BranchProductService  services.BranchProductService
	NotificationService   services.NotificationService
	ActivityService       services.ActivityService
	OTPService            services.OTPService
	UploadService         services.UploadService
	RegionService         services.RegionService
	SettingService        services.SettingService
	LoginService          services.AuthService
	AccountService        services.AccountService
	BillingService        services.BillingService
	PPOBService           services.PPOBService
	PointService          services.PointService
	BranchService         services.BranchService
	StaffService          services.StaffService
	EngagementService     services.EngagementService
	DataResetService      services.DataResetService
	SaleService           services.SaleService
	PurchaseService       services.PurchaseService
	DebtService           services.DebtService
	ReturnService         services.ReturnService
	ShiftService          services.ShiftService
	OrderService          services.OrderService
	PaymentAccountService services.PaymentAccountService
	ReportService         services.ReportService
}

// MakeHandler connects to the database and builds repositories and services.
func MakeHandler(ctx context.Context, cfg config.Config) (*Service, error) {
	ctxt := "Router-MakeHandler"
	dbWrite, err := config.CreateDbConnection(ctx, cfg.DatabaseURL, cfg.MaxConns, cfg.Timezone)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrCreateDbConnection")
		return nil, err
	}
	dbRead := dbWrite
	if cfg.DatabaseReadURL != cfg.DatabaseURL {
		if dbRead, err = config.CreateDbConnection(ctx, cfg.DatabaseReadURL, cfg.MaxConns, cfg.Timezone); err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrCreateDbConnection")
			dbWrite.Close()
			return nil, err
		}
	}
	return NewService(cfg, dbRead, dbWrite), nil
}

// NewService builds the service graph on existing pools (nil pools are enough to list the routes).
func NewService(cfg config.Config, dbRead, dbWrite *pgxpool.Pool) *Service {
	helper.AppName = config.AppName
	if location, err := time.LoadLocation(cfg.Timezone); err == nil {
		helper.Location = location
	}
	repositories.SetDbWrite(dbWrite)
	authService := &middlewares.Service{DB: dbWrite, Secret: []byte(cfg.JWTSecret), AccessTTL: cfg.AccessTokenTTL, RefreshTTL: cfg.RefreshTokenTTL}
	// outgoing messages and payment links: plug an SMS / e-mail provider and a payment gateway in here
	var (
		sender   services.Sender   = services.LogSender{}
		payments services.Payments = services.ManualPayments{}
	)
	taxRepository := repositories.NewTaxRepository(dbRead, dbWrite)
	categoryRepository := repositories.NewCategoryRepository(dbRead, dbWrite)
	cashDrawerRepository := repositories.NewCashDrawerRepository(dbRead, dbWrite)
	supplierRepository := repositories.NewSupplierRepository(dbRead, dbWrite)
	customerTypeRepository := repositories.NewCustomerTypeRepository(dbRead, dbWrite)
	customerRepository := repositories.NewCustomerRepository(dbRead, dbWrite)
	financeEntryRepository := repositories.NewFinanceEntryRepository(dbRead, dbWrite)
	discountRepository := repositories.NewDiscountRepository(dbRead, dbWrite)
	feeRepository := repositories.NewFeeRepository(dbRead, dbWrite)
	paymentMethodRepository := repositories.NewPaymentMethodRepository(dbRead, dbWrite)
	promotionRepository := repositories.NewPromotionRepository(dbRead, dbWrite)
	commissionRepository := repositories.NewCommissionRepository(dbRead, dbWrite)
	jobRepository := repositories.NewJobRepository(dbRead, dbWrite)
	notificationRepository := repositories.NewNotificationRepository(dbRead, dbWrite)
	productRepository := repositories.NewProductRepository(dbRead, dbWrite)
	stockRepository := repositories.NewStockRepository(dbRead, dbWrite)
	stockOpnameRepository := repositories.NewStockOpnameRepository(dbRead, dbWrite)
	stockTransferRepository := repositories.NewStockTransferRepository(dbRead, dbWrite)
	branchProductRepository := repositories.NewBranchProductRepository(dbRead, dbWrite)
	userRepository := repositories.NewUserRepository(dbRead, dbWrite)
	storeRepository := repositories.NewStoreRepository(dbRead, dbWrite)
	settingRepository := repositories.NewSettingRepository(dbRead, dbWrite)
	otpRepository := repositories.NewOTPRepository(dbRead, dbWrite)
	activityRepository := repositories.NewActivityRepository(dbRead, dbWrite)
	uploadRepository := repositories.NewUploadRepository(dbRead, dbWrite)
	regionRepository := repositories.NewRegionRepository(dbRead, dbWrite)
	billingRepository := repositories.NewBillingRepository(dbRead, dbWrite)
	ppobRepository := repositories.NewPPOBRepository(dbRead, dbWrite)
	pointRepository := repositories.NewPointRepository(dbRead, dbWrite)
	branchRepository := repositories.NewBranchRepository(dbRead, dbWrite)
	staffRepository := repositories.NewStaffRepository(dbRead, dbWrite)
	engagementRepository := repositories.NewEngagementRepository(dbRead, dbWrite)
	saleRepository := repositories.NewSaleRepository(dbRead, dbWrite)
	purchaseRepository := repositories.NewPurchaseRepository(dbRead, dbWrite)
	debtRepository := repositories.NewDebtRepository(dbRead, dbWrite)
	returnRepository := repositories.NewReturnRepository(dbRead, dbWrite)
	shiftRepository := repositories.NewShiftRepository(dbRead, dbWrite)
	orderRepository := repositories.NewOrderRepository(dbRead, dbWrite)
	paymentAccountRepository := repositories.NewPaymentAccountRepository(dbRead, dbWrite)
	reportRepository := repositories.NewReportRepository(dbRead, dbWrite)
	taxService := services.NewTaxService(taxRepository)
	categoryService := services.NewCategoryService(categoryRepository)
	cashDrawerService := services.NewCashDrawerService(cashDrawerRepository)
	supplierService := services.NewSupplierService(supplierRepository)
	customerTypeService := services.NewCustomerTypeService(customerTypeRepository)
	customerService := services.NewCustomerService(customerRepository)
	financeEntryService := services.NewFinanceEntryService(financeEntryRepository)
	discountService := services.NewDiscountService(discountRepository)
	feeService := services.NewFeeService(feeRepository)
	paymentMethodService := services.NewPaymentMethodService(paymentMethodRepository)
	promotionService := services.NewPromotionService(promotionRepository)
	commissionService := services.NewCommissionService(commissionRepository)
	jobService := services.NewJobService(jobRepository)
	productService := services.NewProductService(productRepository, stockRepository)
	stockService := services.NewStockService(stockRepository, productRepository, jobService)
	stockOpnameService := services.NewStockOpnameService(stockOpnameRepository, productRepository, stockRepository)
	stockTransferService := services.NewStockTransferService(stockTransferRepository, productRepository, stockRepository, notificationRepository)
	branchProductService := services.NewBranchProductService(branchProductRepository)
	notificationService := services.NewNotificationService(notificationRepository)
	activityService := services.NewActivityService(activityRepository)
	otpService := services.NewOTPService(otpRepository)
	uploadService := services.NewUploadService(uploadRepository)
	regionService := services.NewRegionService(regionRepository)
	settingService := services.NewSettingService(settingRepository)
	loginService := services.NewAuthService(authService, userRepository, storeRepository, settingRepository)
	accountService := services.NewAccountService(userRepository, otpService, sender)
	billingService := services.NewBillingService(billingRepository, storeRepository, notificationRepository, payments)
	ppobService := services.NewPPOBService(ppobRepository, payments)
	pointService := services.NewPointService(pointRepository, customerRepository)
	branchService := services.NewBranchService(branchRepository, storeRepository, notificationRepository, jobService)
	staffService := services.NewStaffService(staffRepository, sender)
	engagementService := services.NewEngagementService(engagementRepository, storeRepository)
	dataResetService := services.NewDataResetService(storeRepository, notificationRepository, jobService)
	saleService := services.NewSaleService(saleRepository, stockRepository, pointRepository, debtRepository, settingService)
	purchaseService := services.NewPurchaseService(purchaseRepository, stockRepository, debtRepository)
	debtService := services.NewDebtService(debtRepository)
	returnService := services.NewReturnService(returnRepository, saleRepository, stockRepository, debtRepository)
	shiftService := services.NewShiftService(shiftRepository)
	orderService := services.NewOrderService(orderRepository, saleRepository, stockRepository, notificationRepository, settingService)
	paymentAccountService := services.NewPaymentAccountService(paymentAccountRepository)
	reportService := services.NewReportService(reportRepository)
	return &Service{
		Config:                cfg,
		DbRead:                dbRead,
		DbWrite:               dbWrite,
		Migration:             migrations.NewMigration(dbWrite),
		AuthService:           authService,
		Uploader:              controllers.NewUploader(cfg, uploadService),
		TaxService:            taxService,
		CategoryService:       categoryService,
		CashDrawerService:     cashDrawerService,
		SupplierService:       supplierService,
		CustomerTypeService:   customerTypeService,
		CustomerService:       customerService,
		FinanceEntryService:   financeEntryService,
		DiscountService:       discountService,
		FeeService:            feeService,
		PaymentMethodService:  paymentMethodService,
		PromotionService:      promotionService,
		CommissionService:     commissionService,
		JobService:            jobService,
		ProductService:        productService,
		StockService:          stockService,
		StockOpnameService:    stockOpnameService,
		StockTransferService:  stockTransferService,
		BranchProductService:  branchProductService,
		NotificationService:   notificationService,
		ActivityService:       activityService,
		OTPService:            otpService,
		UploadService:         uploadService,
		RegionService:         regionService,
		SettingService:        settingService,
		LoginService:          loginService,
		AccountService:        accountService,
		BillingService:        billingService,
		PPOBService:           ppobService,
		PointService:          pointService,
		BranchService:         branchService,
		StaffService:          staffService,
		EngagementService:     engagementService,
		DataResetService:      dataResetService,
		SaleService:           saleService,
		PurchaseService:       purchaseService,
		DebtService:           debtService,
		ReturnService:         returnService,
		ShiftService:          shiftService,
		OrderService:          orderService,
		PaymentAccountService: paymentAccountService,
		ReportService:         reportService,
	}
}

// SeedDeps is what the demo seeder needs from the application.
func (q *Service) SeedDeps() seeds.Deps {
	return seeds.Deps{DB: q.DbWrite, Cfg: q.Config, Auth: q.AuthService, LoginService: q.LoginService}
}

// Close releases the database pools.
func (q *Service) Close() {
	if q.DbRead != nil && q.DbRead != q.DbWrite {
		q.DbRead.Close()
	}
	if q.DbWrite != nil {
		q.DbWrite.Close()
	}
}

var routeConstraint = regexp.MustCompile(`<[^>]*>`)

// Routes lists "METHOD /path" for every endpoint under the API prefix.
func (q *Service) Routes(app *fiber.App) []string {
	var out []string
	for _, r := range app.GetRoutes(true) {
		if r.Method == fiber.MethodHead || r.Method == fiber.MethodOptions || r.Method == fiber.MethodConnect || r.Method == fiber.MethodTrace {
			continue
		}
		if path, ok := strings.CutPrefix(r.Path, q.Config.APIPrefix); ok && path != "" {
			// ":id<guid>" is listed as ":id"
			out = append(out, r.Method+" "+routeConstraint.ReplaceAllString(path, ""))
		}
	}
	return out
}
