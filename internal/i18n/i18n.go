// Package i18n translates user-facing text into the client's language (English or Bahasa Indonesia).
//
// English text is the dictionary key, with {name} placeholders filled from name/value pairs:
//
//	i18n.ID.T("Row {row}: product not found.", "row", 3) // "Baris 3: produk tidak ditemukan."
//
// A value that is itself a Text is translated too, so nouns can be passed along:
//
//	i18n.ID.T("{what} not found", "what", i18n.Tr("Product")) // "Produk tidak ditemukan"
//
// Text that has no entry stays in English. The language of a request comes from Accept-Language, then from the
// branch's display settings (language), then English (see httpx.Lang).
package i18n

import (
	"fmt"
	"strconv"
	"strings"
)

// Lang is a supported language code.
type Lang string

const (
	EN Lang = "en"
	ID Lang = "id"
)

// Supported reports whether l is a language the API speaks.
func Supported(l string) bool { return l == string(EN) || l == string(ID) }

// Parse picks the best supported language from an Accept-Language header ("id-ID,id;q=0.9,en;q=0.8").
// ok is false when the header names no supported language.
func Parse(header string) (lang Lang, ok bool) {
	best := -1.0
	for part := range strings.SplitSeq(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		tag = strings.ToLower(strings.TrimSpace(tag))
		base, _, _ := strings.Cut(tag, "-")
		var l Lang
		switch base {
		case "id", "in": // "in" is the legacy code for Indonesian
			l = ID
		case "en":
			l = EN
		default:
			continue
		}
		q := 1.0
		if k, v, found := strings.Cut(strings.TrimSpace(params), "="); found && strings.TrimSpace(k) == "q" {
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				q = f
			}
		}
		if q > best && q > 0 {
			best, lang, ok = q, l, true
		}
	}
	return lang, ok
}

// T translates s and fills its {placeholders} from name/value pairs.
func (l Lang) T(s string, args ...any) string {
	out := s
	if l == ID {
		if t, found := id[s]; found {
			out = t
		}
	}
	if len(args) < 2 || !strings.Contains(out, "{") {
		return out
	}
	pairs := make([]string, 0, len(args))
	for i := 0; i+1 < len(args); i += 2 {
		pairs = append(pairs, "{"+fmt.Sprint(args[i])+"}", l.value(args[i+1]))
	}
	return strings.NewReplacer(pairs...).Replace(out)
}

func (l Lang) value(v any) string {
	switch x := v.(type) {
	case Text:
		return x.In(l)
	case *Text:
		if x == nil {
			return ""
		}
		return x.In(l)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// Has reports whether s has an Indonesian translation (used by tests).
func Has(s string) bool {
	_, ok := id[s]
	return ok
}

// Text is a message kept untranslated until the language of the response is known.
type Text struct {
	Key  string
	Args []any
}

// Tr builds a Text: Tr("Row {row}: product not found.", "row", 3).
func Tr(key string, args ...any) Text { return Text{Key: key, Args: args} }

// In renders the text in a language.
func (t Text) In(l Lang) string { return l.T(t.Key, t.Args...) }

// String renders the text in English.
func (t Text) String() string { return t.In(EN) }

// Params returns the placeholder values as a JSON-friendly map for storage. Text values are stored as
// {"$t": key} so they are translated when read back (see Restore).
func Params(args ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(args); i += 2 {
		k := fmt.Sprint(args[i])
		switch x := args[i+1].(type) {
		case Text:
			m[k] = map[string]any{"$t": x.Key}
		default:
			m[k] = x
		}
	}
	return m
}

// Restore turns stored params back into T arguments.
func Restore(m map[string]any) []any {
	args := make([]any, 0, 2*len(m))
	for k, v := range m {
		if o, ok := v.(map[string]any); ok {
			if key, ok := o["$t"].(string); ok {
				args = append(args, k, Tr(key))
				continue
			}
		}
		args = append(args, k, v)
	}
	return args
}
