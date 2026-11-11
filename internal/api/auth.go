package api

import (
	"context"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/repositories"
)

func (a *API) registerPublic() {
	a.addH("POST", "/auth/login", a.login)
	a.addH("POST", "/auth/refresh", a.refresh)
	a.addH("POST", "/auth/register", a.register) // sign-up for a new store (not used by the back-office client)
	a.addH("POST", "/webhooks/payments", a.paymentWebhook)
}

func (a *API) registerAuth() {
	a.addH("POST", "/auth/logout", a.logout)
	a.addH("GET", "/auth/me", a.me)
	a.addH("PUT", "/auth/password", a.changePassword)
}

func (a *API) login(c fiber.Ctx) error {
	var in struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		RememberMe bool   `json:"remember_me"`
		OTP        string `json:"otp"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	v := &helper.Validation{}
	if strings.TrimSpace(in.Email) == "" {
		v.Add("email", "Required")
	}
	encoded := in.Password != ""
	helper.Passwords(v, map[string]*string{"password": &in.Password})
	if in.Password == "" && !encoded {
		v.Add("password", "Required")
	}
	if err := v.Err(); err != nil {
		return err
	}
	var (
		id, storeID  string
		hash, status string
		tfaOn        bool
		tfaSecret    *string
		recovery     []string
	)
	err := a.DB.QueryRow(c.Context(), `SELECT id, store_id, password_hash, status, two_factor_enabled, two_factor_secret, recovery_codes
		FROM users WHERE lower(email) = lower($1) AND deleted_at IS NULL`, strings.TrimSpace(in.Email)).
		Scan(&id, &storeID, &hash, &status, &tfaOn, &tfaSecret, &recovery)
	if err != nil || !middlewares.CheckPassword(hash, in.Password) {
		return helper.Unauthorized("invalid_credentials", "Email or password is incorrect.")
	}
	if status == "disabled" {
		return helper.Forbidden("This account has been disabled.")
	}
	if tfaOn && tfaSecret != nil {
		if in.OTP == "" {
			return helper.Unauthorized("two_factor_required", "Enter the code from your authenticator app.")
		}
		if !middlewares.VerifyTOTP(*tfaSecret, in.OTP) {
			used := -1
			for i, rc := range recovery {
				if rc == middlewares.SHA256(strings.TrimSpace(in.OTP)) {
					used = i
				}
			}
			if used < 0 {
				return helper.Unauthorized("invalid_otp", "The verification code is incorrect.")
			}
			recovery = append(recovery[:used], recovery[used+1:]...)
			_, _ = a.DB.Exec(c.Context(), `UPDATE users SET recovery_codes = $2 WHERE id = $1`, id, recovery)
		}
	}
	tok, err := a.Auth.Issue(c.Context(), id, storeID, in.RememberMe)
	if err != nil {
		return err
	}
	_, _ = a.DB.Exec(c.Context(), `UPDATE users SET last_login_at = now(), status = CASE WHEN status = 'invited' THEN 'active' ELSE status END WHERE id = $1`, id)
	_, _ = a.DB.Exec(c.Context(), `INSERT INTO activity_logs (store_id, user_id, action, description, ip_address, user_agent) VALUES ($1,$2,'sign_in','Signed in',$3,$4)`,
		storeID, id, c.IP(), c.Get("User-Agent"))
	user, err := a.userView(c.Context(), id, "")
	if err != nil {
		return err
	}
	return helper.OK(c, fiber.Map{"access_token": tok.AccessToken, "refresh_token": tok.RefreshToken, "token_type": tok.TokenType, "expires_in": tok.ExpiresIn, "user": user})
}

func (a *API) refresh(c fiber.Ctx) error {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	tok, err := a.Auth.Refresh(c.Context(), in.RefreshToken)
	if err != nil {
		return err
	}
	return helper.OK(c, fiber.Map{"access_token": tok.AccessToken, "refresh_token": tok.RefreshToken, "expires_in": tok.ExpiresIn})
}

func (a *API) logout(c fiber.Ctx) error {
	if err := a.Auth.RevokeAll(c.Context(), helper.P(c).UserID); err != nil {
		return err
	}
	return helper.NoContent(c)
}

func (a *API) me(c fiber.Ctx) error {
	p := helper.P(c)
	u, err := a.userView(c.Context(), p.UserID, p.BranchID)
	if err != nil {
		return err
	}
	return helper.OK(c, u)
}

// userView is the shape of GET /auth/me and the login user.
func (a *API) userView(ctx context.Context, userID, branchID string) (map[string]any, error) {
	u, err := repositories.JSONRow(ctx, a.DB, `
		SELECT u.id, u.name, u.email, u.role, u.avatar_url, u.permissions, u.two_factor_enabled, u.phone, u.phone_verified,
		  json_build_object('id', s.id, 'name', s.name, 'logo_url', (SELECT value->>'logo_url' FROM settings st JOIN branches hb ON hb.id = st.branch_id WHERE hb.store_id = s.id AND hb.is_head_office AND st.key = 'store'),
		                    'plan', s.plan, 'plan_expires_at', s.plan_expires_at) AS store,
		  (SELECT json_build_object('id', b.id, 'name', b.name, 'is_head_office', b.is_head_office) FROM branches b
		    WHERE b.id = COALESCE($2::uuid, u.branch_id, (SELECT id FROM branches WHERE store_id = s.id AND is_head_office ORDER BY id LIMIT 1))) AS branch,
		  COALESCE((SELECT array_agg(code ORDER BY code) FROM store_plugins sp WHERE sp.store_id = s.id AND sp.status IN ('active','trial') AND (sp.expires_at IS NULL OR sp.expires_at > now())), '{}') AS active_plugins
		FROM users u JOIN stores s ON s.id = u.store_id WHERE u.id = $1`, userID, nullID(branchID))
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, helper.NotFound("User")
	}
	if u["role"] == "owner" || u["role"] == "administrator" {
		u["permissions"] = []string{"*"}
	}
	return u, nil
}

func (a *API) changePassword(c fiber.Ctx) error {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
		Confirm string `json:"new_password_confirmation"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	p := helper.P(c)
	var hash string
	if err := a.DB.QueryRow(c.Context(), `SELECT password_hash FROM users WHERE id = $1`, p.UserID).Scan(&hash); err != nil {
		return err
	}
	v := &helper.Validation{}
	helper.Passwords(v, map[string]*string{"current_password": &in.Current, "new_password": &in.New, "new_password_confirmation": &in.Confirm})
	if err := v.Err(); err != nil {
		return err
	}
	if !middlewares.CheckPassword(hash, in.Current) {
		v.Add("current_password", "Current password is incorrect.")
	}
	if len(in.New) < 8 {
		v.Add("new_password", "Use at least 8 characters.")
	}
	if in.New != in.Confirm {
		v.Add("new_password_confirmation", "Passwords do not match.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	nh, err := middlewares.HashPassword(in.New)
	if err != nil {
		return err
	}
	if _, err := a.DB.Exec(c.Context(), `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, p.UserID, nh); err != nil {
		return err
	}
	a.activity(c, "password_changed", "Changed password")
	return helper.OK(c, fiber.Map{"changed": true})
}

// register creates a new store with its owner and head office.
func (a *API) register(c fiber.Ctx) error {
	var in struct {
		StoreName string `json:"store_name"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		Password  string `json:"password"`
		Phone     string `json:"phone"`
		Referral  string `json:"referral_code"`
	}
	if err := helper.Decode(c, &in); err != nil {
		return err
	}
	v := &helper.Validation{}
	helper.Passwords(v, map[string]*string{"password": &in.Password})
	if strings.TrimSpace(in.StoreName) == "" {
		v.Add("store_name", "Required")
	}
	if strings.TrimSpace(in.Name) == "" {
		v.Add("name", "Required")
	}
	if !strings.Contains(in.Email, "@") {
		v.Add("email", "Enter a valid email.")
	}
	if len(in.Password) < 8 {
		v.Add("password", "Use at least 8 characters.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	var userID, storeID string
	err := a.tx(c.Context(), func(tx pgx.Tx) error {
		var err error
		userID, storeID, err = a.Provision(c.Context(), tx, Provision{StoreName: in.StoreName, OwnerName: in.Name, Email: in.Email, Password: in.Password, Phone: in.Phone, ReferralCode: in.Referral})
		return err
	})
	if err != nil {
		return err
	}
	tok, err := a.Auth.Issue(c.Context(), userID, storeID, true)
	if err != nil {
		return err
	}
	user, _ := a.userView(c.Context(), userID, "")
	return helper.Created(c, fiber.Map{"access_token": tok.AccessToken, "refresh_token": tok.RefreshToken, "token_type": tok.TokenType, "expires_in": tok.ExpiresIn, "user": user})
}

type Provision struct {
	StoreName, OwnerName, Email, Password, Phone, ReferralCode string
}

// Provision creates a store, its head office, the owner account and default data.
func (a *API) Provision(ctx context.Context, tx pgx.Tx, in Provision) (userID, storeID string, err error) {
	var referredBy *string
	if in.ReferralCode != "" {
		var id string
		if tx.QueryRow(ctx, `SELECT id FROM stores WHERE upper(referral_code) = upper($1)`, in.ReferralCode).Scan(&id) == nil {
			referredBy = &id
		}
	}
	trialEnd := time.Now().AddDate(0, 0, 14)
	if err = tx.QueryRow(ctx, `INSERT INTO stores (name, plan, plan_expires_at, referred_by_store_id) VALUES ($1,'pro_trial',$2,$3) RETURNING id`,
		strings.TrimSpace(in.StoreName), trialEnd, referredBy).Scan(&storeID); err != nil {
		return
	}
	var branchID string
	if err = tx.QueryRow(ctx, `INSERT INTO branches (store_id, name, code, is_head_office, phone, email, subscription_expires_at) VALUES ($1,$2,'HQ',true,$3,$4,$5) RETURNING id`,
		storeID, strings.TrimSpace(in.StoreName), in.Phone, in.Email, trialEnd).Scan(&branchID); err != nil {
		return
	}
	hash, err := middlewares.HashPassword(in.Password)
	if err != nil {
		return
	}
	err = tx.QueryRow(ctx, `INSERT INTO users (store_id, branch_id, name, email, password_hash, phone, role, notifications)
		VALUES ($1,$2,$3,lower($4),$5,NULLIF($6,''),'owner',$7) RETURNING id`,
		storeID, branchID, strings.TrimSpace(in.OwnerName), strings.TrimSpace(in.Email), hash, in.Phone, defaultNotifications).Scan(&userID)
	if repositories.IsUniqueViolation(err) {
		err = helper.Invalid("email", "This email is already registered.")
		return
	}
	if err != nil {
		return
	}
	if err = a.seedBranch(ctx, tx, branchID); err != nil {
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO settings (branch_id, key, value) VALUES ($1, 'store', jsonb_build_object('name', $2::text, 'owner_name', $3::text, 'phone', $4::text, 'currency', 'IDR', 'timezone', 'Asia/Jakarta', 'stock_valuation_method', 'average', 'country_code', 'ID'))`,
		branchID, strings.TrimSpace(in.StoreName), strings.TrimSpace(in.OwnerName), in.Phone)
	return
}

var defaultNotifications = map[string]any{
	"app":      map[string]bool{"billing": true, "ppob": true, "promotions": true, "newsletter": false},
	"email":    map[string]bool{"billing": true, "promotions": false, "newsletter": false},
	"whatsapp": map[string]bool{"billing": true, "promotions": false, "newsletter": false},
}

// seedBranch gives a new branch its default payment methods, cash drawer and PPOB price list.
func (a *API) seedBranch(ctx context.Context, tx pgx.Tx, branchID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO payment_methods (branch_id, name, type) VALUES ($1,'Cash','cash'),($1,'Debit card','debit'),($1,'QRIS','qris'),($1,'Bank transfer','bank_transfer')`, branchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO cash_drawers (branch_id, name) VALUES ($1,'Main drawer')`, branchID); err != nil {
		return err
	}
	for _, p := range ppobCatalog {
		if _, err := tx.Exec(ctx, `INSERT INTO ppob_prices (branch_id, kind, category, provider, product_code, product_name, base_price, selling_price, base_admin_fee, admin_fee)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`,
			branchID, p.Kind, p.Category, p.Provider, p.Code, p.Name, p.BasePrice, p.SellingPrice, p.BaseAdminFee, p.AdminFee); err != nil {
			return err
		}
	}
	return nil
}
