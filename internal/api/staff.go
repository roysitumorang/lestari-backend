package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/internal/crud"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/repositories"
)

var permissionList = []string{"dashboard", "products", "stock", "customers", "suppliers", "discounts", "taxes", "sales", "purchases", "finance", "reports", "debts",
	"returns", "settings", "staff", "branches", "ppob", "online_store", "void_transaction", "edit_price", "give_discount"}

var defaultPermissions = map[string][]string{
	"administrator": permissionList,
	"manager":       {"dashboard", "products", "stock", "customers", "suppliers", "discounts", "taxes", "sales", "purchases", "finance", "reports", "debts", "returns", "ppob", "online_store", "give_discount"},
	"cashier":       {"sales", "customers", "give_discount"},
}

const includedStaff = 2
const staffMonthly = 15000.0

func requireManager(h *crud.Ctx) error {
	if h.P.Role != "owner" && h.P.Role != "administrator" {
		return helper.Forbidden("Only the owner or an administrator can manage staff.")
	}
	return nil
}

func (a *API) staffResource() *crud.Resource {
	return &crud.Resource{
		Name: "Staff", Singular: "Staff member", Table: "users", SoftDelete: true, DefaultSort: "t.name",
		Scope: func(b *crud.Builder, p *helper.Principal) string {
			return "t.store_id = " + b.Add(p.StoreID) + " AND t.role <> 'owner'"
		},
		InsertScope: storeInsert,
		Fields: []crud.Field{
			{Name: "name", Required: true, Search: true},
			{Name: "email", Required: true, Search: true},
			{Name: "password", Kind: crud.Password, Col: "password_hash", Required: true},
			{Name: "phone", Search: true},
			{Name: "role", Required: true, Enum: en("administrator", "manager", "cashier"), Default: "cashier", Filter: true},
			{Name: "ppob_pin", Kind: crud.PIN, Col: "ppob_pin_hash"},
			{Name: "branch_id", Kind: crud.UUID, Filter: true, Ref: &crud.Ref{Key: "branch", Table: "branches", ScopeCol: "store_id"}},
			{Name: "address"},
			{Name: "status", ReadOnly: true, Filter: true},
			{Name: "last_login_at", Kind: crud.Timestamp, ReadOnly: true},
		},
		BeforeWrite: func(ctx context.Context, h *crud.Ctx, vals map[string]any, id string) error {
			if err := requireManager(h); err != nil {
				return err
			}
			if e, ok := vals["email"].(string); ok {
				e = strings.ToLower(e)
				if !strings.Contains(e, "@") {
					return helper.Invalid("email", "Enter a valid email.")
				}
				vals["email"] = e
			}
			if pin, ok := vals["ppob_pin_hash"]; ok && pin != nil {
				if raw := helper.Str(h.Body, "ppob_pin"); len(raw) != 6 || strings.Trim(raw, "0123456789") != "" {
					return helper.Invalid("ppob_pin", "The PIN must be 6 digits.")
				}
			}
			if id == "" {
				vals["status"] = "invited"
				if vals["branch_id"] == nil {
					vals["branch_id"] = h.P.BranchID
				}
				role, _ := vals["role"].(string)
				vals["permissions"] = defaultPermissions[role]
				vals["notifications"] = defaultNotifications
			}
			return nil
		},
		AfterWrite: func(ctx context.Context, h *crud.Ctx, id string, created bool) error {
			if created {
				return nil
			}
			// a role change resets permissions to that role's defaults
			if role := helper.Str(h.Body, "role"); role != "" {
				_, err := h.Tx.Exec(ctx, `UPDATE users SET permissions=$2 WHERE id=$1 AND (permissions = '{}' OR $3)`, id, defaultPermissions[role], helper.Bool(h.Body, "reset_permissions"))
				return err
			}
			return nil
		},
		BeforeDelete: func(ctx context.Context, h *crud.Ctx, id string) error {
			if err := requireManager(h); err != nil {
				return err
			}
			if id == h.P.UserID {
				return helper.Conflict("You cannot delete your own account.")
			}
			_, err := h.Tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
			return err
		},
	}
}

func (a *API) registerStaff() {
	staff := a.staffResource()
	a.CRUD.Register(a.addH, "/staff", staff, "list", "create", "read", "update", "delete", "bulk_delete")
	a.addH("GET", "/staff/billing-estimate", func(c fiber.Ctx) error {
		extra, _ := strconv.Atoi(c.Query("additional_staff", "1"))
		var current int
		if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM users WHERE store_id=$1 AND role <> 'owner' AND deleted_at IS NULL`, helper.P(c).StoreID).Scan(&current); err != nil {
			return err
		}
		billable := max(current+extra-includedStaff, 0)
		return helper.OK(c, fiber.Map{"monthly_cost": float64(billable) * staffMonthly, "currency": "IDR", "included_staff": includedStaff, "current_staff": current})
	})
	a.addH("GET", "/staff/:id/permissions", func(c fiber.Ctx) error {
		id, err := helper.ParamID(c, "id")
		if err != nil {
			return err
		}
		var perms []string
		if err := a.DB.QueryRow(c.Context(), `SELECT permissions FROM users WHERE id=$1 AND store_id=$2 AND role <> 'owner' AND deleted_at IS NULL`, id, helper.P(c).StoreID).Scan(&perms); err != nil {
			return helper.NotFound("Staff member")
		}
		avail := make([]fiber.Map, len(permissionList))
		for i, p := range permissionList {
			avail[i] = fiber.Map{"value": p, "label": strings.ReplaceAll(p, "_", " ")}
		}
		return helper.OK(c, fiber.Map{"permissions": perms, "available": avail})
	})
	a.addH("PUT", "/staff/:id/permissions", func(c fiber.Ctx) error {
		id, err := helper.ParamID(c, "id")
		if err != nil {
			return err
		}
		p := helper.P(c)
		if p.Role != "owner" && p.Role != "administrator" {
			return helper.Forbidden("Only the owner or an administrator can change access rights.")
		}
		var in struct {
			Permissions []string `json:"permissions"`
		}
		if err := helper.Decode(c, &in); err != nil {
			return err
		}
		for _, x := range in.Permissions {
			if !contains(permissionList, x) {
				return helper.Invalid("permissions", "Unknown permission {permission}.", "permission", x)
			}
		}
		if in.Permissions == nil {
			in.Permissions = []string{}
		}
		tag, err := a.DB.Exec(c.Context(), `UPDATE users SET permissions=$3, updated_at=now() WHERE id=$1 AND store_id=$2 AND role <> 'owner' AND deleted_at IS NULL`, id, p.StoreID, in.Permissions)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return helper.NotFound("Staff member")
		}
		a.activity(c, "permissions_changed", "Changed access rights of staff {id}", "id", id)
		return helper.OK(c, fiber.Map{"permissions": in.Permissions})
	})
	a.addH("POST", "/staff/:id/resend-invitation", func(c fiber.Ctx) error {
		id, err := helper.ParamID(c, "id")
		if err != nil {
			return err
		}
		var email, name, status string
		if err := a.DB.QueryRow(c.Context(), `SELECT email, name, status FROM users WHERE id=$1 AND store_id=$2 AND role <> 'owner' AND deleted_at IS NULL`, id, helper.P(c).StoreID).Scan(&email, &name, &status); err != nil {
			return helper.NotFound("Staff member")
		}
		if status != "invited" {
			return helper.Conflict("This staff member has already signed in.")
		}
		if err := a.Sender.Send(c.Context(), "email", email, helper.T(c, "Hi {name}, you have been invited to Lestari. Sign in with this email address and the password your manager gave you.", "name", name)); err != nil {
			return err
		}
		return helper.OK(c, fiber.Map{"sent": true})
	})
	a.addH("GET", "/staff/:id/activity-logs", func(c fiber.Ctx) error {
		id, err := helper.ParamID(c, "id")
		if err != nil {
			return err
		}
		var ok bool
		if err := a.DB.QueryRow(c.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND store_id=$2)`, id, helper.P(c).StoreID).Scan(&ok); err != nil || !ok {
			return helper.NotFound("Staff member")
		}
		return a.activityList(c, id)
	})

	// attendance
	gate := middlewares.RequirePlugin("attendance")
	a.add("GET", "/staff-attendances", gate, a.listAttendance)
	a.add("GET", "/staff-attendances/export", gate, a.exportAttendance)
	a.add("GET", "/staff-attendances/:id", gate, a.getAttendance)
	a.add("POST", "/staff-attendances/clock-in", gate, a.clockIn)   // cashier app
	a.add("POST", "/staff-attendances/clock-out", gate, a.clockOut) // cashier app

	a.registerHelpAccess()
}

// ---------- attendance ----------

const attendanceCols = `a.id, a.date, a.staff_id, u.name AS staff_name, a.clock_in_at, a.clock_out_at,
	round((EXTRACT(EPOCH FROM (COALESCE(a.clock_out_at, a.clock_in_at) - a.clock_in_at)) / 3600)::numeric, 2) AS hours,
	a.status, a.location, a.clock_in_photo_url, a.clock_out_photo_url, a.note`

func (a *API) attendanceWhere(c fiber.Ctx) (string, []any, error) {
	start, end, err := helper.DateRange(c, a.Loc)
	if err != nil {
		return "", nil, err
	}
	staff := helper.QueryUUID(c, "staff_id")
	return `a.branch_id=$1 AND a.date BETWEEN $2::date AND $3::date AND ($4::uuid IS NULL OR a.staff_id = $4::uuid)`, []any{helper.P(c).BranchID, start, end, staff}, nil
}

func (a *API) listAttendance(c fiber.Ctx) error {
	where, args, err := a.attendanceWhere(c)
	if err != nil {
		return err
	}
	pg := helper.Paging(c, 50)
	summary, err := repositories.JSONRow(c.Context(), a.DB, `SELECT count(*) AS attendance_count, count(*) FILTER (WHERE a.status='late') AS late_count,
		COALESCE(round((sum(EXTRACT(EPOCH FROM (a.clock_out_at - a.clock_in_at))) / 3600)::numeric, 2), 0) AS total_hours
		FROM staff_attendances a WHERE `+where, args...)
	if err != nil {
		return err
	}
	rows, err := repositories.JSONRaw(c.Context(), a.DB, `SELECT `+attendanceCols+` FROM staff_attendances a JOIN users u ON u.id = a.staff_id WHERE `+where+
		fmt.Sprintf(` ORDER BY a.date DESC, a.clock_in_at DESC LIMIT %d OFFSET %d`, pg.PerPage, pg.Offset), args...)
	if err != nil {
		return err
	}
	return helper.ListWith(c, rows, helper.NewMeta(pg, int(num(summary["attendance_count"]))), helper.Extras{Summary: summary})
}

func (a *API) exportAttendance(c fiber.Ctx) error {
	where, args, err := a.attendanceWhere(c)
	if err != nil {
		return err
	}
	rows, err := repositories.JSONRows(c.Context(), a.DB, `SELECT `+attendanceCols+` FROM staff_attendances a JOIN users u ON u.id = a.staff_id WHERE `+where+` ORDER BY a.date DESC`, args...)
	if err != nil {
		return err
	}
	return export.Write(c, "attendance", "Staff attendance", []export.Column{{Key: "date", Label: "Date", Format: "date"}, {Key: "staff_name", Label: "Staff"},
		{Key: "clock_in_at", Label: "In", Format: "datetime"}, {Key: "clock_out_at", Label: "Out", Format: "datetime"}, {Key: "hours", Label: "Hours", Format: "number"},
		{Key: "status", Label: "Status"}, {Key: "location", Label: "Location"}}, rows)
}

func (a *API) getAttendance(c fiber.Ctx) error {
	id, err := helper.ParamID(c, "id")
	if err != nil {
		return err
	}
	row, err := repositories.JSONRow(c.Context(), a.DB, `SELECT `+attendanceCols+` FROM staff_attendances a JOIN users u ON u.id = a.staff_id WHERE a.id=$1 AND a.branch_id=$2`, id, helper.P(c).BranchID)
	if err != nil {
		return err
	}
	if row == nil {
		return helper.NotFound("Attendance")
	}
	return helper.OK(c, row)
}

func (a *API) clockIn(c fiber.Ctx) error {
	body, err := helper.Body(c)
	if err != nil {
		return err
	}
	p := helper.P(c)
	now := a.now()
	status := "present"
	if start := helper.Str(body, "shift_start"); start != "" {
		if t, err := time.ParseInLocation("15:04", start, a.Loc); err == nil && now.Hour()*60+now.Minute() > t.Hour()*60+t.Minute()+10 {
			status = "late"
		}
	}
	row, err := repositories.JSONRow(c.Context(), a.DB, `INSERT INTO staff_attendances (branch_id, staff_id, date, clock_in_at, clock_in_photo_url, location, status, note)
		VALUES ($1,$2,$3::date,now(),NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,'')) RETURNING id, date, clock_in_at, status`,
		p.BranchID, p.UserID, now.Format("2006-01-02"), helper.Str(body, "photo_url"), helper.Str(body, "location"), status, helper.Str(body, "note"))
	if err != nil {
		return err
	}
	return helper.Created(c, row)
}

func (a *API) clockOut(c fiber.Ctx) error {
	body, err := helper.Body(c)
	if err != nil {
		return err
	}
	p := helper.P(c)
	row, err := repositories.JSONRow(c.Context(), a.DB, `UPDATE staff_attendances SET clock_out_at=now(), clock_out_photo_url=NULLIF($3,'')
		WHERE id = (SELECT id FROM staff_attendances WHERE branch_id=$1 AND staff_id=$2 AND clock_out_at IS NULL ORDER BY clock_in_at DESC LIMIT 1)
		RETURNING id, date, clock_in_at, clock_out_at, status`, p.BranchID, p.UserID, helper.Str(body, "photo_url"))
	if err != nil {
		return err
	}
	if row == nil {
		return helper.Conflict("You have not clocked in.")
	}
	return helper.OK(c, row)
}

// ---------- support access ----------

func (a *API) registerHelpAccess() {
	res := &crud.Resource{
		Name: "Support access", Singular: "Support access", Table: "help_accesses", DefaultSort: "t.created_at DESC",
		Scope: storeScope, InsertScope: storeInsert,
		Fields: []crud.Field{
			{Name: "platform", Required: true, Enum: en("web", "app"), Default: "web"},
			{Name: "account_id", Kind: crud.UUID, Required: true, Ref: &crud.Ref{Key: "account", Table: "users", Label: "email", ScopeCol: "store_id"}},
			{Name: "account_email", Expr: "(SELECT email FROM users WHERE id = t.account_id)"},
			{Name: "duration_hours", Kind: crud.Int, Required: true, Enum: en("1", "6", "24", "72")},
			{Name: "description", Required: true},
			{Name: "status", Expr: "(CASE WHEN t.status = 'active' AND t.expires_at < now() THEN 'expired' ELSE t.status END)", Filter: true},
			{Name: "expires_at", Kind: crud.Timestamp, ReadOnly: true},
		},
		BeforeWrite: func(ctx context.Context, h *crud.Ctx, vals map[string]any, id string) error {
			if id != "" {
				return helper.Conflict("Support access cannot be edited; revoke it and create a new one.")
			}
			if !h.P.IsOwner() && h.P.Role != "administrator" {
				return helper.Forbidden("Only the owner or an administrator can grant support access.")
			}
			hours, _ := vals["duration_hours"].(int64)
			vals["expires_at"] = time.Now().Add(time.Duration(hours) * time.Hour)
			vals["status"] = "active"
			return nil
		},
	}
	a.CRUD.Register(a.addH, "/help-access", res, "list", "create", "read")
	a.addH("POST", "/help-access/:id/revoke", func(c fiber.Ctx) error {
		id, err := helper.ParamID(c, "id")
		if err != nil {
			return err
		}
		p := helper.P(c)
		tag, err := a.DB.Exec(c.Context(), `UPDATE help_accesses SET status='revoked', revoked_at=now(), updated_at=now() WHERE id=$1 AND store_id=$2 AND status='active'`, id, p.StoreID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return helper.Conflict("This access is no longer active.")
		}
		row, err := a.CRUD.Fetch(c.Context(), a.DB, res, p, id)
		if err != nil {
			return err
		}
		return helper.OK(c, row)
	})
	a.addH("GET", "/help-access/accounts", func(c fiber.Ctx) error {
		raw, err := repositories.JSONRaw(c.Context(), a.DB, `SELECT id, (CASE WHEN role='owner' THEN 'Owner' ELSE 'Staff' END) || ' (' || email || ')' AS label, email, role
			FROM users WHERE store_id=$1 AND deleted_at IS NULL ORDER BY role='owner' DESC, name`, helper.P(c).StoreID)
		if err != nil {
			return err
		}
		return helper.OK(c, raw)
	})
}
