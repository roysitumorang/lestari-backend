package export

import (
	"bytes"
	"fmt"
	"strings"
)

// PDF is a tiny PDF 1.4 writer: Helvetica text, filled rectangles and multiple pages.
// It is enough for receipts, invoices, reports and barcode sheets without extra dependencies.
type PDF struct {
	W, H  float64
	pages []*bytes.Buffer
	cur   *bytes.Buffer
}

func NewPDF(w, h float64) *PDF { return &PDF{W: w, H: h} }

// Page starts a new page.
func (p *PDF) Page() {
	p.cur = &bytes.Buffer{}
	p.pages = append(p.pages, p.cur)
}

func (p *PDF) ensure() {
	if p.cur == nil {
		p.Page()
	}
}

// Text draws a single line at (x, y) measured from the bottom-left corner.
func (p *PDF) Text(x, y, size float64, bold bool, s string) {
	p.ensure()
	font := "F1"
	if bold {
		font = "F2"
	}
	fmt.Fprintf(p.cur, "BT /%s %.2f Tf %.2f %.2f Td (%s) Tj ET\n", font, size, x, y, escape(s))
}

// TextRight draws text right-aligned at x.
func (p *PDF) TextRight(x, y, size float64, bold bool, s string) {
	p.Text(x-TextWidth(s, size), y, size, bold, s)
}

// Rect fills a rectangle with a gray level (0 = black, 1 = white).
func (p *PDF) Rect(x, y, w, h, gray float64) {
	p.ensure()
	fmt.Fprintf(p.cur, "%.3f g %.2f %.2f %.2f %.2f re f 0 g\n", gray, x, y, w, h)
}

// Line draws a thin horizontal line.
func (p *PDF) Line(x1, y, x2 float64) {
	p.ensure()
	fmt.Fprintf(p.cur, "0.6 G 0.5 w %.2f %.2f m %.2f %.2f l S 0 G\n", x1, y, x2, y)
}

// TextWidth approximates Helvetica width.
func TextWidth(s string, size float64) float64 { return float64(len([]rune(s))) * size * 0.5 }

func escape(s string) string {
	// WinAnsi-ish: replace characters Helvetica cannot show
	r := strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`, "–", "-", "—", "-", "…", "...", "’", "'", "“", `"`, "”", `"`, "★", "*")
	s = r.Replace(s)
	var b strings.Builder
	for _, ch := range s {
		if ch < 32 {
			continue
		}
		if ch > 126 {
			if ch <= 255 {
				fmt.Fprintf(&b, `\%03o`, ch)
			} else {
				b.WriteByte('?')
			}
			continue
		}
		b.WriteRune(ch)
	}
	return b.String()
}

// Bytes assembles the document.
func (p *PDF) Bytes() []byte {
	p.ensure()
	var out bytes.Buffer
	offsets := []int{}
	obj := func(body string) {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	n := len(p.pages)
	// 1 catalog, 2 pages, 3 font regular, 4 font bold, then page/content pairs
	kids := make([]string, n)
	for i := range p.pages {
		kids[i] = fmt.Sprintf("%d 0 R", 5+i*2)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	for i, pg := range p.pages {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R >>", p.W, p.H, 6+i*2))
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", pg.Len(), pg.String()))
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return out.Bytes()
}

// ---------- barcodes ----------

var code128Patterns = []string{
	"212222", "222122", "222221", "121223", "121322", "131222", "122213", "122312", "132212", "221213",
	"221312", "231212", "112232", "122132", "122231", "113222", "123122", "123221", "223211", "221132",
	"221231", "213212", "223112", "312131", "311222", "321122", "321221", "312212", "322112", "322211",
	"212123", "212321", "232121", "111323", "131123", "131321", "112313", "132113", "132311", "211313",
	"231113", "231311", "112133", "112331", "132131", "113123", "113321", "133121", "313121", "211331",
	"231131", "213113", "213311", "213131", "311123", "311321", "331121", "312113", "312311", "332111",
	"314111", "221411", "431111", "111224", "111422", "121124", "121421", "141122", "141221", "112214",
	"112412", "122114", "122411", "142112", "142211", "241211", "221114", "413111", "241112", "134111",
	"111242", "121142", "121241", "114212", "124112", "124211", "411212", "421112", "421211", "212141",
	"214121", "412121", "111143", "111341", "131141", "114113", "114311", "411113", "411311", "113141",
	"114131", "311141", "411131", "211412", "211214", "211232", "2331112",
}

// Code128 returns bar/space module widths for Code 128-B (printable ASCII).
func Code128(s string) []int {
	const startB, stop = 104, 106
	codes := []int{startB}
	sum := startB
	i := 1
	for _, ch := range s {
		v := int(ch) - 32
		if v < 0 || v > 94 {
			v = 31 // '?'
		}
		codes = append(codes, v)
		sum += v * i
		i++
	}
	codes = append(codes, sum%103, stop)
	var widths []int
	for _, c := range codes {
		for _, d := range code128Patterns[c] {
			widths = append(widths, int(d-'0'))
		}
	}
	return widths
}

// Barcode draws a Code 128 barcode with its bottom-left corner at (x, y).
func (p *PDF) Barcode(x, y, w, h float64, value string) {
	widths := Code128(value)
	total := 0
	for _, m := range widths {
		total += m
	}
	unit := w / float64(total)
	cx := x
	for i, m := range widths {
		if i%2 == 0 {
			p.Rect(cx, y, float64(m)*unit, h, 0)
		}
		cx += float64(m) * unit
	}
}

// Matrix draws a boolean module matrix (e.g. a QR code) as squares.
func (p *PDF) Matrix(x, y, size float64, m [][]bool) {
	if len(m) == 0 {
		return
	}
	cell := size / float64(len(m))
	for r, row := range m {
		for c, on := range row {
			if on {
				p.Rect(x+float64(c)*cell, y+size-float64(r+1)*cell, cell, cell, 0)
			}
		}
	}
}
