//	@title			Lestari API
//	@version		0.1.0
//	@description	Back-office API of the Lestari point of sale. Every response uses the same envelope; lists put a page of rows in data, errors put a code (and field messages) there.

//	@contact.name	Roy Situmorang

//	@BasePath	/api/v1

//	@accept		json
//	@produce	json

//	@schemes	http https

//	@securitydefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				"Bearer <access_token>" from POST /auth/login

// @Security	BearerAuth
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/urfave/cli/v3"

	"github.com/roysitumorang/lestari-backend/config"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/router"
	"github.com/roysitumorang/lestari-backend/seeds"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	ctxt := "Main"
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := helper.NewLogger()
	// .env is optional: in containers the environment is set directly
	_ = godotenv.Load(".env")
	// connect builds the service graph and applies pending migrations
	connect := func(ctx context.Context) (*router.Service, error) {
		cfg := config.Load()
		service, err := router.MakeHandler(ctx, cfg)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrMakeHandler")
			return nil, err
		}
		if err = service.Migration.Migrate(ctx); err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrMigrate")
			service.Close()
			return nil, err
		}
		return service, nil
	}
	cmd := &cli.Command{
		Name:  config.AppName,
		Usage: "Lestari back-office API",
		Commands: []*cli.Command{
			{
				Name:    "version",
				Aliases: []string{"v"},
				Usage:   "print version",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					_, _ = os.Stdout.WriteString(helper.Sprintf("Version: %s\nCommit: %s\nBuild: %s\n", config.Version, config.Commit, config.Build))
					return nil
				},
			}, {
				Name:    "run",
				Aliases: []string{"serve"},
				Usage:   "migrate, then serve the API",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg := config.Load()
					if cfg.IsProduction() && cfg.JWTSecret == "change-me-in-production" {
						return errors.New("set JWT_SECRET before running in production")
					}
					service, err := connect(ctx)
					if err != nil {
						return err
					}
					defer service.Close()
					return service.HTTPServerMain(ctx, logger)
				},
			}, {
				Name:      "migration",
				Usage:     "migration new|run",
				ArgsUsage: "new | run",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					now := time.Now()
					switch cmd.Args().First() {
					case "new":
						path, err := router.NewService(config.Load(), nil, nil).Migration.CreateMigrationFile()
						if err != nil {
							helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrCreateMigrationFile")
							return err
						}
						helper.Log(ctx, slog.LevelInfo, helper.Sprintf("created %s", path), ctxt, "")
					case "run":
						service, err := connect(ctx)
						if err != nil {
							return err
						}
						defer service.Close()
						helper.Log(ctx, slog.LevelInfo, helper.Sprintf("migrations are up to date (%s)", time.Since(now)), ctxt, "")
					default:
						return errors.New("requires a parameter: new | run")
					}
					return nil
				},
			}, {
				Name:  "seed",
				Usage: "create the demo store (demo@lestari.id)",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					service, err := connect(ctx)
					if err != nil {
						return err
					}
					defer service.Close()
					info, err := seeds.Demo(ctx, service.NewApp(logger), service.API)
					if err != nil {
						helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrDemo")
						return err
					}
					helper.Log(ctx, slog.LevelInfo, helper.Sprintf("demo store ready: %s / %s (store %s)", info.Email, info.Password, info.StoreID), ctxt, "")
					return nil
				},
			}, {
				Name:  "routes",
				Usage: "list every endpoint",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					service := router.NewService(config.Load(), nil, nil)
					routes := service.Routes(service.NewApp(logger))
					sort.Strings(routes)
					for _, r := range routes {
						_, _ = os.Stdout.WriteString(r + "\n")
					}
					_, _ = os.Stderr.WriteString(helper.Sprintf("%d endpoints\n", len(routes)))
					return nil
				},
			},
		},
	}
	err := cmd.Run(ctx, os.Args)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRun")
	}
	return err
}
