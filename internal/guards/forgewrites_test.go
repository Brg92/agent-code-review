package guards

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// forbiddenPrefixes name what ForgeProvider must never expose.
var forbiddenPrefixes = []string{"Approve", "Merge", "Push", "Close", "Resolve"}

// forbiddenInterfaceMethods returns the interface methods in src whose name
// starts with a forbidden prefix.
func forbiddenInterfaceMethods(filename string, src []byte) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var bad []string
	ast.Inspect(file, func(n ast.Node) bool {
		iface, ok := n.(*ast.InterfaceType)
		if !ok {
			return true
		}
		for _, field := range iface.Methods.List {
			for _, name := range field.Names {
				if hasForbiddenPrefix(name.Name) {
					bad = append(bad, name.Name)
				}
			}
		}
		return true
	})
	return bad, nil
}

func hasForbiddenPrefix(name string) bool {
	for _, prefix := range forbiddenPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func TestForbiddenInterfaceMethods(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"allowed methods", "type P interface {\n\tFetch() error\n\tPublish() error\n}", nil},
		{"every prefix", "type P interface {\n\tApproveCR()\n\tMerge()\n\tPushBranch()\n\tCloseIssue()\n\tResolveThread()\n}",
			[]string{"ApproveCR", "Merge", "PushBranch", "CloseIssue", "ResolveThread"}},
		{"second interface", "type A interface{ Fetch() }\ntype B interface{ Merge() }", []string{"Merge"}},
		{"nested interface", "type P interface {\n\tGet() interface{ Close() }\n}", []string{"Close"}},
		{"embedded type", "type A interface{ Fetch() }\ntype B interface{ A }", nil},
		{"concrete method", "type T struct{}\nfunc (T) Merge() {}", nil},
		{"plain function", "func Close() {}", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := forbiddenInterfaceMethods("x.go", []byte("package x\n\n"+tt.src+"\n"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("forbiddenInterfaceMethods = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestForgeInterfacesExposeNoWrites(t *testing.T) {
	root := filepath.Join(repoRoot, "internal", "forges", "base")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		bad, err := forbiddenInterfaceMethods(p, src)
		if err != nil {
			return err
		}
		for _, method := range bad {
			t.Errorf("%s: forbidden method %s", p, method)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
