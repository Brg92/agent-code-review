package guards

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestParseExitCodes(t *testing.T) {
	text := "# Exit codes\n\nprose | with a bar\n\n" +
		"| Code | Status | Meaning | Commands | Typical cause |\n| --- | --- | --- | --- | --- |\n" +
		"| 0 | Binding | Success | all | Done. |\n" +
		"| 3 | Binding | Validation error | `finalize`, `review` | A file. |\n" +
		"| 5 | Reserved | Reserved | none | Not used. |\n" +
		"| 130 | Binding | Interrupted | `auth` | Ctrl+C. |\n\nRules:\n"
	got, err := ParseExitCodes(text)
	if err != nil {
		t.Fatal(err)
	}
	want := []ExitCode{
		{Code: 0, Status: "Binding", Commands: nil, All: true},
		{Code: 3, Status: "Binding", Commands: []string{"finalize", "review"}},
		{Code: 5, Status: "Reserved"},
		{Code: 130, Status: "Binding", Commands: []string{"auth"}},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseExitCodes() = %+v", got)
	}
	for i := range want {
		if got[i].Code != want[i].Code || got[i].Status != want[i].Status || got[i].All != want[i].All || !slices.Equal(got[i].Commands, want[i].Commands) {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseExitCodesRefusesABadTable(t *testing.T) {
	head := "| Code | Status | Meaning | Commands | Typical cause |\n| --- | --- | --- | --- | --- |\n"
	for name, text := range map[string]string{
		"no table":             "nothing",
		"code not a number":    head + "| x | Binding | m | all | c |\n",
		"code twice":           head + "| 1 | Binding | m | all | c |\n| 1 | Binding | m | all | c |\n",
		"unknown status":       head + "| 1 | Proposal | m | all | c |\n",
		"binding with nothing": head + "| 1 | Binding | m | | c |\n",
		"too few cells":        head + "| 1 | Binding | m |\n",
		"commands not quoted":  head + "| 1 | Binding | m | review | c |\n",
	} {
		if _, err := ParseExitCodes(text); err == nil {
			t.Errorf("%s: ParseExitCodes() succeeded", name)
		}
	}
}

func readDoc(t *testing.T) []ExitCode {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "exit-codes.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ParseExitCodes(string(data))
	if err != nil {
		t.Fatalf("docs/exit-codes.md: %v", err)
	}
	return rows
}

// cliCodes returns the exit code constants of internal/cli, by name, and
// the names of those that are used: referenced outside their declaration.
// It also returns the integer literals returned by functions that return an
// int, which would be exit codes that skip the constants.
func cliCodes(t *testing.T) (values map[string]int, used map[string]bool, literals []string) {
	t.Helper()
	dir := filepath.Join("..", "cli")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	values, used = map[string]int{}, map[string]bool{}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				vs := s.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "exit") || i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.INT {
						n, _ := strconv.Atoi(lit.Value)
						values[name.Name] = n
					}
				}
			}
		}
	}
	for _, f := range files {
		declared := map[*ast.Ident]bool{}
		for _, d := range f.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.CONST {
				for _, s := range g.Specs {
					for _, n := range s.(*ast.ValueSpec).Names {
						declared[n] = true
					}
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				if _, ok := values[n.Name]; ok && !declared[n] {
					used[n.Name] = true
				}
			case *ast.FuncDecl:
				if n.Body == nil || n.Type.Results == nil || len(n.Type.Results.List) != 1 {
					return true
				}
				if id, ok := n.Type.Results.List[0].Type.(*ast.Ident); !ok || id.Name != "int" {
					return true
				}
				ast.Inspect(n.Body, func(m ast.Node) bool {
					if r, ok := m.(*ast.ReturnStmt); ok && len(r.Results) == 1 {
						if lit, ok := r.Results[0].(*ast.BasicLit); ok && lit.Kind == token.INT {
							literals = append(literals, n.Name.Name+" returns "+lit.Value)
						}
					}
					return true
				})
			}
			return true
		})
	}
	return values, used, literals
}

func TestExitCodesAreTheOnesTheDocumentLists(t *testing.T) {
	rows := readDoc(t)
	listed := map[int]ExitCode{}
	for _, r := range rows {
		listed[r.Code] = r
	}
	values, used, literals := cliCodes(t)
	if len(values) == 0 {
		t.Fatal("no exit code constant found in internal/cli")
	}
	for _, l := range literals {
		t.Errorf("internal/cli: %s: an exit code is a named constant of docs/exit-codes.md", l)
	}
	returned := map[int]bool{}
	for name, code := range values {
		row, ok := listed[code]
		switch {
		case !ok:
			t.Errorf("%s = %d is not in docs/exit-codes.md", name, code)
		case used[name]:
			returned[code] = true
			if row.Status != "Binding" {
				t.Errorf("%s = %d is returned, but the document says %s", name, code, row.Status)
			}
		}
	}
	for _, r := range rows {
		switch {
		case r.Status == "Binding" && !returned[r.Code]:
			t.Errorf("docs/exit-codes.md lists %d as binding, and no command returns it", r.Code)
		case r.Status != "Binding" && returned[r.Code]:
			t.Errorf("a command returns %d, which the document says is %s", r.Code, r.Status)
		}
	}
	if r, ok := listed[5]; !ok || r.Status != "Reserved" || returned[5] {
		t.Errorf("code 5 must stay reserved and unused: %+v", r)
	}
}

func TestExitCodesNameTheCommandsThatUseThem(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "cli", "cli.go"))
	if err != nil {
		t.Fatal(err)
	}
	var known []string
	for _, line := range strings.Split(string(data), "\n") {
		cmd, _, _ := strings.Cut(strings.TrimPrefix(line, "  "), " ")
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && cmd != "" {
			known = append(known, cmd)
		}
	}
	for _, want := range []string{"review", "next", "finalize", "show", "publish", "auth", "queue", "doctor", "check-criteria"} {
		if !slices.Contains(known, want) {
			t.Fatalf("the usage does not list %s: %v", want, known)
		}
	}
	for _, r := range readDoc(t) {
		for _, c := range r.Commands {
			if c != "version" && !slices.Contains(known, c) {
				t.Errorf("code %d names %q, which is not a command", r.Code, c)
			}
		}
	}
}
