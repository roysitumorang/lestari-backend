// Package sanitizers turns requests into models and filters, and reports what is wrong with them.
package sanitizers

import (
	"strings"
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

// present reports whether the attribute was sent with a value (null counts as absent).
func (b body) present(key string) bool {
	return b[key] != nil
}

func (b body) text(v *helper.Validation, key string) *string {
	if !b.present(key) {
		return nil
	}
	s, ok := b[key].(string)
	if !ok {
		v.Add(key, "Must be text.")
		return nil
	}
	return new(strings.TrimSpace(s))
}

func (b body) number(v *helper.Validation, key string) *float64 {
	if !b.present(key) {
		return nil
	}
	n, ok := b[key].(float64)
	if !ok {
		v.Add(key, "Must be a number.")
		return nil
	}
	return &n
}

func (b body) boolean(v *helper.Validation, key string) *bool {
	if !b.present(key) {
		return nil
	}
	x, ok := b[key].(bool)
	if !ok {
		v.Add(key, "Must be true or false.")
		return nil
	}
	return &x
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

// sorting reads ?sort=<field>&order=asc|desc; a field outside allowed falls back to the default.
func sorting(c fiber.Ctx, allowed map[string]struct{}, fallback models.Sorting) models.Sorting {
	field := c.Query("sort")
	if _, ok := allowed[field]; !ok {
		return fallback
	}
	direction := models.SortingDirectionAsc
	if models.SortingDirectionDesc.Equal(c.Query("order")) {
		direction = models.SortingDirectionDesc
	}
	return models.Sorting{Field: field, Direction: direction}
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
