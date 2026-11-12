package services

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// trimNumber writes a quantity without trailing zeros (2.5, 3).
func trimNumber(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

// discountOf is a discount of a type on base: a percentage of it, or a fixed amount capped at it.
func discountOf(discountType *string, value *float64, base float64) float64 {
	if discountType == nil || value == nil {
		return 0
	}
	if *discountType == "percent" {
		return round2(base * *value / 100)
	}
	return math.Min(*value, base)
}

// priceOf is the price and cost of a product for a quantity: the variant, then the wholesale tier or the
// customer-type price replace the regular price.
func priceOf(product *models.SaleProduct, quantity float64, variantName string, customerTypeID *uuid.UUID) (price, cost float64) {
	price, cost = product.SellingPrice, product.PurchasePrice
	for _, variant := range product.Variants {
		if variantName == "" || !strings.EqualFold(variant.Name, variantName) {
			continue
		}
		if variant.SellingPrice != nil {
			price = *variant.SellingPrice
		}
		if variant.PurchasePrice != nil {
			cost = *variant.PurchasePrice
		}
	}
	switch product.PriceType {
	case "wholesale":
		best := -1.0
		for _, tier := range product.WholesalePrices {
			if quantity >= tier.MinQuantity && tier.MinQuantity > best {
				best, price = tier.MinQuantity, tier.Price
			}
		}
	case "customer_type":
		for _, item := range product.CustomerTypePrices {
			if customerTypeID != nil && item.CustomerTypeID == *customerTypeID {
				price = item.Price
			}
		}
	}
	return
}

// promotionFor is the first product promotion that covers a product.
func promotionFor(promotions []*models.ActivePromotion, productID uuid.UUID) *models.ActivePromotion {
	for _, promotion := range promotions {
		if promotion.Type != models.PromotionTypeMinimumPurchase && promotion.Covers(productID) {
			return promotion
		}
	}
	return nil
}

// minimumPurchasePromotion is the minimum-purchase promotion with the highest threshold a subtotal reaches.
func minimumPurchasePromotion(promotions []*models.ActivePromotion, subtotal float64) (response *models.ActivePromotion) {
	for _, promotion := range promotions {
		if promotion.Type == models.PromotionTypeMinimumPurchase && subtotal >= promotion.MinimumPurchase &&
			(response == nil || promotion.MinimumPurchase > response.MinimumPurchase) {
			response = promotion
		}
	}
	return
}

// roundTotal rounds a total to a multiple and returns the difference.
func roundTotal(total float64, setting *models.RoundingSetting) float64 {
	if !setting.IsEnabled || setting.Multiple <= 0 {
		return 0
	}
	var rounded float64
	switch setting.Mode {
	case "up":
		rounded = math.Ceil(total/setting.Multiple) * setting.Multiple
	case "down":
		rounded = math.Floor(total/setting.Multiple) * setting.Multiple
	default:
		rounded = math.Round(total/setting.Multiple) * setting.Multiple
	}
	return round2(rounded - total)
}

// moveProductStock moves the stock of a sold product, or of its ingredients when it is a recipe: change.Change
// is the signed quantity of the product itself. Products that do not track stock are left alone.
func moveProductStock(
	ctx context.Context,
	tx pgx.Tx,
	stockRepository repositories.StockRepository,
	product *models.SaleProduct,
	change *models.StockChange,
) error {
	if len(product.Ingredients) == 0 {
		if !product.TrackStock {
			return nil
		}
		change.ProductID = product.ID
		_, err := stockRepository.ChangeStock(ctx, tx, change)
		return err
	}
	for _, ingredient := range product.Ingredients {
		if ingredient.Quantity == 0 {
			continue
		}
		part := *change
		part.ProductID = ingredient.ProductID
		part.Change = change.Change * ingredient.Quantity
		part.Reference = change.Reference + " (" + product.Name + ")"
		if _, err := stockRepository.ChangeStock(ctx, tx, &part); err != nil {
			if errors.Is(err, models.ErrProductNotFound) {
				return helper.Invalid("product_id", "Product {id} not found.", "id", ingredient.ProductID.String())
			}
			return err
		}
	}
	return nil
}
