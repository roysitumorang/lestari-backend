package api

import (
	"context"
	"slices"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/internal/crud"
)

var salesPaymentTypes = en("cash", "debit", "credit_card", "qris", "e_wallet", "bank_transfer", "other")

func (a *API) registerPricing() {
	all := []string{"list", "create", "read", "update", "delete"}
	withBulk := append(slices.Clone(all), "bulk_delete")

	a.CRUD.Register(a.addH, "/discounts", &crud.Resource{
		Name: "Discounts", Singular: "Discount", Table: "discounts", DefaultSort: "t.name",
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "type", Required: true, Enum: en("percent", "amount"), Default: "percent"},
			{Name: "value", Kind: crud.Num, Required: true, Min: new(float64(0))},
			{Name: "is_active", Kind: crud.Bool, Default: true, Filter: true},
		},
		BeforeWrite: percentAtMost100("type", "value"),
	}, withBulk...)

	a.CRUD.Register(a.addH, "/fees", &crud.Resource{
		Name: "Fees", Singular: "Fee", Table: "fees", DefaultSort: "t.name",
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "amount_type", Required: true, Enum: en("percent", "amount"), Default: "amount"},
			{Name: "amount", Kind: crud.Num, Required: true, Min: new(float64(0))},
			{Name: "applies_to", Enum: en("sale", "purchase", "both"), Default: "sale", Filter: true},
		},
		BeforeWrite: percentAtMost100("amount_type", "amount"),
	}, all...)

	a.CRUD.Register(a.addH, "/payment-methods", &crud.Resource{
		Name: "Payment methods", Singular: "Payment method", Table: "payment_methods", DefaultSort: "t.id",
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "type", Required: true, Enum: salesPaymentTypes, Filter: true},
			{Name: "code"},
			{Name: "mdr_type", Enum: en("percent", "amount")},
			{Name: "mdr_value", Kind: crud.Num, Min: new(float64(0))},
			{Name: "charge_fee_to_customer", Kind: crud.Bool, Default: false},
			{Name: "edc_card_rates", Kind: crud.JSON, Default: []any{}},
			{Name: "is_active", Kind: crud.Bool, Default: true, Filter: true},
		},
		BeforeWrite: percentAtMost100("mdr_type", "mdr_value"),
	}, all...)
}

// percentAtMost100 rejects percentages above 100 for a type/value field pair.
func percentAtMost100(typeCol, valueCol string) func(context.Context, *crud.Ctx, map[string]any, string) error {
	return func(_ context.Context, _ *crud.Ctx, vals map[string]any, _ string) error {
		if vals[typeCol] == "percent" {
			if v, ok := vals[valueCol].(float64); ok && v > 100 {
				return helper.Invalid(valueCol, "A percentage cannot be more than 100.")
			}
		}
		return nil
	}
}

func (a *API) registerMarketing() {
	promoStatus := crud.Filter{Param: "status", SQL: `(CASE ? WHEN 'active' THEN t.is_active AND t.start_date <= current_date AND t.end_date >= current_date
		WHEN 'scheduled' THEN t.is_active AND t.start_date > current_date WHEN 'ended' THEN t.end_date < current_date OR NOT t.is_active ELSE true END)`}
	promotions := &crud.Resource{
		Name: "Promotions", Singular: "Promotion", Table: "promotions", DefaultSort: "t.start_date DESC, t.id DESC",
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "type", Required: true, Enum: en("product_discount", "buy_x_get_y", "minimum_purchase"), Default: "product_discount"},
			{Name: "start_date", Kind: crud.Date, Required: true},
			{Name: "end_date", Kind: crud.Date, Required: true},
			{Name: "start_time", Kind: crud.Time},
			{Name: "end_time", Kind: crud.Time},
			{Name: "days", Kind: crud.StrArray, Enum: en("monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"), Default: []string{}},
			{Name: "customer_type_ids", Kind: crud.UUIDArray, Default: []string{}},
			{Name: "discount_type", Enum: en("percent", "amount")},
			{Name: "discount_value", Kind: crud.Num, Min: new(float64(0))},
			{Name: "minimum_purchase", Kind: crud.Num, Min: new(float64(0))},
			{Name: "buy_quantity", Kind: crud.Num, Min: new(float64(1))},
			{Name: "get_quantity", Kind: crud.Num, Min: new(float64(1))},
			{Name: "products", Kind: crud.JSON, Default: []any{}},
			{Name: "is_active", Kind: crud.Bool, Default: true},
			{Name: "description"},
		},
		Filters: []crud.Filter{promoStatus},
		BeforeWrite: func(ctx context.Context, h *crud.Ctx, vals map[string]any, id string) error {
			if err := percentAtMost100("discount_type", "discount_value")(ctx, h, vals, id); err != nil {
				return err
			}
			s, e := vals["start_date"], vals["end_date"]
			if id != "" && (s == nil || e == nil) {
				var cs, ce string
				if err := h.Tx.QueryRow(ctx, `SELECT start_date::text, end_date::text FROM promotions WHERE id=$1`, id).Scan(&cs, &ce); err != nil {
					return err
				}
				if s == nil {
					s = cs
				}
				if e == nil {
					e = ce
				}
			}
			if ss, ok := s.(string); ok {
				if es, ok := e.(string); ok && es < ss {
					return helper.Invalid("end_date", "Must be on or after the start date.")
				}
			}
			if vals["type"] == "buy_x_get_y" && (vals["buy_quantity"] == nil || vals["get_quantity"] == nil) && id == "" {
				v := &helper.Validation{}
				if vals["buy_quantity"] == nil {
					v.Add("buy_quantity", "Required for Buy X get Y.")
				}
				if vals["get_quantity"] == nil {
					v.Add("get_quantity", "Required for Buy X get Y.")
				}
				return v.Err()
			}
			return nil
		},
	}
	a.CRUD.Register(a.addH, "/promotions", promotions, "list", "read")
	a.CRUD.Register(a.businessOnly(), "/promotions", promotions, "create", "update", "delete")

	commissions := &crud.Resource{
		Name: "Commissions", Singular: "Commission rule", Table: "commissions", DefaultSort: "t.name",
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "basis", Required: true, Enum: en("product", "transaction"), Default: "product"},
			{Name: "type", Required: true, Enum: en("percent", "amount"), Default: "percent"},
			{Name: "value", Kind: crud.Num, Required: true, Min: new(float64(0))},
			{Name: "staff_ids", Kind: crud.UUIDArray, Default: []string{}},
			{Name: "product_ids", Kind: crud.UUIDArray, Default: []string{}},
			{Name: "is_active", Kind: crud.Bool, Default: true},
		},
		BeforeWrite: percentAtMost100("type", "value"),
	}
	a.CRUD.Register(a.businessOnly(), "/commissions", commissions, "list", "create", "read", "update", "delete")
}
