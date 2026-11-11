// Package repositories holds the SQL of every entity (one repository each), transaction helpers and the
// JSON query helpers still used by the handlers that have not been converted yet.
package repositories

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

// Querier is satisfied by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var dbWrite *pgxpool.Pool

// SetDbWrite registers the pool transactions are opened on.
func SetDbWrite(db *pgxpool.Pool) {
	dbWrite = db
}

// BeginTx opens a transaction on the write pool; controllers begin it, pass it down and commit.
func BeginTx(ctx context.Context) (pgx.Tx, error) {
	ctxt := "Repositories-BeginTx"
	tx, err := dbWrite.Begin(ctx)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrBegin")
	}
	return tx, err
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

// IsInvalidText reports a malformed literal such as an invalid UUID (22P02).
func IsInvalidText(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "22P02"
}

// IsNoRows reports pgx.ErrNoRows.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// equalPtr reports whether two optional values are the same (both NULL, or both set and equal).
func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// writeError maps constraint violations of an INSERT/UPDATE to the errors services understand.
func writeError(err, conflict error) error {
	switch {
	case IsUniqueViolation(err):
		return conflict
	case IsForeignKeyViolation(err):
		return models.ErrReferenceNotFound
	}
	return err
}

// ReferenceExists reports whether table has a row id inside the tenant (scopeColumn = scopeID). table and
// scopeColumn are names from the code, never request text.
func ReferenceExists(ctx context.Context, tx pgx.Tx, table, scopeColumn string, id, scopeID uuid.UUID) (bool, error) {
	ctxt := "Repositories-ReferenceExists"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT EXISTS(SELECT 1 FROM %s WHERE id = $1 AND %s = $2)", table, scopeColumn)
	var exists bool
	if err := tx.QueryRow(ctx, sb.String(), id, scopeID).Scan(&exists); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return false, err
	}
	return exists, nil
}
