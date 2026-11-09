package crud

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/internal/export"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
	"github.com/roysitumorang/lestari-backend/internal/i18n"
)

// ExportColumns lists scalar fields plus referenced labels.
func (r *Resource) ExportColumns() []export.Column {
	cols := []export.Column{{Key: r.idName(), Label: r.idName()}} // UUID, exported as text
	for _, f := range r.Fields {
		if f.NoExport || f.Virtual || f.Kind == Password || f.Kind == PIN || f.Kind == JSON {
			continue
		}
		format := map[Kind]string{Int: "number", Num: "number", Bool: "boolean", Date: "date", Timestamp: "datetime"}[f.Kind]
		cols = append(cols, export.Column{Key: f.Name, Label: f.Name, Format: format})
		if f.Ref != nil {
			l := labelOf(f.Ref)
			cols = append(cols, export.Column{Key: f.Ref.Key + "." + l, Label: f.Ref.Key + "_" + l})
		}
	}
	return append(cols, export.Column{Key: "created_at", Label: "created_at", Format: "datetime"})
}

func (e *Engine) Export(r *Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := e.Rows(c, r, 50000)
		if err != nil {
			return err
		}
		name := strings.ReplaceAll(strings.ToLower(r.Name), " ", "-")
		return export.Write(c, name, r.Name, r.ExportColumns(), rows)
	}
}

// importFields are the scalar writable fields, in template order.
func (r *Resource) importFields() []Field {
	var out []Field
	for _, f := range r.Fields {
		if f.writable() && f.Kind != Password && f.Kind != PIN && f.Kind != JSON {
			out = append(out, f)
		}
	}
	return out
}

func (e *Engine) ImportTemplate(r *Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		var cols []export.Column
		example := map[string]any{}
		for _, f := range r.importFields() {
			label := f.Name
			if f.Required {
				label += "*"
			}
			cols = append(cols, export.Column{Key: f.Name, Label: label})
			switch {
			case len(f.Enum) > 0:
				example[f.Name] = f.Enum[0]
			case f.Kind == Num || f.Kind == Int:
				example[f.Name] = float64(0)
			case f.Kind == Bool:
				example[f.Name] = "true"
			case f.Kind == Date:
				example[f.Name] = "2026-01-31"
			}
		}
		b, err := export.XLSX(httpx.Lang(c), r.Name, cols, []map[string]any{example})
		if err != nil {
			return err
		}
		name := strings.ReplaceAll(strings.ToLower(r.Name), " ", "-") + "-template.xlsx"
		return export.Send(c, name, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", b)
	}
}

type ImportResult struct {
	Created int           `json:"created"`
	Updated int           `json:"updated"`
	Failed  int           `json:"failed"`
	Errors  []ImportError `json:"errors"`
}

type ImportError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

// ImportRows reads the uploaded "file" form field into maps keyed by header names (with "*" removed).
func ImportRows(c fiber.Ctx) ([]map[string]any, error) {
	fh, err := c.FormFile("file")
	if err != nil {
		return nil, httpx.Invalid("file", "Choose a .xlsx or .csv file.")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head, rows, err := export.ReadTable(fh.Filename, f)
	if err != nil {
		return nil, httpx.Invalid("file", err.Error())
	}
	out := make([]map[string]any, 0, len(rows))
	for _, rec := range rows {
		m := map[string]any{}
		empty := true
		for i, h := range head {
			h = strings.TrimSuffix(h, "*")
			if h == "" || i >= len(rec) {
				continue
			}
			if v := strings.TrimSpace(rec[i]); v != "" {
				m[h] = v
				empty = false
			}
		}
		if !empty {
			out = append(out, m)
		}
	}
	return out, nil
}

func (e *Engine) Import(r *Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := ImportRows(c)
		if err != nil {
			return err
		}
		mode := c.FormValue("mode", "create")
		res := ImportResult{Errors: []ImportError{}}
		ctx := context.Background()
		for i, row := range rows {
			rowNo := i + 2 // header is row 1
			var id string
			if mode == "update" && r.ImportKey != "" {
				key := httpx.Str(row, r.ImportKey)
				if key == "" {
					res.Failed++
					res.Errors = append(res.Errors, ImportError{rowNo, r.ImportKey + " is required to update"})
					continue
				}
				f, _ := r.field(r.ImportKey)
				b := &Builder{}
				where := append(r.baseWhere(b, httpx.P(c)), "lower(t."+f.col()+") = lower("+b.Add(key)+")")
				if err := e.DB.QueryRow(ctx, "SELECT t.id FROM "+r.Table+" t WHERE "+strings.Join(where, " AND ")+" LIMIT 1", b.Args...).Scan(&id); err != nil {
					res.Failed++
					res.Errors = append(res.Errors, ImportError{rowNo, "No " + strings.ToLower(r.Singular) + " with " + r.ImportKey + " " + key})
					continue
				}
			}
			if _, err := e.SaveCtx(ctx, c, r, row, id); err != nil {
				res.Failed++
				res.Errors = append(res.Errors, ImportError{rowNo, describe(httpx.Lang(c), err)})
				continue
			}
			if id != "" {
				res.Updated++
			} else {
				res.Created++
			}
		}
		return httpx.OK(c, res)
	}
}

func describe(l i18n.Lang, err error) string {
	he := &httpx.Error{}
	if errors.As(err, &he) {
		if len(he.Fields) > 0 {
			var parts []string
			for k, msgs := range he.Fields {
				texts := make([]string, len(msgs))
				for i, m := range msgs {
					texts[i] = m.In(l)
				}
				parts = append(parts, k+": "+strings.Join(texts, " "))
			}
			sort.Strings(parts)
			return strings.Join(parts, "; ")
		}
		return l.T(he.Message, he.Args...)
	}
	return err.Error()
}

// Register mounts the standard routes for the given operations under r.
func (e *Engine) Register(add func(method, path string, h fiber.Handler), base string, r *Resource, ops ...string) {
	for _, op := range ops {
		switch op {
		case "list":
			add("GET", base, e.List(r))
		case "create":
			add("POST", base, e.Create(r))
		case "read":
			add("GET", base+"/:id", e.Get(r))
		case "update":
			add("PUT", base+"/:id", e.Update(r))
		case "delete":
			add("DELETE", base+"/:id", e.Delete(r))
		case "bulk_delete":
			add("POST", base+"/bulk-delete", e.BulkDelete(r))
		case "import":
			add("POST", base+"/import", e.Import(r))
			add("GET", base+"/import-template", e.ImportTemplate(r))
		case "export":
			add("GET", base+"/export", e.Export(r))
		default:
			panic("unknown crud op " + strconv.Quote(op))
		}
	}
}
