// Package router builds the Fiber application.
package router

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/contrib/v3/swaggo"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	recoverer "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/gofiber/utils/v2"

	"github.com/roysitumorang/lestari-backend/config"
	"github.com/roysitumorang/lestari-backend/controllers"
	_ "github.com/roysitumorang/lestari-backend/docs" // generated Swagger spec
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// NewApp builds the Fiber app: middleware chain, static files and every controller.
func (q *Service) NewApp(log *slog.Logger) *fiber.App {
	cfg, pool := q.Config, q.DbWrite
	trust, proxyHeader := trustProxy(cfg, log)
	app := fiber.New(fiber.Config{
		// Behind a trusted reverse proxy, c.IP() (activity logs, access log) is the client from ProxyHeader.
		// With IP validation, an X-Forwarded-For chain is walked right to left past the trusted proxies, so a
		// client cannot spoof its address by sending its own X-Forwarded-For.
		ProxyHeader:        proxyHeader,
		TrustProxy:         proxyHeader != "",
		TrustProxyConfig:   trust,
		EnableIPValidation: true,
		AppName:            config.AppName,
		JSONEncoder:        json.Marshal,
		JSONDecoder:        json.Unmarshal,
		BodyLimit:          cfg.BodyLimitMB << 20,
		ReadTimeout:        30 * time.Second,
		WriteTimeout:       120 * time.Second,
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if repositories.IsInvalidText(err) {
				err = helper.BadRequest("An id or value in the request is malformed; ids are UUIDs.")
			}
			return helper.ErrorHandler(func(err error) { log.Error("request failed", "err", err) })(c, err)
		},
	})
	app.Use(recoverer.New(recoverer.Config{EnableStackTrace: !cfg.IsProduction()}))
	app.Use(requestid.New(requestid.Config{Generator: utils.UUIDv4}))
	if !cfg.IsProduction() || os.Getenv("ACCESS_LOG") == "1" {
		app.Use(logger.New())
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:  cfg.CORSOrigins,
		AllowMethods:  []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Authorization", "Content-Type", "X-Branch-Id", "Accept", "Accept-Language"},
		ExposeHeaders: []string{"Content-Disposition", "Content-Language"},
		MaxAge:        3600,
	}))

	app.Use(helper.LangMiddleware())

	_ = os.MkdirAll(cfg.UploadDir, 0o750)
	app.Get("/uploads/*", static.New(filepath.Clean(cfg.UploadDir)))
	app.Get("/healthz", func(c fiber.Ctx) error {
		if err := pool.Ping(c.Context()); err != nil {
			return c.Status(503).JSON(fiber.Map{"status": "unavailable"})
		}
		return helper.OK(c, fiber.Map{"status": "ok"})
	})

	if !cfg.IsProduction() {
		// Swagger UI with a data example for every documented endpoint: /swagger/index.html (make doc regenerates it)
		app.Get("/swagger/*", swaggo.HandlerDefault)
	}
	v1 := app.Group(cfg.APIPrefix)
	controllers.NewTaxController(
		q.AuthService,
		q.TaxService,
	).Mount(v1.Group("/taxes"))
	controllers.NewCategoryController(
		q.AuthService,
		q.CategoryService,
	).Mount(v1.Group("/categories"))
	controllers.NewCashDrawerController(
		q.AuthService,
		q.CashDrawerService,
	).Mount(v1.Group("/cash-drawers"))
	controllers.NewSupplierController(
		q.AuthService,
		q.SupplierService,
	).Mount(v1.Group("/suppliers"))
	controllers.NewCustomerTypeController(
		q.AuthService,
		q.CustomerTypeService,
	).Mount(v1.Group("/customer-types"))
	controllers.NewCustomerController(
		q.AuthService,
		q.CustomerService,
	).Mount(v1.Group("/customers"))
	controllers.NewFinanceEntryController(
		q.AuthService,
		q.FinanceEntryService,
	).Mount(v1.Group("/finance-entries"))
	controllers.NewDiscountController(
		q.AuthService,
		q.DiscountService,
	).Mount(v1.Group("/discounts"))
	controllers.NewFeeController(
		q.AuthService,
		q.FeeService,
	).Mount(v1.Group("/fees"))
	controllers.NewPaymentMethodController(
		q.AuthService,
		q.PaymentMethodService,
	).Mount(v1.Group("/payment-methods"))
	controllers.NewPromotionController(
		q.AuthService,
		q.PromotionService,
	).Mount(v1.Group("/promotions"))
	controllers.NewCommissionController(
		q.AuthService,
		q.CommissionService,
	).Mount(v1.Group("/commissions"))
	controllers.NewProductController(
		q.AuthService,
		q.ProductService,
	).Mount(v1.Group("/products"))
	controllers.NewStockController(
		q.AuthService,
		q.StockService,
	).Mount(v1.Group("/inventory/stocks"))
	controllers.NewStockOpnameController(
		q.AuthService,
		q.StockOpnameService,
		q.ProductService,
	).Mount(v1.Group("/stock-opnames"))
	// handlers that still live in internal/api (being moved to controllers, one group at a time)
	q.API.Mount(v1)
	return app
}

// HTTPServerMain listens until ctx is cancelled, then shuts down gracefully.
func (q *Service) HTTPServerMain(ctx context.Context, log *slog.Logger) error {
	ctxt := "Router-HTTPServerMain"
	app := q.NewApp(log)
	errc := make(chan error, 1)
	go func() {
		errc <- app.Listen(q.Config.Addr, fiber.ListenConfig{DisableStartupMessage: q.Config.IsProduction()})
	}()
	helper.Log(ctx, slog.LevelInfo, helper.Sprintf("listening on %s%s", q.Config.Addr, q.Config.APIPrefix), ctxt, "")
	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrListen")
			return err
		}
		return nil
	case <-ctx.Done():
		helper.Log(ctx, slog.LevelInfo, "shutting down", ctxt, "")
		return app.ShutdownWithTimeout(15 * time.Second)
	}
}

// trustProxy turns TRUSTED_PROXIES into Fiber's allowlist. It returns an empty header (proxy headers ignored)
// when no valid proxy is configured.
func trustProxy(cfg config.Config, log *slog.Logger) (fiber.TrustProxyConfig, string) {
	var t fiber.TrustProxyConfig
	for _, p := range cfg.TrustedProxies {
		switch strings.ToLower(p) {
		case "loopback":
			t.Loopback = true
		case "private":
			t.Private = true
		case "linklocal":
			t.LinkLocal = true
		case "unix":
			t.UnixSocket = true
		default:
			if net.ParseIP(p) == nil {
				if _, _, err := net.ParseCIDR(p); err != nil {
					log.Warn("ignoring invalid TRUSTED_PROXIES entry", "value", p)
					continue
				}
			}
			t.Proxies = append(t.Proxies, p)
		}
	}
	if !t.Loopback && !t.Private && !t.LinkLocal && !t.UnixSocket && len(t.Proxies) == 0 {
		return t, ""
	}
	return t, cfg.ProxyHeader
}
