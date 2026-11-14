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
var forbiddenPrefixes = []string{"Approve", "Merge", "Push", "Rebase", "Checkout", "Build", "Close", "Resolve"}

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

// forbiddenWords name what no method parameter, result or struct field in
// the forge interfaces may be about: an action the plugin never takes on
// the platform, or the event of a review, which selects one.
var forbiddenWords = []string{"approve", "merge", "push", "rebase", "checkout", "build", "close", "resolve", "event"}

// forbiddenFieldNames returns the parameters, named results and struct
// fields in src with a word of their camel-case name that starts with a
// forbidden word.
func forbiddenFieldNames(filename string, src []byte) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var bad []string
	check := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				if hasForbiddenWord(name.Name) {
					bad = append(bad, name.Name)
				}
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncType:
			check(n.Params)
			check(n.Results)
		case *ast.StructType:
			check(n.Fields)
		}
		return true
	})
	return bad, nil
}

// hasForbiddenWord reports whether a word of the camel-case name starts
// with a forbidden word.
func hasForbiddenWord(name string) bool {
	start := 0
	for i := 1; i <= len(name); i++ {
		if i < len(name) && !(name[i] >= 'A' && name[i] <= 'Z' && name[i-1] >= 'a' && name[i-1] <= 'z') {
			continue
		}
		word := strings.ToLower(name[start:i])
		for _, w := range forbiddenWords {
			if strings.HasPrefix(word, w) {
				return true
			}
		}
		start = i
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
		{"every prefix", "type P interface {\n\tApproveCR()\n\tMerge()\n\tPushBranch()\n\tRebaseOnto()\n\tCheckoutHead()\n\tBuildProject()\n\tCloseIssue()\n\tResolveThread()\n}",
			[]string{"ApproveCR", "Merge", "PushBranch", "RebaseOnto", "CheckoutHead", "BuildProject", "CloseIssue", "ResolveThread"}},
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

func TestForbiddenFieldNames(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"allowed names", "type S struct {\n\tHead string\n\tRef string\n}\nfunc F(ctx int, project string) (found bool, err error) { return }", nil},
		{"struct field", "type S struct {\n\tApprove bool\n\tReviewEvent string\n\tEvent string\n}", []string{"Approve", "ReviewEvent", "Event"}},
		{"interface parameter", "type I interface {\n\tDo(mergeMethod string, ref string)\n}", []string{"mergeMethod"}},
		{"named result", "func F() (pushed bool) { return }", []string{"pushed"}},
		{"function literal", "var f = func(rebaseOnto, checkoutRef, buildID string) {}", []string{"rebaseOnto", "checkoutRef", "buildID"}},
		{"nested struct", "type S struct {\n\tInner struct{ CloseIssue bool }\n}", []string{"CloseIssue"}},
		{"method parameter", "type T struct{}\nfunc (T) Read(resolveThreads bool) {}", []string{"resolveThreads"}},
		{"word inside a name", "type S struct {\n\tRebuilt bool\n\tUnmerged bool\n\tPrevent string\n}", nil},
		{"embedded field", "type S struct {\n\tMerger\n}", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := forbiddenFieldNames("x.go", []byte("package x\n\n"+tt.src+"\n"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("forbiddenFieldNames = %v, want %v", got, tt.want)
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
		names, err := forbiddenFieldNames(p, src)
		if err != nil {
			return err
		}
		for _, name := range names {
			t.Errorf("%s: forbidden parameter or field %s", p, name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
