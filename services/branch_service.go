package services

import (
	"context"
	"errors"
	"log/slog"
	"time"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

type BranchService interface {
	FindBranches(ctx context.Context, request *models.BranchFilter) ([]*models.Branch, int64, error)
	FindBranch(ctx context.Context, tx pgx.Tx, request *models.BranchFilter) (*models.Branch, error)
	// CreateBranch creates a branch with its login (an administrator account) and the default data of a branch.
	CreateBranch(ctx context.Context, tx pgx.Tx, request *models.Branch) (*models.Branch, error)
	// UpdateBranch saves a branch; a changed email or a new password also changes its login.
	UpdateBranch(ctx context.Context, tx pgx.Tx, request *models.Branch) (*models.Branch, error)
	// DeleteBranch deletes a branch with its users; the head office cannot be deleted.
	DeleteBranch(ctx context.Context, tx pgx.Tx, storeID, branchID uuid.UUID, at time.Time) error
	// CopyProducts copies products of the head office to a branch in a background job.
	CopyProducts(ctx context.Context, request *models.ProductCopy) (*models.JobResponse, error)
	FindBranchBillings(ctx context.Context, storeID uuid.UUID, at time.Time, limit, page int64) ([]*models.BranchBilling, int64, error)
	// CountBranches counts the branches of a store among branchIDs.
	CountBranches(ctx context.Context, storeID uuid.UUID, branchIDs []uuid.UUID) (int64, error)
}

type branchService struct {
	branchRepository       repositories.BranchRepository
	storeRepository        repositories.StoreRepository
	notificationRepository repositories.NotificationRepository
	jobService             JobService
}

func NewBranchService(
	branchRepository repositories.BranchRepository,
	storeRepository repositories.StoreRepository,
	notificationRepository repositories.NotificationRepository,
	jobService JobService,
) BranchService {
	return &branchService{
		branchRepository:       branchRepository,
		storeRepository:        storeRepository,
		notificationRepository: notificationRepository,
		jobService:             jobService,
	}
}

func (q *branchService) FindBranches(ctx context.Context, request *models.BranchFilter) ([]*models.Branch, int64, error) {
	ctxt := "BranchService-FindBranches"
	response, total, err := q.branchRepository.FindBranches(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranches")
	}
	return response, total, err
}

func (q *branchService) FindBranch(ctx context.Context, tx pgx.Tx, request *models.BranchFilter) (*models.Branch, error) {
	ctxt := "BranchService-FindBranch"
	response, err := q.branchRepository.FindBranch(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrBranchNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranch")
	}
	return response, err
}

func (q *branchService) CreateBranch(ctx context.Context, tx pgx.Tx, request *models.Branch) (*models.Branch, error) {
	ctxt := "BranchService-CreateBranch"
	if len(request.Password) < 8 {
		return nil, helper.Invalid("password", "Set a password of at least 8 characters for the branch login.")
	}
	passwordHash, err := middlewares.HashPassword(request.Password)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrHashPassword")
		return nil, err
	}
	response, err := q.branchRepository.CreateBranch(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateBranch")
		return nil, err
	}
	if _, err = q.storeRepository.CreateAccount(ctx, tx, request.StoreID, response.ID, "Branch "+request.Name, deref(request.Email), passwordHash, "", models.RoleAdministrator, request.CreatedAt); err != nil {
		if errors.Is(err, models.ErrEmailTaken) {
			return nil, helper.Invalid("email", "This email is already registered.")
		}
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateAccount")
		return nil, err
	}
	if err = q.storeRepository.SeedBranch(ctx, tx, response.ID, request.CreatedAt); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSeedBranch")
		return nil, err
	}
	return response, nil
}

func (q *branchService) UpdateBranch(ctx context.Context, tx pgx.Tx, request *models.Branch) (*models.Branch, error) {
	ctxt := "BranchService-UpdateBranch"
	var passwordHash string
	if request.Password != "" {
		if len(request.Password) < 8 {
			return nil, helper.Invalid("password", "Use at least 8 characters.")
		}
		var err error
		if passwordHash, err = middlewares.HashPassword(request.Password); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrHashPassword")
			return nil, err
		}
	}
	response, err := q.branchRepository.UpdateBranch(ctx, tx, request)
	if err != nil {
		if !errors.Is(err, models.ErrBranchNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateBranch")
		}
		return nil, err
	}
	if err = q.branchRepository.UpdateBranchLogin(ctx, tx, request.ID, deref(request.Email), passwordHash); err != nil {
		if errors.Is(err, models.ErrEmailTaken) {
			return nil, helper.Invalid("email", "This email is already registered.")
		}
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateBranchLogin")
		return nil, err
	}
	return response, nil
}

func (q *branchService) DeleteBranch(ctx context.Context, tx pgx.Tx, storeID, branchID uuid.UUID, at time.Time) error {
	ctxt := "BranchService-DeleteBranch"
	branch, err := q.FindBranch(ctx, tx, models.NewBranchFilter().WithStoreIDs(storeID).WithBranchIDs(branchID))
	if err != nil {
		return err
	}
	if branch.IsHeadOffice {
		return helper.Conflict("The head office cannot be deleted.")
	}
	if err = q.branchRepository.DeleteBranch(ctx, tx, branchID, at); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteBranch")
	}
	return err
}

func (q *branchService) CopyProducts(ctx context.Context, request *models.ProductCopy) (*models.JobResponse, error) {
	ctxt := "BranchService-CopyProducts"
	branch, err := q.FindBranch(ctx, nil, models.NewBranchFilter().WithStoreIDs(request.StoreID).WithBranchIDs(request.BranchID))
	if err != nil {
		return nil, err
	}
	if branch.IsHeadOffice {
		return nil, helper.Conflict("Products are copied from the head office to other branches.")
	}
	payload, err := json.Marshal(map[string]any{"category_ids": request.CategoryIDs, "include_stock": request.IncludeStock})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarshal")
		return nil, err
	}
	response, err := q.jobService.StartJob(
		ctx,
		&models.Job{StoreID: request.StoreID, Kind: models.JobKindCopyProducts, Payload: payload, CreatedAt: request.At},
		func(ctx context.Context) (any, error) {
			// a background job has no controller, so the transaction is opened here
			tx, err := repositories.BeginTx(ctx)
			if err != nil {
				return nil, err
			}
			defer func() { _ = tx.Rollback(ctx) }()
			copied, err := q.branchRepository.CopyProducts(ctx, tx, request)
			if err != nil {
				return nil, err
			}
			if err = q.notificationRepository.CreateNotification(ctx, tx, models.NewNotification(request.StoreID, &request.BranchID,
				i18n.Tr("Products copied"), i18n.Tr("{n} products were copied from the head office.", "n", copied), "branch-products", request.At)); err != nil {
				return nil, err
			}
			return map[string]int64{"copied": copied}, tx.Commit(ctx)
		},
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrStartJob")
	}
	return response, err
}

func (q *branchService) FindBranchBillings(ctx context.Context, storeID uuid.UUID, at time.Time, limit, page int64) ([]*models.BranchBilling, int64, error) {
	ctxt := "BranchService-FindBranchBillings"
	response, total, err := q.branchRepository.FindBranchBillings(ctx, storeID, at, limit, page)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranchBillings")
	}
	return response, total, err
}

func (q *branchService) CountBranches(ctx context.Context, storeID uuid.UUID, branchIDs []uuid.UUID) (int64, error) {
	ctxt := "BranchService-CountBranches"
	response, err := q.branchRepository.CountBranches(ctx, storeID, branchIDs)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCountBranches")
	}
	return response, err
}
