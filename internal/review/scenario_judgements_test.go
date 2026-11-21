package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/testutil/reviewreplay"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// scenarioRoots are the pull requests the product reviews in each
// scenario: the root of a replay, which reviews its cascade as well. They
// are the only source of work packages for the hand-written judgements in
// testdata/scenarios/<scenario>/judgements.
var scenarioRoots = []struct {
	scenario, repo string
	number         int
}{
	{"S1", "sample-app", 2},
	{"S2", "sample-app", 7},
	{"S3", "core-lib", 1},
	{"S4", "feature-lib", 1},
	{"S5", "sample-app", 3},
	{"S5", "sample-app", 4},
	{"S6", "sample-app", 5},
	{"S6", "sample-app", 6},
	{"S6", "core-lib", 2},
	{"S6", "kmp-shared", 1},
	{"S7", "sample-app", 8},
	{"S7", "kmp-shared", 3},
}

// collectScenarioRoot reviews a root of scenarioRoots as the product does,
// on the recordings.
func collectScenarioRoot(t *testing.T, scenario, repo string, number int) (Deps, *Run) {
	t.Helper()
	var d Deps
	if scenario == "S2" {
		d, _, _ = s2Deps(t)
	} else {
		var r *sandbox.Replay
		d, _, r, _ = replayDeps(t, scenario)
		reviewreplay.AddBuildFiles(t, r, scenario, repo, number)
		reviewreplay.AddNotFound(r, "core-lib", ".github/skills/core-lib-code-review/references", "main")
	}
	run, err := Collect(context.Background(), d, sandboxKey(t, repo, number))
	if err != nil {
		t.Fatalf("%s %s#%d: %v", scenario, repo, number, err)
	}
	return d, run
}

// scenarioJudgementFile is the hand-written judgement of a change request
// reviewed in a scenario, relative to the repository root.
func scenarioJudgementFile(scenario string, k core.Key) string {
	return filepath.ToSlash(filepath.Join("testdata", "scenarios", scenario, "judgements",
		strings.TrimPrefix(k.Project, sandbox.Owner+"/")+"-"+strconv.Itoa(k.Number)+".json"))
}

// readScenarioJudgement reads the hand-written judgement of a change
// request reviewed in a scenario.
func readScenarioJudgement(t testing.TB, scenario string, k core.Key) core.Judgement {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(scenarioJudgementFile(scenario, k))))
	if err != nil {
		t.Fatal(err)
	}
	j, err := core.DecodeJudgement(data)
	if err != nil {
		t.Fatalf("%s: %v", scenarioJudgementFile(scenario, k), err)
	}
	return j
}

// TestScenarioJudgementsValidateAgainstTheProductFlow: the hand-written
// judgements are the only ones. Each validates, with the validation
// finalize runs, against the work package review builds from the
// recordings; every change request the review covers has one, and every
// judgement file belongs to one of them.
func TestScenarioJudgementsValidateAgainstTheProductFlow(t *testing.T) {
	reviewed := map[string]bool{}
	for _, root := range scenarioRoots {
		t.Run(root.scenario+" "+root.repo+"#"+strconv.Itoa(root.number), func(t *testing.T) {
			d, run := collectScenarioRoot(t, root.scenario, root.repo, root.number)
			for _, p := range run.Packages {
				file := scenarioJudgementFile(root.scenario, p.CR)
				if reviewed[file] {
					continue
				}
				reviewed[file] = true
				if _, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(file))); err != nil {
					t.Errorf("%s is reviewed and has no judgement: %v", p.CR, err)
					continue
				}
				rv, err := core.ReviewOf(readWorkPackage(t, run, p.CR), d.Packs)
				if err != nil {
					t.Fatalf("%s: %v", p.CR, err)
				}
				var verr core.ValidationError
				switch err := core.ValidateJudgement(rv, readScenarioJudgement(t, root.scenario, p.CR)); {
				case errors.As(err, &verr):
					var lines []string
					for _, e := range verr {
						lines = append(lines, "  "+e.Pointer+": "+e.Message)
					}
					t.Errorf("%s does not validate against the work package of %s:\n%s", file, p.CR, strings.Join(lines, "\n"))
				case err != nil:
					t.Errorf("%s: %v", file, err)
				}
			}
		})
	}

	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "scenarios", "*", "judgements", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no scenario judgements: %v", err)
	}
	var extra []string
	for _, f := range files {
		rel, err := filepath.Rel(filepath.Join("..", ".."), f)
		if err != nil {
			t.Fatal(err)
		}
		if rel = filepath.ToSlash(rel); !reviewed[rel] {
			extra = append(extra, rel)
		}
	}
	sort.Strings(extra)
	for _, f := range extra {
		t.Errorf("%s belongs to no change request the review covers", f)
	}
}
