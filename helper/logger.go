package helper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"github.com/jackc/pgx/v5"
)

var (
	logger     *slog.Logger
	initLogger = sync.OnceFunc(func() {
		level := slog.LevelInfo
		if os.Getenv("LOG_LEVEL") == "error" {
			level = slog.LevelError
		}
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	})
)

// NewLogger returns the process logger (JSON on stdout).
func NewLogger() *slog.Logger {
	initLogger()
	return logger
}

// Sprintf is fmt.Sprintf; kept here so call sites read the same in every package.
func Sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

func logContext(context, scope string) []any {
	fields := []any{slog.String("service", AppName)}
	if context != "" {
		fields = append(fields, slog.String("context", context))
	}
	if scope != "" {
		fields = append(fields, slog.String("scope", scope))
	}
	return fields
}

func withCaller(fields []any) []any {
	pc, file, line, _ := runtime.Caller(2)
	name := ""
	if fn := runtime.FuncForPC(pc); fn != nil {
		name = fn.Name()
	}
	return append(fields, slog.String("func", name), slog.String("file", Sprintf("%s:%d", file, line)))
}

// Log writes a message; context is "Layer-Method" (e.g. "TaxRepository-FindTaxes") and scope names the failing
// call (e.g. "ErrQuery").
func Log(ctx context.Context, level slog.Level, message, context, scope string) {
	fields := logContext(context, scope)
	if level >= slog.LevelError {
		fields = withCaller(fields)
	}
	NewLogger().Log(ctx, level, message, fields...)
}

// Capture logs an error where it happened (pgx.ErrNoRows is not an error worth reporting).
func Capture(ctx context.Context, level slog.Level, err error, context, scope string) {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return
	}
	fields := logContext(context, scope)
	if level >= slog.LevelError {
		fields = withCaller(fields)
	}
	NewLogger().Log(ctx, level, err.Error(), fields...)
}
