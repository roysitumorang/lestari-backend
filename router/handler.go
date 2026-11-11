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
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/internal/api"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/migrations"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/services"
)

// Service is the composition root: everything a controller can ask for.
type Service struct {
	Config               config.Config
	DbRead               *pgxpool.Pool
	DbWrite              *pgxpool.Pool
	Migration            *migrations.Migration
	AuthService          *middlewares.Service
	TaxService           services.TaxService
	CategoryService      services.CategoryService
	CashDrawerService    services.CashDrawerService
	SupplierService      services.SupplierService
	CustomerTypeService  services.CustomerTypeService
	CustomerService      services.CustomerService
	FinanceEntryService  services.FinanceEntryService
	DiscountService      services.DiscountService
	FeeService           services.FeeService
	PaymentMethodService services.PaymentMethodService
	PromotionService     services.PromotionService
	CommissionService    services.CommissionService
	JobService           services.JobService
	ProductService       services.ProductService
	StockService         services.StockService
	StockOpnameService   services.StockOpnameService
	// API holds the handlers that have not been moved to controllers yet.
	API *api.API
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
	legacy := api.New(dbWrite, cfg, helper.NewLogger())
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
	productRepository := repositories.NewProductRepository(dbRead, dbWrite)
	stockRepository := repositories.NewStockRepository(dbRead, dbWrite)
	stockOpnameRepository := repositories.NewStockOpnameRepository(dbRead, dbWrite)
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
	return &Service{
		Config:               cfg,
		DbRead:               dbRead,
		DbWrite:              dbWrite,
		Migration:            migrations.NewMigration(dbWrite),
		AuthService:          legacy.Auth,
		TaxService:           taxService,
		CategoryService:      categoryService,
		CashDrawerService:    cashDrawerService,
		SupplierService:      supplierService,
		CustomerTypeService:  customerTypeService,
		CustomerService:      customerService,
		FinanceEntryService:  financeEntryService,
		DiscountService:      discountService,
		FeeService:           feeService,
		PaymentMethodService: paymentMethodService,
		PromotionService:     promotionService,
		CommissionService:    commissionService,
		JobService:           jobService,
		ProductService:       productService,
		StockService:         stockService,
		StockOpnameService:   stockOpnameService,
		API:                  legacy,
	}
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
