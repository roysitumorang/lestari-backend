package services

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// validate checks the rules of a promotion that need more than one attribute; create tells an insert from an update.
func (q *promotionService) validate(_ context.Context, _ pgx.Tx, request *models.Promotion, create bool) error {
	if err := percentAtMost100("discount_value", deref(request.DiscountType), deref(request.DiscountValue)); err != nil {
		return err
	}
	// both dates are YYYY-MM-DD, so text order is date order
	if request.EndDate < request.StartDate {
		return helper.Invalid("end_date", "Must be on or after the start date.")
	}
	if create && request.Type == "buy_x_get_y" {
		v := &helper.Validation{}
		if request.BuyQuantity == nil {
			v.Add("buy_quantity", "Required for Buy X get Y.")
		}
		if request.GetQuantity == nil {
			v.Add("get_quantity", "Required for Buy X get Y.")
		}
		return v.Err()
	}
	return nil
}
