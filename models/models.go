// Package models holds the entities, their filters and their errors.
package models

import (
	"errors"
	"strings"
	"uuid"
)

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

// ErrReferenceNotFound is returned when a write points at a row that does not exist (foreign key).
var ErrReferenceNotFound = errors.New("a referenced record does not exist")

// Reference is the {id, name} of a related row, sent next to its foreign key.
type Reference struct {
	ID   uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Name string    `json:"name" example:"Member"`
}

// NewReference builds a Reference when both parts were found (a NULL foreign key has neither).
func NewReference(id *uuid.UUID, name *string) *Reference {
	if id == nil || name == nil {
		return nil
	}
	return &Reference{ID: *id, Name: *name}
}

// Nulls records the request attributes that were sent as null (or empty), which on update means "clear this
// value" — a nil pointer alone only says the attribute was not sent.
type Nulls struct {
	nulls map[string]struct{}
}

func (q *Nulls) SetNull(attribute string) {
	if q.nulls == nil {
		q.nulls = map[string]struct{}{}
	}
	q.nulls[attribute] = struct{}{}
}

func (q *Nulls) IsNull(attribute string) bool {
	_, ok := q.nulls[attribute]
	return ok
}

// ImportResult is the data of an import: how many rows were created, updated and rejected, and why.
type ImportResult struct {
	Created int           `json:"created" example:"12"`
	Updated int           `json:"updated" example:"3"`
	Failed  int           `json:"failed" example:"1"`
	Errors  []ImportError `json:"errors"`
}

type ImportError struct {
	Row     int    `json:"row" example:"5"`
	Message string `json:"message" example:"name: Required"`
}

// DeletedResponse is the data of a bulk delete.
type DeletedResponse struct {
	Deleted int `json:"deleted" example:"3"`
}

// BulkDeleteRequest is the body of the bulk-delete endpoints.
type BulkDeleteRequest struct {
	IDs []string `json:"ids" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a,019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
}
