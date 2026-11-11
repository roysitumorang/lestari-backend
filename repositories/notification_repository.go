package repositories

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

type NotificationRepository interface {
	CreateNotification(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Notification,
	) error
}

type notificationRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewNotificationRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) NotificationRepository {
	return &notificationRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

func (q *notificationRepository) CreateNotification(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Notification,
) error {
	ctxt := "NotificationRepository-CreateNotification"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO notifications (
		store_id
		, branch_id
		, source
		, title
		, message
		, params
		, link_route
		, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8)`)
	_, err := tx.Exec(
		ctx,
		sb.String(),
		request.StoreID,
		request.BranchID,
		request.Source,
		request.Title.Key,
		request.Message.Key,
		i18n.Params(request.Message.Args...),
		request.LinkRoute,
		request.CreatedAt,
	)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
	}
	return err
}
