package services

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// validate checks the rules of a payment account that need more than one attribute: a new QRIS account needs
// its image, a new bank or e-wallet account its number.
func (q *paymentAccountService) validate(_ context.Context, _ pgx.Tx, request *models.PaymentAccount, create bool) error {
	if !create {
		return nil
	}
	if request.Type == "qris" {
		if request.QRISImageURL == nil {
			return helper.Invalid("qris_image_url", "Upload the QRIS image.")
		}
		return nil
	}
	if request.AccountNumber == nil {
		return helper.Invalid("account_number", "Required")
	}
	return nil
}
