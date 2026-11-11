package services

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/models"
)

// validate checks the rules of a fee that need more than one attribute; create tells an insert from an update.
func (q *feeService) validate(_ context.Context, _ pgx.Tx, request *models.Fee, create bool) error {
	_ = create
	return percentAtMost100("amount", request.AmountType, request.Amount)
}
