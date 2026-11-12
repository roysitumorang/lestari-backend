package repositories

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/models"
)

type ActivityRepository interface {
	CreateActivity(ctx context.Context, request *models.Activity) error
	FindActivities(ctx context.Context, request *models.ActivityFilter) ([]*models.Activity, int64, error)
}

type activityRepository struct {
	base
}

func NewActivityRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) ActivityRepository {
	return &activityRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

func (q *activityRepository) CreateActivity(ctx context.Context, request *models.Activity) error {
	ctxt := "ActivityRepository-CreateActivity"
	_, err := execute(ctx, q.dbWrite, ctxt, `INSERT INTO activity_logs (
		store_id
		, user_id
		, action
		, description
		, params
		, ip_address
		, user_agent
		, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		request.StoreID,
		request.UserID,
		request.Action,
		request.Text.Key,
		i18n.Params(request.Text.Args...),
		request.IPAddress,
		request.UserAgent,
		request.CreatedAt,
	)
	return err
}

func (q *activityRepository) FindActivities(ctx context.Context, request *models.ActivityFilter) ([]*models.Activity, int64, error) {
	ctxt := "ActivityRepository-FindActivities"
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1) FROM activity_logs t WHERE t.store_id = $1 AND t.user_id = $2", request.StoreID, request.UserID)
	if err != nil || total == 0 {
		return []*models.Activity{}, 0, err
	}
	response, err := queryRows[models.Activity](ctx, q.dbRead, ctxt, `SELECT t.id
		, t.created_at
		, t.action
		, t.description
		, t.params
		, t.ip_address
		, t.user_agent
	FROM activity_logs t
	WHERE t.store_id = $1
		AND t.user_id = $2
	ORDER BY t.created_at DESC`+paginate(request.Limit, request.Page), request.StoreID, request.UserID)
	return response, total, err
}
