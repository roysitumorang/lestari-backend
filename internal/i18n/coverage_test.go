package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// messageArg maps a function or method name to the index of its message argument. Calls are matched by name
// only (no type information), so the names are specific to this code base.
var messageArg = map[string]int{
	"BadRequest": 0, "Forbidden": 0, "Conflict": 0, "NotImplemented": 0, "NotFound": 0,
	"Unauthorized": 1, "PaymentRequired": 1, "NewError": 2, "Invalid": 1,
	"Add":      1, // Validation.Add(field, msg) — builder Add(x) calls have a single argument and are skipped
	"Tr":       0, // i18n.Tr(key, …)
	"T":        0, // Lang.T(key, …)
	"tr":       0, // per-document helpers: tr := httpx.Lang(c).T
	"notify":   5, // a.notify(ctx, q, store, branch, source, title, message, route, …)
	"activity": 2,
	"Column":   1,
	"Send":     3, // Sender.Send(ctx, channel, to, message)
}

// textKeys are struct-literal keys whose (capitalised) string values are user-facing text.
var textKeys = map[string]bool{"Message": true, "Label": true, "title": true, "Title": true, "Singular": true, "Name": true, "Description": true}

// TestDictionaryCoverage fails when user-facing English text in the API has no Bahasa Indonesia entry, or is
// built by concatenation / fmt.Sprintf instead of a {placeholder} template.
func TestDictionaryCoverage(t *testing.T) {
	root := filepath.Join("..", "..")
	var missing, dynamic []string
	seen := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.Contains(path, string(filepath.Separator)+"seed"+string(filepath.Separator)) || strings.HasSuffix(path, "id.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if kv, ok := n.(*ast.KeyValueExpr); ok {
				// labelled text in struct literals: export columns, report titles, resource names
				if k, ok := kv.Key.(*ast.Ident); ok && textKeys[k.Name] {
					if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						s, _ := strconv.Unquote(lit.Value)
						if s != "" && s[0] >= 'A' && s[0] <= 'Z' && !seen[s] {
							seen[s] = true
							if !Has(s) {
								pos := fset.Position(kv.Pos())
								missing = append(missing, strconv.Quote(s)+"  ("+filepath.Base(pos.Filename)+":"+strconv.Itoa(pos.Line)+")")
							}
						}
					}
				}
				return true
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				name = fn.Name
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			}
			idx, ok := messageArg[name]
			if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && name == "T" {
				if x, isIdent := sel.X.(*ast.Ident); isIdent && x.Name == "httpx" {
					idx = 1 // httpx.T(c, key, …)
				}
			}
			if !ok || idx >= len(call.Args) || (name == "Add" && len(call.Args) < 2) {
				return true
			}
			pos := fset.Position(call.Pos())
			where := filepath.Base(pos.Filename) + ":" + strconv.Itoa(pos.Line)
			for _, arg := range messageArgs(name, call, idx) {
				switch a := arg.(type) {
				case *ast.BasicLit:
					if a.Kind != token.STRING {
						continue
					}
					s, _ := strconv.Unquote(a.Value)
					if s == "" || !regexp.MustCompile(`[A-Za-z]`).MatchString(s) || seen[s] {
						continue
					}
					seen[s] = true
					if !Has(s) {
						missing = append(missing, strconv.Quote(s)+"  ("+where+")")
					}
				case *ast.BinaryExpr:
					if a.Op == token.ADD && hasLiteral(a) {
						dynamic = append(dynamic, where+": message built with +")
					}
				case *ast.CallExpr:
					if sel, ok := a.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" {
						dynamic = append(dynamic, where+": message built with fmt.Sprintf")
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("no Bahasa Indonesia entry for %s", m)
	}
	for _, d := range dynamic {
		t.Errorf("%s — use a {placeholder} template so it can be translated", d)
	}
	t.Logf("%d messages checked, %d dictionary entries", len(seen), len(id))
}

// messageArgs returns the message argument, plus the second message of notify (title and message).
func messageArgs(name string, call *ast.CallExpr, idx int) []ast.Expr {
	out := []ast.Expr{call.Args[idx]}
	if name == "notify" && idx+1 < len(call.Args) {
		out = append(out, call.Args[idx+1])
	}
	return out
}

func hasLiteral(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Kind == token.STRING && strings.TrimSpace(strings.Trim(x.Value, "\"`")) != ""
	case *ast.BinaryExpr:
		return hasLiteral(x.X) || hasLiteral(x.Y)
	}
	return false
}

func TestPlaceholdersMatch(t *testing.T) {
	ph := regexp.MustCompile(`\{\w+\}`)
	for en, tr := range id {
		a, b := ph.FindAllString(en, -1), ph.FindAllString(tr, -1)
		sort.Strings(a)
		sort.Strings(b)
		if strings.Join(a, ",") != strings.Join(b, ",") {
			t.Errorf("placeholders differ: %q → %q", en, tr)
		}
	}
}

func TestParse(t *testing.T) {
	cases := map[string]Lang{"id": ID, "id-ID,id;q=0.9,en;q=0.8": ID, "en-US,en;q=0.9,id;q=0.5": EN, "fr, id;q=0.3": ID, "in": ID}
	for h, want := range cases {
		if got, ok := Parse(h); !ok || got != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", h, got, ok, want)
		}
	}
	if _, ok := Parse("fr-FR"); ok {
		t.Error("Parse(fr-FR) should not match")
	}
}

func TestT(t *testing.T) {
	if got := EN.T("{what} not found", "what", Tr("Product")); got != "Product not found" {
		t.Errorf("EN: %q", got)
	}
	if got := ID.T("{what} not found", "what", Tr("Product")); got != "Produk tidak ditemukan" {
		t.Errorf("ID: %q", got)
	}
}
