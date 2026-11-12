package models

import (
	"errors"
	"time"
	"uuid"
)

const (
	// IncludedStaff accounts are free; each one beyond costs StaffMonthly a month.
	IncludedStaff = 2
	StaffMonthly  = 15000.0

	HelpAccessStatusActive  = "active"
	HelpAccessStatusRevoked = "revoked"
)

var (
	ErrStaffNotFound      = errors.New("staff member not found")
	ErrAttendanceNotFound = errors.New("attendance not found")
	ErrHelpAccessNotFound = errors.New("support access not found")

	// Permissions are the access rights a staff member can have.
	Permissions = []string{"dashboard", "products", "stock", "customers", "suppliers", "discounts", "taxes", "sales", "purchases", "finance",
		"reports", "debts", "returns", "settings", "staff", "branches", "ppob", "online_store", "void_transaction", "edit_price", "give_discount"}

	// DefaultPermissions are the access rights a role starts with.
	DefaultPermissions = map[string][]string{
		RoleAdministrator: Permissions,
		RoleManager: {"dashboard", "products", "stock", "customers", "suppliers", "discounts", "taxes", "sales", "purchases", "finance", "reports",
			"debts", "returns", "ppob", "online_store", "give_discount"},
		RoleCashier: {"sales", "customers", "give_discount"},
	}

	// StaffRoles are the roles a staff member can have; the owner is not staff.
	StaffRoles = []string{RoleAdministrator, RoleManager, RoleCashier}

	// MapStaffSortings lists the fields a client may sort by (?sort=).
	MapStaffSortings = map[string]struct{}{
		"id":            {},
		"created_at":    {},
		"updated_at":    {},
		"name":          {},
		"email":         {},
		"phone":         {},
		"role":          {},
		"branch_id":     {},
		"address":       {},
		"status":        {},
		"last_login_at": {},
		"branch.name":   {},
	}
)

// Staff is a user of the store other than the owner.
type Staff struct {
	ID          uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	StoreID     uuid.UUID  `json:"-"`
	Name        string     `json:"name" example:"Vina Pazary"`
	Email       string     `json:"email" example:"kasir@lestari.id"`
	Phone       *string    `json:"phone" example:"+6281299990001"`
	Role        string     `json:"role" example:"cashier" enums:"administrator,manager,cashier"`
	BranchID    *uuid.UUID `json:"branch_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
	Branch      *Reference `json:"branch"`
	Address     *string    `json:"address" example:"Balige"`
	Status      string     `json:"status" example:"invited" enums:"invited,active,disabled"`
	LastLoginAt *time.Time `json:"last_login_at" example:"2026-11-08T08:02:00+07:00"`
	CreatedAt   time.Time  `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt   time.Time  `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
	// Password and PPOBPin are new secrets to store (hashed by the service); empty means unchanged.
	Password string `json:"-" db:"-"`
	PPOBPin  string `json:"-" db:"-"`
	// ResetPermissions gives the staff member the default access rights of their role.
	ResetPermissions bool `json:"-" db:"-"`
	// RoleChanged tells that the request sent a role.
	RoleChanged bool `json:"-" db:"-"`
}

// StaffRequest is the body of POST /staff and PUT /staff/{id}. On update only the attributes present are changed.
type StaffRequest struct {
	Nulls `json:"-"`
	Name  *string `json:"name" example:"Vina Pazary"`
	Email *string `json:"email" example:"kasir@lestari.id"`
	// base64 of the password; required on create
	Password *string `json:"password" example:"a2FzaXIxMjM0NQ=="`
	Phone    *string `json:"phone" example:"+6281299990001"`
	Role     *string `json:"role" example:"cashier" enums:"administrator,manager,cashier"`
	// six digits that confirm PPOB sales
	PPOBPin *string `json:"ppob_pin" example:"123456"`
	// the active branch when left out on create
	BranchID *uuid.UUID `json:"branch_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6b"`
	Address  *string    `json:"address" example:"Balige"`
	// with a role on update: give the default access rights of that role
	ResetPermissions bool `json:"reset_permissions" example:"false"`
}

// Apply copies the attributes present in the request onto the staff member.
func (q *StaffRequest) Apply(staff *Staff) *Staff {
	if q.Name != nil {
		staff.Name = *q.Name
	}
	if q.Email != nil {
		staff.Email = *q.Email
	}
	if q.Password != nil {
		staff.Password = *q.Password
	}
	if q.Phone != nil || q.IsNull("phone") {
		staff.Phone = q.Phone
	}
	if q.Role != nil {
		staff.Role = *q.Role
		staff.RoleChanged = true
	}
	if q.PPOBPin != nil {
		staff.PPOBPin = *q.PPOBPin
	}
	if q.BranchID != nil || q.IsNull("branch_id") {
		staff.BranchID = q.BranchID
	}
	if q.Address != nil || q.IsNull("address") {
		staff.Address = q.Address
	}
	staff.ResetPermissions = q.ResetPermissions
	return staff
}

// StaffFilter selects staff members that are not deleted; build it with NewStaffFilter().With…().
type StaffFilter struct {
	StaffIDs,
	StoreIDs []uuid.UUID
	Role,
	Status *string
	BranchID *uuid.UUID
	Keyword  string
	Sortings []Sorting
	Limit,
	Page int64
}

func NewStaffFilter() *StaffFilter {
	return &StaffFilter{}
}

func (q *StaffFilter) WithStaffIDs(staffIDs ...uuid.UUID) *StaffFilter {
	q.StaffIDs = staffIDs
	return q
}

func (q *StaffFilter) WithStoreIDs(storeIDs ...uuid.UUID) *StaffFilter {
	q.StoreIDs = storeIDs
	return q
}

func (q *StaffFilter) WithRole(role string) *StaffFilter {
	q.Role = &role
	return q
}

func (q *StaffFilter) WithStatus(status string) *StaffFilter {
	q.Status = &status
	return q
}

func (q *StaffFilter) WithBranchID(branchID uuid.UUID) *StaffFilter {
	q.BranchID = &branchID
	return q
}

func (q *StaffFilter) WithKeyword(keyword string) *StaffFilter {
	q.Keyword = keyword
	return q
}

func (q *StaffFilter) WithSortings(sortings ...Sorting) *StaffFilter {
	q.Sortings = sortings
	return q
}

func (q *StaffFilter) WithLimit(limit int64) *StaffFilter {
	q.Limit = limit
	return q
}

// WithPage sets the 1-based page.
func (q *StaffFilter) WithPage(page int64) *StaffFilter {
	q.Page = page
	return q
}

// StaffBillingEstimate is what the staff accounts of a store cost a month.
type StaffBillingEstimate struct {
	MonthlyCost   float64 `json:"monthly_cost" example:"15000"`
	Currency      string  `json:"currency" example:"IDR"`
	IncludedStaff int64   `json:"included_staff" example:"2"`
	CurrentStaff  int64   `json:"current_staff" example:"2"`
}

// PermissionOption is one access right that can be given.
type PermissionOption struct {
	Value string `json:"value" example:"void_transaction"`
	Label string `json:"label" example:"void transaction"`
}

// StaffPermissions is the data of GET /staff/{id}/permissions.
type StaffPermissions struct {
	Permissions []string           `json:"permissions" example:"sales,customers"`
	Available   []PermissionOption `json:"available,omitempty"`
}

// PermissionsRequest is the body of PUT /staff/{id}/permissions.
type PermissionsRequest struct {
	Permissions []string `json:"permissions" example:"sales,customers,give_discount"`
}

// SentResponse is the data of the endpoints that send a message.
type SentResponse struct {
	Sent bool `json:"sent" example:"true"`
}

// Attendance is one working day of a staff member.
type Attendance struct {
	ID         uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Date       string     `json:"date" example:"2026-11-08"`
	StaffID    uuid.UUID  `json:"staff_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6c"`
	StaffName  string     `json:"staff_name" example:"Vina Pazary"`
	ClockInAt  *time.Time `json:"clock_in_at" example:"2026-11-08T08:02:00+07:00"`
	ClockOutAt *time.Time `json:"clock_out_at" example:"2026-11-08T16:05:00+07:00"`
	Hours      float64    `json:"hours" example:"8.05"`
	// late when the staff member clocked in more than ten minutes after the start of their shift
	Status           string  `json:"status" example:"present" enums:"present,late"`
	Location         *string `json:"location" example:"2.3333,99.0667"`
	ClockInPhotoURL  *string `json:"clock_in_photo_url" example:"/uploads/in.jpg"`
	ClockOutPhotoURL *string `json:"clock_out_photo_url" example:"/uploads/out.jpg"`
	Note             *string `json:"note" example:"Opening shift"`
}

// AttendanceSummary is the summary of the attendance list.
type AttendanceSummary struct {
	AttendanceCount int64   `json:"attendance_count" example:"42"`
	LateCount       int64   `json:"late_count" example:"3"`
	TotalHours      float64 `json:"total_hours" example:"331.5"`
}

// AttendanceFilter selects the attendance of a branch. StartDate and EndDate are YYYY-MM-DD.
type AttendanceFilter struct {
	BranchID uuid.UUID
	StartDate,
	EndDate string
	StaffID *uuid.UUID
	Limit,
	Page int64
}

// ClockInRequest is the body of POST /staff-attendances/clock-in.
type ClockInRequest struct {
	// when the shift starts (HH:mm); later than ten minutes after it is "late"
	ShiftStart string `json:"shift_start" example:"08:00"`
	PhotoURL   string `json:"photo_url" example:"/uploads/in.jpg"`
	Location   string `json:"location" example:"2.3333,99.0667"`
	Note       string `json:"note" example:"Opening shift"`
}

// ClockOutRequest is the body of POST /staff-attendances/clock-out.
type ClockOutRequest struct {
	PhotoURL string `json:"photo_url" example:"/uploads/out.jpg"`
}

// Clocking is the data of a clock-in or clock-out.
type Clocking struct {
	ID         uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	Date       string     `json:"date" example:"2026-11-08"`
	ClockInAt  *time.Time `json:"clock_in_at" example:"2026-11-08T08:02:00+07:00"`
	ClockOutAt *time.Time `json:"clock_out_at,omitempty" example:"2026-11-08T16:05:00+07:00"`
	Status     string     `json:"status" example:"present" enums:"present,late"`
}

// HelpAccess lets Lestari support sign in to an account of the store for a limited time.
type HelpAccess struct {
	ID            uuid.UUID  `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6a"`
	StoreID       uuid.UUID  `json:"-"`
	Platform      string     `json:"platform" example:"web" enums:"web,app"`
	AccountID     uuid.UUID  `json:"account_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6c"`
	Account       *Account   `json:"account"`
	AccountEmail  *string    `json:"account_email" example:"demo@lestari.id"`
	DurationHours int64      `json:"duration_hours" example:"24" enums:"1,6,24,72"`
	Description   string     `json:"description" example:"Receipt printer does not print the logo"`
	Status        string     `json:"status" example:"active" enums:"active,expired,revoked"`
	ExpiresAt     time.Time  `json:"expires_at" example:"2026-11-09T10:32:00+07:00"`
	CreatedAt     time.Time  `json:"created_at" example:"2026-11-08T10:32:00+07:00"`
	UpdatedAt     time.Time  `json:"updated_at" example:"2026-11-08T10:32:00+07:00"`
	RevokedAt     *time.Time `json:"-" db:"-"`
}

// Account is the {id, email} of a user, sent next to its foreign key.
type Account struct {
	ID    uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6c"`
	Email string    `json:"email" example:"demo@lestari.id"`
}

// HelpAccessRequest is the body of POST /help-access.
type HelpAccessRequest struct {
	Platform      string `json:"platform" example:"web" enums:"web,app"`
	AccountID     string `json:"account_id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6c"`
	DurationHours int64  `json:"duration_hours" example:"24" enums:"1,6,24,72"`
	Description   string `json:"description" example:"Receipt printer does not print the logo"`
}

// HelpAccessFilter selects support accesses of a store.
type HelpAccessFilter struct {
	HelpAccessIDs []uuid.UUID
	StoreID       uuid.UUID
	Status        string
	At            time.Time
	Sortings      []Sorting
	Limit,
	Page int64
}

// MapHelpAccessSortings lists the fields a client may sort by (?sort=).
var MapHelpAccessSortings = map[string]struct{}{
	"id":             {},
	"created_at":     {},
	"updated_at":     {},
	"platform":       {},
	"account_id":     {},
	"account_email":  {},
	"duration_hours": {},
	"description":    {},
	"status":         {},
	"expires_at":     {},
	"account.email":  {},
}

// AccountOption is an account support access can be granted for.
type AccountOption struct {
	ID    uuid.UUID `json:"id" example:"019a5f3c-8b2e-7d41-9c3a-5e8f1b2c4d6c"`
	Label string    `json:"label" example:"Owner (demo@lestari.id)"`
	Email string    `json:"email" example:"demo@lestari.id"`
	Role  string    `json:"role" example:"owner"`
}
