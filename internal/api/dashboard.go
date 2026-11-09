package api

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

func (a *API) registerDashboard() {
	a.addH("GET", "/dashboard", a.dashboard)
}

func pctChange(cur, prev float64) any {
	if prev == 0 {
		if cur == 0 {
			return 0.0
		}
		return nil // no baseline
	}
	return round2((cur - prev) / prev * 100)
}

func (a *API) dashboard(c fiber.Ctx) error {
	p := httpx.P(c)
	day := c.Query("date", a.today())
	t, err := time.ParseInLocation("2006-01-02", day, a.Loc)
	if err != nil {
		return httpx.Invalid("date", "Use the format YYYY-MM-DD.")
	}
	prev := t.AddDate(0, 0, -1).Format("2006-01-02")
	unpaidRevenue := c.Query("include_unpaid_revenue") == "true"
	unpaidProfit := c.Query("include_unpaid_profit") == "true"
	ctx := c.Context()

	totals := func(d string) (map[string]any, error) {
		return db.JSONRow(ctx, a.DB, `SELECT count(*) AS transaction_count,
			COALESCE(sum(CASE WHEN $3 OR status <> 'unpaid' THEN grand_total ELSE paid_amount END), 0) AS revenue,
			COALESCE(sum(CASE WHEN $4 OR status <> 'unpaid' THEN profit ELSE 0 END), 0) AS profit,
			COALESCE(round(avg(grand_total)::numeric, 2), 0) AS average_basket
			FROM sales WHERE branch_id=$1 AND created_at::date = $2::date`, p.BranchID, d, unpaidRevenue, unpaidProfit)
	}
	cur, err := totals(day)
	if err != nil {
		return err
	}
	before, err := totals(prev)
	if err != nil {
		return err
	}
	cur["compared_to_previous"] = map[string]any{
		"transaction_count": pctChange(num(cur["transaction_count"]), num(before["transaction_count"])),
		"revenue":           pctChange(num(cur["revenue"]), num(before["revenue"])),
		"profit":            pctChange(num(cur["profit"]), num(before["profit"])),
	}
	hourly, err := db.JSONRows(ctx, a.DB, `SELECT to_char(h, 'HH24":00"') AS hour, count(s.id) AS transaction_count,
		COALESCE(sum(CASE WHEN $3 OR s.status <> 'unpaid' THEN s.grand_total ELSE s.paid_amount END), 0) AS revenue,
		COALESCE(sum(CASE WHEN $4 OR s.status <> 'unpaid' THEN s.profit ELSE 0 END), 0) AS profit
		FROM generate_series($2::date::timestamp, $2::date::timestamp + interval '23 hours', interval '1 hour') h
		LEFT JOIN sales s ON s.branch_id = $1 AND date_trunc('hour', s.created_at::timestamp) = h
		GROUP BY h ORDER BY h`, p.BranchID, day, unpaidRevenue, unpaidProfit)
	if err != nil {
		return err
	}
	top, err := db.JSONRows(ctx, a.DB, `SELECT i.product_id, max(i.product_name) AS name, sum(i.quantity) AS quantity, sum(i.subtotal) AS revenue
		FROM sale_items i JOIN sales s ON s.id = i.sale_id WHERE s.branch_id=$1 AND s.created_at::date = $2::date
		GROUP BY i.product_id ORDER BY revenue DESC LIMIT 5`, p.BranchID, day)
	if err != nil {
		return err
	}
	low, err := db.JSONRows(ctx, a.DB, `SELECT id AS product_id, name, stock, minimum_stock FROM products
		WHERE branch_id=$1 AND deleted_at IS NULL AND track_stock AND minimum_stock IS NOT NULL AND stock <= minimum_stock ORDER BY stock - minimum_stock LIMIT 10`, p.BranchID)
	if err != nil {
		return err
	}
	alerts := []map[string]any{}
	store, _ := a.setting(ctx, a.DB, p.BranchID, "store")
	if store["latitude"] == nil {
		alerts = append(alerts, map[string]any{"type": "store_location", "message": httpx.T(c, "Your store has no map location yet."), "action_label": httpx.T(c, "Set location"), "action_route": "store"})
	}
	var expires *time.Time
	_ = a.DB.QueryRow(ctx, `SELECT plan_expires_at FROM stores WHERE id=$1`, p.StoreID).Scan(&expires)
	if expires != nil && time.Until(*expires) < 7*24*time.Hour {
		msg := httpx.T(c, "Your subscription ends on {date}.", "date", expires.In(a.Loc).Format("02-01-2006"))
		if time.Until(*expires) < 0 {
			msg = httpx.T(c, "Your subscription has ended.")
		}
		alerts = append(alerts, map[string]any{"type": "subscription", "message": msg, "action_label": httpx.T(c, "Renew"), "action_route": "billing"})
	}
	var used, quota float64
	_ = a.DB.QueryRow(ctx, `SELECT COALESCE((SELECT sum(size) FROM uploads WHERE store_id=$1),0)/1048576.0, storage_quota_mb FROM stores WHERE id=$1`, p.StoreID).Scan(&used, &quota)
	if quota > 0 && used/quota > 0.9 {
		alerts = append(alerts, map[string]any{"type": "storage", "message": httpx.T(c, "Your storage is almost full."), "action_label": httpx.T(c, "Add storage"), "action_route": "storage"})
	}
	return httpx.OK(c, map[string]any{"date": day, "summary": cur, "hourly": hourly, "top_products": top, "low_stock": low, "alerts": alerts})
}
