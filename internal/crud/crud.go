// Package crud is a small generic engine for list/get/create/update/delete endpoints.
// Each resource declares its fields once; SQL, validation and JSON shapes follow from that.
package crud

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/internal/auth"
	"github.com/roysitumorang/lestari-backend/internal/db"
	"github.com/roysitumorang/lestari-backend/internal/httpx"
)

type Kind int

const (
	String Kind = iota
	Int
	Num
	Bool
	Date      // YYYY-MM-DD
	Time      // HH:mm
	Timestamp // RFC 3339
	JSON      // JSONB array/object
	IntArray  // BIGINT[]
	StrArray  // TEXT[]
	Password  // write-only, sent base64-encoded, stored as bcrypt hash in Col
	PIN       // write-only numeric PIN (sent as plain digits), stored as bcrypt hash in Col
	UUID      // UUID (ids and foreign keys)
	UUIDArray // UUID[]
)

// Ref adds a nested {id, <label>} object next to a foreign-key field.
type Ref struct {
	Key   string // JSON key of the nested object, e.g. "category"
	Table string
	Label string // label column, default "name"
	// ScopeCol is the referenced table's tenant column checked on write:
	// "branch_id" (default) must equal the active branch, "store_id" the store.
	ScopeCol string
}

// checkRefs verifies that every referenced id belongs to the caller's tenant.
func (r *Resource) checkRefs(ctx context.Context, q db.Querier, p *httpx.Principal, vals map[string]any) error {
	v := &httpx.Validation{}
	for _, f := range r.Fields {
		if f.Ref == nil {
			continue
		}
		val, ok := vals[f.col()]
		if !ok || val == nil {
			continue
		}
		col, scope := "branch_id", p.BranchID
		if f.Ref.ScopeCol == "store_id" {
			col, scope = "store_id", p.StoreID
		}
		var exists bool
		if err := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+f.Ref.Table+" WHERE id = $1 AND "+col+" = $2)", val, scope).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			v.Add(f.Name, "Not found.")
		}
	}
	return v.Err()
}

type Field struct {
	Name       string // JSON attribute
	Col        string // column (defaults to Name)
	Kind       Kind
	Required   bool
	Enum       []string
	Min, Max   *float64
	Default    any    // used on create when the value is missing
	ReadOnly   bool   // returned but never written by clients
	CreateOnly bool   // written on create, ignored on update
	Expr       string // SQL expression for computed read-only values (use alias t)
	Search     bool   // included in ?q= search
	Filter     bool   // ?<name>=value equality filter
	Ref        *Ref
	NoExport   bool
	Virtual    bool // validated and passed to hooks via Ctx.Body, but not a column (e.g. line items)
}

func (f Field) col() string {
	if f.Col != "" {
		return f.Col
	}
	return f.Name
}

func (f Field) writable() bool { return !f.ReadOnly && f.Expr == "" && !f.Virtual }

// Filter is a custom query-parameter filter. SQL uses "?" for the single parameter.
type Filter struct {
	Param string
	SQL   string
	Cast  func(string) (any, bool) // optional; return false to ignore the value
	// Build replaces SQL when the condition depends on the caller (e.g. the active branch).
	Build func(b *Builder, p *httpx.Principal, value string) string
}

// Ctx is passed to hooks.
type Ctx struct {
	Fiber fiber.Ctx
	P     *httpx.Principal
	Tx    pgx.Tx
	Body  map[string]any
}

type Resource struct {
	Name     string // plural, used in messages and export file names
	Singular string
	Table    string
	IDName   string // JSON name of the primary key (default "id")
	Fields   []Field

	// Scope restricts rows to the caller's tenant. Default: t.branch_id = active branch.
	Scope func(b *Builder, p *httpx.Principal) string
	// InsertScope returns columns set on insert from the principal. Default: branch_id.
	InsertScope func(p *httpx.Principal) map[string]any

	Where        string // fixed extra condition, e.g. "t.kind = 'payable'"
	SoftDelete   bool
	NoTimestamps bool
	DefaultSort  string
	Filters      []Filter
	ImportKey    string // field used to match rows in update-mode imports

	BeforeWrite  func(ctx context.Context, h *Ctx, vals map[string]any, id string) error // mutate column values
	AfterWrite   func(ctx context.Context, h *Ctx, id string, created bool) error        // child rows etc.
	BeforeDelete func(ctx context.Context, h *Ctx, id string) error
	Detail       func(ctx context.Context, q db.Querier, p *httpx.Principal, row map[string]any) error
	// Post adjusts rows returned by list and get, e.g. values relative to the caller.
	Post func(p *httpx.Principal, row map[string]any)
}

type Engine struct {
	DB *pgxpool.Pool
}

// ---------- SQL builder ----------

type Builder struct{ Args []any }

func (b *Builder) Add(v any) string {
	b.Args = append(b.Args, v)
	return "$" + strconv.Itoa(len(b.Args))
}

func (r *Resource) idName() string {
	if r.IDName != "" {
		return r.IDName
	}
	return "id"
}

func (r *Resource) scope(b *Builder, p *httpx.Principal) string {
	if r.Scope != nil {
		return r.Scope(b, p)
	}
	return "t.branch_id = " + b.Add(p.BranchID)
}

func (r *Resource) insertScope(p *httpx.Principal) map[string]any {
	if r.InsertScope != nil {
		return r.InsertScope(p)
	}
	return map[string]any{"branch_id": p.BranchID}
}

func labelOf(ref *Ref) string {
	if ref.Label != "" {
		return ref.Label
	}
	return "name"
}

func (r *Resource) selectList() string {
	cols := []string{`t.id AS "` + r.idName() + `"`}
	if r.idName() != "id" {
		cols = append(cols, `t.id AS "id"`)
	}
	for _, f := range r.Fields {
		if f.Kind == Password || f.Kind == PIN || f.Virtual {
			continue
		}
		expr := "t." + f.col()
		if f.Expr != "" {
			expr = f.Expr
		}
		cols = append(cols, expr+` AS "`+f.Name+`"`)
		if f.Ref != nil {
			l := labelOf(f.Ref)
			cols = append(cols, fmt.Sprintf(`(SELECT json_build_object('id', r.id, '%s', r.%s) FROM %s r WHERE r.id = t.%s) AS "%s"`, l, l, f.Ref.Table, f.col(), f.Ref.Key))
		}
	}
	if !r.NoTimestamps {
		cols = append(cols, `t.created_at AS "created_at"`, `t.updated_at AS "updated_at"`)
	}
	return strings.Join(cols, ", ")
}

func (r *Resource) baseWhere(b *Builder, p *httpx.Principal) []string {
	w := []string{r.scope(b, p)}
	if r.SoftDelete {
		w = append(w, "t.deleted_at IS NULL")
	}
	if r.Where != "" {
		w = append(w, r.Where)
	}
	return w
}

func (r *Resource) field(name string) (Field, bool) {
	for _, f := range r.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

func castExpr(k Kind) string {
	switch k {
	case Int:
		return "::bigint"
	case Num:
		return "::numeric"
	case Bool:
		return "::boolean"
	case Date:
		return "::date"
	case Timestamp:
		return "::timestamptz"
	case JSON:
		return "::jsonb"
	case IntArray:
		return "::bigint[]"
	case UUID:
		return "::uuid"
	case UUIDArray:
		return "::uuid[]"
	case StrArray:
		return "::text[]"
	}
	return ""
}

// listWhere applies search and filters from the query string.
func (r *Resource) listWhere(c fiber.Ctx, b *Builder, p *httpx.Principal) []string {
	w := r.baseWhere(b, p)
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		ph := b.Add("%" + q + "%")
		var or []string
		for _, f := range r.Fields {
			if f.Search {
				expr := "t." + f.col()
				if f.Expr != "" {
					expr = f.Expr
				}
				or = append(or, "("+expr+")::text ILIKE "+ph)
			}
		}
		if len(or) > 0 {
			w = append(w, "("+strings.Join(or, " OR ")+")")
		}
	}
	for _, f := range r.Fields {
		if !f.Filter {
			continue
		}
		v := c.Query(f.Name)
		if v == "" {
			continue
		}
		val, err := coerce(f, v)
		if err != "" || val == nil {
			continue
		}
		expr := "t." + f.col()
		if f.Expr != "" {
			expr = f.Expr
		}
		w = append(w, expr+" = "+b.Add(val)+castExpr(f.Kind))
	}
	for _, f := range r.Filters {
		v := c.Query(f.Param)
		if v == "" {
			continue
		}
		if f.Build != nil {
			if cond := f.Build(b, p, v); cond != "" {
				w = append(w, cond)
			}
			continue
		}
		var val any = v
		if f.Cast != nil {
			var ok bool
			if val, ok = f.Cast(v); !ok {
				continue
			}
		}
		w = append(w, strings.ReplaceAll(f.SQL, "?", b.Add(val)))
	}
	return w
}

func (r *Resource) orderBy(c fiber.Ctx) string {
	sort := c.Query("sort")
	dir := "ASC"
	if strings.EqualFold(c.Query("order"), "desc") {
		dir = "DESC"
	}
	if sort != "" {
		if sort == "created_at" || sort == "updated_at" || sort == "id" {
			return "t." + sort + " " + dir + ", t.id " + dir
		}
		for _, f := range r.Fields {
			if f.Kind == Password || f.Kind == PIN || f.Kind == JSON {
				continue
			}
			expr := "t." + f.col()
			if f.Expr != "" {
				expr = f.Expr
			}
			if f.Name == sort {
				return expr + " " + dir + " NULLS LAST, t.id " + dir
			}
			if f.Ref != nil && sort == f.Ref.Key+"."+labelOf(f.Ref) {
				return fmt.Sprintf("(SELECT r.%s FROM %s r WHERE r.id = t.%s) %s NULLS LAST, t.id %s", labelOf(f.Ref), f.Ref.Table, f.col(), dir, dir)
			}
		}
	}
	if r.DefaultSort != "" {
		return r.DefaultSort
	}
	return "t.id DESC"
}

// ---------- handlers ----------

func (e *Engine) List(r *Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		p := httpx.P(c)
		pg := httpx.Pagination(c, 25)
		b := &Builder{}
		where := strings.Join(r.listWhere(c, b, p), " AND ")
		var total int
		if err := e.DB.QueryRow(c.Context(), "SELECT count(*) FROM "+r.Table+" t WHERE "+where, b.Args...).Scan(&total); err != nil {
			return err
		}
		sql := "SELECT " + r.selectList() + " FROM " + r.Table + " t WHERE " + where + " ORDER BY " + r.orderBy(c) +
			" LIMIT " + strconv.Itoa(pg.PerPage) + " OFFSET " + strconv.Itoa(pg.Offset)
		if r.Post != nil {
			rows, err := db.JSONRows(c.Context(), e.DB, sql, b.Args...)
			if err != nil {
				return err
			}
			for _, row := range rows {
				r.Post(p, row)
			}
			return httpx.List(c, rows, httpx.NewMeta(pg, total))
		}
		raw, err := db.JSONRaw(c.Context(), e.DB, sql, b.Args...)
		if err != nil {
			return err
		}
		return httpx.List(c, raw, httpx.NewMeta(pg, total))
	}
}

// Rows returns all rows matching the request's filters (used by exports).
func (e *Engine) Rows(c fiber.Ctx, r *Resource, limit int) ([]map[string]any, error) {
	b := &Builder{}
	where := strings.Join(r.listWhere(c, b, httpx.P(c)), " AND ")
	sql := "SELECT " + r.selectList() + " FROM " + r.Table + " t WHERE " + where + " ORDER BY " + r.orderBy(c) + " LIMIT " + strconv.Itoa(limit)
	rows, err := db.JSONRows(c.Context(), e.DB, sql, b.Args...)
	if err == nil && r.Post != nil {
		for _, row := range rows {
			r.Post(httpx.P(c), row)
		}
	}
	return rows, err
}

// Fetch loads one row (with Detail enrichment) or returns a 404 error.
func (e *Engine) Fetch(ctx context.Context, q db.Querier, r *Resource, p *httpx.Principal, id string) (map[string]any, error) {
	b := &Builder{}
	where := append(r.baseWhere(b, p), "t.id = "+b.Add(id)+"::uuid")
	row, err := db.JSONRow(ctx, q, "SELECT "+r.selectList()+" FROM "+r.Table+" t WHERE "+strings.Join(where, " AND "), b.Args...)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, httpx.NotFound(r.Singular)
	}
	if r.Detail != nil {
		if err := r.Detail(ctx, q, p, row); err != nil {
			return nil, err
		}
	}
	if r.Post != nil {
		r.Post(p, row)
	}
	return row, nil
}

func (e *Engine) Get(r *Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			return err
		}
		row, err := e.Fetch(c.Context(), e.DB, r, httpx.P(c), id)
		if err != nil {
			return err
		}
		return httpx.OK(c, row)
	}
}

func (e *Engine) Create(r *Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		body, err := httpx.Body(c)
		if err != nil {
			return err
		}
		row, err := e.Save(c, r, body, "")
		if err != nil {
			return err
		}
		return httpx.Created(c, row)
	}
}

func (e *Engine) Update(r *Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			return err
		}
		body, err := httpx.Body(c)
		if err != nil {
			return err
		}
		row, err := e.Save(c, r, body, id)
		if err != nil {
			return err
		}
		return httpx.OK(c, row)
	}
}

// Values validates a body against the resource fields and returns column values.
// On update (id != "") only attributes present in the body are written; send null to clear one.
func (r *Resource) Values(body map[string]any, id string) (map[string]any, error) {
	v := &httpx.Validation{}
	vals := map[string]any{}
	for _, f := range r.Fields {
		if !f.writable() {
			continue
		}
		raw, present := body[f.Name]
		if id != "" && (f.CreateOnly || !present) {
			continue
		}
		if f.Kind == Password || f.Kind == PIN {
			s, _ := raw.(string)
			if f.Kind == Password {
				plain, ok := httpx.DecodeBase64Password(s)
				if !ok {
					v.Add(f.Name, "Send the password base64-encoded.")
					continue
				}
				s = plain
			}
			if s == "" {
				if id == "" && f.Required {
					v.Add(f.Name, "Required")
				}
				continue
			}
			if len(s) < 6 {
				v.Add(f.Name, "Use at least 6 characters.")
				continue
			}
			h, err := auth.HashPassword(s)
			if err != nil {
				return nil, err
			}
			vals[f.col()] = h
			continue
		}
		val, msg := coerce(f, raw)
		if msg != "" {
			v.Add(f.Name, msg)
			continue
		}
		if val == nil && f.Default != nil && (id == "" || f.Kind == JSON || f.Kind == IntArray || f.Kind == UUIDArray || f.Kind == StrArray || f.Kind == Bool) {
			val = f.Default
		}
		if val == nil && f.Required {
			v.Add(f.Name, "Required")
			continue
		}
		vals[f.col()] = val
	}
	for _, f := range r.Fields {
		if f.Virtual && f.Required && id == "" {
			if arr, ok := body[f.Name].([]any); !ok || len(arr) == 0 {
				v.Add(f.Name, "Add at least one row.")
			}
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	return vals, nil
}

// Save creates (id == "") or updates a row in a transaction and returns the fresh row.
func (e *Engine) Save(c fiber.Ctx, r *Resource, body map[string]any, id string) (map[string]any, error) {
	return e.SaveCtx(c.Context(), c, r, body, id)
}

func (e *Engine) SaveCtx(ctx context.Context, c fiber.Ctx, r *Resource, body map[string]any, id string) (map[string]any, error) {
	p := httpx.P(c)
	vals, err := r.Values(body, id)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = pgx.BeginFunc(ctx, e.DB, func(tx pgx.Tx) error {
		h := &Ctx{Fiber: c, P: p, Tx: tx, Body: body}
		if id != "" {
			// make sure the row exists in this tenant before writing
			if _, err := e.Fetch(ctx, tx, r, p, id); err != nil {
				return err
			}
		}
		if err := r.checkRefs(ctx, tx, p, vals); err != nil {
			return err
		}
		if r.BeforeWrite != nil {
			if err := r.BeforeWrite(ctx, h, vals, id); err != nil {
				return err
			}
		}
		kinds := map[string]Kind{}
		for _, f := range r.Fields {
			kinds[f.col()] = f.Kind
		}
		b := &Builder{}
		if id == "" {
			for k, v := range r.insertScope(p) {
				if _, set := vals[k]; !set {
					vals[k] = v
				}
			}
			cols, phs := make([]string, 0, len(vals)), make([]string, 0, len(vals))
			for k, v := range vals {
				if v == nil {
					continue // let the column default apply
				}
				cols = append(cols, k)
				phs = append(phs, b.Add(encode(kinds[k], v))+castExpr(kinds[k]))
			}
			if err := tx.QueryRow(ctx, "INSERT INTO "+r.Table+" ("+strings.Join(cols, ",")+") VALUES ("+strings.Join(phs, ",")+") RETURNING id", b.Args...).Scan(&id); err != nil {
				return mapWriteErr(r, err)
			}
			if r.AfterWrite != nil {
				if err := r.AfterWrite(ctx, h, id, true); err != nil {
					return err
				}
			}
		} else {
			sets := make([]string, 0, len(vals)+1)
			for k, v := range vals {
				sets = append(sets, k+" = "+b.Add(encode(kinds[k], v))+castExpr(kinds[k]))
			}
			if !r.NoTimestamps {
				sets = append(sets, "updated_at = now()")
			}
			if len(sets) > 0 {
				if _, err := tx.Exec(ctx, "UPDATE "+r.Table+" SET "+strings.Join(sets, ", ")+" WHERE id = "+b.Add(id)+"::uuid", b.Args...); err != nil {
					return mapWriteErr(r, err)
				}
			}
			if r.AfterWrite != nil {
				if err := r.AfterWrite(ctx, h, id, false); err != nil {
					return err
				}
			}
		}
		var err error
		out, err = e.Fetch(ctx, tx, r, p, id)
		return err
	})
	return out, err
}

func mapWriteErr(r *Resource, err error) error {
	switch {
	case db.IsUniqueViolation(err):
		return httpx.Conflict("A " + strings.ToLower(r.Singular) + " with the same code or name already exists.")
	case db.IsForeignKeyViolation(err):
		return httpx.Invalid("id", "A referenced record does not exist.")
	}
	return err
}

func (e *Engine) deleteOne(ctx context.Context, c fiber.Ctx, tx pgx.Tx, r *Resource, id string) error {
	p := httpx.P(c)
	if _, err := e.Fetch(ctx, tx, r, p, id); err != nil {
		return err
	}
	if r.BeforeDelete != nil {
		if err := r.BeforeDelete(ctx, &Ctx{Fiber: c, P: p, Tx: tx}, id); err != nil {
			return err
		}
	}
	var err error
	if r.SoftDelete {
		_, err = tx.Exec(ctx, "UPDATE "+r.Table+" SET deleted_at = now() WHERE id = $1", id)
	} else {
		_, err = tx.Exec(ctx, "DELETE FROM "+r.Table+" WHERE id = $1", id)
	}
	if db.IsForeignKeyViolation(err) {
		return httpx.Conflict("This " + strings.ToLower(r.Singular) + " is still used by other records.")
	}
	return err
}

func (e *Engine) Delete(r *Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamID(c, "id")
		if err != nil {
			return err
		}
		if err := pgx.BeginFunc(c.Context(), e.DB, func(tx pgx.Tx) error { return e.deleteOne(c.Context(), c, tx, r, id) }); err != nil {
			return err
		}
		return httpx.NoContent(c)
	}
}

func (e *Engine) BulkDelete(r *Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := httpx.Decode(c, &in); err != nil {
			return err
		}
		if len(in.IDs) == 0 {
			return httpx.Invalid("ids", "Choose at least one item.")
		}
		err := pgx.BeginFunc(c.Context(), e.DB, func(tx pgx.Tx) error {
			for _, id := range in.IDs {
				if !httpx.IsUUID(id) {
					return httpx.Invalid("ids", "Each id must be a UUID.")
				}
				if err := e.deleteOne(c.Context(), c, tx, r, id); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		return httpx.OK(c, fiber.Map{"deleted": len(in.IDs)})
	}
}

// ---------- value coercion ----------

func coerce(f Field, raw any) (any, string) {
	if raw == nil {
		return nil, ""
	}
	if s, ok := raw.(string); ok && strings.TrimSpace(s) == "" && f.Kind != JSON {
		return nil, ""
	}
	switch f.Kind {
	case String:
		var s string
		switch x := raw.(type) {
		case string:
			s = strings.TrimSpace(x)
		case float64:
			s = strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			s = strconv.FormatBool(x)
		default:
			return nil, "Must be text."
		}
		if len(f.Enum) > 0 && !contains(f.Enum, s) {
			return nil, "Must be one of: " + strings.Join(f.Enum, ", ") + "."
		}
		return s, ""
	case Int, Num:
		var n float64
		switch x := raw.(type) {
		case float64:
			n = x
		case string:
			var err error
			n, err = strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(x), ",", ""), 64)
			if err != nil {
				return nil, "Must be a number."
			}
		case bool:
			return nil, "Must be a number."
		default:
			return nil, "Must be a number."
		}
		if len(f.Enum) > 0 && !contains(f.Enum, strconv.FormatFloat(n, 'f', -1, 64)) {
			return nil, "Must be one of: " + strings.Join(f.Enum, ", ") + "."
		}
		if f.Min != nil && n < *f.Min {
			return nil, "Must be at least " + strconv.FormatFloat(*f.Min, 'f', -1, 64) + "."
		}
		if f.Max != nil && n > *f.Max {
			return nil, "Must be at most " + strconv.FormatFloat(*f.Max, 'f', -1, 64) + "."
		}
		if f.Kind == Int {
			return int64(n), ""
		}
		return n, ""
	case Bool:
		switch x := raw.(type) {
		case bool:
			return x, ""
		case string:
			s := strings.ToLower(strings.TrimSpace(x))
			return s == "true" || s == "1" || s == "yes" || s == "y" || s == "on", ""
		case float64:
			return x != 0, ""
		}
		return nil, "Must be true or false."
	case Date:
		s, ok := raw.(string)
		if !ok || len(s) < 10 {
			return nil, "Use the format YYYY-MM-DD."
		}
		if _, err := time.Parse("2006-01-02", s[:10]); err != nil {
			return nil, "Use the format YYYY-MM-DD."
		}
		return s[:10], ""
	case Time:
		s, ok := raw.(string)
		if !ok {
			return nil, "Use the format HH:mm."
		}
		if _, err := time.Parse("15:04", s[:min(5, len(s))]); err != nil {
			return nil, "Use the format HH:mm."
		}
		return s[:5], ""
	case Timestamp:
		s, ok := raw.(string)
		if !ok {
			return nil, "Use an ISO 8601 date-time."
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, "Use an ISO 8601 date-time."
		}
		return t, ""
	case JSON:
		if s, ok := raw.(string); ok {
			if strings.TrimSpace(s) == "" {
				return nil, ""
			}
			var x any
			if err := json.Unmarshal([]byte(s), &x); err != nil {
				return nil, "Must be valid JSON."
			}
			raw = x
		}
		switch x := raw.(type) {
		case []any:
			// drop fully empty rows sent by table editors
			clean := make([]any, 0, len(x))
			for _, it := range x {
				if m, ok := it.(map[string]any); ok && emptyRow(m) {
					continue
				}
				clean = append(clean, it)
			}
			return clean, ""
		case map[string]any:
			return x, ""
		}
		return nil, "Must be a list or object."
	case IntArray:
		arr, ok := toList(raw)
		if !ok {
			return nil, "Must be a list of numbers."
		}
		out := make([]int64, 0, len(arr))
		for _, it := range arr {
			switch x := it.(type) {
			case float64:
				out = append(out, int64(x))
			case string:
				n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
				if err != nil {
					return nil, "Must be a list of numbers."
				}
				out = append(out, n)
			default:
				return nil, "Must be a list of numbers."
			}
		}
		return out, ""
	case UUID:
		s, ok := raw.(string)
		s = strings.ToLower(strings.TrimSpace(s))
		if !ok || !httpx.IsUUID(s) {
			return nil, "Must be a UUID."
		}
		return s, ""
	case UUIDArray:
		arr, ok := toList(raw)
		if !ok {
			return nil, "Must be a list of UUIDs."
		}
		out := make([]string, 0, len(arr))
		for _, it := range arr {
			s, _ := it.(string)
			s = strings.ToLower(strings.TrimSpace(s))
			if !httpx.IsUUID(s) {
				return nil, "Must be a list of UUIDs."
			}
			out = append(out, s)
		}
		return out, ""
	case StrArray:
		arr, ok := toList(raw)
		if !ok {
			return nil, "Must be a list."
		}
		out := make([]string, 0, len(arr))
		for _, it := range arr {
			s := fmt.Sprint(it)
			if len(f.Enum) > 0 && !contains(f.Enum, s) {
				return nil, "Values must be from: " + strings.Join(f.Enum, ", ") + "."
			}
			out = append(out, s)
		}
		return out, ""
	}
	return raw, ""
}

func toList(raw any) ([]any, bool) {
	switch x := raw.(type) {
	case []any:
		return x, true
	case string:
		var out []any
		for _, s := range strings.Split(x, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

func emptyRow(m map[string]any) bool {
	for _, v := range m {
		switch x := v.(type) {
		case nil:
		case string:
			if strings.TrimSpace(x) != "" {
				return false
			}
		case bool:
			if x {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// encode converts coerced values into what pgx expects for the column kind.
func encode(k Kind, v any) any {
	if v == nil {
		return nil
	}
	if k == JSON {
		b, _ := json.Marshal(v)
		return string(b)
	}
	return v
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// F is shorthand for float pointers in Min/Max.
func F(v float64) *float64 { return &v }
