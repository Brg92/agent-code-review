// Package scenario builds the inputs of finalizing a sandbox pull request
// from its recordings and from the hand-written scenario judgements in
// testdata/scenarios. It is test support code only.
package scenario

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// Key is the key of a sandbox pull request.
func Key(t testing.TB, repo string, number int) core.Key {
	t.Helper()
	k, err := core.ParseKey("github:github.com/" + sandbox.Owner + "/" + repo + "#" + strconv.Itoa(number))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Rules are the rules the scenario judgements may apply: the core rules
// and the example android catalog of the contracts, which repeats one core
// rule. Their texts are the title and the description; the catalogs have
// no fix.
func Rules(t testing.TB) map[string]core.RuleInfo {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root(), "testdata", "contracts", "rules-catalog", "valid", "android.json"))
	if err != nil {
		t.Fatal(err)
	}
	android, err := rules.LoadCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]core.RuleInfo{}
	for _, r := range slices.Concat(rules.Core().Rules, android.Rules) {
		if _, ok := out[r.ID]; ok {
			continue
		}
		out[r.ID] = core.RuleInfo{Severity: core.Severity(r.Severity), NeedsJudgement: r.NeedsJudgement, Title: r.Title, Why: r.Description}
	}
	return out
}

// Review builds the review of pull request number of repo from the
// recordings of the first scenario that has it: its changed files with
// their diffs, the lines of both versions where they were recorded, and
// the rules of Rules. It has no signals.
func Review(t testing.TB, repo string, number int, scenarios ...string) core.Review {
	t.Helper()
	for _, s := range scenarios {
		if !recorded(t, s, repo, number) {
			continue
		}
		pull := sandbox.PullRequest(t, s, repo, number)
		r := core.Review{CR: Key(t, repo, number), HeadSHA: pull.HeadSHA, Rules: Rules(t)}
		for _, f := range sandbox.PullFiles(t, s, repo, number) {
			patch := ""
			if f.Patch != nil {
				patch = *f.Patch
			}
			d, err := core.NewFileDiff(f.Filename, f.PreviousFilename, core.FileStatus(f.Status), patch)
			if err != nil {
				t.Fatal(err)
			}
			file := core.ChangedFile{Diff: d}
			if src, ok := sandbox.Content(t, s, repo, f.Filename, pull.HeadSHA); ok {
				file.Head = core.SplitLines(src)
			}
			old := f.Filename
			if f.PreviousFilename != "" {
				old = f.PreviousFilename
			}
			if src, ok := sandbox.Content(t, s, repo, old, pull.BaseSHA); ok {
				file.Base = core.SplitLines(src)
			}
			r.Files = append(r.Files, file)
		}
		return r
	}
	t.Fatalf("no recordings of %s#%d in %v", repo, number, scenarios)
	return core.Review{}
}

func recorded(t testing.TB, scenario, repo string, number int) bool {
	t.Helper()
	for _, l := range sandbox.AllPullFiles(t) {
		if l.Scenario == scenario && l.Repo == repo && l.Number == number {
			return true
		}
	}
	return false
}

// Judgement reads testdata/scenarios/<scenario>/judgements/<repo>-<number>.json.
func Judgement(t testing.TB, scenario, repo string, number int) core.Judgement {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root(), "testdata", "scenarios", scenario, "judgements", repo+"-"+strconv.Itoa(number)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	j, err := core.DecodeJudgement(data)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
