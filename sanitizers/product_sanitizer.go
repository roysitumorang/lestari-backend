package sanitizers

import (
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindProducts reads the list query: q, item_type, category_id, stock_status, sort, order, page, per_page.
func FindProducts(c fiber.Ctx) *models.ProductFilter {
	response := models.NewProductFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if itemType := c.Query("item_type"); itemType != "" && slices.Contains([]string{"product", "service"}, itemType) {
		response.WithItemType(itemType)
	}
	if categoryID, ok := queryID(c, "category_id"); ok {
		response.WithCategoryID(categoryID)
	}
	if stockStatus := c.Query("stock_status"); slices.Contains([]string{"in_stock", "low", "out"}, stockStatus) {
		response.WithStockStatus(stockStatus)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapProductSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateProduct reads a product body. On create name, selling_price are required; on update only what was sent is checked.
func ValidateProduct(c fiber.Ctx, create bool) (*models.ProductRequest, error) {
	ctxt := "ProductSanitizer-ValidateProduct"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	return ValidateProductRow(b, create)
}

// ValidateProductRow validates one product given as a decoded object (a request body or an imported row).
func ValidateProductRow(row map[string]any, create bool) (*models.ProductRequest, error) {
	b := body(row)
	v := &helper.Validation{}
	response := &models.ProductRequest{}
	response.ImageURL = b.text(v, response, "image_url")
	response.Name = b.text(v, response, "name")
	response.ItemType = b.text(v, response, "item_type", "product", "service")
	response.SKU = b.text(v, response, "sku")
	response.Barcode = b.text(v, response, "barcode")
	response.CategoryID = b.id(v, response, "category_id")
	response.Unit = b.text(v, response, "unit")
	response.SellingPrice = b.number(v, response, "selling_price", new(float64(0)), nil)
	response.PurchasePrice = b.number(v, response, "purchase_price", new(float64(0)), nil)
	response.PriceType = b.text(v, response, "price_type", "single", "wholesale", "customer_type")
	response.TaxID = b.id(v, response, "tax_id")
	response.DiscountType = b.text(v, response, "discount_type", "percent", "amount")
	response.DiscountValue = b.number(v, response, "discount_value", new(float64(0)), nil)
	response.WholesalePrices = b.document(v, response, "wholesale_prices")
	response.CustomerTypePrices = b.document(v, response, "customer_type_prices")
	response.TrackStock = b.boolean(v, response, "track_stock")
	if create {
		response.Stock = b.number(v, response, "stock", nil, nil)
	}
	response.MinimumStock = b.number(v, response, "minimum_stock", new(float64(0)), nil)
	response.ShelfLocation = b.text(v, response, "shelf_location")
	response.WeightGrams = b.number(v, response, "weight_grams", new(float64(0)), nil)
	response.TrackIMEI = b.boolean(v, response, "track_imei")
	response.Units = b.document(v, response, "units")
	response.Variants = b.document(v, response, "variants")
	response.Ingredients = b.document(v, response, "ingredients")
	response.PointsEarned = b.number(v, response, "points_earned", new(float64(0)), nil)
	response.PointsToRedeem = b.number(v, response, "points_to_redeem", new(float64(0)), nil)
	response.ShowInOnlineStore = b.boolean(v, response, "show_in_online_store")
	response.Description = b.text(v, response, "description")
	response.OnlineDescription = b.text(v, response, "online_description")
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	if response.ItemType == nil && create {
		response.ItemType = new("product")
	}
	if response.SellingPrice == nil && create {
		response.SellingPrice = new(float64(0))
	}
	required(v, "selling_price", response.SellingPrice == nil && (create || response.IsNull("selling_price")))
	if response.PurchasePrice == nil && create {
		response.PurchasePrice = new(float64(0))
	}
	if response.PriceType == nil && create {
		response.PriceType = new("single")
	}
	if response.WholesalePrices == nil && (create || response.IsNull("wholesale_prices")) {
		response.WholesalePrices = json.RawMessage("[]")
	}
	if response.CustomerTypePrices == nil && (create || response.IsNull("customer_type_prices")) {
		response.CustomerTypePrices = json.RawMessage("[]")
	}
	if response.TrackStock == nil && (create || response.IsNull("track_stock")) {
		response.TrackStock = new(true)
	}
	if response.Stock == nil && create {
		response.Stock = new(float64(0))
	}
	if response.TrackIMEI == nil && (create || response.IsNull("track_imei")) {
		response.TrackIMEI = new(false)
	}
	if response.Units == nil && (create || response.IsNull("units")) {
		response.Units = json.RawMessage("[]")
	}
	if response.Variants == nil && (create || response.IsNull("variants")) {
		response.Variants = json.RawMessage("[]")
	}
	if response.Ingredients == nil && (create || response.IsNull("ingredients")) {
		response.Ingredients = json.RawMessage("[]")
	}
	if response.ShowInOnlineStore == nil && (create || response.IsNull("show_in_online_store")) {
		response.ShowInOnlineStore = new(true)
	}
	return response, v.Err()
}

// ProductIDs reads ?product_ids[]= (or ?product_ids=a,b); values that are not UUIDs are ignored.
func ProductIDs(c fiber.Ctx) []uuid.UUID {
	response := []uuid.UUID{}
	for _, item := range helper.QueryArray(c, "product_ids") {
		if productID, err := uuid.Parse(strings.TrimSpace(item)); err == nil {
			response = append(response, productID)
		}
	}
	return response
}

// CheckSKU reads ?sku= (required) and ?exclude_id= of GET /products/check-sku.
func CheckSKU(c fiber.Ctx) (sku string, exceptProductID *uuid.UUID, err error) {
	if sku = strings.TrimSpace(c.Query("sku")); sku == "" {
		return "", nil, helper.Invalid("sku", "Required")
	}
	if productID, ok := queryID(c, "exclude_id"); ok {
		exceptProductID = &productID
	}
	return sku, exceptProductID, nil
}

// BarcodeSheet reads the layout of GET /products/barcodes: copies (1–100), symbology, show_price, paper_size.
func BarcodeSheet(c fiber.Ctx) *models.BarcodeSheet {
	response := &models.BarcodeSheet{
		Copies:    1,
		Symbology: c.Query("symbology", models.BarcodeSymbologyCode128),
		ShowPrice: c.Query("show_price", "true") != "false",
		PaperSize: c.Query("paper_size", models.PaperSizeA4),
	}
	if copies, err := strconv.Atoi(c.Query("copies", "1")); err == nil && copies >= 1 && copies <= 100 {
		response.Copies = copies
	}
	return response
}
