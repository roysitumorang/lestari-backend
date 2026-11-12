package models

import (
	"time"
	"uuid"

	"github.com/goccy/go-json"

	"github.com/roysitumorang/lestari-backend/i18n"
)

// Activity is one entry of the activity log. The description is stored as English text with its parameters and
// shown in the reader's language.
type Activity struct {
	ID          uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	StoreID     uuid.UUID `json:"-"`
	UserID      uuid.UUID `json:"-"`
	CreatedAt   time.Time `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	Action      string    `json:"action" example:"settings_updated"`
	Description string    `json:"description" example:"Updated store settings"`
	// Text is the description to store (new entries).
	Text      i18n.Text       `json:"-" db:"-"`
	Params    json.RawMessage `json:"-"`
	IPAddress string          `json:"ip_address" example:"203.0.113.7"`
	UserAgent string          `json:"user_agent" example:"Mozilla/5.0"`
}

// Localize translates the stored description into a language.
func (q *Activity) Localize(l i18n.Lang) *Activity {
	q.Description = l.T(q.Description, i18n.Restore(decodeParams(q.Params))...)
	return q
}

func decodeParams(raw json.RawMessage) map[string]any {
	params := map[string]any{}
	_ = json.Unmarshal(raw, &params)
	return params
}

// ActivityFilter selects the activity of one user.
type ActivityFilter struct {
	StoreID,
	UserID uuid.UUID
	Limit,
	Page int64
}
