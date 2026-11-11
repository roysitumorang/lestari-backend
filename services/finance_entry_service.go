package services

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type FinanceEntryService interface {
	FindFinanceEntries(
		ctx context.Context,
		request *models.FinanceEntryFilter,
	) ([]*models.FinanceEntry, int64, error)
	FindFinanceEntry(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FinanceEntryFilter,
	) (*models.FinanceEntry, error)
	CreateFinanceEntry(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FinanceEntry,
	) (*models.FinanceEntry, error)
	UpdateFinanceEntry(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FinanceEntry,
	) (*models.FinanceEntry, error)
	DeleteFinanceEntries(
		ctx context.Context,
		tx pgx.Tx,
		request *models.FinanceEntryFilter,
	) error
}

type financeEntryService struct {
	financeEntryRepository repositories.FinanceEntryRepository
}

func NewFinanceEntryService(
	financeEntryRepository repositories.FinanceEntryRepository,
) FinanceEntryService {
	return &financeEntryService{
		financeEntryRepository: financeEntryRepository,
	}
}

func (q *financeEntryService) FindFinanceEntries(
	ctx context.Context,
	request *models.FinanceEntryFilter,
) ([]*models.FinanceEntry, int64, error) {
	ctxt := "FinanceEntryService-FindFinanceEntries"
	response, total, err := q.financeEntryRepository.FindFinanceEntries(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFinanceEntries")
	}
	return response, total, err
}

func (q *financeEntryService) FindFinanceEntry(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FinanceEntryFilter,
) (*models.FinanceEntry, error) {
	ctxt := "FinanceEntryService-FindFinanceEntry"
	response, err := q.financeEntryRepository.FindFinanceEntry(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrFinanceEntryNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindFinanceEntry")
	}
	return response, err
}

// checkReferences makes sure every related row belongs to the same branch.
func (q *financeEntryService) checkReferences(ctx context.Context, tx pgx.Tx, request *models.FinanceEntry) error {
	v := &helper.Validation{}
	if request.CashDrawerID != nil {
		exists, err := repositories.ReferenceExists(ctx, tx, "cash_drawers", "branch_id", *request.CashDrawerID, request.BranchID)
		if err != nil {
			return err
		}
		if !exists {
			v.Add("cash_drawer_id", "Not found.")
		}
	}
	return v.Err()
}

func (q *financeEntryService) CreateFinanceEntry(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FinanceEntry,
) (*models.FinanceEntry, error) {
	ctxt := "FinanceEntryService-CreateFinanceEntry"
	if err := q.checkReferences(ctx, tx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCheckReferences")
		return nil, err
	}
	response, err := q.financeEntryRepository.CreateFinanceEntry(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateFinanceEntry")
	}
	return response, err
}

func (q *financeEntryService) UpdateFinanceEntry(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FinanceEntry,
) (*models.FinanceEntry, error) {
	ctxt := "FinanceEntryService-UpdateFinanceEntry"
	if err := q.checkReferences(ctx, tx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCheckReferences")
		return nil, err
	}
	response, err := q.financeEntryRepository.UpdateFinanceEntry(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateFinanceEntry")
	}
	return response, err
}

// DeleteFinanceEntries deletes every entry of the filter or none: a missing id is ErrFinanceEntryNotFound.
func (q *financeEntryService) DeleteFinanceEntries(
	ctx context.Context,
	tx pgx.Tx,
	request *models.FinanceEntryFilter,
) error {
	ctxt := "FinanceEntryService-DeleteFinanceEntries"
	deleted, err := q.financeEntryRepository.DeleteFinanceEntries(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteFinanceEntries")
		return err
	}
	if deleted != int64(len(request.FinanceEntryIDs)) {
		return models.ErrFinanceEntryNotFound
	}
	return nil
}
