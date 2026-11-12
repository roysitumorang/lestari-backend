package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// recoveryCodeCount is how many recovery codes turning two-step verification on hands out.
const recoveryCodeCount = 8

type AccountService interface {
	FindProfile(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*models.Profile, error)
	// UpdateProfile replaces the editable attributes; a changed phone number has to be verified again.
	UpdateProfile(ctx context.Context, tx pgx.Tx, userID uuid.UUID, request *models.ProfileRequest, at time.Time) (*models.Profile, error)
	// CheckPassword tells whether password is the user's password.
	CheckPassword(ctx context.Context, userID uuid.UUID, password string) (bool, error)
	ChangePPOBPin(ctx context.Context, tx pgx.Tx, userID uuid.UUID, request *models.PPOBPinRequest) error
	// SendOTP creates a code for a purpose and sends message(code) over a channel.
	SendOTP(ctx context.Context, userID uuid.UUID, purpose, target, channel string, at time.Time, message func(code string) string) error
	ConfirmPhone(ctx context.Context, userID uuid.UUID, request *models.PhoneConfirmationRequest, at time.Time) error
	// FindContact returns the email and phone a code can be sent to.
	FindContact(ctx context.Context, userID uuid.UUID) (email string, phone *string, err error)
	// CheckOTP consumes a code of a purpose; a wrong or expired one is a validation error of `field`.
	CheckOTP(ctx context.Context, userID uuid.UUID, purpose, target, code, field string, at time.Time) error
	// SetupTwoFactor creates the secret of an authenticator app; it takes effect once a code confirms it.
	SetupTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, email string) (*models.TwoFactorSetup, error)
	EnableTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, code string) (*models.TwoFactorResponse, error)
	DisableTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, password string) error
	RequestDeactivation(ctx context.Context, tx pgx.Tx, storeID, userID uuid.UUID, at time.Time) (*models.DeactivationRequest, error)
}

type accountService struct {
	userRepository repositories.UserRepository
	otpService     OTPService
	sender         Sender
}

func NewAccountService(
	userRepository repositories.UserRepository,
	otpService OTPService,
	sender Sender,
) AccountService {
	return &accountService{
		userRepository: userRepository,
		otpService:     otpService,
		sender:         sender,
	}
}

func (q *accountService) FindProfile(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (*models.Profile, error) {
	ctxt := "AccountService-FindProfile"
	response, err := q.userRepository.FindProfile(ctx, tx, userID)
	if err != nil && !errors.Is(err, models.ErrUserNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProfile")
	}
	return response, err
}

// optional turns an empty text into NULL.
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (q *accountService) UpdateProfile(ctx context.Context, tx pgx.Tx, userID uuid.UUID, request *models.ProfileRequest, at time.Time) (*models.Profile, error) {
	ctxt := "AccountService-UpdateProfile"
	profile, err := q.userRepository.FindProfile(ctx, tx, userID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProfile")
		return nil, err
	}
	// a new number is not verified
	if deref(profile.Phone) != request.Phone {
		profile.PhoneVerified = false
	}
	profile.Name = request.Name
	profile.Username = optional(request.Username)
	profile.AvatarURL = optional(request.AvatarURL)
	profile.BirthYear = request.BirthYear
	profile.Gender = optional(request.Gender)
	profile.Address = optional(request.Address)
	profile.Phone = optional(request.Phone)
	profile.Notifications = request.Notifications
	profile.UpdatedAt = at
	response, err := q.userRepository.UpdateProfile(ctx, tx, profile)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateProfile")
	}
	return response, err
}

func (q *accountService) CheckPassword(ctx context.Context, userID uuid.UUID, password string) (bool, error) {
	ctxt := "AccountService-CheckPassword"
	passwordHash, err := q.userRepository.FindPasswordHash(ctx, userID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPasswordHash")
		return false, err
	}
	return middlewares.CheckPassword(passwordHash, password), nil
}

func (q *accountService) ChangePPOBPin(ctx context.Context, tx pgx.Tx, userID uuid.UUID, request *models.PPOBPinRequest) error {
	ctxt := "AccountService-ChangePPOBPin"
	correct, err := q.CheckPassword(ctx, userID, request.Password)
	if err != nil {
		return err
	}
	v := &helper.Validation{}
	if !correct {
		v.Add("password", "Password is incorrect.")
	}
	if len(request.NewPin) != 6 || strings.Trim(request.NewPin, "0123456789") != "" {
		v.Add("new_pin", "The PIN must be 6 digits.")
	}
	if err = v.Err(); err != nil {
		return err
	}
	pinHash, err := middlewares.HashPassword(request.NewPin)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrHashPassword")
		return err
	}
	if err = q.userRepository.UpdatePPOBPin(ctx, tx, userID, pinHash); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePPOBPin")
	}
	return err
}

func (q *accountService) SendOTP(ctx context.Context, userID uuid.UUID, purpose, target, channel string, at time.Time, message func(code string) string) error {
	ctxt := "AccountService-SendOTP"
	// the code of a data reset is not bound to where it was sent
	bound := target
	if purpose == models.OTPPurposeResetData {
		bound = ""
	}
	code, err := q.otpService.CreateOTP(ctx, userID, purpose, bound, at)
	if errors.Is(err, models.ErrOTPTooSoon) {
		return helper.NewError(429, "too_many_requests", "Please wait a minute before requesting another code.")
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateOTP")
		return err
	}
	if err = q.sender.Send(ctx, channel, target, message(code)); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSend")
	}
	return err
}

func (q *accountService) CheckOTP(ctx context.Context, userID uuid.UUID, purpose, target, code, field string, at time.Time) error {
	ctxt := "AccountService-CheckOTP"
	err := q.otpService.CheckOTP(ctx, userID, purpose, target, code, at)
	switch {
	case errors.Is(err, models.ErrOTPExpired):
		return helper.Invalid(field, "The code has expired. Request a new one.")
	case errors.Is(err, models.ErrOTPIncorrect):
		return helper.Invalid(field, "The code is incorrect.")
	case err != nil:
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCheckOTP")
	}
	return err
}

func (q *accountService) ConfirmPhone(ctx context.Context, userID uuid.UUID, request *models.PhoneConfirmationRequest, at time.Time) error {
	ctxt := "AccountService-ConfirmPhone"
	phone := strings.TrimSpace(request.Phone)
	if err := q.CheckOTP(ctx, userID, models.OTPPurposePhone, phone, request.Code, "code", at); err != nil {
		return err
	}
	err := q.userRepository.VerifyPhone(ctx, nil, userID, phone)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrVerifyPhone")
	}
	return err
}

func (q *accountService) FindContact(ctx context.Context, userID uuid.UUID) (email string, phone *string, err error) {
	ctxt := "AccountService-FindContact"
	if email, phone, err = q.userRepository.FindContact(ctx, userID); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindContact")
	}
	return
}

func (q *accountService) SetupTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, email string) (*models.TwoFactorSetup, error) {
	ctxt := "AccountService-SetupTwoFactor"
	secret := middlewares.NewTOTPSecret()
	if err := q.userRepository.SetPendingTwoFactor(ctx, tx, userID, secret); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSetPendingTwoFactor")
		return nil, err
	}
	response := &models.TwoFactorSetup{
		Secret:     secret,
		OTPAuthURL: middlewares.OTPAuthURL("Lestari", email, secret),
	}
	if code, err := qrcode.New(response.OTPAuthURL, qrcode.Medium); err == nil {
		response.QRCodeSVG = qrSVG(code.Bitmap())
	}
	return response, nil
}

// qrSVG draws a QR code as an SVG image of one unit per module.
func qrSVG(modules [][]bool) string {
	var sb strings.Builder
	_, _ = sb.WriteString(helper.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="100%%" height="100%%" fill="#fff"/><path fill="#000" d="`, len(modules), len(modules)))
	for y, row := range modules {
		for x, dark := range row {
			if dark {
				_, _ = sb.WriteString(helper.Sprintf("M%d %dh1v1h-1z", x, y))
			}
		}
	}
	_, _ = sb.WriteString(`"/></svg>`)
	return sb.String()
}

func (q *accountService) EnableTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, code string) (*models.TwoFactorResponse, error) {
	ctxt := "AccountService-EnableTwoFactor"
	pending, err := q.userRepository.FindPendingTwoFactor(ctx, tx, userID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPendingTwoFactor")
		return nil, err
	}
	if pending == nil {
		return nil, helper.BadRequest("Start the setup first.")
	}
	if !middlewares.VerifyTOTP(*pending, code) {
		return nil, helper.Invalid("code", "The code is incorrect.")
	}
	response := &models.TwoFactorResponse{Enabled: true, RecoveryCodes: make([]string, recoveryCodeCount)}
	hashes := make([]string, recoveryCodeCount)
	for i := range response.RecoveryCodes {
		response.RecoveryCodes[i] = middlewares.RandomToken(4)
		hashes[i] = middlewares.SHA256(response.RecoveryCodes[i])
	}
	if err = q.userRepository.EnableTwoFactor(ctx, tx, userID, hashes); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrEnableTwoFactor")
		return nil, err
	}
	return response, nil
}

func (q *accountService) DisableTwoFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, password string) error {
	ctxt := "AccountService-DisableTwoFactor"
	correct, err := q.CheckPassword(ctx, userID, password)
	if err != nil {
		return err
	}
	if !correct {
		return helper.Invalid("password", "Password is incorrect.")
	}
	if err = q.userRepository.DisableTwoFactor(ctx, tx, userID); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDisableTwoFactor")
	}
	return err
}

func (q *accountService) RequestDeactivation(ctx context.Context, tx pgx.Tx, storeID, userID uuid.UUID, at time.Time) (*models.DeactivationRequest, error) {
	ctxt := "AccountService-RequestDeactivation"
	response, err := q.userRepository.CreateDeactivationRequest(ctx, tx, storeID, userID, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateDeactivationRequest")
	}
	return response, err
}
