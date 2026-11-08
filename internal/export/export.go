// Package export writes tabular data as XLSX, CSV or PDF and reads spreadsheets back.
package export

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/xuri/excelize/v2"
)

// Column describes one exported column; Key may be dotted ("category.name").
type Column struct {
	Key    string
	Label  string
	Format string // money | number | percent | date | datetime | boolean | text
}

// Value resolves a dotted key in a row.
func Value(row map[string]any, key string) any {
	var cur any = row
	for _, part := range strings.Split(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func text(v any, format string) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		if format == "date" && len(x) >= 10 {
			return x[:10]
		}
		if format == "datetime" {
			if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
				return t.Format("2006-01-02 15:04")
			}
		}
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "Yes"
		}
		return "No"
	case []any, map[string]any:
		b, _ := json.Marshal(x)
		return string(b)
	}
	return fmt.Sprint(v)
}

// XLSX renders rows into a workbook with a bold header row.
func XLSX(title string, cols []Column, rows []map[string]any) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Sheet1"
	if title != "" {
		name := title
		if len(name) > 31 {
			name = name[:31]
		}
		_ = f.SetSheetName("Sheet1", name)
		sheet = name
	}
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"E8F3EF"}, Pattern: 1}})
	money, _ := f.NewStyle(&excelize.Style{NumFmt: 3})
	for i, c := range cols {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, c.Label)
		_ = f.SetCellStyle(sheet, cell, cell, bold)
		colName, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, colName, colName, float64(max(12, min(40, len(c.Label)+4))))
	}
	for r, row := range rows {
		for i, c := range cols {
			cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
			v := Value(row, c.Key)
			if n, ok := v.(float64); ok && c.Format != "text" {
				_ = f.SetCellValue(sheet, cell, n)
				if c.Format == "money" {
					_ = f.SetCellStyle(sheet, cell, cell, money)
				}
				continue
			}
			_ = f.SetCellValue(sheet, cell, text(v, c.Format))
		}
	}
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func CSV(cols []Column, rows []map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf") // BOM so Excel opens UTF-8 correctly
	w := csv.NewWriter(&buf)
	head := make([]string, len(cols))
	for i, c := range cols {
		head[i] = c.Label
	}
	_ = w.Write(head)
	for _, row := range rows {
		rec := make([]string, len(cols))
		for i, c := range cols {
			rec[i] = text(Value(row, c.Key), c.Format)
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// TablePDF renders rows as a simple landscape table.
func TablePDF(title, subtitle string, cols []Column, rows []map[string]any) []byte {
	d := NewPDF(842, 595)
	const margin = 36.0
	usable := 842 - 2*margin
	colW := usable / float64(max(1, len(cols)))
	fontSize := 8.0
	if len(cols) > 10 {
		fontSize = 6.5
	}
	y := 0.0
	header := func() {
		d.Page()
		d.Text(margin, 560, 14, true, title)
		if subtitle != "" {
			d.Text(margin, 544, 9, false, subtitle)
		}
		y = 522
		d.Rect(margin, y-4, usable, 14, 0.91)
		for i, c := range cols {
			d.Text(margin+float64(i)*colW+2, y, fontSize, true, Fit(c.Label, colW, fontSize))
		}
		y -= 16
	}
	header()
	for _, row := range rows {
		if y < margin {
			header()
		}
		for i, c := range cols {
			s := text(Value(row, c.Key), c.Format)
			if n, ok := Value(row, c.Key).(float64); ok && (c.Format == "money" || c.Format == "number") {
				s = Thousands(n)
			}
			d.Text(margin+float64(i)*colW+2, y, fontSize, false, Fit(s, colW, fontSize))
		}
		y -= fontSize + 5
	}
	if len(rows) == 0 {
		d.Text(margin, y, 9, false, "No data.")
	}
	return d.Bytes()
}

// Thousands formats 1234567.5 as "1.234.567,5" (Indonesian style).
func Thousands(n float64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	whole := int64(n)
	frac := n - float64(whole)
	s := strconv.FormatInt(whole, 10)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, s[i])
	}
	res := string(out)
	if frac > 0.004 {
		res += "," + strings.TrimRight(strconv.FormatFloat(frac, 'f', 2, 64)[2:], "0")
	}
	if neg {
		res = "-" + res
	}
	return res
}

// Fit truncates s so it fits a column of width w at the given font size (Helvetica, approx.).
func Fit(s string, w, size float64) string {
	maxChars := int(w / (size * 0.52))
	r := []rune(s)
	if len(r) <= maxChars || maxChars < 2 {
		return s
	}
	return string(r[:maxChars-1]) + "…"
}

// Send writes a file response with Content-Disposition.
func Send(c fiber.Ctx, filename, contentType string, body []byte) error {
	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return c.Send(body)
}

// Write picks the format from ?format= (xlsx default) and sends the file.
func Write(c fiber.Ctx, name, title string, cols []Column, rows []map[string]any) error {
	switch c.Query("format", "xlsx") {
	case "csv":
		b, err := CSV(cols, rows)
		if err != nil {
			return err
		}
		return Send(c, name+".csv", "text/csv; charset=utf-8", b)
	case "pdf":
		sub := ""
		if s, e := c.Query("start_date"), c.Query("end_date"); s != "" {
			sub = "Period " + s + " – " + e
		}
		return Send(c, name+".pdf", "application/pdf", TablePDF(title, sub, cols, rows))
	default:
		b, err := XLSX(title, cols, rows)
		if err != nil {
			return err
		}
		return Send(c, name+".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", b)
	}
}

// ReadTable parses an uploaded .xlsx or .csv into header + rows of strings.
func ReadTable(filename string, r io.Reader) ([]string, [][]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	var all [][]string
	if strings.HasSuffix(strings.ToLower(filename), ".csv") {
		cr := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
		cr.FieldsPerRecord = -1
		all, err = cr.ReadAll()
		if err != nil {
			return nil, nil, fmt.Errorf("read CSV: %w", err)
		}
	} else {
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			return nil, nil, fmt.Errorf("read spreadsheet: %w", err)
		}
		defer f.Close()
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			return nil, nil, fmt.Errorf("the spreadsheet is empty")
		}
		all, err = f.GetRows(sheets[0])
		if err != nil {
			return nil, nil, err
		}
	}
	if len(all) == 0 {
		return nil, nil, fmt.Errorf("the file has no header row")
	}
	head := make([]string, len(all[0]))
	for i, h := range all[0] {
		head[i] = strings.TrimSpace(strings.ToLower(h))
	}
	return head, all[1:], nil
}
