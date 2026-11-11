package models

import (
	"time"
	"uuid"

	"github.com/roysitumorang/lestari-backend/i18n"
)

const NotificationSourceBackOffice = "back_office"

// Notification is an in-app message for a store, or for one branch of it. Title and message are stored as
// English text with their parameters and shown in the reader's language.
type Notification struct {
	StoreID   uuid.UUID
	BranchID  *uuid.UUID
	Source    string
	Title     i18n.Text
	Message   i18n.Text
	LinkRoute string
	CreatedAt time.Time
}
