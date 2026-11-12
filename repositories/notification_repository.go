package repositories

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

type NotificationRepository interface {
	// CreateNotification stores a notification; with a nil tx it is written directly.
	CreateNotification(ctx context.Context, tx pgx.Tx, request *models.Notification) error
	FindNotifications(ctx context.Context, request *models.NotificationFilter) ([]*models.Notification, int64, error)
	FindNotification(ctx context.Context, storeID, notificationID uuid.UUID) (*models.Notification, error)
	CountUnread(ctx context.Context, storeID, branchID uuid.UUID) (int64, error)
	// MarkRead marks one notification as read; one that was read keeps its time.
	MarkRead(ctx context.Context, storeID, notificationID uuid.UUID, readAt time.Time) error
	// MarkAllRead marks what the branch sees as read and returns how many were unread.
	MarkAllRead(ctx context.Context, storeID, branchID uuid.UUID, readAt time.Time) (int64, error)
}

type notificationRepository struct {
	base
}

func NewNotificationRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) NotificationRepository {
	return &notificationRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const notificationColumns = `t.id
	, t.title
	, t.message
	, t.params
	, t.source
	, t.link_route
	, t.attachment_url
	, t.read_at
	, t.created_at`

func (q *notificationRepository) CreateNotification(ctx context.Context, tx pgx.Tx, request *models.Notification) error {
	ctxt := "NotificationRepository-CreateNotification"
	_, err := execute(ctx, q.writer(tx), ctxt, `INSERT INTO notifications (
		store_id
		, branch_id
		, source
		, title
		, message
		, params
		, link_route
		, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		request.StoreID,
		request.BranchID,
		request.Source,
		request.TitleText.Key,
		request.MessageText.Key,
		i18n.Params(request.MessageText.Args...),
		request.LinkRoute,
		request.CreatedAt,
	)
	return err
}

func (q *notificationRepository) FindNotifications(ctx context.Context, request *models.NotificationFilter) ([]*models.Notification, int64, error) {
	ctxt := "NotificationRepository-FindNotifications"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	params := []any{request.StoreID, request.BranchID}
	_, _ = sb.WriteString("WHERE t.store_id = $1 AND (t.branch_id IS NULL OR t.branch_id = $2)")
	for _, filter := range []struct{ value, condition string }{
		{request.Source, " AND t.source = $%d"},
		{request.StartDate, " AND t.created_at::date >= $%d::date"},
		{request.EndDate, " AND t.created_at::date <= $%d::date"},
	} {
		if filter.value != "" {
			params = append(params, filter.value)
			_, _ = sb.WriteString(sprintf(filter.condition, len(params)))
		}
	}
	condition := sb.String()
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1) FROM notifications t "+condition, params...)
	if err != nil || total == 0 {
		return []*models.Notification{}, 0, err
	}
	response, err := queryRows[models.Notification](ctx, q.dbRead, ctxt,
		"SELECT "+notificationColumns+" FROM notifications t "+condition+" ORDER BY t.created_at DESC"+paginate(request.Limit, request.Page), params...)
	return response, total, err
}

func (q *notificationRepository) FindNotification(ctx context.Context, storeID, notificationID uuid.UUID) (*models.Notification, error) {
	ctxt := "NotificationRepository-FindNotification"
	response, err := queryRow[models.Notification](ctx, q.dbWrite, ctxt,
		"SELECT "+notificationColumns+" FROM notifications t WHERE t.id = $1 AND t.store_id = $2", notificationID, storeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotificationNotFound
	}
	return response, err
}

func (q *notificationRepository) CountUnread(ctx context.Context, storeID, branchID uuid.UUID) (int64, error) {
	ctxt := "NotificationRepository-CountUnread"
	return queryValue[int64](ctx, q.dbRead, ctxt, `SELECT COUNT(1)
	FROM notifications t
	WHERE t.store_id = $1
		AND (t.branch_id IS NULL OR t.branch_id = $2)
		AND t.read_at IS NULL`, storeID, branchID)
}

func (q *notificationRepository) MarkRead(ctx context.Context, storeID, notificationID uuid.UUID, readAt time.Time) error {
	ctxt := "NotificationRepository-MarkRead"
	_, err := execute(ctx, q.dbWrite, ctxt, `UPDATE notifications SET
		read_at = $3
	WHERE id = $1
		AND store_id = $2
		AND read_at IS NULL`, notificationID, storeID, readAt)
	return err
}

func (q *notificationRepository) MarkAllRead(ctx context.Context, storeID, branchID uuid.UUID, readAt time.Time) (int64, error) {
	ctxt := "NotificationRepository-MarkAllRead"
	return execute(ctx, q.dbWrite, ctxt, `UPDATE notifications SET
		read_at = $3
	WHERE store_id = $1
		AND (branch_id IS NULL OR branch_id = $2)
		AND read_at IS NULL`, storeID, branchID, readAt)
}
