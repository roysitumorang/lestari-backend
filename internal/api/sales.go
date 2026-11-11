package api

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/repositories"
)

func dateFrom(col string) crud.Filter {
	return crud.Filter{Param: "start_date", SQL: col + " >= ?::date", Cast: isoDate}
}
func dateTo(col string) crud.Filter {
	return crud.Filter{Param: "end_date", SQL: col + " <= ?::date", Cast: isoDate}
}

func isoDate(s string) (any, bool) {
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return nil, false
	}
	return s, true
}

func (a *API) saleResource() *crud.Resource {
	return &crud.Resource{
		Name: "Sales", Singular: "Sale", Table: "sales", DefaultSort: "t.created_at DESC, t.id DESC",
		Fields: []crud.Field{
			{Name: "receipt_number", ReadOnly: true, Search: true},
			{Name: "cashier_id", Kind: crud.UUID, ReadOnly: true, Filter: true},
			{Name: "cashier_name", ReadOnly: true},
			{Name: "customer_id", Kind: crud.UUID, ReadOnly: true, Filter: true, Ref: &crud.Ref{Key: "customer", Table: "customers"}},
			{Name: "customer_name", Expr: "(SELECT name FROM customers WHERE id = t.customer_id)", Search: true},
			{Name: "payment_method", ReadOnly: true, Filter: true},
			{Name: "payment_type", ReadOnly: true, Filter: true},
			{Name: "subtotal", Kind: crud.Num, ReadOnly: true},
			{Name: "discount_total", Kind: crud.Num, ReadOnly: true},
			{Name: "tax_name", ReadOnly: true},
			{Name: "tax_total", Kind: crud.Num, ReadOnly: true},
			{Name: "service_charge", Kind: crud.Num, ReadOnly: true},
			{Name: "fee_total", Kind: crud.Num, ReadOnly: true},
			{Name: "rounding", Kind: crud.Num, ReadOnly: true},
			{Name: "grand_total", Kind: crud.Num, ReadOnly: true},
			{Name: "paid_amount", Kind: crud.Num, ReadOnly: true},
			{Name: "change_amount", Kind: crud.Num, ReadOnly: true},
			{Name: "profit", Kind: crud.Num, ReadOnly: true},
			{Name: "points_earned", Kind: crud.Int, ReadOnly: true},
			{Name: "points_redeemed", Kind: crud.Int, ReadOnly: true},
			{Name: "label", ReadOnly: true, Search: true},
			{Name: "customer_type", ReadOnly: true},
			{Name: "note", ReadOnly: true},
			{Name: "status", ReadOnly: true, Filter: true},
			{Name: "due_date", Kind: crud.Date, ReadOnly: true},
		},
		Filters: []crud.Filter{dateFrom("t.created_at::date"), dateTo("t.created_at::date")},
		Detail: func(ctx context.Context, q repositories.Querier, p *helper.Principal, row map[string]any) error {
			items, err := repositories.JSONRows(ctx, q, `SELECT id, product_id, product_name, variant_name, quantity, unit_price, discount_amount + promotion_discount AS discount_amount, subtotal, points_earned, points_redeemed
				FROM sale_items WHERE sale_id = $1 ORDER BY id`, row["id"])
			if err != nil {
				return err
			}
			fees, err := repositories.JSONRows(ctx, q, `SELECT fee_name, amount FROM sale_fees WHERE sale_id = $1 ORDER BY id`, row["id"])
			row["items"], row["fees"] = items, fees
			return err
		},
	}
}

func (a *API) registerSales() {
	res := a.saleResource()
	a.CRUD.Register(a.addH, "/sales", res, "list", "read", "export")
	a.addH("POST", "/sales", a.createSale) // used by cashier apps
	a.addH("DELETE", "/sales/:id", a.voidSale)
	a.addH("GET", "/sales/:id/receipt", a.saleReceipt)
	a.addH("GET", "/sales/:id/delivery-note", a.saleDeliveryNote)
	a.addH("POST", "/shifts", a.openShift)            // cashier app
	a.addH("POST", "/shifts/:id/close", a.closeShift) // cashier app
}

// ---------- create ----------

type saleLineIn struct {
	ProductID      string   `json:"product_id"`
	VariantName    string   `json:"variant_name"`
	Quantity       float64  `json:"quantity"`
	UnitPrice      *float64 `json:"unit_price"`
	DiscountAmount *float64 `json:"discount_amount"`
	PointsRedeemed int      `json:"points_redeemed"`
}

type saleIn struct {
	ReceiptNumber  string       `json:"receipt_number"`
	CreatedAt      string       `json:"created_at"`
	CustomerID     *string      `json:"customer_id"`
	ShiftID        *string      `json:"shift_id"`
	PaymentMethod  string       `json:"payment_method"`
	PaymentType    string       `json:"payment_type"`
	Items          []saleLineIn `json:"items"`
	DiscountTotal  float64      `json:"discount_total"`
	TaxID          *string      `json:"tax_id"`
	FeeIDs         []string     `json:"fee_ids"`
	PointsRedeemed int          `json:"points_redeemed"`
	PaidAmount     *float64     `json:"paid_amount"`
	DueDate        string       `json:"due_date"`
	Label          string       `json:"label"`
	Note           string       `json:"note"`
}

type productRow struct {
	ID           string
	Name         string
	CategoryID   *string
	Price, Cost  float64
	PriceType    string
	DiscType     *string
	DiscValue    *float64
	Wholesale    []map[string]any
	TypePrices   []map[string]any
	Ingredients  []map[string]any
	Variants     []map[string]any
	Track        bool
	Stock        float64
	PointsEarned float64
	PointsRedeem float64
}

func (a *API) loadProduct(ctx context.Context, tx pgx.Tx, branchID, id string) (*productRow, error) {
	p := &productRow{}
	var ws, tp, ing, va []byte
	var pe, pr *float64
	err := tx.QueryRow(ctx, `SELECT id, name, category_id, selling_price, purchase_price, price_type, discount_type, discount_value, wholesale_prices, customer_type_prices, ingredients, variants, track_stock, stock, points_earned, points_to_redeem
		FROM products WHERE id=$1 AND branch_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, branchID).
		Scan(&p.ID, &p.Name, &p.CategoryID, &p.Price, &p.Cost, &p.PriceType, &p.DiscType, &p.DiscValue, &ws, &tp, &ing, &va, &p.Track, &p.Stock, &pe, &pr)
	if err != nil {
		return nil, helper.Invalid("items", "Product {id} not found.", "id", id)
	}
	_ = json.Unmarshal(ws, &p.Wholesale)
	_ = json.Unmarshal(tp, &p.TypePrices)
	_ = json.Unmarshal(ing, &p.Ingredients)
	_ = json.Unmarshal(va, &p.Variants)
	if pe != nil {
		p.PointsEarned = *pe
	}
	if pr != nil {
		p.PointsRedeem = *pr
	}
	return p, nil
}

// priceFor applies variant, customer-type and wholesale pricing.
func (p *productRow) priceFor(qty float64, variant string, customerTypeID string) (price, cost float64) {
	price, cost = p.Price, p.Cost
	for _, v := range p.Variants {
		if variant != "" && strings.EqualFold(helper.Str(v, "name"), variant) {
			if x, ok := helper.Num(v, "selling_price"); ok {
				price = x
			}
			if x, ok := helper.Num(v, "purchase_price"); ok {
				cost = x
			}
		}
	}
	switch p.PriceType {
	case "wholesale":
		best := -1.0
		for _, t := range p.Wholesale {
			mq, _ := helper.Num(t, "min_quantity")
			pr, ok := helper.Num(t, "price")
			if ok && qty >= mq && mq > best {
				best, price = mq, pr
			}
		}
	case "customer_type":
		for _, t := range p.TypePrices {
			if id, _ := helper.UUID(t, "customer_type_id"); id == customerTypeID && customerTypeID != "" {
				if pr, ok := helper.Num(t, "price"); ok {
					price = pr
				}
			}
		}
	}
	return
}

func discountOf(typ *string, val *float64, base float64) float64 {
	if typ == nil || val == nil {
		return 0
	}
	if *typ == "percent" {
		return round2(base * *val / 100)
	}
	return math.Min(*val, base)
}

func (a *API) createSale(c fiber.Ctx) error {
	var in saleIn
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	p := helper.P(c)
	v := &helper.Validation{}
	if len(in.Items) == 0 {
		v.Add("items", "Add at least one item.")
	}
	if in.PaymentType == "" {
		in.PaymentType = "cash"
	}
	if in.PaymentMethod == "" {
		in.PaymentMethod = "cash"
	}
	if in.PaymentType != "cash" && in.PaymentType != "credit" {
		v.Add("payment_type", "Must be one of: cash, credit.")
	}
	if in.PaymentType == "credit" && in.CustomerID == nil {
		v.Add("customer_id", "A credit sale needs a customer.")
	}
	for i, it := range in.Items {
		if it.Quantity <= 0 {
			v.Add("items", "Row {row}: quantity must be more than 0.", "row", i+1)
		}
	}
	createdAt := a.now()
	if in.CreatedAt != "" {
		t, err := time.Parse(time.RFC3339, in.CreatedAt)
		if err != nil {
			v.Add("created_at", "Use an ISO 8601 date-time.")
		}
		createdAt = t
	}
	if err := v.Err(); err != nil {
		return err
	}

	var saleID string
	err := a.tx(c.Context(), func(tx pgx.Tx) error {
		ctx := c.Context()
		// customer
		var custTypeID string
		var custTypeName string
		var custDiscType *string
		var custDiscVal *float64
		mult := 1.0
		if in.CustomerID != nil {
			err := tx.QueryRow(ctx, `SELECT COALESCE(ct.id::text,''), COALESCE(ct.name,''), ct.discount_type, ct.discount_value, COALESCE(ct.point_multiplier,1)
				FROM customers cu LEFT JOIN customer_types ct ON ct.id = cu.customer_type_id WHERE cu.id=$1 AND cu.branch_id=$2`, *in.CustomerID, p.BranchID).
				Scan(&custTypeID, &custTypeName, &custDiscType, &custDiscVal, &mult)
			if err != nil {
				return helper.Invalid("customer_id", "Customer not found.")
			}
		}
		promos, err := a.activePromotions(ctx, tx, p.BranchID, createdAt, custTypeID)
		if err != nil {
			return err
		}
		storeSet, _ := a.setting(ctx, tx, p.BranchID, "store")
		allowNegative := storeSet["allow_negative_stock"] == true
		wanted := map[string]float64{}
		type line struct {
			in                       saleLineIn
			prod                     *productRow
			price, cost, disc, promo float64
			promoID                  *string
			subtotal                 float64
			pointsEarned             int
		}
		lines := make([]line, 0, len(in.Items))
		var subtotal, costTotal float64
		for _, it := range in.Items {
			prod, err := a.loadProduct(ctx, tx, p.BranchID, it.ProductID)
			if err != nil {
				return err
			}
			wanted[prod.ID] += it.Quantity
			if prod.Track && len(prod.Ingredients) == 0 && !allowNegative && wanted[prod.ID] > prod.Stock {
				return helper.Invalid("items", "Only {qty} of {product} left in stock.", "qty", trimNum(prod.Stock), "product", prod.Name)
			}
			price, cost := prod.priceFor(it.Quantity, it.VariantName, custTypeID)
			if it.UnitPrice != nil {
				price = *it.UnitPrice
			}
			gross := round2(price * it.Quantity)
			disc := discountOf(prod.DiscType, prod.DiscValue, gross)
			if it.DiscountAmount != nil {
				disc = math.Min(*it.DiscountAmount, gross)
			}
			l := line{in: it, prod: prod, price: price, cost: cost, disc: disc}
			if pr := promos.forProduct(prod.ID); pr != nil {
				l.promoID = &pr.ID
				switch pr.Type {
				case "product_discount":
					l.promo = discountOf(pr.DiscType, pr.DiscValue, gross-disc)
				case "buy_x_get_y":
					if pr.Buy > 0 && pr.Get > 0 {
						free := math.Floor(it.Quantity/(pr.Buy+pr.Get)) * pr.Get
						l.promo = round2(free * price)
					}
				}
			}
			l.subtotal = round2(gross - disc - l.promo)
			if prod.PointsEarned > 0 {
				l.pointsEarned = int(prod.PointsEarned * it.Quantity)
			}
			// cost includes recipe ingredients when the product has them
			if len(prod.Ingredients) > 0 {
				cost = 0
				for _, ing := range prod.Ingredients {
					iid, _ := helper.UUID(ing, "product_id")
					iq, _ := helper.Num(ing, "quantity")
					var icost float64
					_ = tx.QueryRow(ctx, `SELECT purchase_price FROM products WHERE id=$1 AND branch_id=$2`, iid, p.BranchID).Scan(&icost)
					cost += icost * iq
				}
				l.cost = cost
			}
			subtotal += l.subtotal
			costTotal += round2(l.cost * it.Quantity)
			lines = append(lines, l)
		}
		subtotal = round2(subtotal)

		// invoice-level discount: manual + customer type + minimum-purchase promotion
		invoiceDisc := math.Min(in.DiscountTotal, subtotal)
		invoiceDisc += discountOf(custDiscType, custDiscVal, subtotal-invoiceDisc)
		var salePromo *string
		if mp := promos.minimumPurchase(subtotal); mp != nil {
			invoiceDisc += discountOf(mp.DiscType, mp.DiscValue, subtotal-invoiceDisc)
			salePromo = &mp.ID
		}
		// points redemption
		pointsSet, _ := a.setting(ctx, tx, p.BranchID, "points")
		redeemed := in.PointsRedeemed
		for _, it := range in.Items {
			redeemed += it.PointsRedeemed
		}
		if redeemed > 0 {
			if in.CustomerID == nil {
				return helper.Invalid("points_redeemed", "Choose a customer to redeem points.")
			}
			var bal int
			if err := tx.QueryRow(ctx, `SELECT points FROM customers WHERE id=$1`, *in.CustomerID).Scan(&bal); err != nil {
				return err
			}
			if bal < redeemed {
				return helper.Invalid("points_redeemed", "The customer has only {n} points.", "n", bal)
			}
			if in.PointsRedeemed > 0 {
				invoiceDisc += float64(in.PointsRedeemed) * num(pointsSet["redeem_point_value"])
			}
		}
		invoiceDisc = math.Min(round2(invoiceDisc), subtotal)
		net := subtotal - invoiceDisc

		// service charge, tax, fees, rounding
		var service float64
		if sc, _ := a.setting(ctx, tx, p.BranchID, "service-charge"); sc["is_enabled"] == true && p.HasPlugin("business") {
			service = round2(net * num(sc["rate"]) / 100)
		}
		var taxID *string
		var taxName *string
		var taxRate, taxTotal float64
		inclusive := false
		if in.TaxID != nil {
			var name string
			if err := tx.QueryRow(ctx, `SELECT name, rate, is_inclusive FROM taxes WHERE id=$1 AND branch_id=$2`, *in.TaxID, p.BranchID).Scan(&name, &taxRate, &inclusive); err != nil {
				return helper.Invalid("tax_id", "Tax not found.")
			}
			taxID, taxName = in.TaxID, &name
		} else if dt, _ := a.setting(ctx, tx, p.BranchID, "default-tax"); dt["is_enabled"] == true {
			name := "Default tax"
			taxName, taxRate, inclusive = &name, num(dt["rate"]), dt["is_inclusive"] == true
		}
		if taxRate > 0 {
			base := net + service
			if inclusive {
				taxTotal = round2(base - base/(1+taxRate/100))
			} else {
				taxTotal = round2(base * taxRate / 100)
			}
		}
		var feeTotal float64
		type fee struct {
			name   string
			amount float64
		}
		var fees []fee
		for _, fid := range in.FeeIDs {
			var name, typ string
			var amt float64
			if err := tx.QueryRow(ctx, `SELECT name, amount_type, amount FROM fees WHERE id=$1 AND branch_id=$2 AND applies_to IN ('sale','both')`, fid, p.BranchID).Scan(&name, &typ, &amt); err != nil {
				return helper.Invalid("fee_ids", "Fee not found.")
			}
			if typ == "percent" {
				amt = round2(net * amt / 100)
			}
			fees = append(fees, fee{name, amt})
			feeTotal += amt
		}
		total := net + service + feeTotal
		if !inclusive {
			total += taxTotal
		}
		var rounding float64
		if rs, _ := a.setting(ctx, tx, p.BranchID, "rounding"); rs["is_enabled"] == true && p.HasPlugin("business") {
			m := num(rs["multiple"])
			if m > 0 {
				var r float64
				switch rs["mode"] {
				case "up":
					r = math.Ceil(total/m) * m
				case "down":
					r = math.Floor(total/m) * m
				default:
					r = math.Round(total/m) * m
				}
				rounding = round2(r - total)
			}
		}
		total = round2(total + rounding)

		// payment
		paid := total
		if in.PaidAmount != nil {
			paid = *in.PaidAmount
		}
		if in.PaymentType == "cash" && paid < total {
			return helper.Invalid("paid_amount", "The amount paid is less than the total.")
		}
		change := 0.0
		if paid > total {
			change = round2(paid - total)
		}
		var mdrRate, mdrAmount float64
		var mdrType *string
		var mdrVal *float64
		if err := tx.QueryRow(ctx, `SELECT mdr_type, mdr_value FROM payment_methods WHERE branch_id=$1 AND type=$2 AND is_active ORDER BY id LIMIT 1`, p.BranchID, in.PaymentMethod).Scan(&mdrType, &mdrVal); err == nil && mdrType != nil && mdrVal != nil {
			if *mdrType == "percent" {
				mdrRate = *mdrVal
			}
			mdrAmount = discountOf(mdrType, mdrVal, math.Min(paid, total))
		}

		// commissions for the cashier
		var commission float64
		crows, err := tx.Query(ctx, `SELECT basis, type, value, product_ids FROM commissions WHERE branch_id=$1 AND is_active AND (cardinality(staff_ids) = 0 OR $2 = ANY(staff_ids))`, p.BranchID, p.UserID)
		if err != nil {
			return err
		}
		type rule struct {
			basis, typ string
			value      float64
			products   []string
		}
		var rules []rule
		for crows.Next() {
			var r rule
			if err := crows.Scan(&r.basis, &r.typ, &r.value, &r.products); err != nil {
				crows.Close()
				return err
			}
			rules = append(rules, r)
		}
		crows.Close()
		for _, r := range rules {
			if r.basis == "transaction" {
				commission += discountOf(&r.typ, &r.value, net)
				continue
			}
			for _, l := range lines {
				if len(r.products) == 0 || containsID(r.products, l.prod.ID) {
					if r.typ == "percent" {
						commission += round2(l.subtotal * r.value / 100)
					} else {
						commission += r.value * l.in.Quantity
					}
				}
			}
		}

		// points earned
		earned := 0
		if in.CustomerID != nil && pointsSet["is_enabled"] == true && (in.PaymentType == "cash" || pointsSet["earn_on_credit_sales"] == true) {
			if per := num(pointsSet["earn_spend_amount"]); per > 0 {
				earned = int(math.Floor(net/per) * num(pointsSet["earn_points"]) * mult)
			}
			for _, l := range lines {
				earned += l.pointsEarned
			}
		}

		receipt := in.ReceiptNumber
		if receipt == "" {
			receipt, err = a.nextNumber(ctx, tx, "S", "sales", "receipt_number", "branch_id", p.BranchID)
			if err != nil {
				return err
			}
		}
		var dueDate any
		if in.DueDate != "" {
			dueDate = in.DueDate
		}
		var custType any
		if custTypeName != "" {
			custType = custTypeName
		}
		status := "completed"
		if in.PaymentType == "credit" && paid < total {
			status = "unpaid"
		}
		profit := round2(net - costTotal)
		err = tx.QueryRow(ctx, `INSERT INTO sales (branch_id, receipt_number, cashier_id, cashier_name, customer_id, shift_id, payment_method, payment_type,
			subtotal, discount_total, tax_id, tax_name, tax_rate, tax_total, service_charge, fee_total, rounding, grand_total, paid_amount, change_amount,
			cost_total, profit, mdr_rate, mdr_amount, commission_total, points_earned, points_redeemed, promotion_id, label, customer_type, note, status, due_date, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,NULLIF($29,''),$30,NULLIF($31,''),$32,$33::date,$34,$34) RETURNING id`,
			p.BranchID, receipt, p.UserID, p.Name, in.CustomerID, in.ShiftID, in.PaymentMethod, in.PaymentType,
			subtotal, invoiceDisc, taxID, taxName, taxRate, taxTotal, service, feeTotal, rounding, total, math.Min(paid, total)+0, change,
			round2(costTotal), profit, mdrRate, mdrAmount, round2(commission), earned, redeemed, salePromo, in.Label, custType, in.Note, status, dueDate, createdAt).Scan(&saleID)
		if repositories.IsUniqueViolation(err) {
			return helper.Conflict("A sale with this receipt number already exists.")
		}
		if err != nil {
			return err
		}
		for _, l := range lines {
			var variant any
			if l.in.VariantName != "" {
				variant = l.in.VariantName
			}
			if _, err := tx.Exec(ctx, `INSERT INTO sale_items (sale_id, product_id, product_name, variant_name, category_id, quantity, unit_price, unit_cost, discount_amount, subtotal, points_earned, points_redeemed, promotion_id, promotion_discount)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
				saleID, l.prod.ID, l.prod.Name, variant, l.prod.CategoryID, l.in.Quantity, l.price, l.cost, l.disc, l.subtotal, l.pointsEarned, l.in.PointsRedeemed, l.promoID, l.promo); err != nil {
				return err
			}
			if err := a.consumeStock(ctx, tx, p, l.prod, l.in.Quantity, -1, "sale", receipt); err != nil {
				return err
			}
		}
		for _, f := range fees {
			if _, err := tx.Exec(ctx, `INSERT INTO sale_fees (sale_id, fee_name, amount) VALUES ($1,$2,$3)`, saleID, f.name, f.amount); err != nil {
				return err
			}
		}
		if in.CustomerID != nil {
			if redeemed > 0 {
				if _, err := changePoints(ctx, tx, p.BranchID, *in.CustomerID, -redeemed, "redeemed", receipt, ""); err != nil {
					return err
				}
			}
			if earned > 0 {
				if _, err := changePoints(ctx, tx, p.BranchID, *in.CustomerID, earned, "earned", receipt, ""); err != nil {
					return err
				}
			}
		}
		if in.PaymentType == "credit" && paid < total {
			_, err := tx.Exec(ctx, `INSERT INTO debts (branch_id, kind, customer_id, sale_id, reference_number, transaction_date, due_date, total_amount, paid_amount, status)
				VALUES ($1,'receivable',$2,$3,$4,$5::date,$6::date,$7,$8,'unpaid')`, p.BranchID, in.CustomerID, saleID, receipt, createdAt.In(a.Loc).Format("2006-01-02"), dueDate, total, paid)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	row, err := a.CRUD.Fetch(c.Context(), a.DB, a.saleResource(), p, saleID)
	if err != nil {
		return err
	}
	return helper.Created(c, row)
}

// consumeStock moves stock for a product (and its recipe ingredients). dir is -1 to remove, +1 to restore.
func (a *API) consumeStock(ctx context.Context, tx pgx.Tx, p *helper.Principal, prod *productRow, qty, dir float64, typ, ref string) error {
	if len(prod.Ingredients) > 0 {
		for _, ing := range prod.Ingredients {
			iid, ok := helper.UUID(ing, "product_id")
			iq, _ := helper.Num(ing, "quantity")
			if !ok || iq == 0 {
				continue
			}
			if _, err := productStock(ctx, tx, p.BranchID, iid, dir*iq*qty, typ, ref+" ("+prod.Name+")", nil, p.UserID, p.Name, ""); err != nil {
				return err
			}
		}
		return nil
	}
	if !prod.Track {
		return nil
	}
	_, err := productStock(ctx, tx, p.BranchID, prod.ID, dir*qty, typ, ref, nil, p.UserID, p.Name, "")
	return err
}

func containsID(list []string, id string) bool {
	return slices.Contains(list, id)
}

// ---------- promotions in effect ----------

type promo struct {
	ID        string
	Type      string
	DiscType  *string
	DiscValue *float64
	MinBuy    float64
	Buy, Get  float64
	Products  map[string]bool
}

type promoSet []promo

func (ps promoSet) forProduct(id string) *promo {
	for i := range ps {
		if ps[i].Type != "minimum_purchase" && ps[i].Products[id] {
			return &ps[i]
		}
	}
	return nil
}

func (ps promoSet) minimumPurchase(subtotal float64) *promo {
	var best *promo
	for i := range ps {
		if ps[i].Type == "minimum_purchase" && subtotal >= ps[i].MinBuy && (best == nil || ps[i].MinBuy > best.MinBuy) {
			best = &ps[i]
		}
	}
	return best
}

func (a *API) activePromotions(ctx context.Context, tx pgx.Tx, branchID string, at time.Time, customerTypeID string) (promoSet, error) {
	at = at.In(a.Loc)
	day := strings.ToLower(at.Weekday().String())
	rows, err := tx.Query(ctx, `SELECT id, type, discount_type, discount_value, COALESCE(minimum_purchase,0), COALESCE(buy_quantity,0), COALESCE(get_quantity,0), products
		FROM promotions WHERE branch_id=$1 AND is_active AND start_date <= $2::date AND end_date >= $2::date
		AND (cardinality(days) = 0 OR $3 = ANY(days))
		AND (start_time IS NULL OR start_time <= $4) AND (end_time IS NULL OR end_time >= $4)
		AND (cardinality(customer_type_ids) = 0 OR $5::uuid = ANY(customer_type_ids))
		ORDER BY id`, branchID, at.Format("2006-01-02"), day, at.Format("15:04"), nullID(customerTypeID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out promoSet
	for rows.Next() {
		var pr promo
		var prods []byte
		if err := rows.Scan(&pr.ID, &pr.Type, &pr.DiscType, &pr.DiscValue, &pr.MinBuy, &pr.Buy, &pr.Get, &prods); err != nil {
			return nil, err
		}
		var list []map[string]any
		_ = json.Unmarshal(prods, &list)
		pr.Products = map[string]bool{}
		for _, it := range list {
			if id, ok := helper.UUID(it, "product_id"); ok {
				pr.Products[id] = true
			}
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

// ---------- void ----------

func (a *API) voidSale(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	p := helper.P(c)
	if p.Role == "cashier" {
		return helper.Forbidden("Cashiers cannot delete sales.")
	}
	err = a.tx(c.Context(), func(tx pgx.Tx) error {
		ctx := c.Context()
		var receipt string
		var customerID *string
		var earned, redeemed int
		if err := tx.QueryRow(ctx, `SELECT receipt_number, customer_id, points_earned, points_redeemed FROM sales WHERE id=$1 AND branch_id=$2 FOR UPDATE`, id, p.BranchID).
			Scan(&receipt, &customerID, &earned, &redeemed); err != nil {
			return helper.NotFound("Sale")
		}
		rows, err := tx.Query(ctx, `SELECT product_id, quantity FROM sale_items WHERE sale_id=$1 AND product_id IS NOT NULL`, id)
		if err != nil {
			return err
		}
		type it struct {
			pid string
			qty float64
		}
		var items []it
		for rows.Next() {
			var x it
			if err := rows.Scan(&x.pid, &x.qty); err != nil {
				rows.Close()
				return err
			}
			items = append(items, x)
		}
		rows.Close()
		for _, x := range items {
			prod, err := a.loadProduct(ctx, tx, p.BranchID, x.pid)
			if err != nil {
				continue // product was deleted; nothing to restore
			}
			if err := a.consumeStock(ctx, tx, p, prod, x.qty, 1, "void", receipt); err != nil {
				return err
			}
		}
		if customerID != nil && (earned > 0 || redeemed > 0) {
			if _, err := changePoints(ctx, tx, p.BranchID, *customerID, redeemed-earned, "adjusted", receipt, "Sale deleted"); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM sales WHERE id=$1`, id)
		return err
	})
	if err != nil {
		return err
	}
	a.activity(c, "sale_deleted", "Deleted sale")
	return helper.NoContent(c)
}

// ---------- shifts (cashier app) ----------

func (a *API) openShift(c fiber.Ctx) error {
	var in struct {
		CashDrawerID string  `json:"cash_drawer_id"`
		OpeningCash  float64 `json:"opening_cash"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	p := helper.P(c)
	var open bool
	if err := a.DB.QueryRow(c.Context(), `SELECT EXISTS(SELECT 1 FROM cash_drawers WHERE id=$1 AND branch_id=$2)`, in.CashDrawerID, p.BranchID).Scan(&open); err != nil || !open {
		return helper.Invalid("cash_drawer_id", "Cash drawer not found.")
	}
	_ = a.DB.QueryRow(c.Context(), `SELECT EXISTS(SELECT 1 FROM shifts WHERE cash_drawer_id=$1 AND closed_at IS NULL)`, in.CashDrawerID).Scan(&open)
	if open {
		return helper.Conflict("This cash drawer already has an open shift.")
	}
	row, err := repositories.JSONRow(c.Context(), a.DB, `INSERT INTO shifts (branch_id, cash_drawer_id, cashier_id, opening_cash) VALUES ($1,$2,$3,$4) RETURNING id, cash_drawer_id, cashier_id, opened_at, opening_cash`,
		p.BranchID, in.CashDrawerID, p.UserID, in.OpeningCash)
	if err != nil {
		return err
	}
	return helper.Created(c, row)
}

func (a *API) closeShift(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	var in struct {
		ActualCash float64 `json:"actual_cash"`
		CashIn     float64 `json:"cash_in"`
		CashOut    float64 `json:"cash_out"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	tag, err := a.DB.Exec(c.Context(), `UPDATE shifts SET closed_at=now(), actual_cash=$3, cash_in=$4, cash_out=$5 WHERE id=$1 AND branch_id=$2 AND closed_at IS NULL`,
		id, helper.P(c).BranchID, in.ActualCash, in.CashIn, in.CashOut)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return helper.Conflict("The shift is already closed or does not exist.")
	}
	return a.shiftDetail(c)
}

// ---------- documents ----------

func (a *API) storeHeader(ctx context.Context, branchID string) (name, address, phone string) {
	s, _ := a.setting(ctx, a.DB, branchID, "store")
	return fmt.Sprint(s["name"]), strOr(s["address"], ""), strOr(s["phone"], "")
}

func strOr(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func (a *API) saleReceipt(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	p := helper.P(c)
	s, err := a.CRUD.Fetch(c.Context(), a.DB, a.saleResource(), p, id)
	if err != nil {
		return err
	}
	rs, _ := a.setting(c.Context(), a.DB, p.BranchID, "receipt")
	name, addr, phone := a.storeHeader(c.Context(), p.BranchID)
	items, _ := s["items"].([]map[string]any)
	h := 260.0 + float64(len(items))*24
	d := export.NewPDF(226, h)
	tr := helper.Lang(c).T
	d.Page()
	y := h - 24
	center := func(size float64, bold bool, t string) {
		d.Text((226-export.TextWidth(t, size))/2, y, size, bold, t)
		y -= size + 4
	}
	center(11, true, name)
	if addr != "" {
		center(7, false, export.Fit(addr, 210, 7))
	}
	if phone != "" {
		center(7, false, phone)
	}
	if hn := strOr(rs["header_note"], ""); hn != "" {
		center(7, false, export.Fit(hn, 210, 7))
	}
	y -= 4
	d.Line(8, y+6, 218)
	if rs["show_receipt_number"] != false {
		d.Text(8, y-4, 7, false, tr("No")+": "+fmt.Sprint(s["receipt_number"]))
		y -= 10
	}
	d.Text(8, y-4, 7, false, formatTime(s["created_at"], a.Loc)+"  ·  "+fmt.Sprint(s["cashier_name"]))
	y -= 12
	if cu, ok := s["customer"].(map[string]any); ok {
		d.Text(8, y-4, 7, false, tr("Customer")+": "+fmt.Sprint(cu["name"]))
		y -= 10
	}
	d.Line(8, y, 218)
	y -= 10
	totalQty := 0.0
	for _, it := range items {
		d.Text(8, y, 7, false, export.Fit(fmt.Sprint(it["product_name"]), 200, 7))
		y -= 9
		q := num(it["quantity"])
		totalQty += q
		d.Text(14, y, 7, false, export.Thousands(q)+" x "+export.Thousands(num(it["unit_price"])))
		d.TextRight(218, y, 7, false, export.Thousands(num(it["subtotal"])))
		y -= 11
	}
	d.Line(8, y+4, 218)
	y -= 6
	row := func(label string, v float64, bold bool) {
		if v == 0 && !bold {
			return
		}
		d.Text(8, y, 7.5, bold, label)
		d.TextRight(218, y, 7.5, bold, export.Thousands(v))
		y -= 11
	}
	if rs["show_total_quantity"] == true {
		d.Text(8, y, 7.5, false, tr("Total qty")+": "+export.Thousands(totalQty))
		y -= 11
	}
	row(tr("Subtotal"), num(s["subtotal"]), false)
	row(tr("Discount"), -num(s["discount_total"]), false)
	row(tr("Service charge"), num(s["service_charge"]), false)
	row(strOr(s["tax_name"], tr("Tax")), num(s["tax_total"]), false)
	row(tr("Fees"), num(s["fee_total"]), false)
	row(tr("Rounding"), num(s["rounding"]), false)
	row(tr("TOTAL"), num(s["grand_total"]), true)
	row(tr("Paid ({method})", "method", i18n.Tr(fmt.Sprint(s["payment_method"]))), num(s["paid_amount"])+num(s["change_amount"]), false)
	row(tr("Change"), num(s["change_amount"]), false)
	if rs["show_points"] == true && num(s["points_earned"]) > 0 {
		d.Text(8, y, 7, false, tr("Points earned")+": "+export.Thousands(num(s["points_earned"])))
		y -= 10
	}
	y -= 6
	center(7, false, strOr(rs["footer_note"], tr("Thank you!")))
	if rs["show_signature"] == true {
		y -= 26
		d.Line(130, y, 218)
		d.Text(150, y-9, 7, false, tr("Signature"))
	}
	return export.Send(c, "receipt-"+strings.ReplaceAll(fmt.Sprint(s["receipt_number"]), "/", "-")+".pdf", "application/pdf", d.Bytes())
}

func formatTime(v any, loc *time.Location) string {
	if s, ok := v.(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.In(loc).Format("02 Jan 2006 15:04")
		}
	}
	return fmt.Sprint(v)
}

func (a *API) saleDeliveryNote(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	p := helper.P(c)
	s, err := a.CRUD.Fetch(c.Context(), a.DB, a.saleResource(), p, id)
	if err != nil {
		return err
	}
	name, addr, phone := a.storeHeader(c.Context(), p.BranchID)
	cust := map[string]any{}
	if cu, ok := s["customer"].(map[string]any); ok {
		cust, _ = repositories.JSONRow(c.Context(), a.DB, `SELECT name, phone, address FROM customers WHERE id=$1`, cu["id"])
	}
	d := export.NewPDF(595, 842)
	tr := helper.Lang(c).T
	d.Page()
	d.Text(40, 790, 16, true, tr("DELIVERY NOTE"))
	d.Text(40, 770, 10, true, name)
	d.Text(40, 756, 9, false, addr)
	d.Text(40, 744, 9, false, phone)
	d.Text(360, 790, 9, false, tr("No")+": "+fmt.Sprint(s["receipt_number"]))
	d.Text(360, 776, 9, false, tr("Date")+": "+formatTime(s["created_at"], a.Loc))
	d.Text(40, 712, 9, true, tr("Deliver to")+":")
	if cust != nil {
		d.Text(40, 698, 9, false, strOr(cust["name"], "-"))
		d.Text(40, 686, 9, false, strOr(cust["address"], ""))
		d.Text(40, 674, 9, false, strOr(cust["phone"], ""))
	}
	y := 640.0
	d.Rect(40, y-4, 515, 16, 0.92)
	d.Text(46, y, 9, true, "#")
	d.Text(70, y, 9, true, tr("Item"))
	d.Text(420, y, 9, true, tr("Qty"))
	d.Text(470, y, 9, true, tr("Checked"))
	y -= 20
	items, _ := s["items"].([]map[string]any)
	for i, it := range items {
		if y < 120 {
			d.Page()
			y = 800
		}
		d.Text(46, y, 9, false, strconv.Itoa(i+1))
		d.Text(70, y, 9, false, export.Fit(fmt.Sprint(it["product_name"]), 340, 9))
		d.Text(420, y, 9, false, export.Thousands(num(it["quantity"])))
		d.Rect(478, y-2, 10, 10, 0.85)
		y -= 16
	}
	y -= 40
	for i, label := range []string{tr("Sender"), tr("Courier"), tr("Receiver")} {
		x := 40 + float64(i)*180
		d.Line(x, y, x+140)
		d.Text(x, y-12, 9, false, label)
	}
	return export.Send(c, "delivery-note.pdf", "application/pdf", d.Bytes())
}
