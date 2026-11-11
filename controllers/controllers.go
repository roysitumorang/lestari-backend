// Package controllers holds the HTTP handlers: read the request with a sanitizer, call services inside a
// transaction where data changes, and write the response envelope.
package controllers

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/models"
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

// exportRows turns models into the keyed rows the export writers take (the same names as the JSON).
func exportRows(v any) ([]map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	err = json.Unmarshal(b, &rows)
	return rows, err
}

// describeError is the text of an import row error: the field messages, or the message of the error.
func describeError(l i18n.Lang, err error) string {
	he := &helper.Error{}
	if !errors.As(err, &he) {
		return err.Error()
	}
	if len(he.Fields) == 0 {
		return l.T(he.Message, he.Args...)
	}
	parts := make([]string, 0, len(he.Fields))
	for k, msgs := range he.Fields {
		texts := make([]string, len(msgs))
		for i, m := range msgs {
			texts[i] = m.In(l)
		}
		parts = append(parts, k+": "+strings.Join(texts, " "))
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

// writeError turns the errors shared by every entity into responses.
func writeError(err error) error {
	if errors.Is(err, models.ErrReferenceNotFound) {
		return helper.Invalid("id", "A referenced record does not exist.")
	}
	return err
}

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// currentStoreID is the store of the signed-in user.
func currentStoreID(c fiber.Ctx) uuid.UUID {
	id, _ := uuid.Parse(helper.P(c).StoreID)
	return id
}

// currentActor is the signed-in user as recorded on stock movements and documents.
func currentActor(c fiber.Ctx) models.Actor {
	p := helper.P(c)
	id, _ := uuid.Parse(p.UserID)
	return models.Actor{ID: id, Name: p.Name}
}
