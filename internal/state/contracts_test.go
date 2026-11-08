package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func contractExamples(t *testing.T, kind string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "contracts", "state", kind, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no %s state examples", kind)
	}
	return files
}

// copyExample puts an example in a new work directory as its state file.
func copyExample(t *testing.T, path string) (dir string, data []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, data
}

func TestValidContractExamplesRoundTrip(t *testing.T) {
	for _, path := range contractExamples(t, "valid") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			dir, want := copyExample(t, path)
			s, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := save(t, t.TempDir(), s); !bytes.Equal(got, want) {
				t.Errorf("saved state differs from the example:\n%s", got)
			}
		})
	}
}

func TestInvalidContractExamplesDoNotLoad(t *testing.T) {
	for _, path := range contractExamples(t, "invalid") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			dir, _ := copyExample(t, path)
			s, err := Load(dir)
			var ie *InvalidStateError
			if !errors.As(err, &ie) {
				t.Fatalf("Load() = %+v, %v; want an *InvalidStateError", s, err)
			}
			t.Log(err)
		})
	}
}
