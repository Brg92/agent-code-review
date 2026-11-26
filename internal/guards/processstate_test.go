package guards

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// processState is what a command must be given and never read from the
// process: the arguments, the standard files, the environment, and the way to
// end the process. Commands run in-process, and main passes these in.
var processState = map[string]bool{
	"Args": true, "Exit": true, "Stdin": true, "Stdout": true, "Stderr": true,
	"Getenv": true, "LookupEnv": true, "Environ": true,
}

// processStateUses returns the position of every use of a name of
// processState from package os in src.
func processStateUses(filename string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	osName := importName(file, "os")
	if osName == "" {
		return nil, nil
	}
	var bad []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == osName && processState[sel.Sel.Name] {
			bad = append(bad, fset.Position(sel.Pos()).String())
		}
		return true
	})
	return bad, nil
}

func TestProcessStateUses(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"arguments", "package x\nimport \"os\"\nvar _ = os.Args\n", []string{"x.go:3:9"}},
		{"exit", "package x\nimport \"os\"\nfunc f() { os.Exit(1) }\n", []string{"x.go:3:12"}},
		{"standard files", "package x\nimport \"os\"\nvar a, b, c = os.Stdin, os.Stdout, os.Stderr\n", []string{"x.go:3:15", "x.go:3:25", "x.go:3:36"}},
		{"environment", "package x\nimport \"os\"\nvar a, b, c = os.Getenv, os.LookupEnv, os.Environ\n", []string{"x.go:3:15", "x.go:3:26", "x.go:3:40"}},
		{"aliased import", "package x\nimport o \"os\"\nvar _ = o.Args\n", []string{"x.go:3:9"}},
		{"other names of os", "package x\nimport \"os\"\nvar _ = os.ReadFile\nvar _ = os.File{}\n", nil},
		{"another package", "package x\nimport \"example.com/os\"\nvar _ = os.Args\n", nil},
		{"a comment", "package x\n// os.Args\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := processStateUses("x.go", []byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("processStateUses = %v, want %v", got, tt.want)
			}
		})
	}
}

// processStateExceptions is the closed list of files of internal/ that may
// use the process state, with the reason. Only the helper process of the
// tests is in it: it is not a command, it is a program the tests run as git
// would run a credential helper, and its job is to read its arguments and
// standard files and to end with an exit code.
var processStateExceptions = map[string]string{
	"internal/testutil/credhelper/credhelper.go": "the test binary acts as a git credential helper process",
}

func TestInternalCodeUsesNoProcessState(t *testing.T) {
	used := map[string]bool{}
	root := filepath.Join(repoRoot, "internal")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		bad, err := processStateUses(p, src)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot, p)
		rel = filepath.ToSlash(rel)
		if reason, ok := processStateExceptions[rel]; ok {
			used[rel] = len(bad) > 0 && reason != ""
			return nil
		}
		for _, pos := range bad {
			t.Errorf("process state used at %s: take it as a parameter, main passes it in", pos)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var stale []string
	for file := range processStateExceptions {
		if !used[file] {
			stale = append(stale, file)
		}
	}
	slices.Sort(stale)
	if len(stale) > 0 {
		t.Errorf("exceptions that are no longer needed, remove them: %v", stale)
	}
}
