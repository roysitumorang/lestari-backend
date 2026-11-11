package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/roysitumorang/lestari-backend/internal/auth"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

func (a *API) registerAccount() {
	a.addH("GET", "/account/profile", a.getProfile)
	a.addH("PUT", "/account/profile", a.putProfile)
	a.addH("PUT", "/account/ppob-pin", a.changePPOBPin)
	a.addH("POST", "/account/phone-verification", a.sendPhoneOTP)
	a.addH("POST", "/account/phone-verification/confirm", a.confirmPhoneOTP)
	a.addH("POST", "/account/two-factor/setup", a.twoFactorSetup)
	a.addH("POST", "/account/two-factor/enable", a.twoFactorEnable)
	a.addH("POST", "/account/two-factor/disable", a.twoFactorDisable)
	a.addH("GET", "/account/activity-logs", a.myActivity)
	a.addH("POST", "/account/deactivation-requests", a.requestDeactivation)
}

func (a *API) profileRow(ctx context.Context, id string) (map[string]any, error) {
	return db.JSONRow(ctx, a.DB, `SELECT avatar_url, name, username, email, phone, phone_verified, birth_year, gender, address, notifications, two_factor_enabled
		FROM users WHERE id = $1`, id)
}

func (a *API) getProfile(c fiber.Ctx) error {
	row, err := a.profileRow(c.Context(), httpx.P(c).UserID)
	if err != nil {
		return err
	}
	return httpx.OK(c, row)
}

func (a *API) putProfile(c fiber.Ctx) error {
	body, err := httpx.Body(c)
	if err != nil {
		return err
	}
	p := httpx.P(c)
	v := &httpx.Validation{}
	name := httpx.Str(body, "name")
	if name == "" {
		v.Add("name", "Required")
	}
	var birth any
	if y, ok := httpx.Int(body, "birth_year"); ok {
		if y < 1900 || y > int64(time.Now().Year()) {
			v.Add("birth_year", "Enter a valid year.")
		}
		birth = y
	}
	gender := httpx.Str(body, "gender")
	if gender != "" && gender != "male" && gender != "female" {
		v.Add("gender", "Must be one of: male, female.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	notif := defaultNotifications
	if n, ok := body["notifications"].(map[string]any); ok {
		notif = n
	}
	nb, _ := json.Marshal(notif)
	phone := httpx.Str(body, "phone")
	_, err = a.DB.Exec(c.Context(), `UPDATE users SET name=$2, username=NULLIF($3,''), avatar_url=NULLIF($4,''), birth_year=$5, gender=NULLIF($6,''), address=NULLIF($7,''),
		phone_verified = CASE WHEN COALESCE(phone,'') = $8 THEN phone_verified ELSE false END, phone=NULLIF($8,''), notifications=$9::jsonb, updated_at=now() WHERE id=$1`,
		p.UserID, name, httpx.Str(body, "username"), httpx.Str(body, "avatar_url"), birth, gender, httpx.Str(body, "address"), phone, string(nb))
	if err != nil {
		return err
	}
	a.activity(c, "profile_updated", "Updated profile")
	return a.getProfile(c)
}

func (a *API) changePPOBPin(c fiber.Ctx) error {
	var in struct {
		Password string `json:"password"`
		NewPin   string `json:"new_pin"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	p := httpx.P(c)
	var hash string
	if err := a.DB.QueryRow(c.Context(), `SELECT password_hash FROM users WHERE id=$1`, p.UserID).Scan(&hash); err != nil {
		return err
	}
	v := &httpx.Validation{}
	httpx.Passwords(v, map[string]*string{"password": &in.Password})
	if err := v.Err(); err != nil {
		return err
	}
	if !auth.CheckPassword(hash, in.Password) {
		v.Add("password", "Password is incorrect.")
	}
	if len(in.NewPin) != 6 || strings.Trim(in.NewPin, "0123456789") != "" {
		v.Add("new_pin", "The PIN must be 6 digits.")
	}
	if err := v.Err(); err != nil {
		return err
	}
	ph, _ := auth.HashPassword(in.NewPin)
	if _, err := a.DB.Exec(c.Context(), `UPDATE users SET ppob_pin_hash=$2 WHERE id=$1`, p.UserID, ph); err != nil {
		return err
	}
	return httpx.OK(c, fiber.Map{"changed": true})
}

// ---------- OTPs ----------

func (a *API) createOTP(ctx context.Context, userID string, purpose, target string) (string, error) {
	var recent int
	_ = a.DB.QueryRow(ctx, `SELECT count(*) FROM otp_codes WHERE user_id=$1 AND purpose=$2 AND created_at > now() - interval '1 minute'`, userID, purpose).Scan(&recent)
	if recent > 0 {
		return "", httpx.NewError(429, "too_many_requests", "Please wait a minute before requesting another code.")
	}
	code := auth.RandomDigits(6)
	_, err := a.DB.Exec(ctx, `INSERT INTO otp_codes (user_id, purpose, target, code_hash, expires_at) VALUES ($1,$2,$3,$4, now() + interval '5 minutes')`,
		userID, purpose, target, auth.SHA256(code))
	return code, err
}

// checkOTP consumes a valid code; it allows five attempts per code.
func (a *API) checkOTP(ctx context.Context, userID string, purpose, target, code string) error {
	var id string
	var hash string
	err := a.DB.QueryRow(ctx, `SELECT id, code_hash FROM otp_codes WHERE user_id=$1 AND purpose=$2 AND ($3 = '' OR target=$3) AND used_at IS NULL AND expires_at > now() AND attempts < 5
		ORDER BY id DESC LIMIT 1`, userID, purpose, target).Scan(&id, &hash)
	if err != nil {
		return httpx.Invalid("code", "The code has expired. Request a new one.")
	}
	if hash != auth.SHA256(strings.TrimSpace(code)) {
		_, _ = a.DB.Exec(ctx, `UPDATE otp_codes SET attempts = attempts + 1 WHERE id=$1`, id)
		return httpx.Invalid("code", "The code is incorrect.")
	}
	_, err = a.DB.Exec(ctx, `UPDATE otp_codes SET used_at = now() WHERE id=$1`, id)
	return err
}

func (a *API) sendPhoneOTP(c fiber.Ctx) error {
	var in struct {
		Phone   string `json:"phone"`
		Channel string `json:"channel"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	in.Phone = strings.TrimSpace(in.Phone)
	if len(strings.Trim(in.Phone, "+0123456789 -")) > 0 || len(in.Phone) < 8 {
		return httpx.Invalid("phone", "Enter a valid phone number, e.g. +6281234567890.")
	}
	if in.Channel != "sms" {
		in.Channel = "whatsapp"
	}
	p := httpx.P(c)
	code, err := a.createOTP(c.Context(), p.UserID, "phone", in.Phone)
	if err != nil {
		return err
	}
	if err := a.Sender.Send(c.Context(), in.Channel, in.Phone, httpx.T(c, "Your Lestari verification code is {code}", "code", code)); err != nil {
		return err
	}
	return httpx.OK(c, fiber.Map{"expires_in": 300})
}

func (a *API) confirmPhoneOTP(c fiber.Ctx) error {
	var in struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	p := httpx.P(c)
	if err := a.checkOTP(c.Context(), p.UserID, "phone", strings.TrimSpace(in.Phone), in.Code); err != nil {
		return err
	}
	if _, err := a.DB.Exec(c.Context(), `UPDATE users SET phone=$2, phone_verified=true WHERE id=$1`, p.UserID, strings.TrimSpace(in.Phone)); err != nil {
		return err
	}
	return httpx.OK(c, fiber.Map{"phone_verified": true})
}

// ---------- two-factor ----------

func (a *API) twoFactorSetup(c fiber.Ctx) error {
	p := httpx.P(c)
	secret := auth.NewTOTPSecret()
	if _, err := a.DB.Exec(c.Context(), `UPDATE users SET two_factor_pending=$2 WHERE id=$1`, p.UserID, secret); err != nil {
		return err
	}
	url := auth.OTPAuthURL("Lestari", p.Email, secret)
	svg := ""
	if q, err := qrcode.New(url, qrcode.Medium); err == nil {
		svg = qrSVG(q.Bitmap())
	}
	return httpx.OK(c, fiber.Map{"secret": secret, "otpauth_url": url, "qr_code_svg": svg})
}

func qrSVG(m [][]bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="100%%" height="100%%" fill="#fff"/><path fill="#000" d="`, len(m), len(m))
	for y, row := range m {
		for x, on := range row {
			if on {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x, y)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}

func (a *API) twoFactorEnable(c fiber.Ctx) error {
	var in struct {
		Code string `json:"code"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	p := httpx.P(c)
	var pending *string
	if err := a.DB.QueryRow(c.Context(), `SELECT two_factor_pending FROM users WHERE id=$1`, p.UserID).Scan(&pending); err != nil {
		return err
	}
	if pending == nil {
		return httpx.BadRequest("Start the setup first.")
	}
	if !auth.VerifyTOTP(*pending, in.Code) {
		return httpx.Invalid("code", "The code is incorrect.")
	}
	codes := make([]string, 8)
	hashes := make([]string, 8)
	for i := range codes {
		codes[i] = auth.RandomToken(4)
		hashes[i] = auth.SHA256(codes[i])
	}
	if _, err := a.DB.Exec(c.Context(), `UPDATE users SET two_factor_secret=two_factor_pending, two_factor_pending=NULL, two_factor_enabled=true, recovery_codes=$2 WHERE id=$1`, p.UserID, hashes); err != nil {
		return err
	}
	a.activity(c, "two_factor_enabled", "Turned on two-step verification")
	return httpx.OK(c, fiber.Map{"enabled": true, "recovery_codes": codes})
}

func (a *API) twoFactorDisable(c fiber.Ctx) error {
	var in struct {
		Password string `json:"password"`
	}
	if err := httpx.Decode(c, &in); err != nil {
		return err
	}
	p := httpx.P(c)
	var hash string
	if err := a.DB.QueryRow(c.Context(), `SELECT password_hash FROM users WHERE id=$1`, p.UserID).Scan(&hash); err != nil {
		return err
	}
	v := &httpx.Validation{}
	if httpx.Passwords(v, map[string]*string{"password": &in.Password}); v.Err() != nil {
		return v.Err()
	}
	if !auth.CheckPassword(hash, in.Password) {
		return httpx.Invalid("password", "Password is incorrect.")
	}
	if _, err := a.DB.Exec(c.Context(), `UPDATE users SET two_factor_enabled=false, two_factor_secret=NULL, recovery_codes='{}' WHERE id=$1`, p.UserID); err != nil {
		return err
	}
	a.activity(c, "two_factor_disabled", "Turned off two-step verification")
	return httpx.OK(c, fiber.Map{"enabled": false})
}

// ---------- logs & deactivation ----------

func (a *API) activityList(c fiber.Ctx, userID string) error {
	p := httpx.P(c)
	pg := httpx.Paging(c, 50)
	var total int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM activity_logs WHERE store_id=$1 AND user_id=$2`, p.StoreID, userID).Scan(&total); err != nil {
		return err
	}
	rows, err := db.JSONRows(c.Context(), a.DB, `SELECT id, created_at, action, description, params, ip_address, user_agent FROM activity_logs
		WHERE store_id=$1 AND user_id=$2 ORDER BY created_at DESC LIMIT $3 OFFSET $4`, p.StoreID, userID, pg.PerPage, pg.Offset)
	if err != nil {
		return err
	}
	for _, r := range rows {
		localize(httpx.Lang(c), r, "description")
	}
	return httpx.List(c, rows, httpx.NewMeta(pg, total))
}

func (a *API) myActivity(c fiber.Ctx) error { return a.activityList(c, httpx.P(c).UserID) }

func (a *API) requestDeactivation(c fiber.Ctx) error {
	p := httpx.P(c)
	if !p.IsOwner() {
		return httpx.Forbidden("Only the owner can deactivate the account.")
	}
	var row map[string]any
	err := a.tx(c.Context(), func(tx pgx.Tx) error {
		var err error
		row, err = db.JSONRow(c.Context(), tx, `INSERT INTO deactivation_requests (store_id, user_id) VALUES ($1,$2) RETURNING id, status, created_at`, p.StoreID, p.UserID)
		return err
	})
	if err != nil {
		return err
	}
	a.activity(c, "deactivation_requested", "Requested account deactivation")
	return httpx.OK(c, row)
}
