package models

import (
	"time"
	"uuid"

	"github.com/goccy/go-json"
)

const (
	JobStatusQueued    = "queued"
	JobStatusRunning   = "running"
	JobStatusCompleted = "completed"
	JobStatusFailed    = "failed"

	JobKindStockRecalculate = "stock_recalculate"
)

// Job is a background task; clients poll GET /jobs/{id} for its status and result.
type Job struct {
	ID         uuid.UUID       `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	StoreID    uuid.UUID       `json:"-"`
	Kind       string          `json:"kind" example:"stock_recalculate"`
	Status     string          `json:"status" example:"queued" enums:"queued,running,completed,failed"`
	Payload    json.RawMessage `json:"payload" swaggertype:"object"`
	Result     json.RawMessage `json:"result" swaggertype:"object"`
	Error      *string         `json:"error"`
	CreatedAt  time.Time       `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	FinishedAt *time.Time      `json:"finished_at" example:"2026-11-08T10:32:04+07:00"`
}

// JobResponse is the data of the endpoints that start a background job.
type JobResponse struct {
	JobID  uuid.UUID `json:"job_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Status string    `json:"status" example:"queued"`
}
