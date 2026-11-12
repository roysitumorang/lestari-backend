package sanitizers

import (
	"slices"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// dateFilter reads ?start_date and ?end_date as optional filters.
func dateFilter(c fiber.Ctx) (startDate, endDate string) {
	startDate, _ = queryDate(c, "start_date")
	endDate, _ = queryDate(c, "end_date")
	return
}

// sortings reads ?sort and ?order against the fields a list may be sorted by.
func sortings(c fiber.Ctx, allowed map[string]struct{}) []models.Sorting {
	if item, ok := sorting(c, allowed); ok {
		return []models.Sorting{item}
	}
	return nil
}

// FindSales reads the list query of sales.
func FindSales(c fiber.Ctx) *models.SaleFilter {
	response := &models.SaleFilter{
		Keyword:       strings.TrimSpace(c.Query("q")),
		PaymentMethod: c.Query("payment_method"),
		PaymentType:   c.Query("payment_type"),
		Status:        c.Query("status"),
		Sortings:      sortings(c, models.MapSaleSortings),
	}
	response.CashierID, _ = QueryID(c, "cashier_id")
	response.CustomerID, _ = QueryID(c, "customer_id")
	response.StartDate, response.EndDate = dateFilter(c)
	response.Limit, response.Page = paging(c)
	return response
}

// ValidateSale reads the body of POST /sales. The second value is when the sale happened: created_at, or now.
func ValidateSale(c fiber.Ctx, now time.Time) (*models.SaleRequest, time.Time, error) {
	response, err := Decode[models.SaleRequest](c)
	if err != nil {
		return nil, now, err
	}
	v := &helper.Validation{}
	if len(response.Items) == 0 {
		v.Add("items", "Add at least one item.")
	}
	if response.PaymentType == "" {
		response.PaymentType = models.PaymentTypeCash
	}
	if response.PaymentMethod == "" {
		response.PaymentMethod = "cash"
	}
	if !slices.Contains(models.PaymentTypes, response.PaymentType) {
		v.Add("payment_type", "Must be one of: cash, credit.")
	}
	if response.PaymentType == models.PaymentTypeCredit && response.CustomerID == nil {
		v.Add("customer_id", "A credit sale needs a customer.")
	}
	for i, item := range response.Items {
		if item == nil || item.Quantity <= 0 {
			v.Add("items", "Row {row}: quantity must be more than 0.", "row", i+1)
		}
	}
	if response.CreatedAt != "" {
		if now, err = time.Parse(time.RFC3339, response.CreatedAt); err != nil {
			v.Add("created_at", "Use an ISO 8601 date-time.")
		}
	}
	if response.DueDate != "" {
		if _, err = time.Parse(time.DateOnly, response.DueDate); err != nil {
			v.Add("due_date", "Use the format YYYY-MM-DD.")
		}
	}
	return response, now, v.Err()
}

// FindReturns reads the list query of sales returns and purchase returns.
func FindReturns(c fiber.Ctx, kind string) *models.ReturnFilter {
	response := &models.ReturnFilter{
		Kind:     kind,
		Keyword:  strings.TrimSpace(c.Query("q")),
		Sortings: sortings(c, models.MapReturnSortings),
	}
	response.StartDate, response.EndDate = dateFilter(c)
	response.Limit, response.Page = paging(c)
	return response
}

// ValidateReturn reads the body of a return; a purchase return is refunded in cash or by reducing the payable.
func ValidateReturn(c fiber.Ctx, kind string) (*models.ReturnRequest, error) {
	response, err := Decode[models.ReturnRequest](c)
	if err != nil {
		return nil, err
	}
	if response.RefundMethod == "" {
		response.RefundMethod = models.RefundMethodCash
	}
	if kind == models.ReturnKindPurchase && response.RefundMethod != models.RefundMethodCash && response.RefundMethod != models.RefundMethodReducePayable {
		return nil, helper.Invalid("refund_method", "Must be one of: cash, reduce_payable.")
	}
	return response, nil
}

// FindPurchases reads the list query of purchases.
func FindPurchases(c fiber.Ctx) *models.PurchaseFilter {
	response := &models.PurchaseFilter{
		Keyword:     strings.TrimSpace(c.Query("q")),
		PaymentType: c.Query("payment_type"),
		Sortings:    sortings(c, models.MapPurchaseSortings),
	}
	response.SupplierID, _ = QueryID(c, "supplier_id")
	response.StartDate, response.EndDate = dateFilter(c)
	response.Limit, response.Page = paging(c)
	return response
}

// ValidatePurchase reads the body of a purchase. A new purchase needs a date and items and is paid in cash unless
// it says otherwise; an update changes what was sent.
func ValidatePurchase(c fiber.Ctx, create bool) (*models.PurchaseWrite, error) {
	b, err := parseBody(c)
	if err != nil {
		return nil, err
	}
	response := &models.PurchaseWrite{}
	v := &helper.Validation{}
	response.SupplierID = b.id(v, response, "supplier_id")
	response.PurchaseDate = b.date(v, response, "purchase_date")
	response.InvoiceNumber = b.text(v, response, "invoice_number")
	response.PaymentType = b.text(v, response, "payment_type", models.PaymentTypes...)
	response.PaymentMethod = b.text(v, response, "payment_method", models.SalePaymentMethods...)
	response.DueDate = b.date(v, response, "due_date")
	response.PaidAmount = b.number(v, response, "paid_amount", zero, nil)
	response.DiscountTotal = b.number(v, response, "discount_total", zero, nil)
	response.TaxTotal = b.number(v, response, "tax_total", zero, nil)
	response.FeeTotal = b.number(v, response, "fee_total", zero, nil)
	response.Note = b.text(v, response, "note")
	if raw, sent := b["items"]; sent && raw != nil {
		response.HasItems = true
		encoded, err := json.Marshal(raw)
		if err == nil {
			err = json.Unmarshal(encoded, &response.Items)
		}
		if err != nil {
			v.Add("items", "Row {row} needs a product, a quantity above 0 and a unit cost.", "row", 1)
		}
	}
	if create {
		required(v, "purchase_date", response.PurchaseDate == nil)
		required(v, "items", !response.HasItems)
		if response.PaymentType == nil {
			response.PaymentType = new(models.PaymentTypeCash)
		}
	}
	return response, v.Err()
}

// FindDebts reads the list query of payables (kind payable, filtered by supplier_id) and receivables (kind
// receivable, filtered by customer_id).
func FindDebts(c fiber.Ctx, kind string) *models.DebtFilter {
	response := &models.DebtFilter{
		Kind:     kind,
		Keyword:  strings.TrimSpace(c.Query("q")),
		Status:   c.Query("status"),
		Overdue:  c.Query("overdue") == "true",
		Sortings: sortings(c, models.MapDebtSortings),
	}
	party := "supplier_id"
	if kind == models.DebtKindReceivable {
		party = "customer_id"
	}
	response.PartyID, _ = QueryID(c, party)
	response.Limit, response.Page = paging(c)
	return response
}

// ValidateDebtPayment reads the body of a payment against a payable or a receivable.
func ValidateDebtPayment(c fiber.Ctx) (*models.DebtPaymentRequest, error) {
	response, err := Decode[models.DebtPaymentRequest](c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	if response.Amount <= 0 {
		v.Add("amount", "Enter an amount above 0.")
	}
	if _, err = time.Parse(time.DateOnly, response.PaymentDate); err != nil {
		v.Add("payment_date", "Use the format YYYY-MM-DD.")
	}
	if !slices.Contains(models.SalePaymentMethods, response.PaymentMethod) {
		v.Add("payment_method", "Choose a payment method.")
	}
	return response, v.Err()
}

// FindOrders reads the list query of online orders; sort_by=grand_total lists the biggest orders first.
func FindOrders(c fiber.Ctx) *models.OrderFilter {
	response := &models.OrderFilter{
		Keyword:       strings.TrimSpace(c.Query("q")),
		PaymentMethod: c.Query("payment_method"),
		Status:        c.Query("status"),
		Sortings:      sortings(c, models.MapOrderSortings),
	}
	if c.Query("sort_by") == "grand_total" {
		response.Sortings = []models.Sorting{{Field: "grand_total", Direction: models.SortingDirectionDesc}}
	}
	response.StartDate, response.EndDate = dateFilter(c)
	response.Limit, response.Page = paging(c)
	return response
}

// ValidateOrder reads the body of POST /catalog/orders.
func ValidateOrder(c fiber.Ctx) (*models.OrderRequest, error) {
	response, err := Decode[models.OrderRequest](c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	response.CustomerName = strings.TrimSpace(response.CustomerName)
	required(v, "customer_name", response.CustomerName == "")
	required(v, "items", len(response.Items) == 0)
	if response.ShippingMethod != "" && !slices.Contains(models.OrderShippingMethods, response.ShippingMethod) {
		v.Add("shipping_method", "Must be one of: {values}.", "values", strings.Join(models.OrderShippingMethods, ", "))
	}
	if response.PaymentMethod == "" {
		response.PaymentMethod = "bank_transfer"
	}
	if !slices.Contains(models.OrderPaymentMethods, response.PaymentMethod) {
		v.Add("payment_method", "Must be one of: {values}.", "values", strings.Join(models.OrderPaymentMethods, ", "))
	}
	if response.ShippingFee < 0 {
		v.Add("shipping_fee", "Must be at least {min}.", "min", 0)
	}
	return response, v.Err()
}

// ValidateOrderStatus reads the body of a status change; cancelling and rejecting a payment need a reason.
func ValidateOrderStatus(c fiber.Ctx) (*models.OrderStatusRequest, error) {
	response, err := Decode[models.OrderStatusRequest](c)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(models.OrderStatuses, response.Status) {
		return nil, helper.Invalid("status", "Must be one of: {values}.", "values", strings.Join(models.OrderStatuses, ", "))
	}
	response.Reason = strings.TrimSpace(response.Reason)
	if (response.Status == models.OrderStatusCancelled || response.Status == models.OrderStatusPaymentRejected) && response.Reason == "" {
		return nil, helper.Invalid("reason", "Tell the customer why.")
	}
	return response, nil
}

// FindReport reads the parameters of a report: the period when the report has one (today by default), the
// filters and the page (50 rows by default).
func FindReport(c fiber.Ctx, dated bool) (*models.ReportFilter, error) {
	response := &models.ReportFilter{
		Keyword:       strings.TrimSpace(c.Query("q")),
		Type:          strings.TrimSpace(c.Query("type")),
		PaymentMethod: strings.TrimSpace(c.Query("payment_method")),
		PaymentType:   strings.TrimSpace(c.Query("payment_type")),
		Status:        strings.TrimSpace(c.Query("status")),
		GroupBy:       strings.TrimSpace(c.Query("group_by")),
		Direction:     strings.TrimSpace(c.Query("direction")),
		Role:          strings.TrimSpace(c.Query("role")),
		IncludeUnpaid: c.Query("include_unpaid") == "true",
		Rating:        int64(fiber.Query[int](c, "rating")),
	}
	response.CashierID, _ = QueryID(c, "cashier_id")
	response.CategoryID, _ = QueryID(c, "category_id")
	response.StaffID, _ = QueryID(c, "staff_id")
	response.SupplierID, _ = QueryID(c, "supplier_id")
	response.CustomerID, _ = QueryID(c, "customer_id")
	response.PromotionID, _ = QueryID(c, "promotion_id")
	response.CashDrawerID, _ = QueryID(c, "cash_drawer_id")
	response.Limit, response.Page = pageOf(c, 50)
	if !dated {
		return response, nil
	}
	var err error
	response.StartDate, response.EndDate, err = DateRange(c)
	return response, err
}

// FindDashboard reads ?date (today by default) and the unpaid-sales switches of the dashboard.
func FindDashboard(c fiber.Ctx) (*models.DashboardFilter, error) {
	now := time.Now().In(helper.Location)
	day, err := time.ParseInLocation(time.DateOnly, c.Query("date", now.Format(time.DateOnly)), helper.Location)
	if err != nil {
		return nil, helper.Invalid("date", "Use the format YYYY-MM-DD.")
	}
	return &models.DashboardFilter{
		Date:                 day.Format(time.DateOnly),
		PreviousDate:         day.AddDate(0, 0, -1).Format(time.DateOnly),
		IncludeUnpaidRevenue: c.Query("include_unpaid_revenue") == "true",
		IncludeUnpaidProfit:  c.Query("include_unpaid_profit") == "true",
	}, nil
}
