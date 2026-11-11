package services

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	"github.com/goccy/go-json"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// jobTimeout is how long a background job may run.
const jobTimeout = 30 * time.Minute

type JobService interface {
	// StartJob records a job and runs task in the background; the task's result or error is stored on the job.
	StartJob(
		ctx context.Context,
		request *models.Job,
		task func(ctx context.Context) (any, error),
	) (*models.JobResponse, error)
}

type jobService struct {
	jobRepository repositories.JobRepository
}

func NewJobService(
	jobRepository repositories.JobRepository,
) JobService {
	return &jobService{
		jobRepository: jobRepository,
	}
}

func (q *jobService) StartJob(
	ctx context.Context,
	request *models.Job,
	task func(ctx context.Context) (any, error),
) (*models.JobResponse, error) {
	ctxt := "JobService-StartJob"
	request.Status = models.JobStatusQueued
	if request.Payload == nil {
		request.Payload = json.RawMessage("{}")
	}
	jobID, err := q.jobRepository.CreateJob(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateJob")
		return nil, err
	}
	// the job outlives the request, so it gets its own context
	go q.run(context.WithoutCancel(ctx), jobID, task)
	return &models.JobResponse{JobID: jobID, Status: models.JobStatusQueued}, nil
}

func (q *jobService) run(parent context.Context, jobID uuid.UUID, task func(ctx context.Context) (any, error)) {
	ctxt := "JobService-run"
	ctx, cancel := context.WithTimeout(parent, jobTimeout)
	defer cancel()
	if err := q.jobRepository.StartJob(ctx, jobID); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrStartJob")
	}
	var (
		result  json.RawMessage
		failure string
	)
	response, err := task(ctx)
	if err == nil {
		result, err = json.Marshal(response)
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrTask")
		failure = err.Error()
	}
	if err = q.jobRepository.FinishJob(ctx, jobID, result, failure, time.Now()); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFinishJob")
	}
}
