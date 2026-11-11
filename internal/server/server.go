// Package server builds the Fiber application.
package server

import (
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	recoverer "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/internal/api"
	"github.com/roysitumorang/lestari-backend/internal/config"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

// New returns the configured app and the API (for its route list).
func New(cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) (*fiber.App, *api.API) {
	trust, proxyHeader := trustProxy(cfg, log)
	app := fiber.New(fiber.Config{
		// Behind a trusted reverse proxy, c.IP() (activity logs, access log) is the client from ProxyHeader.
		// With IP validation, an X-Forwarded-For chain is walked right to left past the trusted proxies, so a
		// client cannot spoof its address by sending its own X-Forwarded-For.
		ProxyHeader:        proxyHeader,
		TrustProxy:         proxyHeader != "",
		TrustProxyConfig:   trust,
		EnableIPValidation: true,
		AppName:            "lestari-backend",
		JSONEncoder:        json.Marshal,
		JSONDecoder:        json.Unmarshal,
		BodyLimit:          cfg.BodyLimitMB << 20,
		ReadTimeout:        30 * time.Second,
		WriteTimeout:       120 * time.Second,
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if db.IsInvalidText(err) {
				err = httpx.BadRequest("An id or value in the request is malformed; ids are UUIDs.")
			}
			return httpx.ErrorHandler(func(err error) { log.Error("request failed", "err", err) })(c, err)
		},
	})
	app.Use(recoverer.New(recoverer.Config{EnableStackTrace: !cfg.IsProduction()}))
	app.Use(requestid.New())
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

	app.Use(httpx.LangMiddleware())

	_ = os.MkdirAll(cfg.UploadDir, 0o750)
	app.Get("/uploads/*", static.New(filepath.Clean(cfg.UploadDir)))
	app.Get("/healthz", func(c fiber.Ctx) error {
		if err := pool.Ping(c.Context()); err != nil {
			return c.Status(503).JSON(fiber.Map{"status": "unavailable"})
		}
		return httpx.OK(c, fiber.Map{"status": "ok"})
	})

	a := api.New(pool, cfg, log)
	a.Mount(app.Group(cfg.APIPrefix))
	return app, a
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
