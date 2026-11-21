package render

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/render")

var repoRoot = filepath.Join("..", "..")

// golden compares got with testdata/render/<name>.txt, or rewrites the
// file with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(repoRoot, "testdata", "render", name+".txt")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/render -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file at line %d:\n%s", name, firstDiff(got, string(want)), got)
	}
}

// TestGoldenIncompleteCascade renders a cascade whose related change was not
// found; the goldens of the S2 cascade of the product flow are in
// flow_golden_test.go.
func TestGoldenIncompleteCascade(t *testing.T) {
	res, reviewed := testCascade(t, true)
	summary, err := Cascade(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "incomplete-cascade", summary)
}
