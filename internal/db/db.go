// Package db owns the PostgreSQL pool, migrations and small query helpers.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Querier is satisfied by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func Connect(ctx context.Context, url, timezone string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if timezone != "" {
		cfg.ConnConfig.RuntimeParams["timezone"] = timezone
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

// Migrate applies embedded migrations that have not run yet, each in its own transaction.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return nil, err
	}
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var applied []string
	for _, name := range names {
		version := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&exists); err != nil {
			return applied, err
		}
		if exists {
			continue
		}
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return applied, err
		}
		err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return fmt.Errorf("%s: %w", version, err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version)
			return err
		})
		if err != nil {
			return applied, err
		}
		applied = append(applied, version)
	}
	return applied, nil
}

// JSONRows runs a query wrapped as json_agg and returns the rows as []map[string]any.
// The query must be a plain SELECT; column aliases become the JSON keys.
func JSONRows(ctx context.Context, q Querier, sql string, args ...any) ([]map[string]any, error) {
	raw, err := JSONRaw(ctx, q, sql, args...)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

// JSONRaw returns the rows of a SELECT as a raw JSON array.
func JSONRaw(ctx context.Context, q Querier, sql string, args ...any) (json.RawMessage, error) {
	var raw []byte
	wrapped := `SELECT COALESCE(json_agg(row_to_json(_q)), '[]'::json) FROM (` + sql + `) _q`
	head := strings.ToUpper(strings.TrimSpace(sql))
	if strings.HasPrefix(head, "INSERT") || strings.HasPrefix(head, "UPDATE") || strings.HasPrefix(head, "DELETE") {
		// data-modifying statements are only allowed in a WITH clause
		wrapped = `WITH _q AS (` + sql + `) SELECT COALESCE(json_agg(row_to_json(_q)), '[]'::json) FROM _q`
	}
	err := q.QueryRow(ctx, wrapped, args...).Scan(&raw)
	return raw, err
}

// JSONRow returns a single row as a map, or nil when there is none.
func JSONRow(ctx context.Context, q Querier, sql string, args ...any) (map[string]any, error) {
	rows, err := JSONRows(ctx, q, sql, args...)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// IsUniqueViolation reports whether err is a PostgreSQL unique-constraint error.
func IsUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// IsForeignKeyViolation reports whether err is a PostgreSQL foreign-key error.
func IsForeignKeyViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23503"
}

// IsNoRows reports pgx.ErrNoRows.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
