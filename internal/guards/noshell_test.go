package guards

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// shells are the program names that must never be executed.
var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "cmd": true, "powershell": true, "pwsh": true,
}

// shellExecCalls returns the position of every exec.Command or
// exec.CommandContext call in src that runs a shell. A program name that is
// not a literal is flagged when a literal -c or /c follows it; a literal
// program that is no shell may take -c, as `git -c` does.
func shellExecCalls(filename string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	execName := importName(file, "os/exec")
	if execName == "" {
		return nil, nil
	}

	var bad []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != execName {
			return true
		}
		var args []ast.Expr
		switch sel.Sel.Name {
		case "Command":
			args = call.Args
		case "CommandContext":
			if len(call.Args) > 1 {
				args = call.Args[1:]
			}
		}
		if len(args) > 0 && runsShell(args) {
			bad = append(bad, fset.Position(call.Pos()).String())
		}
		return true
	})
	return bad, nil
}

// importName returns the name file uses for the package at importPath, or ""
// if file does not import it.
func importName(file *ast.File, importPath string) string {
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err != nil || p != importPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return path.Base(importPath)
	}
	return ""
}

// runsShell reports whether args, a program name and its arguments, run a shell.
func runsShell(args []ast.Expr) bool {
	name, literal := stringLiteral(args[0])
	if literal {
		base := path.Base(strings.ReplaceAll(strings.ToLower(name), `\`, "/"))
		return shells[strings.TrimSuffix(base, ".exe")]
	}
	for _, arg := range args[1:] {
		if s, ok := stringLiteral(arg); ok && (s == "-c" || s == "/c") {
			return true
		}
	}
	return false
}

func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func TestShellExecCalls(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"direct program", `exec.Command("git", "status")`, nil},
		{"git config flag", `exec.Command("git", "-c", "a=b", "status")`, nil},
		{"variable program", `exec.Command(name, "--flag")`, nil},
		{"sh", `exec.Command("sh", "-c", "ls")`, []string{"x.go:6:6"}},
		{"bash", `exec.Command("bash", "script")`, []string{"x.go:6:6"}},
		{"zsh", `exec.Command("zsh")`, []string{"x.go:6:6"}},
		{"cmd", `exec.Command("cmd", "/c", "dir")`, []string{"x.go:6:6"}},
		{"cmd.exe", `exec.Command("cmd.exe", "/c", "dir")`, []string{"x.go:6:6"}},
		{"upper case", `exec.Command("CMD.EXE")`, []string{"x.go:6:6"}},
		{"powershell", `exec.Command("powershell", "-Command", "ls")`, []string{"x.go:6:6"}},
		{"pwsh", `exec.Command("pwsh")`, []string{"x.go:6:6"}},
		{"absolute path", `exec.Command("/bin/sh")`, []string{"x.go:6:6"}},
		{"windows path", `exec.Command("C:\\Windows\\System32\\cmd.exe")`, []string{"x.go:6:6"}},
		{"context", `exec.CommandContext(ctx, "bash", "-c", "ls")`, []string{"x.go:6:6"}},
		{"context variable -c", `exec.CommandContext(ctx, name, "-c", "ls")`, []string{"x.go:6:6"}},
		{"variable -c", `exec.Command(name, "-c", "ls")`, []string{"x.go:6:6"}},
		{"variable /c", `exec.Command(name, "/c", "dir")`, []string{"x.go:6:6"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package x\n\nimport \"os/exec\"\n\nfunc f() {\n\t_ = " + tt.body + "\n}\n"
			got, err := shellExecCalls("x.go", []byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("shellExecCalls = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShellExecCallsHonoursImports(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"aliased import", "package x\nimport run \"os/exec\"\nvar _ = run.Command(\"sh\")\n", []string{"x.go:3:9"}},
		{"unrelated package", "package x\nimport \"example.com/exec\"\nvar _ = exec.Command(\"sh\")\n", nil},
		{"no imports", "package x\nvar _ = 1\n", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := shellExecCalls("x.go", []byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("shellExecCalls = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProductionCodeRunsNoShell(t *testing.T) {
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			bad, err := shellExecCalls(p, src)
			if err != nil {
				return err
			}
			for _, pos := range bad {
				t.Errorf("shell execution at %s", pos)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
