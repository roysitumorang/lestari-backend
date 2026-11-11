// Package api wires every endpoint of the Lestari back-office API.
package api

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/config"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// Sender delivers OTPs and messages. The default implementation only logs them;
// plug in an SMS/WhatsApp/e-mail provider here.
type Sender interface {
	Send(ctx context.Context, channel, to, message string) error
}

type LogSender struct{ Log *slog.Logger }

func (s LogSender) Send(_ context.Context, channel, to, message string) error {
	s.Log.Info("outgoing message (no provider configured)", "channel", channel, "to", to, "message", message)
	return nil
}

// Payments creates payment links for invoices and top-ups. The default returns
// manual-transfer instructions; plug in a gateway (e.g. NICEPay, Midtrans, Xendit) here.
type Payments interface {
	// instructions are written in lang.
	Create(ctx context.Context, reference string, amount float64, method string, lang i18n.Lang) (url string, instructions string, err error)
}

type ManualPayments struct{}

func (ManualPayments) Create(_ context.Context, ref string, amount float64, method string, lang i18n.Lang) (string, string, error) {
	return "", lang.T("Pay Rp {amount} via {method} and include reference {reference}. The payment is confirmed once it is received.",
		"amount", strconv.FormatFloat(amount, 'f', 0, 64), "method", i18n.Tr(method), "reference", ref), nil
}

type API struct {
	DB       *pgxpool.Pool
	Cfg      config.Config
	Auth     *middlewares.Service
	CRUD     *crud.Engine
	Log      *slog.Logger
	Loc      *time.Location
	Sender   Sender
	Payments Payments

	routes []route
	defs   map[string]*settingDef
}

type route struct {
	method, path string
	handlers     []any
}

func New(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger) *API {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.FixedZone("WIB", 7*3600)
	}
	return &API{
		DB:       pool,
		Cfg:      cfg,
		Log:      log,
		Loc:      loc,
		CRUD:     &crud.Engine{DB: pool},
		Auth:     &middlewares.Service{DB: pool, Secret: []byte(cfg.JWTSecret), AccessTTL: cfg.AccessTokenTTL, RefreshTTL: cfg.RefreshTokenTTL},
		Sender:   LogSender{Log: log},
		Payments: ManualPayments{},
	}
}

// add records a route; Mount registers them sorted so static segments win over params.
func (a *API) add(method, path string, handlers ...any) {
	a.routes = append(a.routes, route{method, path, handlers})
}

func (a *API) addH(method, path string, h fiber.Handler) { a.add(method, path, h) }

func lessPath(x, y string) bool {
	xs, ys := strings.Split(x, "/"), strings.Split(y, "/")
	for i := 0; i < len(xs) && i < len(ys); i++ {
		xp, yp := strings.HasPrefix(xs[i], ":"), strings.HasPrefix(ys[i], ":")
		if xp != yp {
			return !xp // static first
		}
		if xs[i] != ys[i] {
			return xs[i] < ys[i]
		}
	}
	return len(xs) > len(ys)
}

// Mount registers all routes on r: public ones first, the rest behind authentication.
func (a *API) Mount(r fiber.Router) {
	a.routes = nil
	a.registerPublic()
	public := a.routes
	a.routes = nil
	a.registerAll()

	for _, rt := range public {
		r.Add([]string{rt.method}, rt.path, rt.handlers[0], rt.handlers[1:]...)
	}
	sort.SliceStable(a.routes, func(i, j int) bool { return lessPath(a.routes[i].path, a.routes[j].path) })
	protected := r.Group("", a.Auth.Middleware())
	for _, rt := range a.routes {
		protected.Add([]string{rt.method}, rt.path, rt.handlers[0], rt.handlers[1:]...)
	}
	a.routes = append(public, a.routes...)
}

// Routes lists "METHOD /path" for every registered endpoint (used by tests and `lestari routes`).
func (a *API) Routes() []string {
	out := make([]string, 0, len(a.routes))
	for _, r := range a.routes {
		out = append(out, r.method+" "+r.path)
	}
	return out
}

func (a *API) registerAll() {
	a.registerAuth()
	a.registerAccount()
	a.registerFiles()
	a.registerDashboard()
	a.registerContacts()
	a.registerSales()
	a.registerPurchases()
	a.registerDebts()
	a.registerReturns()
	a.registerReports()
	a.registerBranches()
	a.registerSettings()
	a.registerStaff()
	a.registerOnlineCatalog()
	a.registerPPOB()
	a.registerBilling()
	a.registerEngagement()
	a.registerNotifications()
	a.registerRegions()
}

// ---------- shared helpers ----------

func (a *API) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, a.DB, fn)
}

func (a *API) now() time.Time { return time.Now().In(a.Loc) }

func (a *API) today() string { return a.now().Format("2006-01-02") }

// nextNumber builds human-friendly document numbers such as INV/20261108/0007.
func (a *API) nextNumber(ctx context.Context, q repositories.Querier, prefix, table, col, scopeCol, scope string) (string, error) {
	day := a.now().Format("20060102")
	base := prefix + "/" + day + "/"
	var n int
	err := q.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s = $1 AND %s LIKE $2`, table, scopeCol, col), scope, base+"%").Scan(&n)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%04d", base, n+1), nil
}

// activity writes an activity-log entry; failures are logged, never returned.
// The description is English text with optional {placeholder} name/value pairs; it is stored untranslated
// with its params and shown in the reader's language (see localizeRows).
func (a *API) activity(c fiber.Ctx, action, description string, args ...any) {
	p := helper.P(c)
	if _, err := a.DB.Exec(context.Background(), `INSERT INTO activity_logs (store_id, user_id, action, description, params, ip_address, user_agent) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		p.StoreID, p.UserID, action, description, i18n.Params(args...), c.IP(), c.Get("User-Agent")); err != nil {
		a.Log.Warn("activity log", "err", err)
	}
}

// notify stores an in-app notification for the store. Title and message are English text with optional
// {placeholder} name/value pairs (args); they are translated when the notification is read.
func (a *API) notify(ctx context.Context, q repositories.Querier, storeID string, branchID *string, source, title, message, route string, args ...any) {
	if _, err := q.Exec(ctx, `INSERT INTO notifications (store_id, branch_id, source, title, message, params, link_route) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''))`,
		storeID, branchID, source, title, message, i18n.Params(args...), route); err != nil {
		a.Log.Warn("notification", "err", err)
	}
}

// localize translates text columns of a stored row (notification, activity log) into the reader's language,
// filling {placeholders} from the row's "params" column, which is removed from the output.
func localize(l i18n.Lang, row map[string]any, keys ...string) {
	params, _ := row["params"].(map[string]any)
	delete(row, "params")
	args := i18n.Restore(params)
	for _, k := range keys {
		if s, ok := row[k].(string); ok {
			row[k] = l.T(s, args...)
		}
	}
}

// nullID turns an empty id into SQL NULL (use with "$n::uuid").
func nullID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// branchScope / storeScope are crud scopes.
func storeScope(b *crud.Builder, p *helper.Principal) string {
	return "t.store_id = " + b.Add(p.StoreID)
}
func storeInsert(p *helper.Principal) map[string]any { return map[string]any{"store_id": p.StoreID} }

// requirePlugin is used inside handlers that serve both plugin and non-plugin paths.
func requirePlugin(c fiber.Ctx, code string) error {
	if !helper.P(c).HasPlugin(code) {
		return helper.PaymentRequired("plugin_required", "This feature needs the {plugin} plugin.", "plugin", code)
	}
	return nil
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case int:
		return float64(x)
	}
	return 0
}

func round2(f float64) float64 {
	if f < 0 {
		return -round2(-f)
	}
	return float64(int64(f*100+0.5)) / 100
}

// en is a crud field shorthand for enumerations.
func en(vals ...string) []string { return vals }
