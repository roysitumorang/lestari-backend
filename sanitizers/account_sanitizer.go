package sanitizers

import (
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/utils/v2"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// Decode reads a JSON body into a new T; the checks of its values are up to the caller.
func Decode[T any](c fiber.Ctx) (*T, error) {
	ctxt := "Sanitizer-Decode"
	response := new(T)
	if err := helper.Decode(c, response); err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return nil, err
	}
	return response, nil
}

// pageOf reads ?page and ?per_page with a page size of its own for lists that default to more than 25 rows.
func pageOf(c fiber.Ctx, defaultLimit int64) (limit, page int64) {
	if limit, _ = utils.ParseInt(c.Query("per_page")); limit < 1 {
		limit = defaultLimit
	}
	page, _ = utils.ParseInt(c.Query("page"))
	return min(limit, models.MaxLimit), max(page, 1)
}

// PageOf is pageOf for controllers whose list has no filter of its own.
func PageOf(c fiber.Ctx, defaultLimit int64) (limit, page int64) {
	return pageOf(c, defaultLimit)
}

// DateRange reads ?start_date and ?end_date (YYYY-MM-DD, today by default, swapped when reversed).
func DateRange(c fiber.Ctx) (startDate, endDate string, err error) {
	return helper.DateRange(c, helper.Location)
}

// Login reads the body of a sign-in: email and password (base64) are required.
func Login(c fiber.Ctx) (*models.LoginRequest, error) {
	response, err := Decode[models.LoginRequest](c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	if strings.TrimSpace(response.Email) == "" {
		v.Add("email", "Required")
	}
	sent := response.Password != ""
	helper.Passwords(v, map[string]*string{"password": &response.Password})
	if !sent {
		v.Add("password", "Required")
	}
	return response, v.Err()
}

// Register reads the body of a registration.
func Register(c fiber.Ctx) (*models.RegisterRequest, error) {
	response, err := Decode[models.RegisterRequest](c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	helper.Passwords(v, map[string]*string{"password": &response.Password})
	if strings.TrimSpace(response.StoreName) == "" {
		v.Add("store_name", "Required")
	}
	if strings.TrimSpace(response.Name) == "" {
		v.Add("name", "Required")
	}
	if !strings.Contains(response.Email, "@") {
		v.Add("email", "Enter a valid email.")
	}
	if len(response.Password) < 8 {
		v.Add("password", "Use at least 8 characters.")
	}
	return response, v.Err()
}

// Password reads the body of a password change and decodes the three base64 passwords.
func Password(c fiber.Ctx) (*models.PasswordRequest, error) {
	response, err := Decode[models.PasswordRequest](c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	helper.Passwords(v, map[string]*string{
		"current_password":          &response.CurrentPassword,
		"new_password":              &response.NewPassword,
		"new_password_confirmation": &response.NewPasswordConfirmation,
	})
	return response, v.Err()
}

// PasswordConfirm reads a body that confirms an action with the account password (base64).
func PasswordConfirm(c fiber.Ctx) (string, error) {
	response, err := Decode[models.PasswordConfirmRequest](c)
	if err != nil {
		return "", err
	}
	v := &helper.Validation{}
	helper.Passwords(v, map[string]*string{"password": &response.Password})
	return response.Password, v.Err()
}

// PPOBPin reads the body of a PPOB PIN change and decodes the password.
func PPOBPin(c fiber.Ctx) (*models.PPOBPinRequest, error) {
	response, err := Decode[models.PPOBPinRequest](c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	helper.Passwords(v, map[string]*string{"password": &response.Password})
	return response, v.Err()
}

// Profile reads the body of a profile update: name is required.
func Profile(c fiber.Ctx) (*models.ProfileRequest, error) {
	ctxt := "AccountSanitizer-Profile"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	v := &helper.Validation{}
	nulls := &models.Nulls{}
	response := &models.ProfileRequest{
		Name:      helper.Str(b, "name"),
		Username:  helper.Str(b, "username"),
		AvatarURL: helper.Str(b, "avatar_url"),
		Phone:     helper.Str(b, "phone"),
		Gender:    helper.Str(b, "gender"),
		Address:   helper.Str(b, "address"),
	}
	if response.Name == "" {
		v.Add("name", "Required")
	}
	if birthYear, ok := helper.Int(b, "birth_year"); ok {
		if birthYear < 1900 || birthYear > int64(time.Now().Year()) {
			v.Add("birth_year", "Enter a valid year.")
		}
		response.BirthYear = &birthYear
	}
	if response.Gender != "" && response.Gender != "male" && response.Gender != "female" {
		v.Add("gender", "Must be one of: male, female.")
	}
	if _, isObject := b["notifications"].(map[string]any); isObject {
		response.Notifications = b.document(v, nulls, "notifications")
	}
	if response.Notifications == nil {
		response.Notifications = []byte(models.DefaultNotifications)
	}
	return response, v.Err()
}

// PhoneVerification reads the body of a phone verification: a plausible number and a channel.
func PhoneVerification(c fiber.Ctx) (*models.PhoneVerificationRequest, error) {
	response, err := Decode[models.PhoneVerificationRequest](c)
	if err != nil {
		return nil, err
	}
	response.Phone = strings.TrimSpace(response.Phone)
	if strings.Trim(response.Phone, "+0123456789 -") != "" || len(response.Phone) < 8 {
		return nil, helper.Invalid("phone", "Enter a valid phone number, e.g. +6281234567890.")
	}
	if response.Channel != "sms" {
		response.Channel = "whatsapp"
	}
	return response, nil
}

// FindNotifications reads the list query: source, start_date, end_date, page, per_page.
func FindNotifications(c fiber.Ctx) *models.NotificationFilter {
	response := &models.NotificationFilter{Source: c.Query("source")}
	response.StartDate, _ = queryDate(c, "start_date")
	response.EndDate, _ = queryDate(c, "end_date")
	response.Limit, response.Page = pageOf(c, models.DefaultLimit)
	return response
}

// QueryID reads an optional UUID of the query string.
func QueryID(c fiber.Ctx, key string) (*uuid.UUID, bool) {
	id, ok := queryID(c, key)
	if !ok {
		return nil, false
	}
	return &id, true
}

// lastDayOfMonth is the last date of a month as YYYY-MM-DD.
func lastDayOfMonth(year, month int) string {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
}

// PointAdjustment reads the body of a point adjustment and returns the signed number of points.
func PointAdjustment(c fiber.Ctx) (points int64, note string, err error) {
	request, err := Decode[models.PointAdjustmentRequest](c)
	if err != nil {
		return 0, "", err
	}
	if request.Points < 1 {
		return 0, "", helper.Invalid("points", "Must be at least 1.")
	}
	switch request.Mode {
	case "add":
		return int64(request.Points), request.Note, nil
	case "subtract":
		return -int64(request.Points), request.Note, nil
	}
	return 0, "", helper.Invalid("mode", "Must be one of: add, subtract.")
}
