package sanitizers

import (
	"slices"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// payable tells whether an invoice can be paid with a method; premium coins only where allowCoin says so.
func payable(method string, allowCoin bool) bool {
	return allowCoin && method == models.PaymentMethodCoin || slices.Contains(models.PaymentChannels, method)
}

// PeriodMonths maps a billing period to its number of months.
var PeriodMonths = map[string]int{"monthly": 1, "quarterly": 3, "yearly": 12}

// FindInvoices reads the list query: start_date, end_date (today by default), status, q, page, per_page.
func FindInvoices(c fiber.Ctx) (*models.InvoiceFilter, error) {
	response := &models.InvoiceFilter{Status: c.Query("status"), Keyword: c.Query("q")}
	var err error
	if response.StartDate, response.EndDate, err = DateRange(c); err != nil {
		return nil, err
	}
	response.Limit, response.Page = pageOf(c, 50)
	return response, nil
}

// Subscription reads the body of a plan purchase.
func Subscription(c fiber.Ctx) (*models.SubscriptionRequest, error) {
	response, err := Decode[models.SubscriptionRequest](c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	if _, ok := models.Plans[response.Plan]; !ok {
		v.Add("plan", "Choose a plan.")
	}
	if !payable(response.PaymentMethod, true) {
		v.Add("payment_method", "Choose a payment method.")
	}
	return response, v.Err()
}

// CoinTopUp reads the body of a premium coin top-up.
func CoinTopUp(c fiber.Ctx) (*models.CoinTopUpRequest, error) {
	response, err := Decode[models.CoinTopUpRequest](c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	if response.Amount < 10000 {
		v.Add("amount", "The minimum top-up is Rp 10,000.")
	}
	if !payable(response.PaymentMethod, false) {
		v.Add("payment_method", "Choose a payment method.")
	}
	return response, v.Err()
}

// PluginSubscription reads the body of a plugin purchase and returns the months of its period.
func PluginSubscription(c fiber.Ctx) (*models.PluginSubscriptionRequest, int, error) {
	response, err := Decode[models.PluginSubscriptionRequest](c)
	if err != nil {
		return nil, 0, err
	}
	v := &helper.Validation{}
	months := map[string]int{"monthly": 1, "yearly": 12}[response.Period]
	if months == 0 {
		v.Add("period", "Must be one of: monthly, yearly.")
	}
	if !payable(response.PaymentMethod, true) {
		v.Add("payment_method", "Choose a payment method.")
	}
	return response, months, v.Err()
}
