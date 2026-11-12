package services

import (
	"context"
	"log/slog"

	"github.com/goccy/go-json"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

const jobKindResetData = "reset_data"

type DataResetService interface {
	// ResetData deletes the chosen data of a branch in a background job and notifies the branch when it is done.
	ResetData(ctx context.Context, request *models.DataReset) (*models.JobResponse, error)
}

type dataResetService struct {
	storeRepository        repositories.StoreRepository
	notificationRepository repositories.NotificationRepository
	jobService             JobService
}

func NewDataResetService(
	storeRepository repositories.StoreRepository,
	notificationRepository repositories.NotificationRepository,
	jobService JobService,
) DataResetService {
	return &dataResetService{
		storeRepository:        storeRepository,
		notificationRepository: notificationRepository,
		jobService:             jobService,
	}
}

func (q *dataResetService) ResetData(ctx context.Context, request *models.DataReset) (*models.JobResponse, error) {
	ctxt := "DataResetService-ResetData"
	payload, err := json.Marshal(map[string]any{"data_types": request.DataTypes, "start_date": request.StartDate, "end_date": request.EndDate})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarshal")
		return nil, err
	}
	response, err := q.jobService.StartJob(
		ctx,
		&models.Job{StoreID: request.StoreID, Kind: jobKindResetData, Payload: payload, CreatedAt: request.At},
		func(ctx context.Context) (any, error) {
			// a background job has no controller, so the transaction is opened here
			tx, err := repositories.BeginTx(ctx)
			if err != nil {
				return nil, err
			}
			defer func() { _ = tx.Rollback(ctx) }()
			deleted, err := q.storeRepository.ResetData(ctx, tx, request)
			if err != nil {
				return nil, err
			}
			if err = q.notificationRepository.CreateNotification(ctx, tx, models.NewNotification(request.StoreID, &request.BranchID,
				i18n.Tr("Data reset finished"), i18n.Tr("The data you selected has been deleted."), "", request.At)); err != nil {
				return nil, err
			}
			return deleted, tx.Commit(ctx)
		},
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrStartJob")
	}
	return response, err
}
