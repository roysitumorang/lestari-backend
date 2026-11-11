// Package router wires the application: database pools, repositories, services and controllers.
package router

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/config"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/internal/api"
	"github.com/roysitumorang/lestari-backend/migrations"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// Service is the composition root: everything a controller can ask for.
type Service struct {
	Config    config.Config
	DbRead    *pgxpool.Pool
	DbWrite   *pgxpool.Pool
	Migration *migrations.Migration
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
	repositories.SetDbWrite(dbWrite)
	return &Service{
		Config:    cfg,
		DbRead:    dbRead,
		DbWrite:   dbWrite,
		Migration: migrations.NewMigration(dbWrite),
		API:       api.New(dbWrite, cfg, helper.NewLogger()),
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

// Routes lists "METHOD /path" for every endpoint under the API prefix.
func (q *Service) Routes(app *fiber.App) []string {
	var out []string
	for _, r := range app.GetRoutes(true) {
		if r.Method == fiber.MethodHead || r.Method == fiber.MethodOptions || r.Method == fiber.MethodConnect || r.Method == fiber.MethodTrace {
			continue
		}
		if path, ok := strings.CutPrefix(r.Path, q.Config.APIPrefix); ok && path != "" {
			out = append(out, r.Method+" "+path)
		}
	}
	return out
}
