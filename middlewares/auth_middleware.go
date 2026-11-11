// Package middlewares issues and verifies tokens, hashes secrets and authenticates requests.
package middlewares

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP is defined over HMAC-SHA1; authenticator apps expect it
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
)

type Service struct {
	DB         *pgxpool.Pool
	Secret     []byte
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

type Claims struct {
	StoreID string `json:"sid"`
	jwt.RegisteredClaims
}

// ---------- passwords ----------

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// ---------- tokens ----------

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func (s *Service) Issue(ctx context.Context, userID, storeID string, remember bool) (Tokens, error) {
	now := time.Now()
	claims := Claims{StoreID: storeID,
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.AccessTTL)),
		Issuer:    "lestari"}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.Secret)
	if err != nil {
		return Tokens{}, err
	}
	refresh := RandomToken(32)
	ttl := s.RefreshTTL
	if !remember {
		ttl = 24 * time.Hour
	}
	if _, err := s.DB.Exec(ctx, `INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1,$2,$3)`,
		userID, SHA256(refresh), now.Add(ttl)); err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, RefreshToken: refresh, TokenType: "Bearer", ExpiresIn: int(s.AccessTTL.Seconds())}, nil
}

// Refresh rotates a refresh token.
func (s *Service) Refresh(ctx context.Context, token string) (Tokens, error) {
	var id, userID, storeID string
	var expires time.Time
	err := s.DB.QueryRow(ctx, `
		UPDATE refresh_tokens r SET revoked_at = now()
		FROM users u
		WHERE r.token_hash = $1 AND r.revoked_at IS NULL AND r.expires_at > now()
		  AND u.id = r.user_id AND u.deleted_at IS NULL AND u.status <> 'disabled'
		RETURNING r.id, r.user_id, u.store_id, r.expires_at`, SHA256(token)).Scan(&id, &userID, &storeID, &expires)
	if err != nil {
		return Tokens{}, helper.Unauthorized("invalid_refresh_token", "Session expired, please sign in again.")
	}
	return s.Issue(ctx, userID, storeID, time.Until(expires) > 25*time.Hour)
}

func (s *Service) RevokeAll(ctx context.Context, userID string) error {
	_, err := s.DB.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

func (s *Service) parse(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.Secret, nil
	}, jwt.WithIssuer("lestari"), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// Middleware authenticates the bearer token and resolves the active branch.
func (s *Service) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		h := c.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			return helper.Unauthorized("", "Sign in to continue.")
		}
		claims, err := s.parse(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			return helper.Unauthorized("", "Session expired, please sign in again.")
		}
		p := &helper.Principal{UserID: claims.Subject, StoreID: claims.StoreID}
		if !helper.IsUUID(p.UserID) || !helper.IsUUID(p.StoreID) {
			return helper.Unauthorized("", "Session expired, please sign in again.")
		}
		var userBranch *string
		err = s.DB.QueryRow(c.Context(), `
			SELECT u.role, u.name, u.email, u.permissions, u.branch_id,
			       (SELECT id FROM branches WHERE store_id = u.store_id AND is_head_office AND deleted_at IS NULL ORDER BY id LIMIT 1),
			       COALESCE((SELECT array_agg(code) FROM store_plugins sp WHERE sp.store_id = u.store_id AND sp.status IN ('active','trial') AND (sp.expires_at IS NULL OR sp.expires_at > now())), '{}')
			FROM users u WHERE u.id = $1 AND u.store_id = $2 AND u.deleted_at IS NULL AND u.status <> 'disabled'`,
			p.UserID, p.StoreID).Scan(&p.Role, &p.Name, &p.Email, &p.Permissions, &userBranch, &p.HeadOffice, &p.Plugins)
		if err != nil {
			return helper.Unauthorized("", "Session expired, please sign in again.")
		}
		p.BranchID = p.HeadOffice
		if userBranch != nil {
			p.BranchID = *userBranch
		}
		if hb := strings.ToLower(strings.TrimSpace(c.Get("X-Branch-Id"))); hb != "" {
			bid := hb
			if !helper.IsUUID(bid) {
				return helper.BadRequest("X-Branch-Id must be a UUID.")
			}
			if bid != p.BranchID {
				if p.Role != "owner" && p.Role != "administrator" {
					return helper.Forbidden("You can only work in your own branch.")
				}
				var ok bool
				if err := s.DB.QueryRow(c.Context(), `SELECT EXISTS(SELECT 1 FROM branches WHERE id=$1 AND store_id=$2 AND deleted_at IS NULL)`, bid, p.StoreID).Scan(&ok); err != nil || !ok {
					return helper.NotFound("Branch")
				}
				p.BranchID = bid
			}
		}
		helper.SetPrincipal(c, p)
		if !helper.LangExplicit(c) {
			// no Accept-Language: answer in the language chosen in the branch's display settings
			var lang *string
			if err := s.DB.QueryRow(c.Context(), `SELECT value->>'language' FROM settings WHERE branch_id=$1 AND key='display'`, p.BranchID).Scan(&lang); err == nil && lang != nil && i18n.Supported(*lang) {
				helper.SetLang(c, i18n.Lang(*lang), false)
			}
		}
		return c.Next()
	}
}

// RequirePlugin rejects requests when the store has no active subscription for a plugin.
func RequirePlugin(code string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !helper.P(c).HasPlugin(code) {
			return helper.PaymentRequired("plugin_required", "This feature needs the {plugin} plugin.", "plugin", code)
		}
		return c.Next()
	}
}

// RequireRole allows only the listed roles.
func RequireRole(roles ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
		r := helper.P(c).Role
		if slices.Contains(roles, r) {
			return c.Next()
		}
		return helper.Forbidden("Your role cannot do this.")
	}
}

// ---------- random values & hashing ----------

func RandomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RandomDigits returns a numeric code such as an OTP.
func RandomDigits(n int) string {
	var sb strings.Builder
	for range n {
		d, _ := rand.Int(rand.Reader, big.NewInt(10))
		sb.WriteByte("0123456789"[d.Int64()])
	}
	return sb.String()
}

func SHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---------- TOTP (RFC 6238, 30s, 6 digits, SHA1) ----------

func NewTOTPSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

func totpAt(secret string, step int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte // big-endian time step
	for i := 7; i >= 0; i-- {
		msg[i] = byte(step & 0xff)
		step >>= 8
	}
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", code), nil
}

// VerifyTOTP accepts the current code and one step either side for clock drift.
func VerifyTOTP(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	now := time.Now().Unix() / 30
	for _, d := range []int64{-1, 0, 1} {
		want, err := totpAt(secret, now+d)
		if err == nil && hmac.Equal([]byte(want), []byte(code)) {
			return true
		}
	}
	return false
}

// TOTPCode returns the current code (used by tests).
func TOTPCode(secret string) string {
	c, _ := totpAt(secret, time.Now().Unix()/30)
	return c
}

func OTPAuthURL(issuer, account, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30", issuer, account, secret, issuer)
}
