package sanitizers

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
)

// ImportRows reads the uploaded "file" form field (.xlsx or .csv) into rows keyed by the header names (the "*"
// that marks a required column is dropped). Empty rows are skipped.
func ImportRows(c fiber.Ctx) ([]map[string]any, error) {
	fh, err := c.FormFile("file")
	if err != nil {
		return nil, helper.Invalid("file", "Choose a .xlsx or .csv file.")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head, rows, err := export.ReadTable(fh.Filename, f)
	if err != nil {
		return nil, helper.Invalid("file", err.Error())
	}
	response := make([]map[string]any, 0, len(rows))
	for _, rec := range rows {
		m := map[string]any{}
		for i, h := range head {
			h = strings.TrimSuffix(h, "*")
			if h == "" || i >= len(rec) {
				continue
			}
			if v := strings.TrimSpace(rec[i]); v != "" {
				m[h] = v
			}
		}
		if len(m) > 0 {
			response = append(response, m)
		}
	}
	return response, nil
}

// ImportMode is "create" (default) or "update" (match rows by their key column).
func ImportMode(c fiber.Ctx) string {
	return c.FormValue("mode", "create")
}
