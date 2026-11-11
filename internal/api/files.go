package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/repositories"
)

var allowedUploads = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif", "image/svg+xml": ".svg",
	"application/pdf": ".pdf",
}

func (a *API) registerFiles() {
	a.addH("POST", "/uploads", a.upload)
}

// saveUpload stores the multipart field `field` and returns its public URL.
func (a *API) saveUpload(c fiber.Ctx, field, purpose string) (map[string]any, error) {
	fh, err := c.FormFile(field)
	if err != nil {
		return nil, helper.Invalid(field, "Choose a file to upload.")
	}
	if fh.Size > 10<<20 {
		return nil, helper.Invalid(field, "The file is larger than 10 MB.")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := f.Read(head)
	mime := http.DetectContentType(head[:n])
	if strings.HasSuffix(strings.ToLower(fh.Filename), ".svg") && strings.Contains(string(head[:n]), "<svg") {
		mime = "image/svg+xml"
	}
	ext, ok := allowedUploads[mime]
	if !ok {
		return nil, helper.Invalid(field, "Only images (JPG, PNG, WebP, GIF, SVG) and PDF files are allowed.")
	}
	p := helper.P(c)
	rel := filepath.Join(p.StoreID, middlewares.RandomToken(16)+ext)
	dst := filepath.Join(a.Cfg.UploadDir, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return nil, err
	}
	if err := c.SaveFile(fh, dst); err != nil {
		return nil, err
	}
	url := a.Cfg.PublicURL + "/uploads/" + filepath.ToSlash(rel)
	var id string
	if err := a.DB.QueryRow(c.Context(), `INSERT INTO uploads (store_id, path, url, mime_type, size, purpose) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		p.StoreID, rel, url, mime, fh.Size, purpose).Scan(&id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "url": url, "mime_type": mime, "size": fh.Size}, nil
}

func (a *API) upload(c fiber.Ctx) error {
	row, err := a.saveUpload(c, "file", c.FormValue("purpose"))
	if err != nil {
		return err
	}
	return helper.Created(c, row)
}

// ---------- regions ----------

func (a *API) registerRegions() {
	a.addH("GET", "/regions/provinces", func(c fiber.Ctx) error {
		raw, err := repositories.JSONRaw(c.Context(), a.DB, `SELECT id, name FROM provinces WHERE country_code = $1 AND ($2 = '' OR name ILIKE '%'||$2||'%') ORDER BY name`,
			c.Query("country_code", "ID"), c.Query("q"))
		if err != nil {
			return err
		}
		return helper.OK(c, raw)
	})
	a.addH("GET", "/regions/cities", func(c fiber.Ctx) error {
		pid := helper.QueryUUID(c, "province_id")
		raw, err := repositories.JSONRaw(c.Context(), a.DB, `SELECT id, name, province_id FROM cities WHERE ($1::uuid IS NULL OR province_id = $1::uuid) AND ($2 = '' OR name ILIKE '%'||$2||'%') ORDER BY name`,
			pid, c.Query("q"))
		if err != nil {
			return err
		}
		return helper.OK(c, raw)
	})
}

// ---------- notifications ----------

func (a *API) registerNotifications() {
	a.addH("GET", "/notifications", a.listNotifications)
	a.addH("GET", "/notifications/unread-count", func(c fiber.Ctx) error {
		p := helper.P(c)
		var n int
		if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM notifications WHERE store_id=$1 AND (branch_id IS NULL OR branch_id=$2) AND read_at IS NULL`, p.StoreID, p.BranchID).Scan(&n); err != nil {
			return err
		}
		return helper.OK(c, fiber.Map{"count": n})
	})
	a.addH("POST", "/notifications/read-all", func(c fiber.Ctx) error {
		p := helper.P(c)
		tag, err := a.DB.Exec(c.Context(), `UPDATE notifications SET read_at = now() WHERE store_id=$1 AND (branch_id IS NULL OR branch_id=$2) AND read_at IS NULL`, p.StoreID, p.BranchID)
		if err != nil {
			return err
		}
		return helper.OK(c, fiber.Map{"updated": tag.RowsAffected()})
	})
	a.addH("GET", "/notifications/:id", func(c fiber.Ctx) error {
		id, err := helper.ParamID(c, "id")
		if err != nil {
			return err
		}
		row, err := a.notification(c, id)
		if err != nil {
			return err
		}
		return helper.OK(c, row)
	})
	a.addH("POST", "/notifications/:id/read", func(c fiber.Ctx) error {
		id, err := helper.ParamID(c, "id")
		if err != nil {
			return err
		}
		p := helper.P(c)
		if _, err := a.DB.Exec(c.Context(), `UPDATE notifications SET read_at = COALESCE(read_at, now()) WHERE id=$1 AND store_id=$2`, id, p.StoreID); err != nil {
			return err
		}
		row, err := a.notification(c, id)
		if err != nil {
			return err
		}
		return helper.OK(c, row)
	})
}

const notificationCols = `id, title, message, params, source, link_route, attachment_url, read_at, created_at`

func (a *API) notification(c fiber.Ctx, id string) (map[string]any, error) {
	p := helper.P(c)
	row, err := repositories.JSONRow(c.Context(), a.DB, `SELECT `+notificationCols+` FROM notifications WHERE id=$1 AND store_id=$2`, id, p.StoreID)
	if err == nil && row == nil {
		err = helper.NotFound("Notification")
	}
	if row != nil {
		localize(helper.Lang(c), row, "title", "message")
	}
	return row, err
}

func (a *API) listNotifications(c fiber.Ctx) error {
	p := helper.P(c)
	pg := helper.Paging(c, 25)
	where := `store_id=$1 AND (branch_id IS NULL OR branch_id=$2) AND ($3 = '' OR source = $3)
		AND (NULLIF($4,'') IS NULL OR created_at::date >= NULLIF($4,'')::date) AND (NULLIF($5,'') IS NULL OR created_at::date <= NULLIF($5,'')::date)`
	args := []any{p.StoreID, p.BranchID, c.Query("source"), c.Query("start_date"), c.Query("end_date")}
	var total int
	if err := a.DB.QueryRow(c.Context(), `SELECT count(*) FROM notifications WHERE `+where, args...).Scan(&total); err != nil {
		return err
	}
	rows, err := repositories.JSONRows(c.Context(), a.DB, `SELECT `+notificationCols+` FROM notifications WHERE `+where+` ORDER BY created_at DESC LIMIT `+strconv.Itoa(pg.PerPage)+` OFFSET `+strconv.Itoa(pg.Offset), args...)
	if err != nil {
		return err
	}
	for _, r := range rows {
		localize(helper.Lang(c), r, "title", "message")
	}
	return helper.List(c, rows, helper.NewMeta(pg, total))
}
