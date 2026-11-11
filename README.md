# lestari-backend

REST API for the **Lestari** back office (point of sale for small shops). It implements every endpoint the
[lestari-client](https://github.com/roysitumorang/lestari-client) calls — the contract is the client's
[`ENDPOINTS.md`](https://github.com/roysitumorang/lestari-client/blob/dev/ENDPOINTS.md) — plus the endpoints a cashier app needs to record sales.

| | |
|---|---|
| Language | Go 1.27 |
| HTTP | [Fiber v3](https://github.com/gofiber/fiber) |
| Database | PostgreSQL 18 via [pgx v5](https://github.com/jackc/pgx) (pool); primary keys are `UUID DEFAULT uuidv7()` |
| JSON | [goccy/go-json](https://github.com/goccy/go-json) (also Fiber's encoder/decoder) |
| Auth | JWT access tokens ([golang-jwt v5](https://github.com/golang-jwt/jwt), HS256) + rotating refresh tokens, bcrypt passwords, TOTP two-step verification |
| Files | XLSX/CSV via excelize, PDFs (receipts, invoices, barcode sheets, reports) by a built-in writer, QR codes via go-qrcode |

## Quick start

```bash
cp .env.sample .env             # adjust DB_WRITE_URL and JWT_SECRET
docker compose up -d db         # or use your own PostgreSQL
make seed                       # migrate + demo store: demo@lestari.id / demo12345
make run                        # http://localhost:8080/api/v1
```

Point the client at it (`js/config.js` → `apiBaseUrl: 'http://localhost:8080/api/v1'`), serve the client folder
(e.g. `python3 -m http.server 5500`) and sign in with the demo account.

Everything in Docker: `docker compose up --build` (API on :8080, migrations run on start). Seed once with
`docker compose run --rm api seed`.

### Commands

```
lestari run             # run pending migrations, then serve HTTP (alias: serve)
lestari migration run   # apply migrations and exit
lestari migration new   # create an empty migration file
lestari version         # version, commit and build time
lestari seed      # (re)create the demo store with sample data
lestari routes    # list every endpoint
```

### Configuration (environment)

| Variable | Default | |
|---|---|---|
| `DB_WRITE_URL` (or `DATABASE_URL`) | `postgres://postgres:postgres@localhost:5432/lestari?sslmode=disable` | |
| `JWT_SECRET` | `change-me-in-production` | **required** in production (server refuses to start otherwise); also signs payment webhooks |
| `ENV` (or `APP_ENV`) | `development` | `production` hides stack traces and the access log |
| `PORT` (or `APP_ADDR`) | `8080` | |
| `DB_READ_URL` | same as write | optional read replica |
| `DB_MAX_CONNECTIONS` | pgx default | per pool |
| `LOG_LEVEL` | `info` | `error` logs only errors (JSON on stdout) |
| `API_PREFIX` | `/api/v1` | |
| `CORS_ORIGINS` | *(all)* | comma-separated origins of the client, e.g. `https://backoffice.example.com` |
| `PUBLIC_URL` | `http://localhost:8080` | used to build upload and share links |
| `UPLOAD_DIR` | `./storage/uploads` | uploaded images/PDFs, served at `/uploads/…` |
| `ACCESS_TOKEN_TTL` / `REFRESH_TOKEN_TTL` | `1h` / `720h` | Go durations |
| `APP_TIMEZONE` | `Asia/Jakarta` | database session time zone (dates in reports) |
| `BODY_LIMIT_MB` | `20` | |
| `TRUSTED_PROXIES` | `loopback` | reverse proxies whose `PROXY_HEADER` is believed for the client IP (activity logs, access log): comma-separated IPs/CIDRs and the keywords `loopback`, `private`, `linklocal`, `unix`; `none` = always use the TCP peer. nginx on the same machine needs the default; the API in Docker behind host nginx needs `private` (or the bridge subnet) |
| `PROXY_HEADER` | `X-Forwarded-For` | or `X-Real-IP`. A forwarded chain is walked right to left past trusted proxies, so clients cannot spoof their address |

## Project layout

```
main.go                 entry point: version / run / migration new|run / seed / routes (urfave/cli)
config/                 environment, build info (ldflags), database pools
router/handler.go       composition root: pools → repositories → services (Service)
router/http.go          Fiber app, middleware chain, controllers mounted per resource (+ integration tests)
controllers/            HTTP handlers, one file per entity; open the transaction, call services, write the envelope
sanitizers/             request → model / filter, with field validation messages
services/               business rules (interface + implementation), given the transaction by the caller
repositories/           SQL per entity (interface + implementation), read and write pools, BeginTx
models/                 entities, request bodies, filters with With…() builders, errors
middlewares/            JWT, refresh tokens, bcrypt, TOTP, auth and plugin middleware
migrations/             schema as Go functions registered by version, applied in one transaction
helper/                 response envelope, errors, principal, language, logging (Log / Capture)
i18n/                   English / Bahasa Indonesia: Lang.T(), Accept-Language parsing, dictionary (id.go)
export/                 XLSX, CSV, PDF writer (text, tables, Code 128, QR)
docs/                   generated Swagger spec (make doc), served at /swagger/index.html outside production
seeds/                  demo store, created through the HTTP API
internal/api, crud      endpoints not converted to the layers above yet (see "Conversion status")
```

Resources that are plain records (products, customers, taxes, …) are declared once as a `crud.Resource`
(fields, types, validation, lookups, filters, hooks); the engine generates SQL, validation errors, the JSON
shape, CSV/XLSX import & export and an import template. Business operations (sales, purchases, stock,
transfers, debts, billing, reports) are explicit handlers that run in transactions.

## Conversion status

The API is being moved from one generic CRUD engine to explicit model / sanitizer / repository / service / controller
files per entity. Converted so far (114 operations, all with Swagger examples):

- master data: taxes, categories, cash drawers, suppliers, customer types, customers, cash in & out, discounts, fees,
  payment methods, promotions, commissions;
- products and stock: products (SKU, duplicate, barcodes, labels), branch products, stock levels (adjustments,
  history, import, recalculation), stock opname, stock transfers.

Still served by `internal/api` and `internal/crud`, behaving as before: sales and purchases with their returns and
debts, branches, staff, online store, PPOB, account, settings, billing, dashboard, reports. Both packages are deleted
when the last group is converted. Until then the remaining sale and purchase handlers change stock through the old
`productStock` helper; converted code uses `StockRepository.ChangeStock`, the single place stock changes.

## Languages

The API answers in **English** or **Bahasa Indonesia**:

1. `Accept-Language` (`id`, `id-ID`, `en`, … with q-values) — both clients send it on every request;
2. otherwise the branch's display setting (`PUT /settings/display {"language": "id"}`);
3. otherwise English.

Translated: error and validation messages (`error.message`, `error.fields`), import row errors, notifications and
activity-log descriptions, dashboard alerts, missions, plugins, integrations and loan partners, payment instructions,
invoice descriptions, outgoing codes and invitations, and generated files — report exports (XLSX / CSV / PDF titles,
column headers, status values), receipts, invoices, delivery notes, transfer notes, shift reports and menu books.
CRUD exports and import templates keep their machine headers (`sku`, `name`, …) so they can be imported again.
Responses carry `Content-Language` and `Vary: Accept-Language`.

Text is written in English with `{placeholders}` and translated where the language is known:

```go
return httpx.Invalid("items", "Row {row}: product not found.", "row", i+1)   // errors: rendered per request
a.notify(ctx, tx, store, &to, "back_office", "Transfer accepted", "Transfer {number} was received.", "report-transfers", "number", n)
tr := httpx.Lang(c).T                                                         // documents
d.Text(40, 790, 16, true, tr("DELIVERY NOTE"))
```

Notifications and activity logs are stored untranslated with their placeholder values (`params`, migration
`0003_i18n`) and translated when listed, so each reader sees their own language. The dictionary is
`i18n/id.go`; `go test ./i18n` fails when a message has no translation, when placeholders differ,
or when a message is built with `+` / `fmt.Sprintf` instead of a template. Terminology follows lestari-client
`js/i18n/id.js`.

## Behaviour worth knowing

- **Passwords are base64 on the wire.** `password`, `current_password`, `new_password` and
  `new_password_confirmation` (login, register, change password, staff and branch accounts, 2FA off, PIN change) must be
  the base64 of the UTF-8 password (standard alphabet; unpadded is accepted). Anything else is a `422` field error.
  PINs (`new_pin`, `ppob_pin`) stay plain digits. This is an encoding for transport, not encryption — run the API
  behind HTTPS. Passwords are stored as bcrypt hashes of the decoded value.
- **Ids are UUID v7.** Every table's primary key is `id UUID PRIMARY KEY DEFAULT uuidv7()` (PostgreSQL 18's built-in
  generator), and every foreign key / id array is `UUID` / `UUID[]`. The API never generates ids itself: inserts omit
  `id` and read it back with `RETURNING id`. UUID v7 is time-ordered, so `ORDER BY id` still follows creation order
  and inserts stay index-friendly. In JSON ids are strings. A path id that is not a UUID answers `404`; a malformed id
  in a body answers `422` (field validation) or `400`. Provinces keep their Kemendagri number in `code`; missions
  (a fixed catalogue, not a table) use their `code` as `id`.
- **Tenancy.** A store has branches; operational data belongs to a branch. Every request acts on the user's
  branch, or on the branch in `X-Branch-Id` (owner/administrator only). Lookups referenced by id are checked to
  belong to the caller's branch/store.
- **Stock ledger.** Every stock change (opening stock, sale, purchase, adjustment, opname, transfer, return,
  void, online order) goes through one function and writes `stock_movements`; `POST /inventory/stocks/recalculate`
  rebuilds stock from that ledger. Sales refuse to oversell unless the store setting `allow_negative_stock` is on.
- **Costing.** Purchases and stock received with a cost update the product cost by weighted average; sales record
  the unit cost at the moment of sale, so profit reports stay correct when costs change. Products with a recipe
  (`ingredients`) deduct and cost their raw materials instead.
- **Pricing on a sale.** Variant price → customer-type or wholesale tier → product discount → active promotion
  (product discount or buy X get Y) → invoice discount (manual + customer type + minimum-purchase promotion +
  redeemed points) → service charge → tax (inclusive or exclusive) → fees → rounding.
- **Credit.** Credit sales and purchases create receivables/payables; payments update status on both the debt and
  the document.
- **Updates are partial.** `PUT` writes only the attributes present in the body; send `null` to clear one.
- **Plugins.** Features of paid plugins answer `402 plugin_required` until the store has the plugin
  (`/plugins/{code}/trial` starts a 7-day trial).
- **Payments.** Invoices and PPOB top-ups paid with premium coins are applied immediately. Other methods go through
  the `api.Payments` interface; the default implementation returns manual-transfer instructions. A gateway calls
  `POST /api/v1/webhooks/payments` with `{"reference":"INV/…","status":"paid"}` and header
  `X-Signature: hex(HMAC-SHA256(JWT_SECRET, body))`.
- **Messages.** OTPs and invitations go through the `api.Sender` interface; the default writes them to the log.
  Plug in an SMS/WhatsApp/e-mail provider there.
- **Background jobs** (stock recalculation, copying products to a branch, data reset) run in-process and are
  recorded in the `jobs` table.

### Endpoints beyond the back-office contract

| Endpoint | For |
|---|---|
| `POST /auth/register` | sign-up of a new store (owner, head office, defaults) |
| `POST /sales`, `POST /sales/{id}/returns` | cashier app: record a sale / a customer return |
| `POST /shifts`, `POST /shifts/{id}/close` | cashier app: open and close a cash-drawer shift |
| `POST /ppob/transactions` | cashier app: sell a PPOB product from the branch balance |
| `POST /staff-attendances/clock-in`, `…/clock-out` | cashier app: attendance plugin |
| `POST /store-ratings` | customer feedback from a receipt link or the cashier app |
| `POST /catalog/orders` | online store: place an order |
| `GET /billing/invoices/export` | invoice download |
| `POST /webhooks/payments` | payment gateway callback (public, signed) |

`POST /sales` accepts `items[{product_id, quantity, variant_name?, unit_price?, discount_amount?, points_redeemed?}]`,
`customer_id`, `payment_method`, `payment_type` (`cash`/`credit`), `paid_amount`, `discount_total`, `tax_id`,
`fee_ids`, `points_redeemed`, `shift_id`, `label`, `note`, `due_date`, and optionally `created_at` and
`receipt_number` for offline sync.

## Tests

```bash
make test                         # unit tests (route coverage of the client contract)
make test-integration             # + integration tests against PostgreSQL 18 (needs uuidv7())
make lint                         # go vet + staticcheck (all checks) + golangci-lint (.golangci.yml)
```

The integration suite (`TEST_DATABASE_URL`) migrates a database, seeds the demo store through the API, then:
calls every GET endpoint (lists, details, reports, PDF/XLSX/CSV downloads) with real ids, writes every settings page
and every editable record back unchanged, and runs the main workflows (wholesale pricing, oversell refusal, stock
adjustment and opname, credit purchase → payable → payment, transfer with confirmation, vouchers, coin purchases,
order status rules, uploads, error envelopes, UUID v7 ids and malformed-id handling, base64 passwords, and messages,
notifications and files in Bahasa Indonesia via `Accept-Language` or the display setting).

`router/testdata/endpoints.txt` is the client's endpoint list; refresh it when the client changes
(`node tools/generate-endpoints.js` in the client repo, then list `METHOD path` lines).

## Data

- Regions: all Indonesian provinces and a starter set of cities/regencies (`0002_regions.sql`); load the full
  regency list for production.
- PPOB: every new branch gets a starter price list (`internal/api/ppob.go`); connect a PPOB aggregator to sell for real.
