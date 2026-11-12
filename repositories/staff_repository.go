package repositories

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
)

// StaffRepository manages the users of a store other than its owner, their attendance and the support accesses.
type StaffRepository interface {
	FindStaffs(ctx context.Context, request *models.StaffFilter) ([]*models.Staff, int64, error)
	FindStaff(ctx context.Context, tx pgx.Tx, request *models.StaffFilter) (*models.Staff, error)
	// CreateStaff creates an invited account with the default access rights of its role; an email in use is
	// ErrEmailTaken.
	CreateStaff(ctx context.Context, tx pgx.Tx, request *models.Staff, passwordHash string, pinHash *string) (*models.Staff, error)
	// UpdateStaff writes only the attributes that differ from the stored ones; empty hashes keep the secrets.
	UpdateStaff(ctx context.Context, tx pgx.Tx, request *models.Staff, passwordHash, pinHash string) (*models.Staff, error)
	// DeleteStaffs marks staff members as deleted, ends their sessions and returns how many went.
	DeleteStaffs(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, staffIDs []uuid.UUID, deletedAt time.Time) (int64, error)
	CountStaffs(ctx context.Context, storeID uuid.UUID) (int64, error)
	FindPermissions(ctx context.Context, tx pgx.Tx, storeID, staffID uuid.UUID) ([]string, error)
	// UpdatePermissions replaces the access rights. With onlyIfEmpty it leaves a staff member alone who already
	// has some.
	UpdatePermissions(ctx context.Context, tx pgx.Tx, storeID, staffID uuid.UUID, permissions []string, onlyIfEmpty bool, updatedAt time.Time) error
	FindAttendances(ctx context.Context, request *models.AttendanceFilter) ([]*models.Attendance, *models.AttendanceSummary, error)
	FindAttendance(ctx context.Context, branchID, attendanceID uuid.UUID) (*models.Attendance, error)
	ClockIn(ctx context.Context, tx pgx.Tx, branchID, staffID uuid.UUID, date, status string, request *models.ClockInRequest, at time.Time) (*models.Clocking, error)
	// ClockOut closes the open attendance of a staff member; nil when there is none.
	ClockOut(ctx context.Context, tx pgx.Tx, branchID, staffID uuid.UUID, photoURL string, at time.Time) (*models.Clocking, error)
	FindHelpAccesses(ctx context.Context, request *models.HelpAccessFilter) ([]*models.HelpAccess, int64, error)
	FindHelpAccess(ctx context.Context, tx pgx.Tx, request *models.HelpAccessFilter) (*models.HelpAccess, error)
	CreateHelpAccess(ctx context.Context, tx pgx.Tx, request *models.HelpAccess) (uuid.UUID, error)
	// RevokeHelpAccess revokes an active access and tells whether there was one.
	RevokeHelpAccess(ctx context.Context, tx pgx.Tx, storeID, helpAccessID uuid.UUID, at time.Time) (bool, error)
	FindAccountOptions(ctx context.Context, storeID uuid.UUID) ([]*models.AccountOption, error)
	// AccountExists tells whether a user that is not deleted belongs to the store.
	AccountExists(ctx context.Context, tx pgx.Tx, storeID, userID uuid.UUID) (bool, error)
}

type staffRepository struct {
	base
}

func NewStaffRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) StaffRepository {
	return &staffRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

const staffColumns = `t.id
	, t.store_id
	, t.name
	, t.email
	, t.phone
	, t.role
	, t.branch_id
	, (SELECT JSON_BUILD_OBJECT('id', r.id, 'name', r.name) FROM branches r WHERE r.id = t.branch_id) AS branch
	, t.address
	, t.status
	, t.last_login_at
	, t.created_at
	, t.updated_at`

// staffSortings maps the sortable fields to their SQL expressions.
var staffSortings = map[string]string{
	"id":            "t.id",
	"created_at":    "t.created_at",
	"updated_at":    "t.updated_at",
	"name":          "t.name",
	"email":         "t.email",
	"phone":         "t.phone",
	"role":          "t.role",
	"branch_id":     "t.branch_id",
	"address":       "t.address",
	"status":        "t.status",
	"last_login_at": "t.last_login_at",
	"branch.name":   "(SELECT r.name FROM branches r WHERE r.id = t.branch_id)",
}

func (q *staffRepository) buildCondition(request *models.StaffFilter) *conditions {
	response := &conditions{}
	response.raw("t.deleted_at IS NULL")
	response.raw("t.role <> 'owner'")
	if len(request.StaffIDs) > 0 {
		response.add("t.id = ANY($%d)", request.StaffIDs)
	}
	if len(request.StoreIDs) > 0 {
		response.add("t.store_id = ANY($%d)", request.StoreIDs)
	}
	if request.Role != nil {
		response.add("t.role = $%d", *request.Role)
	}
	if request.Status != nil {
		response.add("t.status = $%d", *request.Status)
	}
	if request.BranchID != nil {
		response.add("t.branch_id = $%d", *request.BranchID)
	}
	if request.Keyword != "" {
		response.add("(t.name ILIKE $%d OR t.email ILIKE $%d OR t.phone ILIKE $%d)", "%"+request.Keyword+"%")
	}
	return response
}

func (q *staffRepository) FindStaffs(ctx context.Context, request *models.StaffFilter) ([]*models.Staff, int64, error) {
	ctxt := "StaffRepository-FindStaffs"
	condition := q.buildCondition(request)
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1) FROM users t "+condition.where(), condition.params...)
	if err != nil || total == 0 {
		return []*models.Staff{}, 0, err
	}
	response, err := queryRows[models.Staff](ctx, q.dbRead, ctxt, "SELECT "+staffColumns+" FROM users t "+condition.where()+
		orderBy(request.Sortings, staffSortings, "t.name")+paginate(request.Limit, request.Page), condition.params...)
	return response, total, err
}

func (q *staffRepository) FindStaff(ctx context.Context, tx pgx.Tx, request *models.StaffFilter) (*models.Staff, error) {
	ctxt := "StaffRepository-FindStaff"
	condition := q.buildCondition(request)
	response, err := queryRow[models.Staff](ctx, q.reader(tx), ctxt, "SELECT "+staffColumns+" FROM users t "+condition.where()+" LIMIT 1", condition.params...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrStaffNotFound
	}
	return response, err
}

func (q *staffRepository) CreateStaff(ctx context.Context, tx pgx.Tx, request *models.Staff, passwordHash string, pinHash *string) (*models.Staff, error) {
	ctxt := "StaffRepository-CreateStaff"
	id, err := queryValue[uuid.UUID](ctx, tx, ctxt, `INSERT INTO users (
		store_id
		, branch_id
		, name
		, email
		, password_hash
		, phone
		, role
		, ppob_pin_hash
		, address
		, status
		, permissions
		, notifications
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14)
	RETURNING id`,
		request.StoreID,
		request.BranchID,
		request.Name,
		request.Email,
		passwordHash,
		request.Phone,
		request.Role,
		pinHash,
		request.Address,
		request.Status,
		models.DefaultPermissions[request.Role],
		models.DefaultNotifications,
		request.CreatedAt,
		request.UpdatedAt,
	)
	if IsUniqueViolation(err) {
		return nil, models.ErrEmailTaken
	}
	if IsForeignKeyViolation(err) {
		return nil, models.ErrReferenceNotFound
	}
	if err != nil {
		return nil, err
	}
	return q.FindStaff(ctx, tx, models.NewStaffFilter().WithStaffIDs(id))
}

func (q *staffRepository) UpdateStaff(ctx context.Context, tx pgx.Tx, request *models.Staff, passwordHash, pinHash string) (*models.Staff, error) {
	ctxt := "StaffRepository-UpdateStaff"
	current, err := queryRow[models.Staff](ctx, tx, ctxt, `SELECT t.name
		, t.email
		, t.phone
		, t.role
		, t.branch_id
		, t.address
	FROM users t
	WHERE t.id = $1
		AND t.store_id = $2
		AND t.role <> 'owner'
		AND t.deleted_at IS NULL
	FOR NO KEY UPDATE`, request.ID, request.StoreID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrStaffNotFound
	}
	if err != nil {
		return nil, err
	}
	var update changes
	update.set(current.Name != request.Name, "name", request.Name)
	update.set(current.Email != request.Email, "email", request.Email)
	update.set(!equalPtr(current.Phone, request.Phone), "phone", request.Phone)
	update.set(current.Role != request.Role, "role", request.Role)
	update.set(!equalPtr(current.BranchID, request.BranchID), "branch_id", request.BranchID)
	update.set(!equalPtr(current.Address, request.Address), "address", request.Address)
	// a hash differs from the stored one even for the same secret, so a secret that was sent is always written
	update.set(passwordHash != "", "password_hash", passwordHash)
	update.set(pinHash != "", "ppob_pin_hash", pinHash)
	err = update.apply(ctx, tx, ctxt, "users", request.ID, request.UpdatedAt)
	if IsUniqueViolation(err) {
		return nil, models.ErrEmailTaken
	}
	if IsForeignKeyViolation(err) {
		return nil, models.ErrReferenceNotFound
	}
	if err != nil {
		return nil, err
	}
	return q.FindStaff(ctx, tx, models.NewStaffFilter().WithStaffIDs(request.ID))
}

func (q *staffRepository) DeleteStaffs(ctx context.Context, tx pgx.Tx, storeID uuid.UUID, staffIDs []uuid.UUID, deletedAt time.Time) (int64, error) {
	ctxt := "StaffRepository-DeleteStaffs"
	deleted, err := execute(ctx, tx, ctxt, `UPDATE users SET
		deleted_at = $3
		, updated_at = $3
	WHERE id = ANY($1)
		AND store_id = $2
		AND role <> 'owner'
		AND deleted_at IS NULL`, staffIDs, storeID, deletedAt)
	if err != nil {
		return 0, err
	}
	_, err = execute(ctx, tx, ctxt, "UPDATE refresh_tokens SET revoked_at = $2 WHERE user_id = ANY($1) AND revoked_at IS NULL", staffIDs, deletedAt)
	return deleted, err
}

func (q *staffRepository) CountStaffs(ctx context.Context, storeID uuid.UUID) (int64, error) {
	ctxt := "StaffRepository-CountStaffs"
	return queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1) FROM users t WHERE t.store_id = $1 AND t.role <> 'owner' AND t.deleted_at IS NULL", storeID)
}

func (q *staffRepository) FindPermissions(ctx context.Context, tx pgx.Tx, storeID, staffID uuid.UUID) ([]string, error) {
	ctxt := "StaffRepository-FindPermissions"
	response, err := queryValue[[]string](ctx, q.reader(tx), ctxt, `SELECT t.permissions
	FROM users t
	WHERE t.id = $1
		AND t.store_id = $2
		AND t.role <> 'owner'
		AND t.deleted_at IS NULL`, staffID, storeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrStaffNotFound
	}
	return response, err
}

func (q *staffRepository) UpdatePermissions(ctx context.Context, tx pgx.Tx, storeID, staffID uuid.UUID, permissions []string, onlyIfEmpty bool, updatedAt time.Time) error {
	ctxt := "StaffRepository-UpdatePermissions"
	if permissions == nil {
		permissions = []string{}
	}
	_, err := execute(ctx, tx, ctxt, `UPDATE users SET
		permissions = $3
		, updated_at = $5
	WHERE id = $1
		AND store_id = $2
		AND role <> 'owner'
		AND deleted_at IS NULL
		AND permissions <> $3
		AND (NOT $4 OR permissions = '{}')`, staffID, storeID, permissions, onlyIfEmpty, updatedAt)
	return err
}

const attendanceColumns = `t.id
	, t.date::text AS date
	, t.staff_id
	, u.name AS staff_name
	, t.clock_in_at
	, t.clock_out_at
	, COALESCE(ROUND((EXTRACT(EPOCH FROM (COALESCE(t.clock_out_at, t.clock_in_at) - t.clock_in_at)) / 3600)::numeric, 2), 0) AS hours
	, t.status
	, t.location
	, t.clock_in_photo_url
	, t.clock_out_photo_url
	, t.note`

func (q *staffRepository) attendanceCondition(request *models.AttendanceFilter) *conditions {
	response := &conditions{}
	response.add("t.branch_id = $%d", request.BranchID)
	response.add("t.date >= $%d::date", request.StartDate)
	response.add("t.date <= $%d::date", request.EndDate)
	if request.StaffID != nil {
		response.add("t.staff_id = $%d", *request.StaffID)
	}
	return response
}

func (q *staffRepository) FindAttendances(ctx context.Context, request *models.AttendanceFilter) ([]*models.Attendance, *models.AttendanceSummary, error) {
	ctxt := "StaffRepository-FindAttendances"
	condition := q.attendanceCondition(request)
	summary, err := queryRow[models.AttendanceSummary](ctx, q.dbRead, ctxt, `SELECT COUNT(1) AS attendance_count
		, COUNT(1) FILTER (WHERE t.status = 'late') AS late_count
		, COALESCE(ROUND((SUM(EXTRACT(EPOCH FROM (t.clock_out_at - t.clock_in_at))) / 3600)::numeric, 2), 0) AS total_hours
	FROM staff_attendances t `+condition.where(), condition.params...)
	if err != nil {
		return nil, nil, err
	}
	response, err := queryRows[models.Attendance](ctx, q.dbRead, ctxt, "SELECT "+attendanceColumns+`
	FROM staff_attendances t
	JOIN users u ON u.id = t.staff_id `+condition.where()+` ORDER BY t.date DESC, t.clock_in_at DESC`+paginate(request.Limit, request.Page), condition.params...)
	return response, summary, err
}

func (q *staffRepository) FindAttendance(ctx context.Context, branchID, attendanceID uuid.UUID) (*models.Attendance, error) {
	ctxt := "StaffRepository-FindAttendance"
	response, err := queryRow[models.Attendance](ctx, q.dbRead, ctxt, "SELECT "+attendanceColumns+`
	FROM staff_attendances t
	JOIN users u ON u.id = t.staff_id
	WHERE t.id = $1
		AND t.branch_id = $2`, attendanceID, branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrAttendanceNotFound
	}
	return response, err
}

func (q *staffRepository) ClockIn(ctx context.Context, tx pgx.Tx, branchID, staffID uuid.UUID, date, status string, request *models.ClockInRequest, at time.Time) (*models.Clocking, error) {
	ctxt := "StaffRepository-ClockIn"
	return queryRow[models.Clocking](ctx, tx, ctxt, `INSERT INTO staff_attendances (
		branch_id
		, staff_id
		, date
		, clock_in_at
		, clock_in_photo_url
		, location
		, status
		, note
	) VALUES ($1, $2, $3::date, $4, NULLIF($5, ''), NULLIF($6, ''), $7, NULLIF($8, ''))
	RETURNING id, date::text AS date, clock_in_at, status`,
		branchID, staffID, date, at, request.PhotoURL, request.Location, status, request.Note)
}

func (q *staffRepository) ClockOut(ctx context.Context, tx pgx.Tx, branchID, staffID uuid.UUID, photoURL string, at time.Time) (*models.Clocking, error) {
	ctxt := "StaffRepository-ClockOut"
	response, err := queryRow[models.Clocking](ctx, tx, ctxt, `UPDATE staff_attendances SET
		clock_out_at = $4
		, clock_out_photo_url = NULLIF($3, '')
	WHERE id = (
		SELECT t.id
		FROM staff_attendances t
		WHERE t.branch_id = $1
			AND t.staff_id = $2
			AND t.clock_out_at IS NULL
		ORDER BY t.clock_in_at DESC
		LIMIT 1
	)
	RETURNING id, date::text AS date, clock_in_at, clock_out_at, status`, branchID, staffID, photoURL, at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return response, err
}

// helpAccessStatus is the status of a support access at $1: an active one whose time ran out is "expired".
const helpAccessStatus = "(CASE WHEN t.status = 'active' AND t.expires_at < $1 THEN 'expired' ELSE t.status END)"

const helpAccessColumns = `t.id
	, t.store_id
	, t.platform
	, t.account_id
	, (SELECT JSON_BUILD_OBJECT('id', r.id, 'email', r.email) FROM users r WHERE r.id = t.account_id) AS account
	, (SELECT r.email FROM users r WHERE r.id = t.account_id) AS account_email
	, t.duration_hours
	, t.description
	, ` + helpAccessStatus + ` AS status
	, t.expires_at
	, t.created_at
	, t.updated_at`

// helpAccessSortings maps the sortable fields to their SQL expressions.
var helpAccessSortings = map[string]string{
	"id":             "t.id",
	"created_at":     "t.created_at",
	"updated_at":     "t.updated_at",
	"platform":       "t.platform",
	"account_id":     "t.account_id",
	"account_email":  "(SELECT r.email FROM users r WHERE r.id = t.account_id)",
	"account.email":  "(SELECT r.email FROM users r WHERE r.id = t.account_id)",
	"duration_hours": "t.duration_hours",
	"description":    "t.description",
	"status":         helpAccessStatus,
	"expires_at":     "t.expires_at",
}

func (q *staffRepository) helpAccessCondition(request *models.HelpAccessFilter) *conditions {
	response := &conditions{}
	// $1 is the time the status is judged at
	response.add("$%d::timestamptz IS NOT NULL", request.At)
	response.add("t.store_id = $%d", request.StoreID)
	if len(request.HelpAccessIDs) > 0 {
		response.add("t.id = ANY($%d)", request.HelpAccessIDs)
	}
	if request.Status != "" {
		response.add(helpAccessStatus+" = $%d", request.Status)
	}
	return response
}

func (q *staffRepository) FindHelpAccesses(ctx context.Context, request *models.HelpAccessFilter) ([]*models.HelpAccess, int64, error) {
	ctxt := "StaffRepository-FindHelpAccesses"
	condition := q.helpAccessCondition(request)
	total, err := queryValue[int64](ctx, q.dbRead, ctxt, "SELECT COUNT(1) FROM help_accesses t "+condition.where(), condition.params...)
	if err != nil || total == 0 {
		return []*models.HelpAccess{}, 0, err
	}
	response, err := queryRows[models.HelpAccess](ctx, q.dbRead, ctxt, "SELECT "+helpAccessColumns+" FROM help_accesses t "+condition.where()+
		orderBy(request.Sortings, helpAccessSortings, "t.created_at DESC")+paginate(request.Limit, request.Page), condition.params...)
	return response, total, err
}

func (q *staffRepository) FindHelpAccess(ctx context.Context, tx pgx.Tx, request *models.HelpAccessFilter) (*models.HelpAccess, error) {
	ctxt := "StaffRepository-FindHelpAccess"
	condition := q.helpAccessCondition(request)
	response, err := queryRow[models.HelpAccess](ctx, q.reader(tx), ctxt, "SELECT "+helpAccessColumns+" FROM help_accesses t "+condition.where()+" LIMIT 1", condition.params...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrHelpAccessNotFound
	}
	return response, err
}

func (q *staffRepository) CreateHelpAccess(ctx context.Context, tx pgx.Tx, request *models.HelpAccess) (uuid.UUID, error) {
	ctxt := "StaffRepository-CreateHelpAccess"
	return queryValue[uuid.UUID](ctx, tx, ctxt, `INSERT INTO help_accesses (
		store_id
		, platform
		, account_id
		, duration_hours
		, description
		, status
		, expires_at
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
	RETURNING id`,
		request.StoreID,
		request.Platform,
		request.AccountID,
		request.DurationHours,
		request.Description,
		request.Status,
		request.ExpiresAt,
		request.CreatedAt,
	)
}

func (q *staffRepository) RevokeHelpAccess(ctx context.Context, tx pgx.Tx, storeID, helpAccessID uuid.UUID, at time.Time) (bool, error) {
	ctxt := "StaffRepository-RevokeHelpAccess"
	revoked, err := execute(ctx, tx, ctxt, `UPDATE help_accesses SET
		status = 'revoked'
		, revoked_at = $3
		, updated_at = $3
	WHERE id = $1
		AND store_id = $2
		AND status = 'active'`, helpAccessID, storeID, at)
	return revoked > 0, err
}

func (q *staffRepository) FindAccountOptions(ctx context.Context, storeID uuid.UUID) ([]*models.AccountOption, error) {
	ctxt := "StaffRepository-FindAccountOptions"
	return queryRows[models.AccountOption](ctx, q.dbRead, ctxt, `SELECT t.id
		, (CASE WHEN t.role = 'owner' THEN 'Owner' ELSE 'Staff' END) || ' (' || t.email || ')' AS label
		, t.email
		, t.role
	FROM users t
	WHERE t.store_id = $1
		AND t.deleted_at IS NULL
	ORDER BY t.role = 'owner' DESC
		, t.name`, storeID)
}

func (q *staffRepository) AccountExists(ctx context.Context, tx pgx.Tx, storeID, userID uuid.UUID) (bool, error) {
	ctxt := "StaffRepository-AccountExists"
	return queryValue[bool](ctx, q.reader(tx), ctxt, "SELECT EXISTS(SELECT 1 FROM users t WHERE t.id = $1 AND t.store_id = $2)", userID, storeID)
}
