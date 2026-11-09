// Package httpx holds the API envelope, errors, the request principal and parsing helpers.
package httpx

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/internal/i18n"
)

// Error is rendered as {"error":{"code","message","fields"}} in the language of the request.
// Message and field messages are English text (dictionary keys) with optional {placeholders}.
type Error struct {
	Status  int
	Code    string
	Message string
	Args    []any
	Fields  map[string][]i18n.Text
}

// Error returns the English message.
func (e *Error) Error() string { return i18n.EN.T(e.Message, e.Args...) }

// In renders the error body in a language.
func (e *Error) In(l i18n.Lang) fiber.Map {
	out := fiber.Map{"code": e.Code, "message": l.T(e.Message, e.Args...)}
	if len(e.Fields) > 0 {
		f := make(map[string][]string, len(e.Fields))
		for k, msgs := range e.Fields {
			for _, m := range msgs {
				f[k] = append(f[k], m.In(l))
			}
		}
		out["fields"] = f
	}
	return out
}

// NewError builds an error; args are {placeholder} name/value pairs for msg.
func NewError(status int, code, msg string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: msg, Args: args}
}

func BadRequest(msg string, args ...any) *Error { return NewError(400, "bad_request", msg, args...) }
func Unauthorized(code, msg string, args ...any) *Error {
	if code == "" {
		code = "unauthenticated"
	}
	return NewError(401, code, msg, args...)
}
func PaymentRequired(code, msg string, args ...any) *Error {
	return NewError(402, code, msg, args...)
}
func Forbidden(msg string, args ...any) *Error { return NewError(403, "forbidden", msg, args...) }

// NotFound says "<what> not found"; what is an English noun ("Product") and is translated too.
func NotFound(what string) *Error {
	return NewError(404, "not_found", "{what} not found", "what", i18n.Tr(what))
}
func Conflict(msg string, args ...any) *Error { return NewError(409, "conflict", msg, args...) }
func NotImplemented(msg string, args ...any) *Error {
	return NewError(501, "not_implemented", msg, args...)
}

// Validation collects field errors; call Err() to get nil when there are none.
type Validation struct{ Fields map[string][]i18n.Text }

// Add records a message (English, with optional {placeholder} name/value pairs) for a field.
func (v *Validation) Add(field, msg string, args ...any) {
	v.AddText(field, i18n.Tr(msg, args...))
}

// AddText records an already built message.
func (v *Validation) AddText(field string, t i18n.Text) {
	if v.Fields == nil {
		v.Fields = map[string][]i18n.Text{}
	}
	v.Fields[field] = append(v.Fields[field], t)
}

func (v *Validation) Err() error {
	if len(v.Fields) == 0 {
		return nil
	}
	return &Error{Status: 422, Code: "validation_failed", Message: "Some fields are invalid.", Fields: v.Fields}
}

// Invalid is a validation error for one field.
func Invalid(field, msg string, args ...any) error {
	v := &Validation{}
	v.Add(field, msg, args...)
	return v.Err()
}

// ---------- language ----------

const langKey = "lestari.lang"

type langInfo struct {
	lang     i18n.Lang
	explicit bool
}

// LangMiddleware reads Accept-Language (en / id) for the rest of the request.
func LangMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if l, ok := i18n.Parse(c.Get("Accept-Language")); ok {
			SetLang(c, l, true)
		}
		c.Append("Vary", "Accept-Language")
		return c.Next()
	}
}

// SetLang sets the response language; explicit means the client asked for it (Accept-Language).
func SetLang(c fiber.Ctx, l i18n.Lang, explicit bool) {
	c.Locals(langKey, langInfo{l, explicit})
}

// Lang is the language of the response (English unless the client or the branch settings chose another).
func Lang(c fiber.Ctx) i18n.Lang {
	if li, ok := c.Locals(langKey).(langInfo); ok {
		return li.lang
	}
	return i18n.EN
}

// LangExplicit reports whether the client sent a supported Accept-Language.
func LangExplicit(c fiber.Ctx) bool {
	li, ok := c.Locals(langKey).(langInfo)
	return ok && li.explicit
}

// T translates text into the language of the request.
func T(c fiber.Ctx, s string, args ...any) string { return Lang(c).T(s, args...) }

// ErrorHandler is installed as Fiber's ErrorHandler.
func ErrorHandler(log func(err error)) fiber.ErrorHandler {
	return func(c fiber.Ctx, err error) error {
		var e *Error
		var fe *fiber.Error
		switch {
		case errors.As(err, &e):
		case errors.As(err, &fe):
			code := map[int]string{404: "not_found", 405: "method_not_allowed", 413: "payload_too_large", 429: "too_many_requests"}[fe.Code]
			if code == "" {
				code = "http_" + strconv.Itoa(fe.Code)
			}
			e = NewError(fe.Code, code, fe.Message)
		default:
			if log != nil {
				log(err)
			}
			e = NewError(500, "internal_error", "Something went wrong. Please try again.")
		}
		c.Set("Content-Language", string(Lang(c)))
		return c.Status(e.Status).JSON(fiber.Map{"error": e.In(Lang(c))})
	}
}

// ---------- responses ----------

type Meta struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

func NewMeta(p Page, total int) Meta {
	tp := 1
	if p.PerPage > 0 {
		tp = int(math.Ceil(float64(total) / float64(p.PerPage)))
	}
	if tp < 1 {
		tp = 1
	}
	return Meta{Page: p.Page, PerPage: p.PerPage, Total: total, TotalPages: tp}
}

func OK(c fiber.Ctx, data any) error { return c.JSON(fiber.Map{"data": data}) }
func Created(c fiber.Ctx, data any) error {
	return c.Status(201).JSON(fiber.Map{"data": data})
}
func NoContent(c fiber.Ctx) error { return c.SendStatus(204) }
func List(c fiber.Ctx, data any, meta Meta) error {
	return c.JSON(fiber.Map{"data": data, "meta": meta})
}

// ---------- request parsing ----------

type Page struct {
	Page    int
	PerPage int
	Offset  int
}

func Pagination(c fiber.Ctx, def int) Page {
	p := Page{Page: atoi(c.Query("page"), 1), PerPage: atoi(c.Query("per_page"), def)}
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage < 1 {
		p.PerPage = def
	}
	if p.PerPage > 1000 {
		p.PerPage = 1000
	}
	p.Offset = (p.Page - 1) * p.PerPage
	return p
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// Body decodes the JSON body into a map (empty map for an empty body).
func Body(c fiber.Ctx) (map[string]any, error) {
	b := c.Body()
	m := map[string]any{}
	if len(strings.TrimSpace(string(b))) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, BadRequest("Request body must be a JSON object.")
	}
	return m, nil
}

// Decode decodes the JSON body into a struct.
func Decode(c fiber.Ctx, v any) error {
	b := c.Body()
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return BadRequest("Request body is not valid JSON: {error}", "error", err.Error())
	}
	return nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsUUID reports whether s is a canonical UUID string.
func IsUUID(s string) bool { return uuidRe.MatchString(s) }

// ParamID reads a UUID path parameter (lower-cased); anything else is a 404.
func ParamID(c fiber.Ctx, name string) (string, error) {
	v := strings.ToLower(c.Params(name))
	if !IsUUID(v) {
		return "", NotFound("Resource")
	}
	return v, nil
}

// UUID reads a UUID attribute from a decoded body.
func UUID(m map[string]any, k string) (string, bool) {
	s, ok := m[k].(string)
	s = strings.ToLower(strings.TrimSpace(s))
	return s, ok && IsUUID(s)
}

// QueryUUID returns a UUID query parameter, or nil when it is missing or malformed
// (so SQL can use "$n::uuid IS NULL OR col = $n").
func QueryUUID(c fiber.Ctx, k string) any {
	v := strings.ToLower(strings.TrimSpace(c.Query(k)))
	if IsUUID(v) {
		return v
	}
	return nil
}

// QueryArray returns ?k[]=a&k[]=b (or ?k=a,b) values.
func QueryArray(c fiber.Ctx, key string) []string {
	var out []string
	args := c.RequestCtx().QueryArgs()
	for _, v := range args.PeekMulti(key + "[]") {
		out = append(out, string(v))
	}
	if len(out) == 0 {
		if v := c.Query(key); v != "" {
			for s := range strings.SplitSeq(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// DateRange reads start_date / end_date (YYYY-MM-DD), defaulting to today.
func DateRange(c fiber.Ctx, loc *time.Location) (string, string, error) {
	today := time.Now().In(loc).Format("2006-01-02")
	s, e := c.Query("start_date", today), c.Query("end_date", today)
	for k, v := range map[string]string{"start_date": s, "end_date": e} {
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return "", "", Invalid(k, "Use the format YYYY-MM-DD.")
		}
	}
	if e < s {
		s, e = e, s
	}
	return s, e, nil
}

// ---------- passwords ----------

// Passwords travel base64-encoded (standard alphabet, UTF-8 bytes). Base64 is an encoding,
// not encryption: it keeps any character safe in transit, while TLS protects the content.

// DecodeBase64Password decodes one base64 password; an empty string stays empty.
func DecodeBase64Password(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		if b, err = base64.RawStdEncoding.DecodeString(s); err != nil {
			return "", false
		}
	}
	return string(b), true
}

// Passwords decodes base64 password attributes in place, adding a field error for any that is not base64.
func Passwords(v *Validation, fields map[string]*string) {
	for name, ptr := range fields {
		plain, ok := DecodeBase64Password(*ptr)
		if !ok {
			v.Add(name, "Send the password base64-encoded.")
			*ptr = ""
			continue
		}
		*ptr = plain
	}
}

// ---------- value coercion for map bodies ----------

func Str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func Num(m map[string]any, k string) (float64, bool) {
	switch v := m[k].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}

func Int(m map[string]any, k string) (int64, bool) {
	f, ok := Num(m, k)
	return int64(f), ok
}

func Bool(m map[string]any, k string) bool {
	switch v := m[k].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1" || v == "on"
	case float64:
		return v != 0
	}
	return false
}

func Items(m map[string]any, k string) []map[string]any {
	arr, _ := m[k].([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, it := range arr {
		if mm, ok := it.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}
