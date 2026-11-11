package sanitizers

import (
	"log/slog"
	"slices"
	"strings"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindPromotions reads the list query: q, status, sort, order, page, per_page.
func FindPromotions(c fiber.Ctx) *models.PromotionFilter {
	response := models.NewPromotionFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if status := c.Query("status"); slices.Contains([]string{"active", "scheduled", "ended"}, status) {
		response.WithStatus(status)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapPromotionSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidatePromotion reads a promotion body. On create name, type, start_date, end_date are required; on update only what was sent is checked.
func ValidatePromotion(c fiber.Ctx, create bool) (*models.PromotionRequest, error) {
	ctxt := "PromotionSanitizer-ValidatePromotion"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidatePromotionRow(b, create)
}

// ValidatePromotionRow validates one promotion given as a decoded object (a request body or an imported row).
func ValidatePromotionRow(row map[string]any, create bool) (*models.PromotionRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.PromotionRequest{}
	response.Name = b.text(v, response, "name")
	response.Type = b.text(v, response, "type", "product_discount", "buy_x_get_y", "minimum_purchase")
	response.StartDate = b.date(v, response, "start_date")
	response.EndDate = b.date(v, response, "end_date")
	response.StartTime = b.clock(v, response, "start_time")
	response.EndTime = b.clock(v, response, "end_time")
	response.Days = b.texts(v, response, "days", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday")
	response.CustomerTypeIds = b.idList(v, response, "customer_type_ids")
	response.DiscountType = b.text(v, response, "discount_type", "percent", "amount")
	response.DiscountValue = b.number(v, response, "discount_value", new(float64(0)), nil)
	response.MinimumPurchase = b.number(v, response, "minimum_purchase", new(float64(0)), nil)
	response.BuyQuantity = b.number(v, response, "buy_quantity", new(float64(1)), nil)
	response.GetQuantity = b.number(v, response, "get_quantity", new(float64(1)), nil)
	response.Products = b.document(v, response, "products")
	response.IsActive = b.boolean(v, response, "is_active")
	response.Description = b.text(v, response, "description")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	if response.Type == nil && create {
		response.Type = new("product_discount")
	}
	required(v, "type", response.Type == nil && (create || response.IsNull("type")))
	required(v, "start_date", response.StartDate == nil && (create || response.IsNull("start_date")))
	required(v, "end_date", response.EndDate == nil && (create || response.IsNull("end_date")))
	if response.Days == nil && (create || response.IsNull("days")) {
		response.Days = []string{}
	}
	if response.CustomerTypeIds == nil && (create || response.IsNull("customer_type_ids")) {
		response.CustomerTypeIds = []uuid.UUID{}
	}
	if response.Products == nil && (create || response.IsNull("products")) {
		response.Products = json.RawMessage("[]")
	}
	if response.IsActive == nil && (create || response.IsNull("is_active")) {
		response.IsActive = new(true)
	}
	return response, v.Err()
}
