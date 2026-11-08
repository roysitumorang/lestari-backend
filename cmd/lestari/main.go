// Command lestari runs the Lestari back-office API.
//
//	lestari serve     start the HTTP server (default; runs pending migrations first)
//	lestari migrate   apply database migrations and exit
//	lestari seed      create a demo store with sample data
//	lestari routes    print every registered endpoint
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/internal/config"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/seed"
	"github.com/roysitumorang/lestari-backend/internal/server"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if err := run(cmd, cfg, log); err != nil {
		log.Error(cmd+" failed", "err", err)
		os.Exit(1)
	}
}

func run(cmd string, cfg config.Config, log *slog.Logger) error {
	if cfg.IsProduction() && cfg.JWTSecret == "change-me-in-production" && cmd == "serve" {
		return errors.New("set JWT_SECRET before running in production")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cmd == "routes" {
		app, a := server.New(cfg, nil, log)
		_ = app
		routes := a.Routes()
		sort.Strings(routes)
		for _, r := range routes {
			fmt.Println(r)
		}
		fmt.Fprintf(os.Stderr, "%d endpoints\n", len(routes))
		return nil
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL, cfg.Timezone)
	if err != nil {
		return err
	}
	defer pool.Close()
	applied, err := db.Migrate(ctx, pool)
	if err != nil {
		return err
	}
	for _, v := range applied {
		log.Info("migration applied", "version", v)
	}

	switch cmd {
	case "migrate":
		return nil
	case "seed":
		app, a := server.New(cfg, pool, log)
		info, err := seed.Demo(ctx, app, a)
		if err != nil {
			return err
		}
		log.Info("demo store ready", "email", info.Email, "password", info.Password, "store_id", info.StoreID)
		return nil
	case "serve":
		app, _ := server.New(cfg, pool, log)
		errc := make(chan error, 1)
		go func() {
			errc <- app.Listen(cfg.Addr, fiber.ListenConfig{DisableStartupMessage: cfg.IsProduction(), EnablePrefork: false})
		}()
		log.Info("listening", "addr", cfg.Addr, "api", cfg.APIPrefix)
		select {
		case err := <-errc:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
			log.Info("shutting down")
			return app.ShutdownWithTimeout(15 * time.Second)
		}
	default:
		return fmt.Errorf("unknown command %q (use serve, migrate, seed or routes)", cmd)
	}
}
