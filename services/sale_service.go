package services

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type SaleService interface {
	FindSales(ctx context.Context, request *models.SaleFilter) ([]*models.Sale, int64, error)
	FindSale(ctx context.Context, tx pgx.Tx, branchID, saleID uuid.UUID) (*models.Sale, error)
	// CreateSale prices a sale (product, wholesale and customer-type prices, promotions, points, service charge,
	// tax, fees, rounding), takes the stock, moves the points and records a receivable for a credit sale.
	CreateSale(ctx context.Context, tx pgx.Tx, request *models.SaleCreate) (*models.Sale, error)
	// DeleteSale voids a sale: the stock and the points go back. It returns the receipt number.
	DeleteSale(ctx context.Context, tx pgx.Tx, branchID, saleID uuid.UUID, actor models.Actor, at time.Time) (string, error)
}

type saleService struct {
	saleRepository  repositories.SaleRepository
	stockRepository repositories.StockRepository
	pointRepository repositories.PointRepository
	debtRepository  repositories.DebtRepository
	settingService  SettingService
}

func NewSaleService(
	saleRepository repositories.SaleRepository,
	stockRepository repositories.StockRepository,
	pointRepository repositories.PointRepository,
	debtRepository repositories.DebtRepository,
	settingService SettingService,
) SaleService {
	return &saleService{
		saleRepository:  saleRepository,
		stockRepository: stockRepository,
		pointRepository: pointRepository,
		debtRepository:  debtRepository,
		settingService:  settingService,
	}
}

func (q *saleService) FindSales(ctx context.Context, request *models.SaleFilter) ([]*models.Sale, int64, error) {
	ctxt := "SaleService-FindSales"
	response, total, err := q.saleRepository.FindSales(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSales")
	}
	return response, total, err
}

func (q *saleService) FindSale(ctx context.Context, tx pgx.Tx, branchID, saleID uuid.UUID) (*models.Sale, error) {
	ctxt := "SaleService-FindSale"
	response, err := q.saleRepository.FindSale(ctx, tx, branchID, saleID)
	if err != nil && !errors.Is(err, models.ErrSaleNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSale")
	}
	return response, err
}

// saleLine is a line of a sale while it is priced.
type saleLine struct {
	request *models.SaleItemRequest
	product *models.SaleProduct
	record  models.SaleItemRecord
}

func (q *saleService) CreateSale(ctx context.Context, tx pgx.Tx, request *models.SaleCreate) (*models.Sale, error) {
	ctxt := "SaleService-CreateSale"
	customer := &models.SaleCustomer{PointMultiplier: 1}
	var err error
	if request.CustomerID != nil {
		if customer, err = q.saleRepository.FindSaleCustomer(ctx, tx, request.BranchID, *request.CustomerID); err != nil {
			if errors.Is(err, models.ErrCustomerNotFound) {
				return nil, helper.Invalid("customer_id", "Customer not found.")
			}
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSaleCustomer")
			return nil, err
		}
	}
	promotions, err := q.saleRepository.FindActivePromotions(ctx, tx, request.BranchID, request.At, customer.CustomerTypeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindActivePromotions")
		return nil, err
	}
	var (
		store         models.StoreSetting
		points        models.PointsSetting
		serviceCharge models.ServiceChargeSetting
		defaultTax    models.DefaultTaxSetting
		rounding      models.RoundingSetting
	)
	for key, into := range map[string]any{
		models.SettingStore:         &store,
		models.SettingPoints:        &points,
		models.SettingServiceCharge: &serviceCharge,
		models.SettingDefaultTax:    &defaultTax,
		models.SettingRounding:      &rounding,
	} {
		if err = q.settingService.FindSetting(ctx, tx, request.BranchID, key, into); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSetting")
			return nil, err
		}
	}

	// lines: price, discount, promotion, cost
	var (
		lines     = make([]*saleLine, 0, len(request.Items))
		wanted    = map[uuid.UUID]float64{}
		subtotal  float64
		costTotal float64
		redeemed  = request.PointsRedeemed
	)
	for _, item := range request.Items {
		product, err := q.saleRepository.LockSaleProduct(ctx, tx, request.BranchID, item.ProductID)
		if err != nil {
			if errors.Is(err, models.ErrProductNotFound) {
				return nil, helper.Invalid("items", "Product {id} not found.", "id", item.ProductID.String())
			}
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockSaleProduct")
			return nil, err
		}
		wanted[product.ID] += item.Quantity
		if product.TrackStock && len(product.Ingredients) == 0 && !store.AllowNegativeStock && wanted[product.ID] > product.Stock {
			return nil, helper.Invalid("items", "Only {qty} of {product} left in stock.", "qty", trimNumber(product.Stock), "product", product.Name)
		}
		price, cost := priceOf(product, item.Quantity, item.VariantName, customer.CustomerTypeID)
		if item.UnitPrice != nil {
			price = *item.UnitPrice
		}
		gross := round2(price * item.Quantity)
		discount := discountOf(product.DiscountType, product.DiscountValue, gross)
		if item.DiscountAmount != nil {
			discount = math.Min(*item.DiscountAmount, gross)
		}
		line := &saleLine{request: item, product: product}
		if promotion := promotionFor(promotions, product.ID); promotion != nil {
			line.record.PromotionID = &promotion.ID
			switch promotion.Type {
			case models.PromotionTypeProductDiscount:
				line.record.PromotionDiscount = discountOf(promotion.DiscountType, promotion.DiscountValue, gross-discount)
			case models.PromotionTypeBuyXGetY:
				if promotion.BuyQuantity > 0 && promotion.GetQuantity > 0 {
					free := math.Floor(item.Quantity/(promotion.BuyQuantity+promotion.GetQuantity)) * promotion.GetQuantity
					line.record.PromotionDiscount = round2(free * price)
				}
			}
		}
		// a recipe costs what its ingredients cost
		if len(product.Ingredients) > 0 {
			cost = 0
			for _, ingredient := range product.Ingredients {
				unitCost, err := q.saleRepository.FindUnitCost(ctx, tx, request.BranchID, ingredient.ProductID)
				if err != nil {
					helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindUnitCost")
					return nil, err
				}
				cost += unitCost * ingredient.Quantity
			}
		}
		line.record.SaleItem = models.SaleItem{
			ProductID:      &product.ID,
			ProductName:    product.Name,
			Quantity:       item.Quantity,
			UnitPrice:      price,
			DiscountAmount: discount,
			Subtotal:       round2(gross - discount - line.record.PromotionDiscount),
			PointsRedeemed: item.PointsRedeemed,
		}
		if item.VariantName != "" {
			line.record.VariantName = &item.VariantName
		}
		if product.PointsEarned != nil && *product.PointsEarned > 0 {
			line.record.PointsEarned = int64(*product.PointsEarned * item.Quantity)
		}
		line.record.CategoryID, line.record.UnitCost = product.CategoryID, cost
		subtotal += line.record.Subtotal
		costTotal += round2(cost * item.Quantity)
		redeemed += item.PointsRedeemed
		lines = append(lines, line)
	}
	subtotal = round2(subtotal)

	// discount on the whole sale: manual, customer type, minimum-purchase promotion, redeemed points
	record := &models.SaleRecord{BranchID: request.BranchID, ShiftID: request.ShiftID}
	discount := math.Min(request.DiscountTotal, subtotal)
	discount += discountOf(customer.DiscountType, customer.DiscountValue, subtotal-discount)
	if promotion := minimumPurchasePromotion(promotions, subtotal); promotion != nil {
		discount += discountOf(promotion.DiscountType, promotion.DiscountValue, subtotal-discount)
		record.PromotionID = &promotion.ID
	}
	if redeemed > 0 {
		if request.CustomerID == nil {
			return nil, helper.Invalid("points_redeemed", "Choose a customer to redeem points.")
		}
		if customer.Points < redeemed {
			return nil, helper.Invalid("points_redeemed", "The customer has only {n} points.", "n", customer.Points)
		}
		discount += float64(request.PointsRedeemed) * points.RedeemPointValue
	}
	discount = math.Min(round2(discount), subtotal)
	net := subtotal - discount

	// service charge, tax, fees, rounding
	if serviceCharge.IsEnabled && request.BusinessPlugin {
		record.ServiceCharge = round2(net * serviceCharge.Rate / 100)
	}
	inclusive := false
	if request.TaxID != nil {
		tax, err := q.saleRepository.FindSaleTax(ctx, tx, request.BranchID, *request.TaxID)
		if err != nil {
			if errors.Is(err, models.ErrTaxNotFound) {
				return nil, helper.Invalid("tax_id", "Tax not found.")
			}
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSaleTax")
			return nil, err
		}
		record.TaxID, record.TaxName, record.TaxRate, inclusive = request.TaxID, &tax.Name, tax.Rate, tax.IsInclusive
	} else if defaultTax.IsEnabled {
		record.TaxName, record.TaxRate, inclusive = new("Default tax"), defaultTax.Rate, defaultTax.IsInclusive
	}
	if record.TaxRate > 0 {
		taxable := net + record.ServiceCharge
		if inclusive {
			record.TaxTotal = round2(taxable - taxable/(1+record.TaxRate/100))
		} else {
			record.TaxTotal = round2(taxable * record.TaxRate / 100)
		}
	}
	fees := make([]*models.SaleFee, 0, len(request.FeeIDs))
	for _, feeID := range request.FeeIDs {
		fee, err := q.saleRepository.FindSaleFee(ctx, tx, request.BranchID, feeID)
		if err != nil {
			if errors.Is(err, models.ErrFeeNotFound) {
				return nil, helper.Invalid("fee_ids", "Fee not found.")
			}
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSaleFee")
			return nil, err
		}
		amount := fee.Amount
		if fee.AmountType == "percent" {
			amount = round2(net * amount / 100)
		}
		fees = append(fees, &models.SaleFee{FeeName: fee.Name, Amount: amount})
		record.FeeTotal += amount
	}
	total := net + record.ServiceCharge + record.FeeTotal
	if !inclusive {
		total += record.TaxTotal
	}
	if request.BusinessPlugin {
		record.Rounding = roundTotal(total, &rounding)
	}
	total = round2(total + record.Rounding)

	// payment and what it costs the store
	paid := total
	if request.PaidAmount != nil {
		paid = *request.PaidAmount
	}
	if request.PaymentType == models.PaymentTypeCash && paid < total {
		return nil, helper.Invalid("paid_amount", "The amount paid is less than the total.")
	}
	if paid > total {
		record.ChangeAmount = round2(paid - total)
	}
	mdr, err := q.saleRepository.FindMDR(ctx, tx, request.BranchID, request.PaymentMethod)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindMDR")
		return nil, err
	}
	if mdr != nil && mdr.MDRType != nil && mdr.MDRValue != nil {
		if *mdr.MDRType == "percent" {
			record.MDRRate = *mdr.MDRValue
		}
		record.MDRAmount = discountOf(mdr.MDRType, mdr.MDRValue, math.Min(paid, total))
	}
	rules, err := q.saleRepository.FindCommissionRules(ctx, tx, request.BranchID, request.Actor.ID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCommissionRules")
		return nil, err
	}
	for _, rule := range rules {
		if rule.Basis == "transaction" {
			record.CommissionTotal += discountOf(&rule.Type, &rule.Value, net)
			continue
		}
		for _, line := range lines {
			if len(rule.ProductIDs) > 0 && !slices.Contains(rule.ProductIDs, line.product.ID) {
				continue
			}
			if rule.Type == "percent" {
				record.CommissionTotal += round2(line.record.Subtotal * rule.Value / 100)
			} else {
				record.CommissionTotal += rule.Value * line.request.Quantity
			}
		}
	}
	record.CommissionTotal = round2(record.CommissionTotal)

	// points earned
	var earned int64
	if request.CustomerID != nil && points.IsEnabled && (request.PaymentType == models.PaymentTypeCash || points.EarnOnCreditSales) {
		if points.EarnSpendAmount > 0 {
			earned = int64(math.Floor(net/points.EarnSpendAmount) * points.EarnPoints * customer.PointMultiplier)
		}
		for _, line := range lines {
			earned += line.record.PointsEarned
		}
	}

	if record.ReceiptNumber = request.ReceiptNumber; record.ReceiptNumber == "" {
		if record.ReceiptNumber, err = q.saleRepository.NextReceiptNumber(ctx, tx, request.BranchID, request.At); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextReceiptNumber")
			return nil, err
		}
	}
	credit := request.PaymentType == models.PaymentTypeCredit && paid < total
	record.CreatedAt = request.At
	record.CashierID, record.CashierName = &request.Actor.ID, request.Actor.Name
	record.CustomerID, record.CustomerType = request.CustomerID, customer.CustomerTypeName
	record.PaymentMethod, record.PaymentType = request.PaymentMethod, request.PaymentType
	record.Subtotal, record.DiscountTotal, record.GrandTotal = subtotal, discount, total
	record.PaidAmount = math.Min(paid, total)
	record.CostTotal, record.Profit = round2(costTotal), round2(net-costTotal)
	record.PointsEarned, record.PointsRedeemed = earned, redeemed
	record.Label, record.Note = optional(request.Label), optional(request.Note)
	record.Status = models.SaleStatusCompleted
	if credit {
		record.Status = models.SaleStatusUnpaid
	}
	if request.DueDate != "" {
		record.DueDate = &request.DueDate
	}
	if err = q.saleRepository.CreateSale(ctx, tx, record); err != nil {
		if errors.Is(err, models.ErrSaleConflict) {
			return nil, helper.Conflict("A sale with this receipt number already exists.")
		}
		if errors.Is(err, models.ErrReferenceNotFound) {
			return nil, helper.Invalid("shift_id", "Shift not found.")
		}
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateSale")
		return nil, err
	}
	for _, line := range lines {
		line.record.SaleID = record.ID
		if err = q.saleRepository.CreateSaleItem(ctx, tx, &line.record); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateSaleItem")
			return nil, err
		}
		if err = moveProductStock(ctx, tx, q.stockRepository, line.product, &models.StockChange{
			BranchID:  request.BranchID,
			Change:    -line.request.Quantity,
			Type:      models.StockMovementTypeSale,
			Reference: record.ReceiptNumber,
			Actor:     request.Actor,
			At:        request.At,
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMoveProductStock")
			return nil, err
		}
	}
	for _, fee := range fees {
		if err = q.saleRepository.CreateSaleFee(ctx, tx, record.ID, fee); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateSaleFee")
			return nil, err
		}
	}
	if request.CustomerID != nil {
		for _, change := range []models.PointChange{
			{Points: -redeemed, Type: models.PointTypeRedeemed},
			{Points: earned, Type: models.PointTypeEarned},
		} {
			if change.Points == 0 {
				continue
			}
			change.BranchID, change.CustomerID, change.Reference, change.At = request.BranchID, *request.CustomerID, record.ReceiptNumber, request.At
			if _, err = q.pointRepository.ChangePoints(ctx, tx, &change); err != nil {
				helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangePoints")
				return nil, err
			}
		}
	}
	if credit {
		if err = q.debtRepository.CreateDebt(ctx, tx, &models.DebtRecord{
			BranchID:        request.BranchID,
			Kind:            models.DebtKindReceivable,
			CustomerID:      request.CustomerID,
			SaleID:          &record.ID,
			ReferenceNumber: record.ReceiptNumber,
			TransactionDate: request.At.In(helper.Location).Format(time.DateOnly),
			DueDate:         record.DueDate,
			TotalAmount:     total,
			PaidAmount:      paid,
			Status:          models.DebtStatusUnpaid,
			At:              request.At,
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateDebt")
			return nil, err
		}
	}
	response, err := q.saleRepository.FindSale(ctx, tx, request.BranchID, record.ID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSale")
	}
	return response, err
}

func (q *saleService) DeleteSale(ctx context.Context, tx pgx.Tx, branchID, saleID uuid.UUID, actor models.Actor, at time.Time) (string, error) {
	ctxt := "SaleService-DeleteSale"
	sale, err := q.saleRepository.LockSale(ctx, tx, branchID, saleID)
	if err != nil {
		if !errors.Is(err, models.ErrSaleNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockSale")
		}
		return "", err
	}
	items, err := q.saleRepository.FindSoldQuantities(ctx, tx, saleID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSoldQuantities")
		return "", err
	}
	for _, item := range items {
		product, err := q.saleRepository.LockSaleProduct(ctx, tx, branchID, item.ProductID)
		if errors.Is(err, models.ErrProductNotFound) {
			continue // the product was deleted: nothing to give back
		}
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrLockSaleProduct")
			return "", err
		}
		if err = moveProductStock(ctx, tx, q.stockRepository, product, &models.StockChange{
			BranchID:  branchID,
			Change:    item.Quantity,
			Type:      models.StockMovementTypeVoid,
			Reference: sale.ReceiptNumber,
			Actor:     actor,
			At:        at,
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMoveProductStock")
			return "", err
		}
	}
	if sale.CustomerID != nil && (sale.PointsEarned > 0 || sale.PointsRedeemed > 0) {
		if _, err = q.pointRepository.ChangePoints(ctx, tx, &models.PointChange{
			BranchID:   branchID,
			CustomerID: *sale.CustomerID,
			Points:     sale.PointsRedeemed - sale.PointsEarned,
			Type:       models.PointTypeAdjusted,
			Reference:  sale.ReceiptNumber,
			Note:       "Sale deleted",
			At:         at,
		}); err != nil && !errors.Is(err, models.ErrCustomerNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangePoints")
			return "", err
		}
	}
	if err = q.saleRepository.DeleteSale(ctx, tx, saleID); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteSale")
	}
	return sale.ReceiptNumber, err
}
