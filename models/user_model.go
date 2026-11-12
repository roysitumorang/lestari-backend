package models

import (
	"errors"
	"time"
	"uuid"

	"github.com/goccy/go-json"
)

const (
	RoleOwner         = "owner"
	RoleAdministrator = "administrator"
	RoleManager       = "manager"
	RoleCashier       = "cashier"

	UserStatusActive   = "active"
	UserStatusInvited  = "invited"
	UserStatusDisabled = "disabled"

	OTPPurposePhone     = "phone"
	OTPPurposeResetData = "reset_data"

	// OTPLifetime is how long a one-time code can be used, in seconds.
	OTPLifetime = 300
)

var (
	ErrUserNotFound = errors.New("user not found")
	// ErrEmailTaken is returned when another account already uses the email.
	ErrEmailTaken = errors.New("email is already registered")
	// ErrOTPTooSoon is returned when a code was requested less than a minute ago.
	ErrOTPTooSoon = errors.New("a code was requested less than a minute ago")
	// ErrOTPExpired and ErrOTPIncorrect are the two ways a one-time code is refused.
	ErrOTPExpired   = errors.New("the code has expired")
	ErrOTPIncorrect = errors.New("the code is incorrect")
)

// UserStore is the store of the signed-in user.
type UserStore struct {
	ID            uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Name          string     `json:"name" example:"Apotik Lestari"`
	LogoURL       *string    `json:"logo_url" example:"/uploads/logo.png"`
	Plan          string     `json:"plan" example:"pro"`
	PlanExpiresAt *time.Time `json:"plan_expires_at" example:"2027-11-08T10:32:00+07:00"`
}

// UserBranch is the branch the signed-in user works in.
type UserBranch struct {
	ID           uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
	Name         string    `json:"name" example:"Apotik Lestari"`
	IsHeadOffice bool      `json:"is_head_office" example:"true"`
}

// User is the signed-in user as GET /auth/me and the sign-in answer describe them.
type User struct {
	ID        uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6c"`
	Name      string    `json:"name" example:"Abigael Lestari"`
	Email     string    `json:"email" example:"demo@lestari.id"`
	Role      string    `json:"role" example:"owner" enums:"owner,administrator,manager,cashier"`
	AvatarURL *string   `json:"avatar_url" example:"/uploads/avatar.jpg"`
	// owners and administrators have ["*"]
	Permissions      []string    `json:"permissions" example:"products,sales"`
	TwoFactorEnabled bool        `json:"two_factor_enabled" example:"false"`
	Phone            *string     `json:"phone" example:"+6281234567890"`
	PhoneVerified    bool        `json:"phone_verified" example:"true"`
	Store            *UserStore  `json:"store"`
	Branch           *UserBranch `json:"branch"`
	ActivePlugins    []string    `json:"active_plugins" example:"attendance,business"`
}

// Credentials is what signing in checks.
type Credentials struct {
	ID               uuid.UUID
	StoreID          uuid.UUID
	PasswordHash     string
	Status           string
	TwoFactorEnabled bool
	TwoFactorSecret  *string
	RecoveryCodes    []string
}

// LoginRequest is the body of POST /auth/login.
type LoginRequest struct {
	Email string `json:"email" example:"demo@lestari.id"`
	// base64 of the password
	Password   string `json:"password" example:"ZGVtbzEyMzQ1"`
	RememberMe bool   `json:"remember_me" example:"true"`
	// authenticator or recovery code, when two-step verification is on
	OTP string `json:"otp" example:"123456"`
}

// RefreshRequest is the body of POST /auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" example:"3f9c1d…"`
}

// RegisterRequest is the body of POST /auth/register.
type RegisterRequest struct {
	StoreName string `json:"store_name" example:"Apotik Lestari"`
	Name      string `json:"name" example:"Abigael Lestari"`
	Email     string `json:"email" example:"owner@example.com"`
	// base64 of the password, at least 8 characters
	Password     string `json:"password" example:"cmFoYXNpYTEyMw=="`
	Phone        string `json:"phone" example:"+6281234567890"`
	ReferralCode string `json:"referral_code" example:"LESTARI"`
}

// PasswordRequest is the body of PUT /auth/password; all three are base64.
type PasswordRequest struct {
	CurrentPassword         string `json:"current_password" example:"ZGVtbzEyMzQ1"`
	NewPassword             string `json:"new_password" example:"cmFoYXNpYTEyMw=="`
	NewPasswordConfirmation string `json:"new_password_confirmation" example:"cmFoYXNpYTEyMw=="`
}

// Session is the data of a successful sign-in or registration.
type Session struct {
	AccessToken  string `json:"access_token" example:"eyJhbGciOiJIUzI1NiIs…"`
	RefreshToken string `json:"refresh_token" example:"3f9c1d…"`
	TokenType    string `json:"token_type" example:"Bearer"`
	ExpiresIn    int    `json:"expires_in" example:"900"`
	User         *User  `json:"user"`
}

// RefreshedSession is the data of POST /auth/refresh.
type RefreshedSession struct {
	AccessToken  string `json:"access_token" example:"eyJhbGciOiJIUzI1NiIs…"`
	RefreshToken string `json:"refresh_token" example:"8a2b7e…"`
	ExpiresIn    int    `json:"expires_in" example:"900"`
}

// ChangedResponse is the data of the endpoints that change a secret.
type ChangedResponse struct {
	Changed bool `json:"changed" example:"true"`
}

// Profile is the signed-in user's own account.
type Profile struct {
	ID            uuid.UUID `json:"-"`
	AvatarURL     *string   `json:"avatar_url" example:"/uploads/avatar.jpg"`
	Name          string    `json:"name" example:"Abigael Lestari"`
	Username      *string   `json:"username" example:"abigael"`
	Email         string    `json:"email" example:"demo@lestari.id"`
	Phone         *string   `json:"phone" example:"+6281234567890"`
	PhoneVerified bool      `json:"phone_verified" example:"true"`
	BirthYear     *int64    `json:"birth_year" example:"1990"`
	Gender        *string   `json:"gender" example:"female" enums:"male,female"`
	Address       *string   `json:"address" example:"Jl. Sisingamangaraja No. 12, Balige"`
	// which messages arrive on which channel: {"app": {"billing": true, …}, "email": {…}, "whatsapp": {…}}
	Notifications    json.RawMessage `json:"notifications" swaggertype:"object"`
	TwoFactorEnabled bool            `json:"two_factor_enabled" example:"false"`
	UpdatedAt        time.Time       `json:"-"`
}

// ProfileRequest is the body of PUT /account/profile. It replaces the editable attributes: one left out is cleared.
type ProfileRequest struct {
	Name          string          `json:"name" example:"Abigael Lestari"`
	Username      string          `json:"username" example:"abigael"`
	AvatarURL     string          `json:"avatar_url" example:"/uploads/avatar.jpg"`
	Phone         string          `json:"phone" example:"+6281234567890"`
	BirthYear     *int64          `json:"birth_year" example:"1990"`
	Gender        string          `json:"gender" example:"female" enums:"male,female"`
	Address       string          `json:"address" example:"Jl. Sisingamangaraja No. 12, Balige"`
	Notifications json.RawMessage `json:"notifications" swaggertype:"object"`
}

// DefaultNotifications is what a new account receives.
const DefaultNotifications = `{"app":{"billing":true,"ppob":true,"promotions":true,"newsletter":false},"email":{"billing":true,"promotions":false,"newsletter":false},"whatsapp":{"billing":true,"promotions":false,"newsletter":false}}`

// PPOBPinRequest is the body of PUT /account/ppob-pin.
type PPOBPinRequest struct {
	// base64 of the account password
	Password string `json:"password" example:"ZGVtbzEyMzQ1"`
	NewPin   string `json:"new_pin" example:"123456"`
}

// PhoneVerificationRequest is the body of POST /account/phone-verification.
type PhoneVerificationRequest struct {
	Phone   string `json:"phone" example:"+6281234567890"`
	Channel string `json:"channel" example:"whatsapp" enums:"whatsapp,sms"`
}

// PhoneConfirmationRequest is the body of POST /account/phone-verification/confirm.
type PhoneConfirmationRequest struct {
	Phone string `json:"phone" example:"+6281234567890"`
	Code  string `json:"code" example:"123456"`
}

// OTPSentResponse tells how long a code that was just sent stays valid, in seconds.
type OTPSentResponse struct {
	ExpiresIn int `json:"expires_in" example:"300"`
	// where the code went, masked; only for the data-reset code
	SentTo string `json:"sent_to,omitempty" example:"d***@lestari.id"`
}

// PhoneVerifiedResponse is the data of a confirmed phone number.
type PhoneVerifiedResponse struct {
	PhoneVerified bool `json:"phone_verified" example:"true"`
}

// TwoFactorSetup is the data of POST /account/two-factor/setup: add the secret to an authenticator app.
type TwoFactorSetup struct {
	Secret     string `json:"secret" example:"JBSWY3DPEHPK3PXP"`
	OTPAuthURL string `json:"otpauth_url" example:"otpauth://totp/Lestari:demo@lestari.id?secret=JBSWY3DPEHPK3PXP&issuer=Lestari"`
	QRCodeSVG  string `json:"qr_code_svg" example:"<svg …></svg>"`
}

// CodeRequest carries a one-time code.
type CodeRequest struct {
	Code string `json:"code" example:"123456"`
}

// PasswordConfirmRequest confirms an action with the account password (base64).
type PasswordConfirmRequest struct {
	Password string `json:"password" example:"ZGVtbzEyMzQ1"`
}

// TwoFactorResponse is the data of turning two-step verification on or off.
type TwoFactorResponse struct {
	Enabled bool `json:"enabled" example:"true"`
	// shown once, when it is turned on
	RecoveryCodes []string `json:"recovery_codes,omitempty" example:"9f2c41aa,0b7d93ce"`
}

// DeactivationRequest is a request of the owner to close the account.
type DeactivationRequest struct {
	ID        uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Status    string    `json:"status" example:"pending"`
	CreatedAt time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
}

// Provision describes a new store with its owner.
type Provision struct {
	StoreName,
	OwnerName,
	Email,
	Password,
	Phone,
	ReferralCode string
	At time.Time
}
