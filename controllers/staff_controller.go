package controllers

import (
	"errors"
	"log/slog"
	"slices"
	"time"
	"uuid"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/utils/v2"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

const staffManagersOnly = "Only the owner or an administrator can manage staff."

type staffController struct {
	authService     *middlewares.Service
	staffService    services.StaffService
	activityService services.ActivityService
}

func NewStaffController(
	authService *middlewares.Service,
	staffService services.StaffService,
	activityService services.ActivityService,
) *staffController {
	return &staffController{
		authService:     authService,
		staffService:    staffService,
		activityService: activityService,
	}
}

// Mount registers the routes of /staff, /staff-attendances (attendance plugin) and /help-access.
func (q *staffController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	staff := private.Group("/staff")
	staff.Get("", q.V1Index)
	staff.Post("", q.V1Create)
	staff.Post("/bulk-delete", q.V1BulkDelete)
	staff.Get("/billing-estimate", q.V1BillingEstimate)
	staff.Get("/:id<guid>", q.V1Show)
	staff.Put("/:id<guid>", q.V1Update)
	staff.Delete("/:id<guid>", q.V1Delete)
	staff.Get("/:id<guid>/permissions", q.V1Permissions)
	staff.Put("/:id<guid>/permissions", q.V1UpdatePermissions)
	staff.Post("/:id<guid>/resend-invitation", q.V1ResendInvitation)
	staff.Get("/:id<guid>/activity-logs", q.V1Activity)
	attendances := private.Group("/staff-attendances", middlewares.RequirePlugin("attendance"))
	attendances.Get("", q.V1Attendances)
	attendances.Get("/export", q.V1ExportAttendances)
	attendances.Post("/clock-in", q.V1ClockIn)
	attendances.Post("/clock-out", q.V1ClockOut)
	attendances.Get("/:id<guid>", q.V1Attendance)
	helpAccess := private.Group("/help-access")
	helpAccess.Get("", q.V1HelpAccesses)
	helpAccess.Post("", q.V1CreateHelpAccess)
	helpAccess.Get("/accounts", q.V1HelpAccounts)
	helpAccess.Get("/:id<guid>", q.V1HelpAccess)
	helpAccess.Post("/:id<guid>/revoke", q.V1RevokeHelpAccess)
}

// staffError turns the staff errors into responses; anything else goes to the error handler as it is.
func staffError(err error) error {
	switch {
	case errors.Is(err, models.ErrStaffNotFound):
		return helper.NotFound("Staff member")
	case errors.Is(err, models.ErrAttendanceNotFound):
		return helper.NotFound("Attendance")
	case errors.Is(err, models.ErrHelpAccessNotFound):
		return helper.NotFound("Support access")
	}
	return writeError(err)
}

// oneStaff selects a staff member of the signed-in user's store by the id of the path.
func oneStaff(c fiber.Ctx, ctxt string) (uuid.UUID, *models.StaffFilter, error) {
	staffID, err := sanitizers.ParamID(c, "id", "Staff member")
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return staffID, nil, err
	}
	return staffID, models.NewStaffFilter().WithStoreIDs(currentStoreID(c)).WithStaffIDs(staffID), nil
}

// V1Index godoc
//
//	@Summary		List staff
//	@Description	The users of the store other than the owner.
//	@Tags			Staff
//	@Param			q			query		string	false	"search in name, email, phone"
//	@Param			role		query		string	false	"only rows with this role"	Enums(administrator, manager, cashier)
//	@Param			branch_id	query		string	false	"only staff of this branch (UUID)"
//	@Param			status		query		string	false	"only rows with this status"	Enums(invited, active, disabled)
//	@Param			sort		query		string	false	"sort field"					Enums(name, email, phone, role, branch_id, address, status, last_login_at, branch.name, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"				Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"					default(1)
//	@Param			per_page	query		int		false	"rows per page"					default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Staff}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/staff [get]
func (q *staffController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1Index"
	request := sanitizers.FindStaffs(c).WithStoreIDs(currentStoreID(c))
	response, total, err := q.staffService.FindStaffs(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStaffs")
		return err
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

// V1Show godoc
//
//	@Summary	Get one staff member
//	@Tags		Staff
//	@Param		id	path		string	true	"staff id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Staff}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/staff/{id} [get]
func (q *staffController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1Show"
	_, filter, err := oneStaff(c, ctxt)
	if err != nil {
		return err
	}
	response, err := q.staffService.FindStaff(ctx, nil, filter)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStaff")
		return staffError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary		Create a staff member
//	@Description	Only the owner or an administrator. The account starts as invited with the default access rights of its role, in the active branch unless another is named. The password travels base64-encoded.
//	@Tags			Staff
//	@Param			request	body		models.StaffRequest	true	"staff member"
//	@Success		201		{object}	helper.Response{data=models.Staff}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/staff [post]
func (q *staffController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1Create"
	request, err := sanitizers.ValidateStaff(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateStaff")
		return err
	}
	if err = manager(c, ctxt, staffManagersOnly); err != nil {
		return err
	}
	now := time.Now()
	staff := request.Apply(&models.Staff{StoreID: currentStoreID(c), Status: models.UserStatusInvited, CreatedAt: now, UpdatedAt: now})
	if staff.BranchID == nil {
		staff.BranchID = new(currentBranchID(c))
	}
	var response *models.Staff
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.staffService.CreateStaff(ctx, tx, staff); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStaff")
		}
		return staffError(err)
	}); err != nil {
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a staff member
//	@Description	Only the owner or an administrator. Only the attributes present in the body are changed. A new role gives its default access rights to a staff member who has none, or to anyone with reset_permissions.
//	@Tags			Staff
//	@Param			id		path		string				true	"staff id (UUID)"
//	@Param			request	body		models.StaffRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Staff}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/staff/{id} [put]
func (q *staffController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1Update"
	_, filter, err := oneStaff(c, ctxt)
	if err != nil {
		return err
	}
	request, err := sanitizers.ValidateStaff(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateStaff")
		return err
	}
	if err = manager(c, ctxt, staffManagersOnly); err != nil {
		return err
	}
	staff, err := q.staffService.FindStaff(ctx, nil, filter)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStaff")
		return staffError(err)
	}
	var response *models.Staff
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		request.Apply(staff).UpdatedAt = time.Now()
		if response, err = q.staffService.UpdateStaff(ctx, tx, staff); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateStaff")
		}
		return staffError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// deleteStaffs deletes staff members after the checks both delete endpoints share.
func (q *staffController) deleteStaffs(c fiber.Ctx, ctxt string, staffIDs []uuid.UUID) error {
	ctx := c.Context()
	if err := manager(c, ctxt, staffManagersOnly); err != nil {
		return err
	}
	if slices.Contains(staffIDs, currentUserID(c)) {
		err := helper.Conflict("You cannot delete your own account.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrOwnAccount")
		return err
	}
	return inTx(c, ctxt, func(tx pgx.Tx) error {
		err := q.staffService.DeleteStaffs(ctx, tx, currentStoreID(c), staffIDs, time.Now())
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteStaffs")
		}
		return staffError(err)
	})
}

// V1Delete godoc
//
//	@Summary		Delete a staff member
//	@Description	Only the owner or an administrator, and not their own account. The sessions of the staff member end.
//	@Tags			Staff
//	@Param			id	path	string	true	"staff id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/staff/{id} [delete]
func (q *staffController) V1Delete(c fiber.Ctx) error {
	ctxt := "StaffController-V1Delete"
	staffID, _, err := oneStaff(c, ctxt)
	if err != nil {
		return err
	}
	if err = q.deleteStaffs(c, ctxt, []uuid.UUID{staffID}); err != nil {
		return err
	}
	return helper.NoContent(c)
}

// V1BulkDelete godoc
//
//	@Summary		Delete several staff members
//	@Description	All or nothing: an unknown id (404) deletes none.
//	@Tags			Staff
//	@Param			request	body		models.BulkDeleteRequest	true	"ids to delete"
//	@Success		200		{object}	helper.Response{data=models.DeletedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/staff/bulk-delete [post]
func (q *staffController) V1BulkDelete(c fiber.Ctx) error {
	ctxt := "StaffController-V1BulkDelete"
	staffIDs, err := sanitizers.BulkDeleteIDs(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrBulkDeleteIDs")
		return err
	}
	if err = q.deleteStaffs(c, ctxt, staffIDs); err != nil {
		return err
	}
	return helper.OK(c, models.DeletedResponse{Deleted: len(staffIDs)})
}

// V1BillingEstimate godoc
//
//	@Summary		Estimate the cost of staff accounts
//	@Description	Two staff accounts are included; each one more costs Rp 15,000 a month.
//	@Tags			Staff
//	@Param			additional_staff	query		int	false	"accounts about to be added"	default(1)
//	@Success		200					{object}	helper.Response{data=models.StaffBillingEstimate}
//	@Failure		401					{object}	helper.Response{data=helper.ErrorData}
//	@Router			/staff/billing-estimate [get]
func (q *staffController) V1BillingEstimate(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1BillingEstimate"
	additionalStaff, _ := utils.ParseInt(c.Query("additional_staff", "1"))
	response, err := q.staffService.EstimateBilling(ctx, currentStoreID(c), additionalStaff)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrEstimateBilling")
		return err
	}
	return helper.OK(c, response)
}

// V1Permissions godoc
//
//	@Summary	Get the access rights of a staff member
//	@Tags		Staff
//	@Param		id	path		string	true	"staff id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.StaffPermissions}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/staff/{id}/permissions [get]
func (q *staffController) V1Permissions(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1Permissions"
	staffID, _, err := oneStaff(c, ctxt)
	if err != nil {
		return err
	}
	response, err := q.staffService.FindPermissions(ctx, currentStoreID(c), staffID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPermissions")
		return staffError(err)
	}
	return helper.OK(c, response)
}

// V1UpdatePermissions godoc
//
//	@Summary		Replace the access rights of a staff member
//	@Description	Only the owner or an administrator.
//	@Tags			Staff
//	@Param			id		path		string						true	"staff id (UUID)"
//	@Param			request	body		models.PermissionsRequest	true	"access rights"
//	@Success		200		{object}	helper.Response{data=models.StaffPermissions}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/staff/{id}/permissions [put]
func (q *staffController) V1UpdatePermissions(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1UpdatePermissions"
	staffID, _, err := oneStaff(c, ctxt)
	if err != nil {
		return err
	}
	if err = manager(c, ctxt, "Only the owner or an administrator can change access rights."); err != nil {
		return err
	}
	permissions, err := sanitizers.ValidatePermissions(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidatePermissions")
		return err
	}
	if err = inTx(c, ctxt, func(tx pgx.Tx) error {
		err := q.staffService.UpdatePermissions(ctx, tx, currentStoreID(c), staffID, permissions, time.Now())
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdatePermissions")
		}
		return staffError(err)
	}); err != nil {
		return err
	}
	record(c, q.activityService, "permissions_changed", i18n.Tr("Changed access rights of staff {id}", "id", staffID.String()))
	return helper.OK(c, models.StaffPermissions{Permissions: permissions})
}

// V1ResendInvitation godoc
//
//	@Summary	Send the invitation again
//	@Tags		Staff
//	@Param		id	path		string	true	"staff id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.SentResponse}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/staff/{id}/resend-invitation [post]
func (q *staffController) V1ResendInvitation(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1ResendInvitation"
	staffID, _, err := oneStaff(c, ctxt)
	if err != nil {
		return err
	}
	if err = q.staffService.ResendInvitation(ctx, currentStoreID(c), staffID, func(name string) string {
		return helper.T(c, "Hi {name}, you have been invited to Lestari. Sign in with this email address and the password your manager gave you.", "name", name)
	}); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrResendInvitation")
		return staffError(err)
	}
	return helper.OK(c, models.SentResponse{Sent: true})
}

// V1Activity godoc
//
//	@Summary	List the activity of a staff member
//	@Tags		Staff
//	@Param		id			path		string	true	"staff id (UUID)"
//	@Param		page		query		int		false	"page, from 1"	default(1)
//	@Param		per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success	200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Activity}}
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/staff/{id}/activity-logs [get]
func (q *staffController) V1Activity(c fiber.Ctx) error {
	ctxt := "StaffController-V1Activity"
	staffID, _, err := oneStaff(c, ctxt)
	if err != nil {
		return err
	}
	// the list is limited to the store, so the id of someone else's user finds nothing
	return listActivity(c, ctxt, q.activityService, &models.ActivityFilter{UserID: staffID})
}

// V1Attendances godoc
//
//	@Summary		List staff attendance
//	@Description	The attendance of the active branch in a date range (today by default), with totals in the summary. Needs the attendance plugin.
//	@Tags			Attendance
//	@Param			start_date	query		string	false	"from this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"to this date (YYYY-MM-DD)"
//	@Param			staff_id	query		string	false	"only this staff member (UUID)"
//	@Param			page		query		int		false	"page, from 1"	default(1)
//	@Param			per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Attendance,summary=models.AttendanceSummary}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/staff-attendances [get]
func (q *staffController) V1Attendances(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1Attendances"
	request, err := sanitizers.FindAttendances(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindAttendances")
		return err
	}
	request.BranchID = currentBranchID(c)
	response, summary, err := q.staffService.FindAttendances(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindAttendances")
		return err
	}
	return helper.ListWith(c, response, page(request.Limit, request.Page, summary.AttendanceCount), helper.Extras{Summary: summary})
}

// attendanceExportColumns are the columns of the attendance export.
var attendanceExportColumns = []export.Column{
	{Key: "date", Label: "Date", Format: "date"},
	{Key: "staff_name", Label: "Staff"},
	{Key: "clock_in_at", Label: "In", Format: "datetime"},
	{Key: "clock_out_at", Label: "Out", Format: "datetime"},
	{Key: "hours", Label: "Hours", Format: "number"},
	{Key: "status", Label: "Status"},
	{Key: "location", Label: "Location"},
}

// V1ExportAttendances godoc
//
//	@Summary	Export staff attendance
//	@Tags		Attendance
//	@Produce	application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param		format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param		start_date	query		string	false	"from this date (YYYY-MM-DD)"
//	@Param		end_date	query		string	false	"to this date (YYYY-MM-DD)"
//	@Param		staff_id	query		string	false	"only this staff member (UUID)"
//	@Success	200			{file}		binary
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure	402			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/staff-attendances/export [get]
func (q *staffController) V1ExportAttendances(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1ExportAttendances"
	request, err := sanitizers.FindAttendances(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindAttendances")
		return err
	}
	request.BranchID, request.Limit = currentBranchID(c), 0
	response, _, err := q.staffService.FindAttendances(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindAttendances")
		return err
	}
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	return export.Write(c, "attendance", "Staff attendance", attendanceExportColumns, rows)
}

// V1Attendance godoc
//
//	@Summary	Get one attendance
//	@Tags		Attendance
//	@Param		id	path		string	true	"attendance id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Attendance}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	402	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/staff-attendances/{id} [get]
func (q *staffController) V1Attendance(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1Attendance"
	attendanceID, err := sanitizers.ParamID(c, "id", "Attendance")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.staffService.FindAttendance(ctx, currentBranchID(c), attendanceID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindAttendance")
		return staffError(err)
	}
	return helper.OK(c, response)
}

// V1ClockIn godoc
//
//	@Summary		Clock in
//	@Description	Used by the cashier app: starts the working day of the signed-in staff member. More than ten minutes after shift_start counts as late.
//	@Tags			Attendance
//	@Param			request	body		models.ClockInRequest	false	"shift start, photo and location"
//	@Success		201		{object}	helper.Response{data=models.Clocking}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/staff-attendances/clock-in [post]
func (q *staffController) V1ClockIn(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1ClockIn"
	request, err := sanitizers.Decode[models.ClockInRequest](c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return err
	}
	var response *models.Clocking
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.staffService.ClockIn(ctx, tx, currentBranchID(c), currentUserID(c), request, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrClockIn")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.Created(c, response)
}

// V1ClockOut godoc
//
//	@Summary		Clock out
//	@Description	Used by the cashier app: ends the open working day of the signed-in staff member.
//	@Tags			Attendance
//	@Param			request	body		models.ClockOutRequest	false	"photo"
//	@Success		200		{object}	helper.Response{data=models.Clocking}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/staff-attendances/clock-out [post]
func (q *staffController) V1ClockOut(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1ClockOut"
	request, err := sanitizers.Decode[models.ClockOutRequest](c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecode")
		return err
	}
	var response *models.Clocking
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.staffService.ClockOut(ctx, tx, currentBranchID(c), currentUserID(c), request.PhotoURL, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrClockOut")
		}
		return err
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1HelpAccesses godoc
//
//	@Summary		List support accesses
//	@Description	The times Lestari support was allowed to sign in to an account of the store.
//	@Tags			Support access
//	@Param			status		query		string	false	"only rows with this status"	Enums(active, expired, revoked)
//	@Param			sort		query		string	false	"sort field"					Enums(platform, account_email, duration_hours, description, status, expires_at, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"				Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"					default(1)
//	@Param			per_page	query		int		false	"rows per page"					default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.HelpAccess}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/help-access [get]
func (q *staffController) V1HelpAccesses(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1HelpAccesses"
	request := sanitizers.FindHelpAccesses(c)
	request.StoreID, request.At = currentStoreID(c), time.Now()
	response, total, err := q.staffService.FindHelpAccesses(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindHelpAccesses")
		return err
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

// V1HelpAccess godoc
//
//	@Summary	Get one support access
//	@Tags		Support access
//	@Param		id	path		string	true	"support access id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.HelpAccess}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/help-access/{id} [get]
func (q *staffController) V1HelpAccess(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1HelpAccess"
	helpAccessID, err := sanitizers.ParamID(c, "id", "Support access")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.staffService.FindHelpAccess(ctx, nil, &models.HelpAccessFilter{StoreID: currentStoreID(c), HelpAccessIDs: []uuid.UUID{helpAccessID}, At: time.Now()})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindHelpAccess")
		return staffError(err)
	}
	return helper.OK(c, response)
}

// V1CreateHelpAccess godoc
//
//	@Summary		Grant support access
//	@Description	Only the owner or an administrator. Lets Lestari support sign in to an account of the store for 1, 6, 24 or 72 hours. An access cannot be edited: revoke it and create a new one.
//	@Tags			Support access
//	@Param			request	body		models.HelpAccessRequest	true	"support access"
//	@Success		201		{object}	helper.Response{data=models.HelpAccess}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/help-access [post]
func (q *staffController) V1CreateHelpAccess(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1CreateHelpAccess"
	helpAccess, err := sanitizers.ValidateHelpAccess(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateHelpAccess")
		return err
	}
	if err = manager(c, ctxt, "Only the owner or an administrator can grant support access."); err != nil {
		return err
	}
	helpAccess.StoreID, helpAccess.CreatedAt = currentStoreID(c), time.Now()
	var response *models.HelpAccess
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.staffService.CreateHelpAccess(ctx, tx, helpAccess); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateHelpAccess")
		}
		return staffError(err)
	}); err != nil {
		return err
	}
	return helper.Created(c, response)
}

// V1RevokeHelpAccess godoc
//
//	@Summary	Revoke a support access
//	@Tags		Support access
//	@Param		id	path		string	true	"support access id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.HelpAccess}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/help-access/{id}/revoke [post]
func (q *staffController) V1RevokeHelpAccess(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1RevokeHelpAccess"
	helpAccessID, err := sanitizers.ParamID(c, "id", "Support access")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	var response *models.HelpAccess
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.staffService.RevokeHelpAccess(ctx, tx, currentStoreID(c), helpAccessID, time.Now()); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrRevokeHelpAccess")
		}
		return staffError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1HelpAccounts godoc
//
//	@Summary	List the accounts support access can be granted for
//	@Tags		Support access
//	@Success	200	{object}	helper.Response{data=[]models.AccountOption}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/help-access/accounts [get]
func (q *staffController) V1HelpAccounts(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StaffController-V1HelpAccounts"
	response, err := q.staffService.FindAccountOptions(ctx, currentStoreID(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindAccountOptions")
		return err
	}
	return helper.OK(c, response)
}
