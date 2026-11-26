package guards

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"testing"
)

// releaseOnly are the packages that only the release tooling may use. The
// shipped binary must not reach them.
var releaseOnly = []string{module + "/internal/release"}

// reachable returns the packages of want that appear in a `go list -deps
// -json` stream.
func reachable(r io.Reader, want []string) ([]string, error) {
	var found []string
	dec := json.NewDecoder(r)
	for {
		var pkg listedPackage
		if err := dec.Decode(&pkg); errors.Is(err, io.EOF) {
			return found, nil
		} else if err != nil {
			return nil, err
		}
		for _, w := range want {
			if pkg.ImportPath == w {
				found = append(found, w)
			}
		}
	}
}

func TestReachable(t *testing.T) {
	const input = `{"ImportPath":"fmt","Standard":true}
{"ImportPath":"example.com/own/internal/release"}
{"ImportPath":"example.com/own/internal/cli"}
`
	got, err := reachable(bytes.NewBufferString(input), []string{"example.com/own/internal/release", "example.com/own/internal/other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "example.com/own/internal/release" {
		t.Errorf("reachable = %v, want only the release package", got)
	}

	if _, err := reachable(bytes.NewBufferString("{"), releaseOnly); err == nil {
		t.Error("reachable = nil error on invalid JSON, want a decoding error")
	}
}

func TestBinaryDoesNotReachReleaseTooling(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-json", "./cmd/...")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	found, err := reachable(bytes.NewReader(out), releaseOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("packages reachable from cmd/ that only the release tooling may use: %v", found)
	}
}
