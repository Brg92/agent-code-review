package review

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
	"github.com/Brg92/agent-code-review/internal/testutil/reviewreplay"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// normalized makes the run directory of a test, and the separators of the
// paths inside it, the same on every system.
func normalized(text, runDir string) string {
	lines := strings.Split(strings.ReplaceAll(text, runDir, "<run>"), "\n")
	for i, l := range lines {
		if at := strings.Index(l, "<run>"); at >= 0 {
			lines[i] = l[:at] + strings.ReplaceAll(l[at:], string(filepath.Separator), "/")
		}
	}
	return strings.Join(lines, "\n")
}

var findingsSchema *jsonschema.Schema

// checkFindings checks a findings.json against the findings contract.
func checkFindings(t *testing.T, path string) {
	t.Helper()
	if findingsSchema == nil {
		data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "findings.schema.json"))
		if err != nil {
			t.Fatal(err)
		}
		if findingsSchema, err = jsonschema.Compile(data); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := findingsSchema.Validate(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range issues {
		t.Errorf("%s: findings contract: %s", path, i)
	}
}

// recordedFlow is the flow of the commands on a recorded scenario: review
// on the replay, the hand-written judgement of each change request, then
// finalize.
type recordedFlow struct {
	run       *Run
	opened    *Opened
	finalized *Finalized
	deps      Deps
}

// judgementFixture is the file of testdata/review/judgements that judges
// k in scenario.
func judgementFixture(scenario string, k core.Key) string {
	return strings.ToLower(scenario) + "-" + strings.TrimPrefix(k.Project, sandbox.Owner+"/") + "-" + strconv.Itoa(k.Number) + ".json"
}

func finalizeRecorded(t *testing.T, d Deps, scenario string, root core.Key) recordedFlow {
	t.Helper()
	run, err := Collect(context.Background(), d, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range run.Packages {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "review", "judgements", judgementFixture(scenario, p.CR)))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.Judgement, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	o, log, _ := open(t, d.WorkDir, "")
	log.SetCommand("finalize")
	f, err := Finalize(o, FinalizeDeps{Packs: d.Packs, Registries: d.Registries, Version: "0.0.0-dev", Now: d.Now}, log)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	for _, p := range run.Packages {
		checkFindings(t, filepath.Join(p.Dir, state.FindingsFile))
		sameAsTheCollection(t, p)
	}
	return recordedFlow{run: run, opened: o, finalized: f, deps: d}
}

// reportOfPackage is the report.txt of a change request of the flow.
func (r recordedFlow) report(t *testing.T, k core.Key) string {
	t.Helper()
	for _, p := range r.run.Packages {
		if p.CR.Same(k) {
			data, err := os.ReadFile(filepath.Join(p.Dir, state.ReportFile))
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
	}
	t.Fatalf("no package for %s", k)
	return ""
}

func TestFinalizeRecordedSingleChangeRequests(t *testing.T) {
	for _, s := range []struct {
		name, scenario, repo string
		number               int
		check                func(t *testing.T, report, final string)
	}{
		{"s1", "S1", "sample-app", 2, func(t *testing.T, report, final string) {
			has(t, report, "Verdict: BLOCKED — 3 blockers in the change and 1 crashing defect in existing code to fix, then review again.")
			has(t, final, "Overall: BLOCKED\n")
		}},
		{"s5-a", "S5", "sample-app", 3, func(t *testing.T, report, final string) {
			// The checks of the head are not all done: the report names the failing one.
			has(t, report, "Pipeline: failing on head: ")
			has(t, final, "Overall: OK\n")
		}},
		{"s6-a", "S6", "sample-app", 5, func(t *testing.T, report, final string) {
			has(t, report, "Notes: Criteria changed in this change request are not applied: .agent-code-review/config.json. The criteria of main are used.")
		}},
	} {
		t.Run(s.name, func(t *testing.T) {
			d, _, r, _ := replayDeps(t, s.scenario)
			reviewreplay.AddBuildFiles(t, r, s.scenario, s.repo, s.number)
			reviewreplay.AddNotFound(r, "core-lib", ".github/skills/core-lib-code-review/references", "main")
			root := sandboxKey(t, s.repo, s.number)
			flow := finalizeRecorded(t, d, s.scenario, root)

			report := flow.report(t, root)
			final := normalized(flow.finalized.Text, flow.run.Dir)
			summary, err := Show(flow.opened, showDeps(t.TempDir()), runlogFor(t), "")
			if err != nil {
				t.Fatal(err)
			}
			golden(t, "finalize/"+s.name+"-report.txt", []byte(report))
			golden(t, "finalize/"+s.name+"-finalize.txt", []byte(final))
			golden(t, "finalize/"+s.name+"-show.txt", []byte(summary))
			s.check(t, report, final)
		})
	}
}

func has(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Errorf("no %q in:\n%s", want, text)
	}
}

func TestFinalizeRecordedS2Cascade(t *testing.T) {
	d, _, _ := s2Deps(t)
	root := sandboxKey(t, "sample-app", 7)
	flow := finalizeRecorded(t, d, "S2", root)

	for _, p := range flow.run.Packages {
		golden(t, "finalize/s2-report-"+strings.TrimPrefix(p.CR.Project, sandbox.Owner+"/")+"-"+strconv.Itoa(p.CR.Number)+".txt", []byte(flow.report(t, p.CR)))
	}
	golden(t, "finalize/s2-finalize.txt", []byte(normalized(flow.finalized.Text, flow.run.Dir)))

	log := runlogFor(t)
	deps := showDeps(t.TempDir())
	summary, err := Show(flow.opened, deps, log, "")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "finalize/s2-show.txt", []byte(summary))
	has(t, summary, "Overall: BLOCKED\n")
	// The dismissed signal whose rule is a BLOCKER by default is counted
	// in the summary and in the final message, and tagged in its report.
	has(t, summary, "Dismissed BLOCKER-default signals: 1 (marked in the reports)\n")
	has(t, flow.finalized.Text, "Dismissed BLOCKER-default signals: 1 (marked in the reports)\n")
	has(t, flow.report(t, sandboxKey(t, "sample-app", 2)), "  - [BLOCKER default] global-scope ")
	// The causes of an incomplete cascade, and what was not followed, are
	// those of the cascade as review resolved it.
	has(t, summary, "Not followed: https://github.com/acr-plugin-sandbox/kmp-shared/pull/1 (no checkbox)\n")

	// The root's report holds the summary of the cascade.
	path, err := Save(flow.opened, deps, log, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "finalize/s2-root-saved.txt", saved)
}

// sameAsTheCollection: finalizing from the work package alone, as the
// command does, gives the findings, ids included, that finalizing on the
// review built while collecting, with the whole files, gives.
func sameAsTheCollection(t *testing.T, p Package) {
	t.Helper()
	data, err := os.ReadFile(p.Judgement)
	if err != nil {
		t.Fatal(err)
	}
	j, err := core.DecodeJudgement(data)
	if err != nil {
		t.Fatal(err)
	}
	want, err := core.Finalize(p.review, j)
	if err != nil {
		t.Fatalf("%s: finalizing the review of the collection: %v", p.CR, err)
	}
	data, err = os.ReadFile(filepath.Join(p.Dir, state.FindingsFile))
	if err != nil {
		t.Fatal(err)
	}
	got, err := core.DecodeDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2][]core.Finding{"findings": {got.Findings, want.Findings}, "preExisting": {got.PreExisting, want.PreExisting}} {
		have, _ := json.Marshal(pair[0])
		wanted, _ := json.Marshal(pair[1])
		if string(have) != string(wanted) {
			t.Errorf("%s %s differ:\nfinalized from the work package: %s\nfinalized on the collection:     %s", p.CR, name, have, wanted)
		}
	}
	if got.Verdict != want.Verdict {
		t.Errorf("%s: verdict %s, want %s", p.CR, got.Verdict, want.Verdict)
	}
}
