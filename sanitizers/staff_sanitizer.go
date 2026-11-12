package sanitizers

import (
	"log/slog"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// FindStaffs reads the list query: q, role, branch_id, status, sort, order, page, per_page.
func FindStaffs(c fiber.Ctx) *models.StaffFilter {
	response := models.NewStaffFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	if role := c.Query("role"); slices.Contains(models.StaffRoles, role) {
		response.WithRole(role)
	}
	if status := c.Query("status"); status != "" {
		response.WithStatus(status)
	}
	if branchID, ok := queryID(c, "branch_id"); ok {
		response.WithBranchID(branchID)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapStaffSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateStaff reads a staff body. On create name, email, password and role are required (the role defaults to
// cashier); on update only what was sent is checked.
func ValidateStaff(c fiber.Ctx, create bool) (*models.StaffRequest, error) {
	ctxt := "StaffSanitizer-ValidateStaff"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	v := &helper.Validation{}
	response := &models.StaffRequest{ResetPermissions: helper.Bool(b, "reset_permissions")}
	response.Name = b.text(v, response, "name")
	response.Email = b.text(v, response, "email")
	response.Password = b.password(v, response, "password")
	response.Phone = b.text(v, response, "phone")
	response.Role = b.text(v, response, "role", models.StaffRoles...)
	response.PPOBPin = b.text(v, response, "ppob_pin")
	response.BranchID = b.id(v, response, "branch_id")
	response.Address = b.text(v, response, "address")
	if response.Email != nil {
		*response.Email = strings.ToLower(*response.Email)
	}
	if response.Role == nil && create && len(v.Fields["role"]) == 0 {
		response.Role = new(models.RoleCashier)
	}
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	required(v, "email", response.Email == nil && (create || response.IsNull("email")))
	required(v, "password", response.Password == nil && (create || response.IsNull("password")))
	required(v, "role", response.Role == nil && (create || response.IsNull("role")))
	return response, v.Err()
}

// ValidatePermissions reads the body of a permission update: every value must be a known access right.
func ValidatePermissions(c fiber.Ctx) ([]string, error) {
	request, err := Decode[models.PermissionsRequest](c)
	if err != nil {
		return nil, err
	}
	for _, permission := range request.Permissions {
		if !slices.Contains(models.Permissions, permission) {
			return nil, helper.Invalid("permissions", "Unknown permission {permission}.", "permission", permission)
		}
	}
	if request.Permissions == nil {
		request.Permissions = []string{}
	}
	return request.Permissions, nil
}

// FindAttendances reads the list query: start_date, end_date (today by default), staff_id, page, per_page.
func FindAttendances(c fiber.Ctx) (*models.AttendanceFilter, error) {
	response := &models.AttendanceFilter{}
	var err error
	if response.StartDate, response.EndDate, err = DateRange(c); err != nil {
		return nil, err
	}
	response.StaffID, _ = QueryID(c, "staff_id")
	response.Limit, response.Page = pageOf(c, 50)
	return response, nil
}

// FindHelpAccesses reads the list query: status, sort, order, page, per_page.
func FindHelpAccesses(c fiber.Ctx) *models.HelpAccessFilter {
	response := &models.HelpAccessFilter{Status: c.Query("status")}
	response.Limit, response.Page = paging(c)
	if sortBy, ok := sorting(c, models.MapHelpAccessSortings); ok {
		response.Sortings = []models.Sorting{sortBy}
	}
	return response
}

// ValidateHelpAccess reads the body of a support access: platform, account, duration and description are required.
func ValidateHelpAccess(c fiber.Ctx) (*models.HelpAccess, error) {
	ctxt := "StaffSanitizer-ValidateHelpAccess"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	v := &helper.Validation{}
	nulls := &models.Nulls{}
	response := &models.HelpAccess{Platform: "web"}
	if platform := b.text(v, nulls, "platform", "web", "app"); platform != nil {
		response.Platform = *platform
	}
	accountID := b.id(v, nulls, "account_id")
	required(v, "account_id", accountID == nil)
	if accountID != nil {
		response.AccountID = *accountID
	}
	duration := b.integer(v, nulls, "duration_hours", nil, nil)
	required(v, "duration_hours", duration == nil)
	if duration != nil {
		if !slices.Contains([]int64{1, 6, 24, 72}, *duration) {
			v.Add("duration_hours", "Must be one of: {values}.", "values", "1, 6, 24, 72")
		}
		response.DurationHours = *duration
	}
	description := b.text(v, nulls, "description")
	required(v, "description", description == nil)
	if description != nil {
		response.Description = *description
	}
	return response, v.Err()
}
