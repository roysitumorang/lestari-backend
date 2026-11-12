package models

import (
	"errors"
	"time"
	"uuid"

	"github.com/goccy/go-json"

	"github.com/roysitumorang/lestari-backend/i18n"
)

const NotificationSourceBackOffice = "back_office"

var ErrNotificationNotFound = errors.New("notification not found")

// Notification is an in-app message for a store, or for one branch of it. Title and message are stored as
// English text with their parameters and shown in the reader's language.
type Notification struct {
	ID       uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	StoreID  uuid.UUID  `json:"-"`
	BranchID *uuid.UUID `json:"-"`
	Title    string     `json:"title" example:"Transfer accepted"`
	Message  string     `json:"message" example:"Transfer TRF/20261108/0001 was received."`
	// TitleText and MessageText are the texts to store (new notifications).
	TitleText     i18n.Text       `json:"-" db:"-"`
	MessageText   i18n.Text       `json:"-" db:"-"`
	Params        json.RawMessage `json:"-"`
	Source        string          `json:"source" example:"back_office"`
	LinkRoute     *string         `json:"link_route" example:"report-transfers"`
	AttachmentURL *string         `json:"attachment_url" example:"/uploads/promo.pdf"`
	ReadAt        *time.Time      `json:"read_at" example:"2026-11-08T11:00:00+07:00"`
	CreatedAt     time.Time       `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
}

// NewNotification builds a back-office notification for a branch (nil: the whole store).
func NewNotification(storeID uuid.UUID, branchID *uuid.UUID, title, message i18n.Text, linkRoute string, at time.Time) *Notification {
	response := &Notification{
		StoreID:     storeID,
		BranchID:    branchID,
		Source:      NotificationSourceBackOffice,
		TitleText:   title,
		MessageText: message,
		CreatedAt:   at,
	}
	if linkRoute != "" {
		response.LinkRoute = &linkRoute
	}
	return response
}

// Localize translates the stored title and message into a language.
func (q *Notification) Localize(l i18n.Lang) *Notification {
	args := i18n.Restore(decodeParams(q.Params))
	q.Title = l.T(q.Title, args...)
	q.Message = l.T(q.Message, args...)
	return q
}

// NotificationFilter selects the notifications a branch sees: its own and those of the whole store.
type NotificationFilter struct {
	StoreID,
	BranchID uuid.UUID
	Source string
	// StartDate and EndDate are YYYY-MM-DD or empty.
	StartDate,
	EndDate string
	Limit,
	Page int64
}

// CountResponse is the data of the endpoints that count.
type CountResponse struct {
	Count int64 `json:"count" example:"3"`
}
