// Package controllers holds the HTTP handlers: read the request with a sanitizer, call services inside a
// transaction where data changes, and write the response envelope.
package controllers

import (
	"context"
	"errors"
	"log/slog"
	"uuid"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
)

// currentBranchID is the active branch of the signed-in user (X-Branch-Id or their default).
func currentBranchID(c fiber.Ctx) uuid.UUID {
	id, _ := uuid.Parse(helper.P(c).BranchID)
	return id
}

// rollback is deferred right after BeginTx; after a commit it is a no-op.
func rollback(ctx context.Context, tx pgx.Tx, ctxt string) {
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRollback")
	}
}

// DeletedResponse is the data of a bulk delete.
type DeletedResponse struct {
	Deleted int `json:"deleted" example:"3"`
}
