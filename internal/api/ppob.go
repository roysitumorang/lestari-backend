package api

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/internal/auth"
	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

type ppobItem struct {
	Kind, Category, Provider, Code, Name            string
	BasePrice, SellingPrice, BaseAdminFee, AdminFee *float64
}

func prepaid(cat, provider, code, name string, base, sell float64) ppobItem {
	return ppobItem{"prepaid", cat, provider, code, name, new(base), new(sell), nil, nil}
}
func postpaid(cat, code, name string, fee float64) ppobItem {
	return ppobItem{"postpaid", cat, "", code, name, nil, nil, new(fee), new(fee + 1000)}
}

// ppobCatalog is the starter price list given to every new branch.
var ppobCatalog = []ppobItem{
	prepaid("pulsa", "Telkomsel", "TSEL10", "Telkomsel 10.000", 10250, 12000),
	prepaid("pulsa", "Telkomsel", "TSEL25", "Telkomsel 25.000", 24900, 27000),
	prepaid("pulsa", "Telkomsel", "TSEL50", "Telkomsel 50.000", 49500, 52000),
	prepaid("pulsa", "Indosat", "ISAT10", "Indosat 10.000", 10600, 12000),
	prepaid("pulsa", "XL", "XL10", "XL 10.000", 10500, 12000),
	prepaid("pulsa", "Tri", "TRI10", "Tri 10.000", 10300, 12000),
	prepaid("data", "Telkomsel", "TSELD5GB", "Telkomsel Data 5 GB 30 days", 52000, 56000),
	prepaid("data", "XL", "XLD10GB", "XL Data 10 GB 30 days", 64000, 69000),
	prepaid("pln_token", "PLN", "PLN20", "PLN Token 20.000", 20500, 22500),
	prepaid("pln_token", "PLN", "PLN50", "PLN Token 50.000", 50500, 52500),
	prepaid("pln_token", "PLN", "PLN100", "PLN Token 100.000", 100500, 102500),
	prepaid("e_money", "GoPay", "GOPAY50", "GoPay 50.000", 51000, 53000),
	prepaid("e_money", "OVO", "OVO50", "OVO 50.000", 51000, 53000),
	prepaid("e_money", "DANA", "DANA50", "DANA 50.000", 50800, 52500),
	prepaid("game_voucher", "Mobile Legends", "ML86", "Mobile Legends 86 Diamonds", 22000, 25000),
	prepaid("game_voucher", "Free Fire", "FF100", "Free Fire 100 Diamonds", 14500, 17000),
	prepaid("streaming", "Vidio", "VIDIO30", "Vidio Premier 30 days", 29000, 33000),
	postpaid("pln_postpaid", "PLNPOST", "PLN Postpaid", 2500),
	postpaid("pdam", "PDAM", "PDAM Water", 2500),
	postpaid("bpjs", "BPJSKES", "BPJS Kesehatan", 2500),
	postpaid("telkom", "TELKOM", "Telkom / IndiHome", 2500),
	postpaid("gas_postpaid", "PGN", "PGN Gas", 2500),
	postpaid("multifinance", "FIF", "FIF Group", 3000),
	postpaid("internet_tv", "BIZNET", "Biznet Home", 2500),
}

func ppobBalance(ctx context.Context, q db.Querier, branchID string) (float64, error) {
	var bal float64
	err := q.QueryRow(ctx, `SELECT COALESCE((SELECT sum(amount) FROM ppob_deposits WHERE branch_id=$1 AND status='success'), 0)
		- COALESCE((SELECT sum(base_price) FROM ppob_transactions WHERE branch_id=$1 AND status IN ('success','pending')), 0)`, branchID).Scan(&bal)
	return bal, err
}

func (a *API) registerPPOB() {
	a.addH("GET", "/ppob/deposits", a.listDeposits)
	a.addH("POST", "/ppob/deposits", a.createDeposit)
	a.addH("POST", "/ppob/deposits/:id/payment-proof", a.depositProof)
	a.addH("POST", "/ppob/deposits/:id/cancel", a.cancelDeposit)
	a.addH("POST", "/ppob/transactions", a.sellPPOB) // cashier app

	for _, kind := range []string{"prepaid", "postpaid"} {
		fields := []crud.Field{
			{Name: "category", ReadOnly: true, Filter: true},
			{Name: "provider", ReadOnly: true, Search: true},
			{Name: "product_code", ReadOnly: true, Search: true},
			{Name: "product_name", ReadOnly: true, Search: true},
			{Name: "is_active", Kind: crud.Bool},
		}
		if kind == "prepaid" {
			fields = append(fields, crud.Field{Name: "base_price", Kind: crud.Num, ReadOnly: true}, crud.Field{Name: "selling_price", Kind: crud.Num, Required: true, Min: new(float64(0))})
		} else {
			fields = append(fields, crud.Field{Name: "base_admin_fee", Kind: crud.Num, ReadOnly: true}, crud.Field{Name: "admin_fee", Kind: crud.Num, Required: true, Min: new(float64(0))})
		}
		k := kind
		res := &crud.Resource{
			Name: "PPOB " + kind + " prices", Singular: "Price", Table: "ppob_prices", Where: "t.kind = '" + kind + "'", DefaultSort: "t.category, t.product_name",
			ImportKey: "product_code", Fields: fields,
			BeforeWrite: func(ctx context.Context, h *crud.Ctx, vals map[string]any, id string) error {
				if id == "" {
					return httpx.Forbidden("New PPOB products come from the provider catalog.")
				}
				if k == "prepaid" {
					if sp, ok := vals["selling_price"].(float64); ok {
						var base float64
						_ = h.Tx.QueryRow(ctx, `SELECT COALESCE(base_price,0) FROM ppob_prices WHERE id=$1`, id).Scan(&base)
						if sp < base {
							return httpx.Invalid("selling_price", "The selling price cannot be below the cost of {cost}.", "cost", trimNum(base))
						}
					}
				}
				return nil
			},
		}
		a.CRUD.Register(a.addH, "/ppob/prices/"+kind, res, "list", "update", "import", "export")
	}
}

const depositCols = `id, reference_id, type, payment_method, amount, admin_fee, balance_after, status, payment_url, payment_instructions, payment_proof_url, expires_at, created_at`

func (a *API) listDeposits(c fiber.Ctx) error {
	p := httpx.P(c)
	_, _ = a.DB.Exec(c.Context(), `UPDATE ppob_deposits SET status='expired', updated_at=now() WHERE branch_id=$1 AND status='pending' AND expires_at < now()`, p.BranchID)
	start, end, err := httpx.DateRange(c, a.Loc)
	if err != nil {
		return err
	}
	pg := httpx.Pagination(c, 50)
	where := `branch_id=$1 AND created_at::date BETWEEN $2::date AND $3::date AND ($4 = '' OR reference_id ILIKE '%'||$4||'%' OR COALESCE(payment_method,'') ILIKE '%'||$4||'%')`
	args := []any{p.BranchID, start, end, c.Query("q")}
	var total int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM ppob_deposits WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	rows, err := db.JSONRaw(c.Context(), a.DB, `SELECT `+depositCols+` FROM ppob_deposits WHERE `+where+fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d OFFSET %d`, pg.PerPage, pg.Offset), args...)
	if err != nil {
		return err
	}
	bal, err := ppobBalance(c.Context(), a.DB, p.BranchID)
	if err != nil {
		return err
	}
	var trx float64
	_ = a.DB.QueryRow(c.Context(), `SELECT COALESCE(sum(selling_price),0) FROM ppob_transactions WHERE branch_id=$1 AND status='success'`, p.BranchID).Scan(&trx)
	return c.JSON(fiber.Map{"data": rows, "summary": fiber.Map{"balance": bal, "transaction_total": trx}, "meta": httpx.NewMeta(pg, total)})
}

func (a *API) createDeposit(c fiber.Ctx) error {
	var in struct {
		Amount        float64 `json:"amount"`
		PaymentMethod string  `json:"payment_method"`
		VoucherCode   string  `json:"voucher_code"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	p := httpx.P(c)
	v := &httpx.Validation{}
	if in.Amount < 100000 {
		v.Add("amount", "The minimum top-up is Rp 100,000.")
	}
	if !validPayMethod(in.PaymentMethod, false) {
		v.Add("payment_method", "Choose a payment method.")
	}
	bal, err := ppobBalance(c.Context(), a.DB, p.BranchID)
	if err != nil {
		return err
	}
	if bal+in.Amount > 2000000 {
		v.Add("amount", "The PPOB balance cannot exceed Rp 2,000,000.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	fee := 0.0
	if in.PaymentMethod != "manual_transfer" {
		fee = 4000
	}
	if in.VoucherCode != "" {
		var ok bool
		_ = a.DB.QueryRow(c.Context(), `SELECT EXISTS(SELECT 1 FROM vouchers WHERE upper(code)=upper($1) AND reward_type='ppob_free_fee' AND used_count < max_uses AND (expires_at IS NULL OR expires_at > now()))`, in.VoucherCode).Scan(&ok)
		if !ok {
			return httpx.Invalid("voucher_code", "This voucher is not valid.")
		}
		fee = 0
		_, _ = a.DB.Exec(c.Context(), `UPDATE vouchers SET used_count = used_count + 1 WHERE upper(code)=upper($1)`, in.VoucherCode)
	}
	ref := "PPOB-" + a.now().Format("060102") + "-" + auth.RandomDigits(6)
	url, instructions, err := a.Payments.Create(c.Context(), ref, in.Amount+fee, in.PaymentMethod, httpx.Lang(c))
	if err != nil {
		return err
	}
	row, err := db.JSONRow(c.Context(), a.DB, `INSERT INTO ppob_deposits (branch_id, reference_id, type, payment_method, amount, admin_fee, status, payment_url, payment_instructions, expires_at)
		VALUES ($1,$2,'top_up',$3,$4,$5,'pending',NULLIF($6,''),$7, now() + interval '24 hours') RETURNING `+depositCols,
		p.BranchID, ref, in.PaymentMethod, in.Amount, fee, url, instructions)
	if err != nil {
		return err
	}
	return httpx.OK(c, row)
}

func (a *API) deposit(c fiber.Ctx, id string) (map[string]any, error) {
	row, err := db.JSONRow(c.Context(), a.DB, `SELECT `+depositCols+` FROM ppob_deposits WHERE id=$1 AND branch_id=$2`, id, httpx.P(c).BranchID)
	if err == nil && row == nil {
		err = httpx.NotFound("Deposit")
	}
	return row, err
}

func (a *API) depositProof(c fiber.Ctx) error {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		return err
	}
	d, err := a.deposit(c, id)
	if err != nil {
		return err
	}
	if d["status"] != "pending" {
		return httpx.Conflict("Only a pending top-up accepts a payment proof.")
	}
	up, err := a.saveUpload(c, "file", "payment_proof")
	if err != nil {
		return err
	}
	if _, err := a.DB.Exec(c.Context(), `UPDATE ppob_deposits SET payment_proof_url=$2, updated_at=now() WHERE id=$1`, id, up["url"]); err != nil {
		return err
	}
	d, err = a.deposit(c, id)
	if err != nil {
		return err
	}
	return httpx.OK(c, d)
}

func (a *API) cancelDeposit(c fiber.Ctx) error {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		return err
	}
	tag, err := a.DB.Exec(c.Context(), `UPDATE ppob_deposits SET status='cancelled', updated_at=now() WHERE id=$1 AND branch_id=$2 AND status='pending'`, id, httpx.P(c).BranchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.Conflict("Only a pending top-up can be cancelled.")
	}
	d, err := a.deposit(c, id)
	if err != nil {
		return err
	}
	return httpx.OK(c, d)
}

// sellPPOB records a PPOB sale made by the cashier and charges the branch balance.
func (a *API) sellPPOB(c fiber.Ctx) error {
	var in struct {
		PriceID        string  `json:"price_id"`
		CustomerNumber string  `json:"customer_number"`
		BillAmount     float64 `json:"bill_amount"` // postpaid bill from the biller
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	p := httpx.P(c)
	if in.CustomerNumber == "" {
		return httpx.Invalid("customer_number", "Required")
	}
	var out map[string]any
	err := a.tx(c.Context(), func(tx pgx.Tx) error {
		ctx := c.Context()
		var kind, name string
		var base, sell, baseFee, fee *float64
		var active bool
		if err := tx.QueryRow(ctx, `SELECT kind, product_name, base_price, selling_price, base_admin_fee, admin_fee, is_active FROM ppob_prices WHERE id=$1 AND branch_id=$2`, in.PriceID, p.BranchID).
			Scan(&kind, &name, &base, &sell, &baseFee, &fee, &active); err != nil || !active {
			return httpx.Invalid("price_id", "This PPOB product is not available.")
		}
		var cost, price float64
		if kind == "prepaid" {
			cost, price = *base, *sell
		} else {
			if in.BillAmount <= 0 {
				return httpx.Invalid("bill_amount", "Enter the bill amount.")
			}
			cost, price = in.BillAmount+*baseFee, in.BillAmount+*fee
		}
		bal, err := ppobBalance(ctx, tx, p.BranchID)
		if err != nil {
			return err
		}
		if bal < cost {
			return httpx.PaymentRequired("insufficient_balance", "The PPOB balance is too low. Top up first.")
		}
		ref := "TRX" + a.now().Format("060102150405") + auth.RandomDigits(3)
		out, err = db.JSONRow(ctx, tx, `INSERT INTO ppob_transactions (branch_id, reference_id, product_name, customer_number, base_price, selling_price, cashier_id, cashier_name, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'success') RETURNING id, reference_id, product_name, customer_number, base_price, selling_price, selling_price - base_price AS profit, status, created_at`,
			p.BranchID, ref, name, in.CustomerNumber, cost, price, p.UserID, p.Name)
		return err
	})
	if err != nil {
		return err
	}
	return httpx.Created(c, out)
}
