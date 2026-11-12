package services

import (
	"context"
	"errors"
	"log/slog"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type NotificationService interface {
	// Notify stores a notification; with a nil tx it is written directly.
	Notify(ctx context.Context, tx pgx.Tx, request *models.Notification) error
	FindNotifications(ctx context.Context, request *models.NotificationFilter) ([]*models.Notification, int64, error)
	FindNotification(ctx context.Context, storeID, notificationID uuid.UUID) (*models.Notification, error)
	CountUnread(ctx context.Context, storeID, branchID uuid.UUID) (int64, error)
	// MarkRead marks a notification as read and returns it.
	MarkRead(ctx context.Context, storeID, notificationID uuid.UUID, at time.Time) (*models.Notification, error)
	MarkAllRead(ctx context.Context, storeID, branchID uuid.UUID, at time.Time) (int64, error)
}

type notificationService struct {
	notificationRepository repositories.NotificationRepository
}

func NewNotificationService(
	notificationRepository repositories.NotificationRepository,
) NotificationService {
	return &notificationService{
		notificationRepository: notificationRepository,
	}
}

func (q *notificationService) Notify(ctx context.Context, tx pgx.Tx, request *models.Notification) error {
	ctxt := "NotificationService-Notify"
	err := q.notificationRepository.CreateNotification(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateNotification")
	}
	return err
}

func (q *notificationService) FindNotifications(ctx context.Context, request *models.NotificationFilter) ([]*models.Notification, int64, error) {
	ctxt := "NotificationService-FindNotifications"
	response, total, err := q.notificationRepository.FindNotifications(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindNotifications")
	}
	return response, total, err
}

func (q *notificationService) FindNotification(ctx context.Context, storeID, notificationID uuid.UUID) (*models.Notification, error) {
	ctxt := "NotificationService-FindNotification"
	response, err := q.notificationRepository.FindNotification(ctx, storeID, notificationID)
	if err != nil && !errors.Is(err, models.ErrNotificationNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindNotification")
	}
	return response, err
}

func (q *notificationService) CountUnread(ctx context.Context, storeID, branchID uuid.UUID) (int64, error) {
	ctxt := "NotificationService-CountUnread"
	response, err := q.notificationRepository.CountUnread(ctx, storeID, branchID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCountUnread")
	}
	return response, err
}

func (q *notificationService) MarkRead(ctx context.Context, storeID, notificationID uuid.UUID, at time.Time) (*models.Notification, error) {
	ctxt := "NotificationService-MarkRead"
	if err := q.notificationRepository.MarkRead(ctx, storeID, notificationID, at); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarkRead")
		return nil, err
	}
	return q.FindNotification(ctx, storeID, notificationID)
}

func (q *notificationService) MarkAllRead(ctx context.Context, storeID, branchID uuid.UUID, at time.Time) (int64, error) {
	ctxt := "NotificationService-MarkAllRead"
	response, err := q.notificationRepository.MarkAllRead(ctx, storeID, branchID, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarkAllRead")
	}
	return response, err
}
