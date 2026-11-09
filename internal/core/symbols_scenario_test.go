package core_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
	"github.com/Brg92/agent-code-review/internal/testutil/scenario"
)

// recordedSearch searches sample-app at main: the base contents of the
// files that S1 changes, recorded at the commit main pointed to.
type recordedSearch struct {
	t     *testing.T
	files map[string]string
	calls []string
}

func newRecordedSearch(t *testing.T) *recordedSearch {
	t.Helper()
	pull := sandbox.PullRequest(t, "S1", "sample-app", 2)
	s := &recordedSearch{t: t, files: map[string]string{}}
	for _, f := range sandbox.PullFiles(t, "S1", "sample-app", 2) {
		if src, ok := sandbox.Content(t, "S1", "sample-app", f.Filename, pull.BaseSHA); ok {
			s.files[f.Filename] = src
		}
	}
	if len(s.files) == 0 {
		t.Fatal("no recorded base contents of sample-app")
	}
	return s
}

func (s *recordedSearch) Search(project, ref, term string) ([]rules.Match, error) {
	s.calls = append(s.calls, term)
	if project != sampleAppKey || ref != "main" {
		s.t.Errorf("search of %s at %s: only sample-app at main is recorded", project, ref)
		return nil, fmt.Errorf("not recorded")
	}
	var out []rules.Match
	for path, src := range s.files {
		for i, line := range strings.Split(src, "\n") {
			if strings.Contains(line, term) {
				out = append(out, rules.Match{Path: path, Line: i + 1})
			}
		}
	}
	return out, nil
}

const (
	sampleAppKey = "github:github.com/acr-plugin-sandbox/sample-app"
	coreLibKey   = "github:github.com/acr-plugin-sandbox/core-lib"
)

// rulesFiles assigns the changed files of a review to the android pack
// and returns them as the change-request rules see them.
func rulesFiles(t *testing.T, set *packs.Set, active []*packs.Pack, r core.Review) []rules.ChangedFile {
	t.Helper()
	var out []rules.ChangedFile
	for _, f := range r.Files {
		var assigned rules.File
		if a := set.Assign(active, f.Diff.Path); a.NotVerified == nil {
			assigned = a.File("", nil)
		}
		out = append(out, f.RulesFile(assigned))
	}
	return out
}

// TestScenarioS2RemovedAPI runs the core rules on core-lib S2, below
// sample-app S2 in the cascade: removing legacyToken() is a candidate on
// the removed line, pointing to the call that sample-app keeps on main;
// expiresAt(), which core-lib S2 adds and sample-app S2 uses, is not.
func TestScenarioS2RemovedAPI(t *testing.T) {
	set, err := packs.Load(filepath.Join("..", "..", "packs"))
	if err != nil {
		t.Fatal(err)
	}
	var android []*packs.Pack
	for _, p := range set.Packs {
		if p.ID == "android" {
			android = append(android, p)
		}
	}
	lib, app := scenario.Review(t, "core-lib", 3, "S2"), scenario.Review(t, "sample-app", 7, "S2")
	search := newRecordedSearch(t)
	cr := rules.ChangeRequest{
		Files:   rulesFiles(t, set, android, lib),
		Project: coreLibKey, Head: lib.HeadSHA, Target: "main", Search: search,
		Ancestors: []rules.Ancestor{{Project: sampleAppKey, Head: app.HeadSHA, Target: "main", Files: rulesFiles(t, set, android, app)}},
	}
	res := rules.RunChangeRequest(rules.Core().Rules, cr)
	if len(res.NotVerified) != 0 {
		t.Errorf("not verified %+v", res.NotVerified)
	}
	if !reflect.DeepEqual(search.calls, []string{"legacyToken"}) {
		t.Errorf("searches %q, want only the removed legacyToken", search.calls)
	}

	// The candidate is the finding the scenario expects.
	want := scenario.Judgement(t, "S2", "core-lib", 3).Findings[0]
	lib.Signals = core.NumberSignals(lib.Files, nil, res.Signals)
	if len(lib.Signals) != 1 {
		t.Fatalf("signals %+v, want one", lib.Signals)
	}
	s := lib.Signals[0]
	if s.Rule != want.Rule || s.Path != want.Path || s.Side != want.Side || s.Line != want.Line || !s.NeedsJudgement ||
		!reflect.DeepEqual(s.References, want.References) {
		t.Errorf("signal %+v, want %s on %s %s:%d with references %+v", s, want.Rule, want.Path, want.Side, want.Line, want.References)
	}

	// Confirmed, it blocks core-lib S2 from the removed line.
	j := core.Judgement{Schema: 1, CR: lib.CR.String(), HeadSHA: lib.HeadSHA, Findings: []core.JudgedFinding{}, PreExisting: []core.JudgedFinding{},
		Signals: []core.SignalDecision{{ID: s.ID, Decision: core.Confirm, Severity: core.Blocker}}}
	final, err := core.Finalize(lib, j)
	if err != nil {
		t.Fatal(err)
	}
	checkContract(t, final)
	f := find(t, final.Findings, "uses-removed-api", 39)
	if final.Verdict != core.Blocked || f.Side != core.Left || anchor(f) != "inline removed LEFT 39" || !reflect.DeepEqual(f.References, want.References) {
		t.Errorf("verdict %s, finding %s %s, references %+v", final.Verdict, f.Side, anchor(f), f.References)
	}

	// sample-app S2 is the root: nothing above it uses its symbols.
	root := rules.ChangeRequest{Files: rulesFiles(t, set, android, app), Project: sampleAppKey, Head: app.HeadSHA, Target: "main", Search: search}
	if res := rules.RunChangeRequest(rules.Core().Rules, root); len(res.Signals) != 0 || len(res.NotVerified) != 0 {
		t.Errorf("root: %+v", res)
	}
}
