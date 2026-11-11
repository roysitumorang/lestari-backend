// Package migrations holds the schema as Go functions registered by version (UTC nanoseconds) and applies
// the missing ones in a single transaction.
package migrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
)

type Migration struct {
	dbWrite *pgxpool.Pool
}

// Migrations maps a version to its function; every file in this package registers itself in init().
var Migrations = map[int64]func(ctx context.Context, tx pgx.Tx) error{}

// legacyVersions maps the SQL-file migrations of the previous layout (table schema_migrations) to their
// versions here, so databases created before the change are not migrated twice.
var legacyVersions = map[string]int64{
	"0001_schema":  1794300000000000001,
	"0002_regions": 1794300000000000002,
	"0003_i18n":    1794300000000000003,
}

func NewMigration(dbWrite *pgxpool.Pool) *Migration {
	return &Migration{dbWrite: dbWrite}
}

// Migrate applies the versions that are not recorded in the migrations table, in order.
func (m *Migration) Migrate(ctx context.Context) error {
	ctxt := "Migration-Migrate"
	tx, err := m.dbWrite.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrBeginTx")
		return err
	}
	defer func(ctx context.Context) {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRollback")
		}
	}(ctx)
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS migrations ("version" bigint NOT NULL PRIMARY KEY)`); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return err
	}
	if err = m.adoptLegacy(ctx, tx); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrAdoptLegacy")
		return err
	}
	rows, err := tx.Query(ctx, `SELECT "version" FROM migrations ORDER BY "version"`)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrQuery")
		return err
	}
	applied, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrCollectRows")
		return err
	}
	versions := slices.Sorted(maps.Keys(Migrations))
	for _, version := range versions {
		if slices.Contains(applied, version) {
			continue
		}
		if err = Migrations[version](ctx, tx); err != nil {
			err = fmt.Errorf("migration %d: %w", version, err)
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrFunction")
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO migrations ("version") VALUES ($1)`, version); err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			return err
		}
		helper.Log(ctx, slog.LevelInfo, helper.Sprintf("migration %d applied", version), ctxt, "")
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrCommit")
	}
	return err
}

// adoptLegacy records the SQL-file migrations already applied by the previous layout and drops their table.
func (m *Migration) adoptLegacy(ctx context.Context, tx pgx.Tx) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, name := range names {
		if version, ok := legacyVersions[name]; ok {
			if _, err = tx.Exec(ctx, `INSERT INTO migrations ("version") VALUES ($1) ON CONFLICT DO NOTHING`, version); err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(ctx, `DROP TABLE schema_migrations`)
	return err
}

// CreateMigrationFile writes an empty migration named after the current time (lestari migration new).
func (m *Migration) CreateMigrationFile() (string, error) {
	now := time.Now().UTC().UnixNano()
	path := helper.Sprintf("./migrations/%d.go", now)
	content := helper.Sprintf(`package migrations

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func init() {
	Migrations[%d] = func(ctx context.Context, tx pgx.Tx) (err error) {
		_, err = tx.Exec(ctx, `+"`"+`
		`+"`"+`)
		return
	}
}
`, now)
	return path, os.WriteFile(path, []byte(content), 0o600)
}
