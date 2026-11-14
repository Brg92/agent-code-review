// Package scenario builds the inputs of finalizing a sandbox pull request
// from its recordings and from the hand-written scenario judgements in
// testdata/scenarios. It is test support code only.
package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
// and the example android catalog of the contracts, which repeats two core
// rules. The example catalog has no match, so its rules have no why or fix.
func Rules(t testing.TB) map[string]*rules.Rule {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root(), "testdata", "contracts", "rules-catalog", "valid", "android.json"))
	if err != nil {
		t.Fatal(err)
	}
	android, err := rules.LoadCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	return core.RulesOf(rules.Core(), android)
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

// tree is a rules.Tree over a recorded tree.
type tree struct {
	scenario string
	rec      sandbox.Tree
}

// RecordedTree is the repository tree of repo at head in a scenario, as
// the change-request rules see it. A tree that was not recorded or that
// GitHub truncated cannot be listed: it returns an error, never a partial
// list that would pass a missing file for a missing test.
func RecordedTree(t testing.TB, scenario, repo, head string) rules.Tree {
	t.Helper()
	return RecordedTreeOrError(scenario, sandbox.TreeOf(t, scenario, repo, head))
}

// RecordedTreeOrError makes a rules.Tree of a recorded tree.
func RecordedTreeOrError(scenario string, rec sandbox.Tree) rules.Tree {
	return tree{scenario, rec}
}

func (r tree) List(prefix string) ([]string, error) {
	switch {
	case !r.rec.Recorded:
		return nil, fmt.Errorf("%s: no tree recorded", r.scenario)
	case r.rec.Truncated:
		return nil, fmt.Errorf("%s: the recorded tree is truncated", r.scenario)
	}
	out := []string{}
	for _, p := range r.rec.Paths {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out, nil
}

// search is a rules.Search over the recorded code searches of a scenario.
type search struct {
	t        testing.TB
	scenario string
}

// RecordedSearch searches the code of the sandbox repositories as the
// recordings of a scenario say. Code search only indexes the default
// branch, so any other ref is an error; so is a search that was not
// recorded, and one GitHub could not complete (incomplete_results), whose
// empty result proves nothing. A complete search gives the lines of the
// files it returned that contain the term, read from their contents at
// the default branch.
func RecordedSearch(t testing.TB, scenario string) rules.Search {
	return search{t, scenario}
}

// defaultBranch is the default branch of every sandbox repository.
const defaultBranch = "main"

func (s search) Search(project, ref, term string) ([]rules.Match, error) {
	s.t.Helper()
	if ref != defaultBranch {
		return nil, fmt.Errorf("code search only indexes the default branch, not %s", ref)
	}
	repo := project[strings.LastIndex(project, "/")+1:]
	rec := sandbox.SearchOf(s.t, s.scenario, repo, term)
	switch {
	case !rec.Recorded:
		return nil, fmt.Errorf("%s: no search of %q in %s recorded", s.scenario, term, repo)
	case rec.Incomplete:
		return nil, fmt.Errorf("%s: the search of %q in %s was incomplete", s.scenario, term, repo)
	}
	var out []rules.Match
	for _, p := range rec.Paths {
		src, ok := sandbox.Content(s.t, s.scenario, repo, p, defaultBranch)
		if !ok {
			return nil, fmt.Errorf("%s: contents of %s at %s not recorded", s.scenario, p, defaultBranch)
		}
		out = append(out, matchesIn(p, src, term)...)
	}
	return out, nil
}

// matchesIn returns the lines of a file that contain the term.
func matchesIn(path, src, term string) []rules.Match {
	var out []rules.Match
	for i, line := range core.SplitLines(src) {
		if strings.Contains(line, term) {
			out = append(out, rules.Match{Path: path, Line: i + 1, Text: line})
		}
	}
	return out
}
