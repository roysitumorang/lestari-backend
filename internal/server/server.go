// Package server builds the Fiber application.
package server

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
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
	app := fiber.New(fiber.Config{
		AppName:      "lestari-backend",
		JSONEncoder:  json.Marshal,
		JSONDecoder:  json.Unmarshal,
		BodyLimit:    cfg.BodyLimitMB << 20,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if db.IsInvalidText(err) {
				err = httpx.BadRequest("An id or value in the request is malformed; ids are UUIDs.")
			}
			return httpx.ErrorHandler(func(err error) { log.Error("request failed", "err", err) })(c, err)
		},
	})
	app.Use(recover.New(recover.Config{EnableStackTrace: !cfg.IsProduction()}))
	app.Use(requestid.New())
	if !cfg.IsProduction() || os.Getenv("ACCESS_LOG") == "1" {
		app.Use(logger.New())
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:  cfg.CORSOrigins,
		AllowMethods:  []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Authorization", "Content-Type", "X-Branch-Id", "Accept"},
		ExposeHeaders: []string{"Content-Disposition"},
		MaxAge:        3600,
	}))

	_ = os.MkdirAll(cfg.UploadDir, 0o755)
	app.Get("/uploads/*", static.New(filepath.Clean(cfg.UploadDir)))
	app.Get("/healthz", func(c fiber.Ctx) error {
		if err := pool.Ping(c.Context()); err != nil {
			return c.Status(503).JSON(fiber.Map{"status": "unavailable"})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	})

	a := api.New(pool, cfg, log)
	a.Mount(app.Group(cfg.APIPrefix))
	return app, a
}
