package review

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/review")

// golden compares got with testdata/review/<name>, or writes it with -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "review", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/review -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs (run go test ./internal/review -update):\n%s", path, got)
	}
}

// portable makes the work directory of a test, and the separators of paths
// inside it, the same on every system: the first line of what next prints
// holds the path of the judgement.
func portable(out, work string) string {
	head, rest, found := strings.Cut(out, "\n")
	if !found {
		return out
	}
	head = strings.ReplaceAll(head, work, "<workdir>")
	if i := strings.Index(head, "<workdir>"); i >= 0 {
		head = head[:i] + strings.ReplaceAll(head[i:], string(filepath.Separator), "/")
	}
	return head + "\n" + rest
}

// TestGoldenS2: the work packages, the summary and the first parts served
// of the S2 cascade, as the commands print them.
func TestGoldenS2(t *testing.T) {
	d, work, _ := s2Deps(t)
	run, err := Collect(context.Background(), d, sandboxKey(t, "sample-app", 7))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range run.Packages {
		data, err := os.ReadFile(p.File)
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "s2-"+strings.TrimPrefix(p.CR.Project, sandbox.Owner+"/")+"-"+strconv.Itoa(p.CR.Number)+"-workpackage.json", data)
	}
	summary, err := Summary(run)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "s2-review.txt", []byte(summary))

	o, log, _ := open(t, work, "")
	first := next(t, o, log)
	golden(t, "s2-next-1.txt", []byte(portable(first.Text, work)))
	// The agent judges it, and the next change request is served.
	writeJudgement(t, filepath.Join(run.Packages[0].Dir, "judgement.json"), validJudgement)
	second := next(t, o, log)
	golden(t, "s2-next-2.txt", []byte(portable(second.Text, work)))
	if first.CR == second.CR {
		t.Error("the same change request is served after its judgement")
	}
}
