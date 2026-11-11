// Package helper holds the API envelope, errors, the request principal and parsing helpers.
package helper

import (
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"

	"github.com/roysitumorang/lestari-backend/i18n"
)

// Error is rendered as a Response with the message and ErrorData{code, fields} in the language of the request.
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

// In renders the error in a language: its code, message and field messages.
func (e *Error) In(l i18n.Lang) (code, message string, fields map[string][]string) {
	if len(e.Fields) > 0 {
		fields = make(map[string][]string, len(e.Fields))
		for k, msgs := range e.Fields {
			for _, m := range msgs {
				fields[k] = append(fields[k], m.In(l))
			}
		}
	}
	return e.Code, l.T(e.Message, e.Args...), fields
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
		code, message, fields := e.In(Lang(c))
		return NewResponse(e.Status).SetMessage(message).SetData(ErrorData{Code: code, Fields: fields}).WriteResponse(c)
	}
}

// ---------- responses ----------

// AppName is reported in every response ("app"); set at build time through config.
var AppName = "lestari"

// Location is the time zone business dates are read in (document numbers, "today"); set from APP_TIMEZONE.
var Location = time.Local

// Response is the envelope of every JSON answer, success or error.
//
//	{"request_id","request_url","status_code","status","message","timestamp","latency","data","app"}
//
// Lists put a Pagination in data; errors put {"code", "fields"?} there.
type Response struct {
	RequestID  string    `json:"request_id" example:"6ba3451b-ac73-483e-8481-2ac53f5e75a2"`
	RequestURL string    `json:"request_url" example:"GET http://localhost:8080/api/v1/taxes"`
	StatusCode int       `json:"status_code" example:"200"`
	Status     string    `json:"status" example:"OK"`
	Message    string    `json:"message" example:""`
	Timestamp  time.Time `json:"timestamp" example:"2026-11-11T14:22:47.608963985+07:00"`
	Latency    string    `json:"latency" example:"7.746177ms"`
	Data       any       `json:"data,omitempty"`
	App        string    `json:"app" example:"lestari"`
}

func NewResponse(statusCode int) *Response {
	return &Response{StatusCode: statusCode, Status: http.StatusText(statusCode), Timestamp: time.Now(), App: AppName}
}

func (r *Response) SetMessage(message string) *Response {
	if r.StatusCode != fiber.StatusNoContent {
		r.Message = message
	}
	return r
}

func (r *Response) SetData(data any) *Response {
	if r.StatusCode != fiber.StatusNoContent {
		r.Data = data
	}
	return r
}

func (r *Response) WriteResponse(c fiber.Ctx) error {
	if r.StatusCode == fiber.StatusNoContent {
		return c.SendStatus(r.StatusCode)
	}
	r.RequestURL = c.Method() + " " + c.BaseURL() + c.OriginalURL()
	r.RequestID = requestid.FromContext(c)
	r.Latency = time.Since(c.RequestCtx().Time()).String()
	return c.Status(r.StatusCode).JSON(r)
}

// ErrorData is the data of an error response: a stable machine code and, for 422, messages per field.
type ErrorData struct {
	Code   string              `json:"code" example:"validation_failed"`
	Fields map[string][]string `json:"fields,omitempty"`
}

// Pagination is the data of a list response.
type Pagination struct {
	Links struct {
		First    string `json:"first" example:"http://localhost:8080/api/v1/taxes?per_page=25"`
		Previous string `json:"previous" example:""`
		Current  string `json:"current" example:"http://localhost:8080/api/v1/taxes?page=1&per_page=25"`
		Next     string `json:"next" example:"http://localhost:8080/api/v1/taxes?page=2&per_page=25"`
		Last     string `json:"last" example:"http://localhost:8080/api/v1/taxes?page=5&per_page=25"`
	} `json:"links"`
	Info struct {
		Limit int `json:"limit" example:"25"`
		Page  int `json:"page" example:"1"`
		Pages int `json:"pages" example:"5"`
		Total int `json:"total" example:"120"`
	} `json:"info"`
	Rows any `json:"rows"`
	// reports only
	Summary any `json:"summary,omitempty"`
	Totals  any `json:"totals,omitempty"`
	Chart   any `json:"chart,omitempty"`
}

// Meta carries the paging numbers of a list (page is 1-based).
type Meta struct {
	Page       int
	PerPage    int
	Total      int
	TotalPages int
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

// Unpaged is the Meta of a list that returns every row at once.
func Unpaged(total int) Meta { return Meta{Page: 1, PerPage: total, Total: total, TotalPages: 1} }

// NewPagination builds the list data with links to the neighbouring pages of the current request.
func NewPagination(c fiber.Ctx, rows any, m Meta) *Pagination {
	p := &Pagination{Rows: rows}
	p.Info.Limit, p.Info.Page, p.Info.Pages, p.Info.Total = m.PerPage, m.Page, m.TotalPages, m.Total
	base := c.BaseURL() + c.Path()
	q, _ := url.ParseQuery(string(c.RequestCtx().QueryArgs().QueryString()))
	link := func(page int) string {
		u := maps.Clone(q)
		if page > 1 {
			u.Set("page", strconv.Itoa(page))
		} else {
			u.Del("page")
		}
		if len(u) == 0 {
			return base
		}
		return base + "?" + u.Encode()
	}
	p.Links.First, p.Links.Current, p.Links.Last = link(1), link(m.Page), link(m.TotalPages)
	if m.Page > 1 {
		p.Links.Previous = link(m.Page - 1)
	}
	if m.Page < m.TotalPages {
		p.Links.Next = link(m.Page + 1)
	}
	return p
}

func OK(c fiber.Ctx, data any) error {
	return NewResponse(fiber.StatusOK).SetData(data).WriteResponse(c)
}
func Created(c fiber.Ctx, data any) error {
	return NewResponse(fiber.StatusCreated).SetData(data).WriteResponse(c)
}
func NoContent(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) }

// List answers with a Pagination of rows.
func List(c fiber.Ctx, rows any, meta Meta) error {
	return OK(c, NewPagination(c, rows, meta))
}

// Extras are the report additions to a list.
type Extras struct{ Summary, Totals, Chart any }

// ListWith is List plus a summary, totals row and chart.
func ListWith(c fiber.Ctx, rows any, meta Meta, x Extras) error {
	p := NewPagination(c, rows, meta)
	p.Summary, p.Totals, p.Chart = x.Summary, x.Totals, x.Chart
	return OK(c, p)
}

// ---------- request parsing ----------

type Page struct {
	Page    int
	PerPage int
	Offset  int
}

func Paging(c fiber.Ctx, def int) Page {
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
