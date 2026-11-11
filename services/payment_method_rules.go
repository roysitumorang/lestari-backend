package services

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/models"
)

// validate checks the rules of a payment method that need more than one attribute; create tells an insert from an update.
func (q *paymentMethodService) validate(_ context.Context, _ pgx.Tx, request *models.PaymentMethod, create bool) error {
	_ = create
	return percentAtMost100("mdr_value", deref(request.MdrType), deref(request.MdrValue))
}
