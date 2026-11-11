// Package sanitizers turns requests into models and filters, and reports what is wrong with them.
package sanitizers

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/utils/v2"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// body is a decoded JSON object; its readers add a message to the validation when a value has the wrong type,
// so the client gets every problem at once, next to the right input.
type body map[string]any

func parseBody(c fiber.Ctx) (body, error) {
	m, err := helper.Body(c)
	return body(m), err
}

// nuller receives the attributes sent as null or empty (models.Nulls).
type nuller interface{ SetNull(attribute string) }

// value returns the raw value of key, or nil when it is absent, null or blank text. A key that was sent
// without a value is recorded as null so an update can clear it.
func (b body) value(n nuller, key string) any {
	raw, sent := b[key]
	if !sent {
		return nil
	}
	if s, isText := raw.(string); raw == nil || isText && strings.TrimSpace(s) == "" {
		n.SetNull(key)
		return nil
	}
	return raw
}

// text reads a string (numbers and booleans from spreadsheets are accepted); enum limits the allowed values.
func (b body) text(v *helper.Validation, n nuller, key string, enum ...string) *string {
	var s string
	switch x := b.value(n, key).(type) {
	case nil:
		return nil
	case string:
		s = strings.TrimSpace(x)
	case float64:
		s = strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		s = strconv.FormatBool(x)
	default:
		v.Add(key, "Must be text.")
		return nil
	}
	if len(enum) > 0 && !slices.Contains(enum, s) {
		v.Add(key, "Must be one of: {values}.", "values", strings.Join(enum, ", "))
		return nil
	}
	return &s
}

// number reads a number (or numeric text such as "1,250.5"); lo and hi are optional bounds.
func (b body) number(v *helper.Validation, n nuller, key string, lo, hi *float64) *float64 {
	var f float64
	switch x := b.value(n, key).(type) {
	case nil:
		return nil
	case float64:
		f = x
	case string:
		var err error
		if f, err = strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(x), ",", ""), 64); err != nil {
			v.Add(key, "Must be a number.")
			return nil
		}
	default:
		v.Add(key, "Must be a number.")
		return nil
	}
	if lo != nil && f < *lo {
		v.Add(key, "Must be at least {min}.", "min", strconv.FormatFloat(*lo, 'f', -1, 64))
		return nil
	}
	if hi != nil && f > *hi {
		v.Add(key, "Must be at most {max}.", "max", strconv.FormatFloat(*hi, 'f', -1, 64))
		return nil
	}
	return &f
}

// integer is number truncated to a whole number.
func (b body) integer(v *helper.Validation, n nuller, key string, lo, hi *float64) *int64 {
	f := b.number(v, n, key, lo, hi)
	if f == nil {
		return nil
	}
	return new(int64(*f))
}

// boolean reads true/false (also "yes", "1", "on" … from spreadsheets).
func (b body) boolean(v *helper.Validation, n nuller, key string) *bool {
	switch x := b.value(n, key).(type) {
	case nil:
		return nil
	case bool:
		return &x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return new(s == "true" || s == "1" || s == "yes" || s == "y" || s == "on")
	case float64:
		return new(x != 0)
	}
	v.Add(key, "Must be true or false.")
	return nil
}

// date reads YYYY-MM-DD (a longer timestamp is cut to its date).
func (b body) date(v *helper.Validation, n nuller, key string) *string {
	raw := b.value(n, key)
	if raw == nil {
		return nil
	}
	s, ok := raw.(string)
	if ok && len(s) >= 10 {
		if _, err := time.Parse(time.DateOnly, s[:10]); err == nil {
			return new(s[:10])
		}
	}
	v.Add(key, "Use the format YYYY-MM-DD.")
	return nil
}

// id reads a UUID.
func (b body) id(v *helper.Validation, n nuller, key string) *uuid.UUID {
	raw := b.value(n, key)
	if raw == nil {
		return nil
	}
	s, _ := raw.(string)
	id, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		v.Add(key, "Must be a UUID.")
		return nil
	}
	return &id
}

// required adds "Required" unless the attribute has a value or already has a message.
func required(v *helper.Validation, key string, missing bool) {
	if missing && len(v.Fields[key]) == 0 {
		v.Add(key, "Required")
	}
}

// ids reads {"ids": ["uuid", …]}.
func (b body) ids(v *helper.Validation, key string) []uuid.UUID {
	list, ok := b[key].([]any)
	if !ok || len(list) == 0 {
		v.Add(key, "Choose at least one item.")
		return nil
	}
	out := make([]uuid.UUID, 0, len(list))
	for _, x := range list {
		s, _ := x.(string)
		id, err := uuid.Parse(s)
		if err != nil {
			v.Add(key, "Each id must be a UUID.")
			return nil
		}
		out = append(out, id)
	}
	return out
}

// ParamID reads a UUID path parameter; anything else is "not found", as for an id that does not exist.
func ParamID(c fiber.Ctx, name, what string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.Nil(), helper.NotFound(what)
	}
	return id, nil
}

// paging reads ?page (from 1) and ?per_page.
func paging(c fiber.Ctx) (limit, page int64) {
	limit, _ = utils.ParseInt(c.Query("per_page"))
	if limit < 1 {
		limit = models.DefaultLimit
	}
	limit = min(limit, models.MaxLimit)
	page, _ = utils.ParseInt(c.Query("page"))
	return limit, max(page, 1)
}

// sorting reads ?sort=<field>&order=asc|desc; ok is false when the field is not in allowed, and the list then
// keeps its default order.
func sorting(c fiber.Ctx, allowed map[string]struct{}) (response models.Sorting, ok bool) {
	field := c.Query("sort")
	if _, ok = allowed[field]; !ok {
		return
	}
	direction := models.SortingDirectionAsc
	if models.SortingDirectionDesc.Equal(c.Query("order")) {
		direction = models.SortingDirectionDesc
	}
	return models.Sorting{Field: field, Direction: direction}, true
}

// queryBool reads ?key=true|false|1|0; ok is false when it is absent or something else.
func queryBool(c fiber.Ctx, key string) (value, ok bool) {
	switch strings.ToLower(c.Query(key)) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	}
	return false, false
}

// queryID reads a UUID filter from the query string; anything else is ignored.
func queryID(c fiber.Ctx, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Query(key))
	return id, err == nil
}

// queryDate reads a YYYY-MM-DD filter from the query string; anything else is ignored.
func queryDate(c fiber.Ctx, key string) (string, bool) {
	s := c.Query(key)
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return "", false
	}
	return s, true
}

// BulkDeleteIDs reads {"ids": […]} of the bulk-delete endpoints.
func BulkDeleteIDs(c fiber.Ctx) ([]uuid.UUID, error) {
	b, err := parseBody(c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	ids := b.ids(v, "ids")
	return ids, v.Err()
}
