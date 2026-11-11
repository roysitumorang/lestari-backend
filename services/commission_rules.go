package services

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/models"
)

// validate checks the rules of a commission that need more than one attribute; create tells an insert from an update.
func (q *commissionService) validate(_ context.Context, _ pgx.Tx, request *models.Commission, create bool) error {
	_ = create
	return percentAtMost100("value", request.Type, request.Value)
}
