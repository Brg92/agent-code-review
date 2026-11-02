package guards

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

const module = "github.com/Brg92/agent-code-review"

// repoRoot is the module root, relative to this package's test directory.
var repoRoot = filepath.Join("..", "..")

// listedPackage is the part of `go list -json` output the guard needs.
type listedPackage struct {
	ImportPath string
	Standard   bool
	Module     *struct{ Path string }
}

// nonStandardDeps returns the packages in a `go list -deps -json` stream that
// are neither in the standard library nor in module.
func nonStandardDeps(r io.Reader, module string) ([]string, error) {
	var bad []string
	dec := json.NewDecoder(r)
	for {
		var pkg listedPackage
		if err := dec.Decode(&pkg); errors.Is(err, io.EOF) {
			return bad, nil
		} else if err != nil {
			return nil, err
		}
		if pkg.Standard || (pkg.Module != nil && pkg.Module.Path == module) {
			continue
		}
		bad = append(bad, pkg.ImportPath)
	}
}

func TestNonStandardDeps(t *testing.T) {
	const input = `{"ImportPath":"fmt","Standard":true}
{"ImportPath":"vendor/golang.org/x/net/idna","Standard":true}
{"ImportPath":"example.com/own/internal/cli","Module":{"Path":"example.com/own"}}
{"ImportPath":"github.com/third/party","Module":{"Path":"github.com/third/party"}}
{"ImportPath":"orphan/without/module"}
`
	want := []string{"github.com/third/party", "orphan/without/module"}

	got, err := nonStandardDeps(bytes.NewBufferString(input), "example.com/own")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nonStandardDeps = %v, want %v", got, want)
	}
}

func TestNonStandardDepsRejectsInvalidJSON(t *testing.T) {
	if _, err := nonStandardDeps(bytes.NewBufferString("{"), module); err == nil {
		t.Error("nonStandardDeps = nil error, want a decoding error")
	}
}

func TestRepoUsesOnlyStandardLibrary(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-json", "./cmd/...", "./internal/...")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	bad, err := nonStandardDeps(bytes.NewReader(out), module)
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 0 {
		t.Errorf("non-standard dependencies: %v", bad)
	}
}
