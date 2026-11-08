// Package auth issues and verifies tokens, hashes secrets and authenticates requests.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

type Service struct {
	DB         *pgxpool.Pool
	Secret     []byte
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

type Claims struct {
	StoreID int64 `json:"sid"`
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

func (s *Service) Issue(ctx context.Context, userID, storeID int64, remember bool) (Tokens, error) {
	now := time.Now()
	claims := Claims{StoreID: storeID, RegisteredClaims: jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.AccessTTL)),
		Issuer:    "lestari",
	}}
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
	var id, userID, storeID int64
	var expires time.Time
	err := s.DB.QueryRow(ctx, `
		UPDATE refresh_tokens r SET revoked_at = now()
		FROM users u
		WHERE r.token_hash = $1 AND r.revoked_at IS NULL AND r.expires_at > now()
		  AND u.id = r.user_id AND u.deleted_at IS NULL AND u.status <> 'disabled'
		RETURNING r.id, r.user_id, u.store_id, r.expires_at`, SHA256(token)).Scan(&id, &userID, &storeID, &expires)
	if err != nil {
		return Tokens{}, httpx.Unauthorized("invalid_refresh_token", "Session expired, please sign in again.")
	}
	return s.Issue(ctx, userID, storeID, time.Until(expires) > 25*time.Hour)
}

func (s *Service) RevokeAll(ctx context.Context, userID int64) error {
	_, err := s.DB.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

func (s *Service) parse(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
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
			return httpx.Unauthorized("", "Sign in to continue.")
		}
		claims, err := s.parse(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			return httpx.Unauthorized("", "Session expired, please sign in again.")
		}
		uid, _ := strconv.ParseInt(claims.Subject, 10, 64)
		p := &httpx.Principal{UserID: uid, StoreID: claims.StoreID}
		var userBranch *int64
		err = s.DB.QueryRow(c.Context(), `
			SELECT u.role, u.name, u.email, u.permissions, u.branch_id,
			       (SELECT id FROM branches WHERE store_id = u.store_id AND is_head_office AND deleted_at IS NULL ORDER BY id LIMIT 1),
			       COALESCE((SELECT array_agg(code) FROM store_plugins sp WHERE sp.store_id = u.store_id AND sp.status IN ('active','trial') AND (sp.expires_at IS NULL OR sp.expires_at > now())), '{}')
			FROM users u WHERE u.id = $1 AND u.store_id = $2 AND u.deleted_at IS NULL AND u.status <> 'disabled'`,
			uid, claims.StoreID).Scan(&p.Role, &p.Name, &p.Email, &p.Permissions, &userBranch, &p.HeadOffice, &p.Plugins)
		if err != nil {
			return httpx.Unauthorized("", "Session expired, please sign in again.")
		}
		p.BranchID = p.HeadOffice
		if userBranch != nil {
			p.BranchID = *userBranch
		}
		if hb := c.Get("X-Branch-Id"); hb != "" {
			bid, err := strconv.ParseInt(hb, 10, 64)
			if err != nil {
				return httpx.BadRequest("X-Branch-Id must be a number.")
			}
			if bid != p.BranchID {
				if p.Role != "owner" && p.Role != "administrator" {
					return httpx.Forbidden("You can only work in your own branch.")
				}
				var ok bool
				if err := s.DB.QueryRow(c.Context(), `SELECT EXISTS(SELECT 1 FROM branches WHERE id=$1 AND store_id=$2 AND deleted_at IS NULL)`, bid, p.StoreID).Scan(&ok); err != nil || !ok {
					return httpx.NotFound("Branch")
				}
				p.BranchID = bid
			}
		}
		httpx.SetPrincipal(c, p)
		return c.Next()
	}
}

// RequirePlugin rejects requests when the store has no active subscription for a plugin.
func RequirePlugin(code string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !httpx.P(c).HasPlugin(code) {
			return httpx.PaymentRequired("plugin_required", "This feature needs the "+code+" plugin.")
		}
		return c.Next()
	}
}

// RequireRole allows only the listed roles.
func RequireRole(roles ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
		r := httpx.P(c).Role
		for _, x := range roles {
			if x == r {
				return c.Next()
			}
		}
		return httpx.Forbidden("Your role cannot do this.")
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
	for i := 0; i < n; i++ {
		d, _ := rand.Int(rand.Reader, big.NewInt(10))
		sb.WriteByte(byte('0' + d.Int64()))
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

func totpAt(secret string, counter uint64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
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
	now := uint64(time.Now().Unix() / 30)
	for _, d := range []int64{-1, 0, 1} {
		want, err := totpAt(secret, uint64(int64(now)+d))
		if err == nil && hmac.Equal([]byte(want), []byte(code)) {
			return true
		}
	}
	return false
}

// TOTPCode returns the current code (used by tests).
func TOTPCode(secret string) string {
	c, _ := totpAt(secret, uint64(time.Now().Unix()/30))
	return c
}

func OTPAuthURL(issuer, account, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30", issuer, account, secret, issuer)
}
