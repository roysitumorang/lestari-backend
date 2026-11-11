// Package models holds the entities, their filters and their errors.
package models

import "strings"

type SortingDirection string

const (
	SortingDirectionAsc  SortingDirection = "ASC"
	SortingDirectionDesc SortingDirection = "DESC"
)

func (q SortingDirection) String() string { return string(q) }

func (q SortingDirection) Equal(v string) bool { return strings.EqualFold(q.String(), v) }

// Sorting is one ORDER BY term; Field is always a name from the entity's sorting map, never request text.
type Sorting struct {
	Field     string
	Direction SortingDirection
}

const (
	SortingFieldID        = "id"
	SortingFieldCreatedAt = "created_at"
	SortingFieldUpdatedAt = "updated_at"

	// DefaultLimit is the page size when the request names none; MaxLimit caps per_page.
	DefaultLimit int64 = 25
	MaxLimit     int64 = 1000
)
