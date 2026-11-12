package services

import (
	"context"
	"log/slog"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type ActivityService interface {
	// Record writes an entry of the activity log. A failure is logged, never returned: the action it describes
	// has already happened.
	Record(ctx context.Context, request *models.Activity)
	FindActivities(ctx context.Context, request *models.ActivityFilter) ([]*models.Activity, int64, error)
}

type activityService struct {
	activityRepository repositories.ActivityRepository
}

func NewActivityService(
	activityRepository repositories.ActivityRepository,
) ActivityService {
	return &activityService{
		activityRepository: activityRepository,
	}
}

func (q *activityService) Record(ctx context.Context, request *models.Activity) {
	ctxt := "ActivityService-Record"
	// the request may be over (and its context cancelled) by the time this is written
	if err := q.activityRepository.CreateActivity(context.WithoutCancel(ctx), request); err != nil {
		helper.Log(ctx, slog.LevelWarn, err.Error(), ctxt, "ErrCreateActivity")
	}
}

func (q *activityService) FindActivities(ctx context.Context, request *models.ActivityFilter) ([]*models.Activity, int64, error) {
	ctxt := "ActivityService-FindActivities"
	response, total, err := q.activityRepository.FindActivities(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindActivities")
	}
	return response, total, err
}
