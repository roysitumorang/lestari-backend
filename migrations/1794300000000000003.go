package migrations

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// 0003_i18n
func init() {
	Migrations[1794300000000000003] = func(ctx context.Context, tx pgx.Tx) (err error) {
		_, err = tx.Exec(ctx, `-- Notifications and activity logs keep their English text as a translation key plus the values of its
-- {placeholders}, and are translated into the reader's language when listed.
ALTER TABLE notifications ADD COLUMN params JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE activity_logs ADD COLUMN params JSONB NOT NULL DEFAULT '{}'::jsonb;`)
		return
	}
}
