package repositories

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

// JobRepository stores background jobs. Its writes run outside request transactions: a job outlives the
// request that queued it.
type JobRepository interface {
	CreateJob(
		ctx context.Context,
		request *models.Job,
	) (uuid.UUID, error)
	StartJob(
		ctx context.Context,
		jobID uuid.UUID,
	) error
	// FinishJob stores the result (status completed) or, when failure is not empty, the error (status failed).
	FinishJob(
		ctx context.Context,
		jobID uuid.UUID,
		result json.RawMessage,
		failure string,
		finishedAt time.Time,
	) error
}

type jobRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewJobRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) JobRepository {
	return &jobRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

func (q *jobRepository) CreateJob(
	ctx context.Context,
	request *models.Job,
) (uuid.UUID, error) {
	ctxt := "JobRepository-CreateJob"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO jobs (
		store_id
		, kind
		, status
		, payload
		, created_at
	) VALUES ($1, $2, $3, $4::jsonb, $5)
	RETURNING id`)
	var id uuid.UUID
	err := q.dbWrite.QueryRow(
		ctx,
		sb.String(),
		request.StoreID,
		request.Kind,
		request.Status,
		request.Payload,
		request.CreatedAt,
	).Scan(&id)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
	}
	return id, err
}

func (q *jobRepository) StartJob(
	ctx context.Context,
	jobID uuid.UUID,
) error {
	ctxt := "JobRepository-StartJob"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString("UPDATE jobs SET status = $2 WHERE id = $1")
	_, err := q.dbWrite.Exec(ctx, sb.String(), jobID, models.JobStatusRunning)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
	}
	return err
}

func (q *jobRepository) FinishJob(
	ctx context.Context,
	jobID uuid.UUID,
	result json.RawMessage,
	failure string,
	finishedAt time.Time,
) error {
	ctxt := "JobRepository-FinishJob"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	params := []any{jobID, finishedAt}
	_, _ = sb.WriteString("UPDATE jobs SET finished_at = $2, status = $3")
	if failure != "" {
		params = append(params, models.JobStatusFailed, failure)
		_, _ = sb.WriteString(", error = $4")
	} else {
		params = append(params, models.JobStatusCompleted, result)
		_, _ = sb.WriteString(", result = $4::jsonb")
	}
	_, _ = sb.WriteString(" WHERE id = $1")
	_, err := q.dbWrite.Exec(ctx, sb.String(), params...)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
	}
	return err
}
