package sanitizers

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindPPOBPrices reads the list query: q, category, sort, order, page, per_page.
func FindPPOBPrices(c fiber.Ctx) *models.PPOBPriceFilter {
	response := models.NewPPOBPriceFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if category := c.Query("category"); category != "" {
		response.WithCategory(category)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapPPOBPriceSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidatePPOBPrice reads the body of a price update.
func ValidatePPOBPrice(c fiber.Ctx) (*models.PPOBPriceRequest, error) {
	ctxt := "PPOBSanitizer-ValidatePPOBPrice"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidatePPOBPriceRow(b)
}

// ValidatePPOBPriceRow validates one price given as a decoded object (a request body or an imported row); the
// price a product is sold at cannot be cleared.
func ValidatePPOBPriceRow(row map[string]any) (*models.PPOBPriceRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	nulls := &models.Nulls{}
	response := &models.PPOBPriceRequest{
		IsActive:     b.boolean(v, nulls, "is_active"),
		SellingPrice: b.number(v, nulls, "selling_price", zero, nil),
		AdminFee:     b.number(v, nulls, "admin_fee", zero, nil),
	}
	required(v, "selling_price", nulls.IsNull("selling_price"))
	required(v, "admin_fee", nulls.IsNull("admin_fee"))
	return response, v.Err()
}

// FindPPOBDeposits reads the list query: start_date, end_date (today by default), q, page, per_page.
func FindPPOBDeposits(c fiber.Ctx) (*models.PPOBDepositFilter, error) {
	response := &models.PPOBDepositFilter{Keyword: c.Query("q")}
	var err error
	if response.StartDate, response.EndDate, err = DateRange(c); err != nil {
		return nil, err
	}
	response.Limit, response.Page = pageOf(c, 50)
	return response, nil
}

// PPOBDeposit reads the body of a top-up: at least Rp 100,000 and a payment method.
func PPOBDeposit(c fiber.Ctx) (*models.PPOBDepositRequest, error) {
	response, err := Decode[models.PPOBDepositRequest](c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	if response.Amount < models.PPOBMinimumDeposit {
		v.Add("amount", "The minimum top-up is Rp 100,000.")
	}
	if !payable(response.PaymentMethod, false) {
		v.Add("payment_method", "Choose a payment method.")
	}
	return response, v.Err()
}

// PPOBSale reads the body of a PPOB sale: the customer number is required.
func PPOBSale(c fiber.Ctx) (*models.PPOBSaleRequest, error) {
	response, err := Decode[models.PPOBSaleRequest](c)
	if err != nil {
		return nil, err
	}
	if response.CustomerNumber == "" {
		return nil, helper.Invalid("customer_number", "Required")
	}
	return response, nil
}
