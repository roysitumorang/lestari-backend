package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// lateAfterMinutes is the grace period of a shift start.
const lateAfterMinutes = 10

type StaffService interface {
	FindStaffs(ctx context.Context, request *models.StaffFilter) ([]*models.Staff, int64, error)
	FindStaff(ctx context.Context, tx pgx.Tx, request *models.StaffFilter) (*models.Staff, error)
	CreateStaff(ctx context.Context, tx pgx.Tx, request *models.Staff) (*models.Staff, error)
	// UpdateStaff saves a staff member; a new role can reset the access rights to the defaults of that role.
	UpdateStaff(ctx context.Context, tx pgx.Tx, request *models.Staff) (*models.Staff, error)
	// DeleteStaffs deletes every staff member of the list or none: a missing id is ErrStaffNotFound.
	DeleteStaffs(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, staffIDs []uuid.UUID, at time.Time) error
	EstimateBilling(ctx context.Context, storeID uuid.UUID, additionalStaff int64) (*models.StaffBillingEstimate, error)
	FindPermissions(ctx context.Context, storeID, staffID uuid.UUID) (*models.StaffPermissions, error)
	UpdatePermissions(ctx context.Context, tx pgx.Tx, storeID, staffID uuid.UUID, permissions []string, at time.Time) error
	// ResendInvitation sends the invitation again to a staff member who has not signed in yet.
	ResendInvitation(ctx context.Context, storeID, staffID uuid.UUID, message func(name string) string) error
	FindAttendances(ctx context.Context, request *models.AttendanceFilter) ([]*models.Attendance, *models.AttendanceSummary, error)
	FindAttendance(ctx context.Context, branchID, attendanceID uuid.UUID) (*models.Attendance, error)
	ClockIn(ctx context.Context, tx pgx.Tx, branchID, staffID uuid.UUID, request *models.ClockInRequest, at time.Time) (*models.Clocking, error)
	ClockOut(ctx context.Context, tx pgx.Tx, branchID, staffID uuid.UUID, photoURL string, at time.Time) (*models.Clocking, error)
	FindHelpAccesses(ctx context.Context, request *models.HelpAccessFilter) ([]*models.HelpAccess, int64, error)
	FindHelpAccess(ctx context.Context, tx pgx.Tx, request *models.HelpAccessFilter) (*models.HelpAccess, error)
	CreateHelpAccess(ctx context.Context, tx pgx.Tx, request *models.HelpAccess) (*models.HelpAccess, error)
	RevokeHelpAccess(ctx context.Context, tx pgx.Tx, storeID, helpAccessID uuid.UUID, at time.Time) (*models.HelpAccess, error)
	FindAccountOptions(ctx context.Context, storeID uuid.UUID) ([]*models.AccountOption, error)
}

type staffService struct {
	staffRepository repositories.StaffRepository
	sender          Sender
}

func NewStaffService(
	staffRepository repositories.StaffRepository,
	sender Sender,
) StaffService {
	return &staffService{
		staffRepository: staffRepository,
		sender:          sender,
	}
}

func (q *staffService) FindStaffs(ctx context.Context, request *models.StaffFilter) ([]*models.Staff, int64, error) {
	ctxt := "StaffService-FindStaffs"
	response, total, err := q.staffRepository.FindStaffs(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStaffs")
	}
	return response, total, err
}

func (q *staffService) FindStaff(ctx context.Context, tx pgx.Tx, request *models.StaffFilter) (*models.Staff, error) {
	ctxt := "StaffService-FindStaff"
	response, err := q.staffRepository.FindStaff(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrStaffNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStaff")
	}
	return response, err
}

// validate checks the rules of a staff member and hashes the secrets that were sent.
func (q *staffService) validate(ctx context.Context, tx pgx.Tx, request *models.Staff) (passwordHash, pinHash string, err error) {
	ctxt := "StaffService-validate"
	v := &helper.Validation{}
	if !strings.Contains(request.Email, "@") {
		v.Add("email", "Enter a valid email.")
	}
	if request.PPOBPin != "" && (len(request.PPOBPin) != 6 || strings.Trim(request.PPOBPin, "0123456789") != "") {
		v.Add("ppob_pin", "The PIN must be 6 digits.")
	}
	if request.BranchID != nil {
		exists, err := repositories.ReferenceExists(ctx, tx, "branches", "store_id", *request.BranchID, request.StoreID)
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrReferenceExists")
			return "", "", err
		}
		if !exists {
			v.Add("branch_id", "Not found.")
		}
	}
	if err = v.Err(); err != nil {
		return "", "", err
	}
	if request.Password != "" {
		if passwordHash, err = middlewares.HashPassword(request.Password); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrHashPassword")
			return "", "", err
		}
	}
	if request.PPOBPin != "" {
		if pinHash, err = middlewares.HashPassword(request.PPOBPin); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrHashPassword")
		}
	}
	return passwordHash, pinHash, err
}

// staffError names the write errors of a staff member.
func staffError(err error) error {
	if errors.Is(err, models.ErrEmailTaken) {
		return helper.Invalid("email", "This email is already registered.")
	}
	return err
}

func (q *staffService) CreateStaff(ctx context.Context, tx pgx.Tx, request *models.Staff) (*models.Staff, error) {
	ctxt := "StaffService-CreateStaff"
	passwordHash, pinHash, err := q.validate(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	response, err := q.staffRepository.CreateStaff(ctx, tx, request, passwordHash, optional(pinHash))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStaff")
	}
	return response, staffError(err)
}

func (q *staffService) UpdateStaff(ctx context.Context, tx pgx.Tx, request *models.Staff) (*models.Staff, error) {
	ctxt := "StaffService-UpdateStaff"
	passwordHash, pinHash, err := q.validate(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	response, err := q.staffRepository.UpdateStaff(ctx, tx, request, passwordHash, pinHash)
	if err != nil {
		if !errors.Is(err, models.ErrStaffNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateStaff")
		}
		return nil, staffError(err)
	}
	if request.RoleChanged {
		// a staff member without access rights gets those of the role; with reset_permissions everyone does
		if err = q.staffRepository.UpdatePermissions(ctx, tx, request.StoreID, request.ID, models.DefaultPermissions[request.Role], !request.ResetPermissions, request.UpdatedAt); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePermissions")
			return nil, err
		}
	}
	return response, nil
}

func (q *staffService) DeleteStaffs(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, staffIDs []uuid.UUID, at time.Time) error {
	ctxt := "StaffService-DeleteStaffs"
	deleted, err := q.staffRepository.DeleteStaffs(ctx, tx, storeID, staffIDs, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteStaffs")
		return err
	}
	if deleted != int64(len(staffIDs)) {
		return models.ErrStaffNotFound
	}
	return nil
}

func (q *staffService) EstimateBilling(ctx context.Context, storeID uuid.UUID, additionalStaff int64) (*models.StaffBillingEstimate, error) {
	ctxt := "StaffService-EstimateBilling"
	current, err := q.staffRepository.CountStaffs(ctx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCountStaffs")
		return nil, err
	}
	return &models.StaffBillingEstimate{
		MonthlyCost:   float64(max(current+additionalStaff-models.IncludedStaff, 0)) * models.StaffMonthly,
		Currency:      "IDR",
		IncludedStaff: models.IncludedStaff,
		CurrentStaff:  current,
	}, nil
}

func (q *staffService) FindPermissions(ctx context.Context, storeID, staffID uuid.UUID) (*models.StaffPermissions, error) {
	ctxt := "StaffService-FindPermissions"
	permissions, err := q.staffRepository.FindPermissions(ctx, nil, storeID, staffID)
	if err != nil {
		if !errors.Is(err, models.ErrStaffNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPermissions")
		}
		return nil, err
	}
	response := &models.StaffPermissions{Permissions: permissions, Available: make([]models.PermissionOption, len(models.Permissions))}
	for i, permission := range models.Permissions {
		response.Available[i] = models.PermissionOption{Value: permission, Label: strings.ReplaceAll(permission, "_", " ")}
	}
	return response, nil
}

func (q *staffService) UpdatePermissions(ctx context.Context, tx pgx.Tx, storeID, staffID uuid.UUID, permissions []string, at time.Time) error {
	ctxt := "StaffService-UpdatePermissions"
	// the lookup tells an unknown staff member from one whose access rights stay as they are
	if _, err := q.staffRepository.FindPermissions(ctx, tx, storeID, staffID); err != nil {
		if !errors.Is(err, models.ErrStaffNotFound) {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPermissions")
		}
		return err
	}
	err := q.staffRepository.UpdatePermissions(ctx, tx, storeID, staffID, permissions, false, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePermissions")
	}
	return err
}

func (q *staffService) ResendInvitation(ctx context.Context, storeID, staffID uuid.UUID, message func(name string) string) error {
	ctxt := "StaffService-ResendInvitation"
	staff, err := q.FindStaff(ctx, nil, models.NewStaffFilter().WithStoreIDs(storeID).WithStaffIDs(staffID))
	if err != nil {
		return err
	}
	if staff.Status != models.UserStatusInvited {
		return helper.Conflict("This staff member has already signed in.")
	}
	if err = q.sender.Send(ctx, "email", staff.Email, message(staff.Name)); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSend")
	}
	return err
}

func (q *staffService) FindAttendances(ctx context.Context, request *models.AttendanceFilter) ([]*models.Attendance, *models.AttendanceSummary, error) {
	ctxt := "StaffService-FindAttendances"
	response, summary, err := q.staffRepository.FindAttendances(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindAttendances")
	}
	return response, summary, err
}

func (q *staffService) FindAttendance(ctx context.Context, branchID, attendanceID uuid.UUID) (*models.Attendance, error) {
	ctxt := "StaffService-FindAttendance"
	response, err := q.staffRepository.FindAttendance(ctx, branchID, attendanceID)
	if err != nil && !errors.Is(err, models.ErrAttendanceNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindAttendance")
	}
	return response, err
}

func (q *staffService) ClockIn(ctx context.Context, tx pgx.Tx, branchID, staffID uuid.UUID, request *models.ClockInRequest, at time.Time) (*models.Clocking, error) {
	ctxt := "StaffService-ClockIn"
	local := at.In(helper.Location)
	status := "present"
	if shiftStart, err := time.Parse("15:04", request.ShiftStart); err == nil && local.Hour()*60+local.Minute() > shiftStart.Hour()*60+shiftStart.Minute()+lateAfterMinutes {
		status = "late"
	}
	response, err := q.staffRepository.ClockIn(ctx, tx, branchID, staffID, local.Format(time.DateOnly), status, request, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrClockIn")
	}
	return response, err
}

func (q *staffService) ClockOut(ctx context.Context, tx pgx.Tx, branchID, staffID uuid.UUID, photoURL string, at time.Time) (*models.Clocking, error) {
	ctxt := "StaffService-ClockOut"
	response, err := q.staffRepository.ClockOut(ctx, tx, branchID, staffID, photoURL, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrClockOut")
		return nil, err
	}
	if response == nil {
		return nil, helper.Conflict("You have not clocked in.")
	}
	return response, nil
}

func (q *staffService) FindHelpAccesses(ctx context.Context, request *models.HelpAccessFilter) ([]*models.HelpAccess, int64, error) {
	ctxt := "StaffService-FindHelpAccesses"
	response, total, err := q.staffRepository.FindHelpAccesses(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindHelpAccesses")
	}
	return response, total, err
}

func (q *staffService) FindHelpAccess(ctx context.Context, tx pgx.Tx, request *models.HelpAccessFilter) (*models.HelpAccess, error) {
	ctxt := "StaffService-FindHelpAccess"
	response, err := q.staffRepository.FindHelpAccess(ctx, tx, request)
	if err != nil && !errors.Is(err, models.ErrHelpAccessNotFound) {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindHelpAccess")
	}
	return response, err
}

func (q *staffService) CreateHelpAccess(ctx context.Context, tx pgx.Tx, request *models.HelpAccess) (*models.HelpAccess, error) {
	ctxt := "StaffService-CreateHelpAccess"
	exists, err := q.staffRepository.AccountExists(ctx, tx, request.StoreID, request.AccountID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrAccountExists")
		return nil, err
	}
	if !exists {
		return nil, helper.Invalid("account_id", "Not found.")
	}
	request.Status = models.HelpAccessStatusActive
	request.ExpiresAt = request.CreatedAt.Add(time.Duration(request.DurationHours) * time.Hour)
	helpAccessID, err := q.staffRepository.CreateHelpAccess(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateHelpAccess")
		return nil, err
	}
	return q.FindHelpAccess(ctx, tx, &models.HelpAccessFilter{StoreID: request.StoreID, HelpAccessIDs: []uuid.UUID{helpAccessID}, At: request.CreatedAt})
}

func (q *staffService) RevokeHelpAccess(ctx context.Context, tx pgx.Tx, storeID, helpAccessID uuid.UUID, at time.Time) (*models.HelpAccess, error) {
	ctxt := "StaffService-RevokeHelpAccess"
	revoked, err := q.staffRepository.RevokeHelpAccess(ctx, tx, storeID, helpAccessID, at)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRevokeHelpAccess")
		return nil, err
	}
	if !revoked {
		return nil, helper.Conflict("This access is no longer active.")
	}
	return q.FindHelpAccess(ctx, tx, &models.HelpAccessFilter{StoreID: storeID, HelpAccessIDs: []uuid.UUID{helpAccessID}, At: at})
}

func (q *staffService) FindAccountOptions(ctx context.Context, storeID uuid.UUID) ([]*models.AccountOption, error) {
	ctxt := "StaffService-FindAccountOptions"
	response, err := q.staffRepository.FindAccountOptions(ctx, storeID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindAccountOptions")
	}
	return response, err
}
