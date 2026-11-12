package repositories

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

// base is embedded by repositories: the read pool, the write pool and the choice between a transaction and a pool.
type base struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

// reader runs a read inside tx when there is one, so it sees the writes of the transaction; otherwise on the
// read pool.
func (q *base) reader(tx pgx.Tx) Querier {
	if tx != nil {
		return tx
	}
	return q.dbRead
}

// writer runs a write inside tx when there is one, otherwise directly on the write pool.
func (q *base) writer(tx pgx.Tx) Querier {
	if tx != nil {
		return tx
	}
	return q.dbWrite
}

// queryRows runs a SELECT and scans every row into a T. Columns are matched to the fields of T by name
// (receipt_number fills ReceiptNumber); json columns fill struct, slice and map fields. The result is never nil.
func queryRows[T any](ctx context.Context, db Querier, ctxt, sql string, args ...any) ([]*T, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrQuery")
		return nil, err
	}
	response, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByNameLax[T])
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrCollectRows")
		return nil, err
	}
	if response == nil {
		response = []*T{}
	}
	return response, nil
}

// queryRow is queryRows for a single row; without one it returns pgx.ErrNoRows, which callers turn into the
// not-found error of their entity.
func queryRow[T any](ctx context.Context, db Querier, ctxt, sql string, args ...any) (*T, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrQuery")
		return nil, err
	}
	response, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByNameLax[T])
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrCollectOneRow")
		}
		return nil, err
	}
	return response, nil
}

// queryValue reads the single value of a single row (a count, a sum, an id); without a row it returns
// pgx.ErrNoRows.
func queryValue[T any](ctx context.Context, db Querier, ctxt, sql string, args ...any) (T, error) {
	var response T
	err := db.QueryRow(ctx, sql, args...).Scan(&response)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) && !IsUniqueViolation(err) && !IsForeignKeyViolation(err) {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
	}
	return response, err
}

// execute runs a statement and returns how many rows it changed. Constraint violations are returned as they are,
// for the caller to name.
func execute(ctx context.Context, db Querier, ctxt, sql string, args ...any) (int64, error) {
	tag, err := db.Exec(ctx, sql, args...)
	if err != nil {
		if !IsUniqueViolation(err) && !IsForeignKeyViolation(err) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		}
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// paginate appends LIMIT and OFFSET for a 1-based page; a limit of 0 means everything.
func paginate(limit, page int64) string {
	if limit <= 0 {
		return ""
	}
	return helper.Sprintf(" LIMIT %d OFFSET %d", limit, max(page-1, 0)*limit)
}

// changes collects the assignments of an UPDATE that writes only what differs from the stored row.
type changes struct {
	sets   []string
	params []any
}

// set adds "column = value" when differs is true; cast is an optional SQL cast such as "::date".
func (q *changes) set(differs bool, column string, value any, cast ...string) {
	if !differs {
		return
	}
	q.params = append(q.params, value)
	q.sets = append(q.sets, helper.Sprintf("%s = $%d%s", column, len(q.params), strings.Join(cast, "")))
}

// apply executes the UPDATE of the row with this id, adding updated_at (the caller's time.Now()). Without any
// difference nothing is executed and updated_at stays as it was.
func (q *changes) apply(ctx context.Context, db Querier, ctxt, table string, id uuid.UUID, updatedAt time.Time) error {
	if len(q.sets) == 0 {
		return nil
	}
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	params := slices.Concat(q.params, []any{updatedAt, id})
	n := len(params)
	_, _ = fmt.Fprintf(sb, "UPDATE %s SET %s, updated_at = $%d WHERE id = $%d", table, strings.Join(q.sets, ", "), n-1, n)
	_, err := execute(ctx, db, ctxt, sb.String(), params...)
	return err
}

// sprintf formats a condition whose placeholders are numbered at run time.
func sprintf(format string, args ...any) string {
	return helper.Sprintf(format, args...)
}

// helperLocation is the time zone of business dates.
func helperLocation() *time.Location {
	return helper.Location
}

// nextNumber builds the next human-friendly document number of a scope for a day (YYYYMMDD), such as
// INV/20261108/0007: the highest number of that day, plus one, so a deleted document never frees a number
// that is still in use.
func nextNumber(ctx context.Context, tx pgx.Tx, ctxt, prefix, day, table, column, scopeColumn string, scopeID uuid.UUID) (string, error) {
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COALESCE(MAX(SUBSTRING(t.%s FROM '([0-9]+)$')::bigint), 0) FROM %s t WHERE t.%s = $1 AND t.%s LIKE $2", column, table, scopeColumn, column)
	base := prefix + "/" + day + "/"
	count, err := queryValue[int64](ctx, tx, ctxt, sb.String(), scopeID, base+"%")
	if err != nil {
		return "", err
	}
	return helper.Sprintf("%s%04d", base, count+1), nil
}

// nullID turns the zero UUID into SQL NULL.
func nullID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil() {
		return nil
	}
	return &id
}

// conditions collects the terms of a WHERE clause and their parameters.
type conditions struct {
	terms  []string
	params []any
}

// add appends a term; every %d in it is replaced by the number of the parameter that carries value.
func (q *conditions) add(term string, value any) {
	q.params = append(q.params, value)
	n := len(q.params)
	q.terms = append(q.terms, strings.ReplaceAll(term, "%d", strconv.Itoa(n)))
}

// raw appends a term without a parameter.
func (q *conditions) raw(term string) {
	q.terms = append(q.terms, term)
}

// where renders the clause, or "" when there is no term.
func (q *conditions) where() string {
	if len(q.terms) == 0 {
		return ""
	}
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString("WHERE ")
	_, _ = sb.WriteString(strings.Join(q.terms, " AND "))
	return sb.String()
}

// orderBy renders an ORDER BY clause from the sortings a client asked for. Each field is looked up in
// expressions, so request text never reaches the SQL; without sortings the fallback is used.
func orderBy(sortings []models.Sorting, expressions map[string]string, fallback string) string {
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(" ORDER BY ")
	if len(sortings) == 0 {
		_, _ = sb.WriteString(fallback)
	}
	for i, item := range sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", expressions[item.Field], item.Direction, item.Direction)
	}
	return sb.String()
}
