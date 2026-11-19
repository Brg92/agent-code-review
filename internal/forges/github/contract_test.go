package github

import (
	"slices"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/testutil/forgecontract"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// scenarioCase is a change request of a recorded scenario and what the
// recordings say about it. What the recordings do not hold, such as a
// directory listing, a complete search or a failure, is added by overlay in
// contract_overlay_test.go, which is synthetic.
type scenarioCase struct {
	scenario, repo string
	number         int
	// config is a file recorded at main; mustBeMissing one recorded as 404.
	config, missing string
	// missingDir was recorded as 404.
	missingDir string
	// term is recorded as an incomplete search when searchFails is set.
	term        string
	searchFails bool
	// ci is what the checks of the head are recorded as.
	ci func() core.CI
}

func completed(name, conclusion string) core.Check {
	return core.Check{Name: name, Status: "completed", Conclusion: &conclusion}
}

func passing(checks ...core.Check) func() core.CI {
	return func() core.CI { return core.CI{State: core.CIPassing, Checks: checks} }
}

var scenarioCases = []scenarioCase{
	{scenario: "S1", repo: "sample-app", number: 2, config: ".agent-code-review/config.json", missing: ".github/mr-review.json",
		missingDir: ".github/skills", term: "SessionRepository",
		ci: passing(completed("test", "success"), completed("lint", "success"), completed("lint", "success"), completed("test", "success"))},
	{scenario: "S2", repo: "sample-app", number: 7, config: ".agent-code-review/config.json", missing: ".github/mr-review.json",
		missingDir: ".github/skills", term: "legacyToken", searchFails: true,
		ci: passing(completed("test", "success"), completed("lint", "success"), completed("test", "success"), completed("lint", "success"))},
	{scenario: "S5", repo: "sample-app", number: 3, config: ".agent-code-review/config.json", missing: ".github/mr-review.json",
		missingDir: ".github/skills", term: "HomeScreen",
		ci: func() core.CI {
			running := core.Check{Name: "instrumented", Status: "in_progress"}
			return core.CI{State: core.CIFailing, Checks: []core.Check{completed("lint", "failure"), completed("test", "success"), running,
				completed("lint", "failure"), completed("test", "success"), running}}
		}},
	{scenario: "S7", repo: "kmp-shared", number: 3, config: "", missing: ".github/mr-review.json",
		missingDir: ".github/skills", term: "deviceModel", searchFails: true,
		ci: passing(completed("lint", "success"), completed("test", "success"))},
}

// TestContract runs the contract suite on the provider, end to end through
// the transport, on the replay of recorded scenarios.
func TestContract(t *testing.T) {
	for _, sc := range scenarioCases {
		t.Run(sc.scenario, func(t *testing.T) {
			r := sandbox.NewReplay(t, sc.scenario)
			fx := fixtureOf(t, sc)
			overlay(t, r, sc, &fx)
			tok := &tokens{token: canary}
			p, err := newProvider("github.com", r.URL(), tok, contractOptions())
			if err != nil {
				t.Fatal(err)
			}
			forgecontract.Run(t, p, fx)
			if tok.asked.Load() == 0 {
				t.Error("the token was never asked for")
			}
		})
	}
}

// fixtureOf builds the fixture from the recordings of sc.
func fixtureOf(t *testing.T, sc scenarioCase) forgecontract.Fixture {
	t.Helper()
	cr := key(sc.repo, sc.number)
	pull := sandbox.PullRequest(t, sc.scenario, sc.repo, sc.number)
	fx := forgecontract.Fixture{
		Capabilities:  Capabilities(),
		ChangeRequest: cr,
		Item:          cascade.Item{Kind: cascade.KindChangeRequest, Title: pull.Title, Description: pull.Body, Head: pull.HeadSHA},
		Metadata: core.ChangeRequest{Key: cr, Title: pull.Title, Description: pull.Body, HeadSHA: pull.HeadSHA, BaseSHA: pull.BaseSHA,
			TargetBranch: pull.BaseRef, Author: pull.Author, State: core.StateOpen},
		Issue:         inNumber(cr, 50),
		Missing:       inNumber(cr, 51),
		Project:       cr,
		DefaultBranch: "main",
		OtherRef:      "release/2.0",
		MissingPath:   sc.missing,
		MissingDir:    sc.missingDir,
		TreeRef:       pull.HeadSHA,
		Term:          sc.term,
		Head:          pull.HeadSHA,
		CI:            sc.ci(),
		Author:        pull.Author,
	}
	for _, f := range sandbox.PullFiles(t, sc.scenario, sc.repo, sc.number) {
		if f.Patch == nil || f.PreviousFilename != "" {
			t.Fatalf("%s: the contract fixture lists files with a patch and no rename", f.Filename)
		}
		fx.ChangedFiles = append(fx.ChangedFiles, core.FileChange{Path: f.Filename, Status: core.FileStatus(f.Status), Patch: *f.Patch})
	}
	if sc.config != "" {
		rec := sandbox.Contents(t, sc.scenario, sc.repo, sc.config, "main")
		if !rec.Recorded || rec.Status != 200 {
			t.Fatalf("%s is not recorded at main", sc.config)
		}
		fx.Path, fx.Content = sc.config, string(rec.File)
	}
	tree := sandbox.TreeOf(t, sc.scenario, sc.repo, pull.HeadSHA)
	if !tree.Recorded || len(tree.Paths) == 0 {
		t.Fatalf("no recorded tree at %s", pull.HeadSHA)
	}
	prefix := tree.Paths[0][:strings.Index(tree.Paths[0]+"/", "/")+1]
	fx.TreePrefix = prefix
	for _, p := range tree.Paths {
		if strings.HasPrefix(p, prefix) {
			fx.TreePaths = append(fx.TreePaths, p)
		}
	}
	slices.Sort(fx.TreePaths)
	if sc.searchFails {
		rec := sandbox.SearchOf(t, sc.scenario, sc.repo, sc.term)
		if !rec.Recorded || !rec.Incomplete {
			t.Fatalf("the search of %s is not recorded as incomplete", sc.term)
		}
		fx.SearchError = base.ErrSearchUnavailable
	}
	return fx
}

func inNumber(k core.Key, n int) core.Key {
	k.Number = n
	return k
}
