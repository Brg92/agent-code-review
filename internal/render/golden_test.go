package render

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
	"github.com/Brg92/agent-code-review/internal/testutil/scenario"
	"github.com/Brg92/agent-code-review/internal/version"
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

// reviewRules are the rules of the scenario judgements and of the android
// pack, so that a case can add findings and signals of pack rules.
func reviewRules(t *testing.T) map[string]*rules.Rule {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "packs", "android", "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	android, err := rules.LoadCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	out := scenario.Rules(t)
	for id, r := range core.RulesOf(android) {
		if _, ok := out[id]; !ok {
			out[id] = r
		}
	}
	return out
}

var findingsSchema *jsonschema.Schema

// checkDocument checks a document against the findings contract.
func checkDocument(t *testing.T, d core.Document) {
	t.Helper()
	if findingsSchema == nil {
		data, err := os.ReadFile(filepath.Join(repoRoot, "schemas", "findings.schema.json"))
		if err != nil {
			t.Fatal(err)
		}
		if findingsSchema, err = jsonschema.Compile(data); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := findingsSchema.Validate(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range issues {
		t.Errorf("%s: findings contract: %s", d.CR, i)
	}
}

// crCase is a sandbox change request rendered from its scenario judgement.
type crCase struct {
	// scen holds the judgement; rec the recordings, when they are in
	// another scenario.
	scen, rec, repo string
	number          int
	title           string
	header          core.Header
	change          func(r *core.Review, j *core.Judgement)
}

func (c crCase) document(t *testing.T) core.Document {
	t.Helper()
	rec := c.rec
	if rec == "" {
		rec = c.scen
	}
	r := scenario.Review(t, c.repo, c.number, rec)
	r.Rules = reviewRules(t)
	j := scenario.Judgement(t, c.scen, c.repo, c.number)
	if c.change != nil {
		c.change(&r, &j)
	}
	res, err := core.Finalize(r, j)
	if err != nil {
		t.Fatal(err)
	}
	h := c.header
	h.Title = c.title
	if h.BaseRef == "" {
		h.BaseRef = "main"
	}
	for _, f := range sandbox.PullFiles(t, rec, c.repo, c.number) {
		h.Files = append(h.Files, f.Filename)
	}
	d := res.Document(h)
	checkDocument(t, d)
	return d
}

func checks(list ...core.Check) core.CI {
	return core.CI{State: core.CIPassing, Checks: list}
}

var (
	lintOK  = check("lint", "completed", "success")
	testOK  = check("test", "completed", "success")
	passing = checks(lintOK, testOK)

	sampleApp = core.Criteria{Source: "overlay", Profile: "app", ProfileReason: "set in .agent-code-review/config.json", Packs: []string{"android"},
		Dimensions: map[string]core.DimensionChoice{"ui-toolkit": {Value: "compose", Reason: "androidx.compose in app/build.gradle.kts"}}}
	coreLib = core.Criteria{Source: "legacy", Profile: "library", ProfileReason: "set in .github/mr-review.json", Packs: []string{"android"},
		Dimensions: map[string]core.DimensionChoice{"ui-toolkit": {Value: "views", Reason: "no Compose dependency"}}}
	featureLib = core.Criteria{Source: "default", Profile: "library", ProfileReason: "maven-publish in build.gradle.kts", Packs: []string{"android"},
		Dimensions: map[string]core.DimensionChoice{"ui-toolkit": {Value: "views", Reason: "no Compose dependency"}}}
	kmpShared = core.Criteria{Source: "default", Profile: "library", ProfileReason: "default profile of kotlin-multiplatform",
		Packs:      []string{"kotlin-multiplatform", "android"},
		Dimensions: map[string]core.DimensionChoice{"ui-toolkit": {Value: "views", Reason: "no Compose dependency"}}}

	featureLibNotVerified = []core.UnverifiedFile{
		{Path: "gradle/verification-metadata.xml", Reason: "noise", Detail: "android"},
		{Path: "src/main/java/com/example/feature/catalog/IconCatalog.kt", Reason: "generated", Detail: "The platform omitted the diff of this 30007-line file."},
	}
)

const s1Dir = "app/src/main/java/com/example/sample/"

var (
	s1 = crCase{scen: "S1", repo: "sample-app", number: 2, title: "Prefetch the session on startup",
		header: core.Header{CI: passing, Criteria: sampleApp},
		change: func(r *core.Review, j *core.Judgement) {
			// The same hard-coded text twice in existing code: one grouped entry.
			for _, line := range []int{14, 22} {
				j.PreExisting = append(j.PreExisting, core.JudgedFinding{
					Rule: "hardcoded-string", Severity: core.Minor, Path: s1Dir + "ui/HomeViewModel.kt", Side: core.Right, Line: line,
					Title: "Greeting hard-coded in the ViewModel", Why: "The text cannot be translated.",
					Fix: "Move it to a string resource and expose its id.", References: []core.Reference{},
				})
			}
			r.Signals = append(r.Signals, core.Signal{ID: "s1", Rule: "missing-kdoc", Path: s1Dir + "data/SessionRepository.kt", Line: 18,
				Column: 5, Origin: core.OriginMR, NeedsJudgement: true})
			j.Signals = append(j.Signals, core.SignalDecision{ID: "s1", Decision: core.Dismiss,
				Reason: "prefetch() is internal to the app; the app profile asks no KDoc for it."})
			j.Questions = append(j.Questions, core.Question{Path: s1Dir + "data/SessionRepository.kt", Line: 18,
				Text: "Should prefetch() start again after a configuration change?"})
			j.NotVerified = append(j.NotVerified, "Whether SessionManager.refresh() can return null for a signed-in user.")
			j.Notes = append(j.Notes, "The overlay of main makes every runBlocking outside tests a BLOCKER.")
		}}
	s3 = crCase{scen: "S3", repo: "core-lib", number: 1, title: "Reorganize session files",
		header: core.Header{CI: passing, Criteria: coreLib,
			NotVerified: []core.UnverifiedFile{{Path: "src/main/res/drawable-nodpi/badge_background.png", Reason: "binary"}}}}
	s4 = crCase{scen: "S4", repo: "feature-lib", number: 1, title: "Regenerate the icon catalog",
		header: core.Header{CI: passing, Criteria: featureLib, NotVerified: featureLibNotVerified}}
	s5Failing = crCase{scen: "S5", repo: "sample-app", number: 3, title: "Increase home screen padding",
		header: core.Header{Criteria: sampleApp, CI: core.CI{State: core.CIFailing, Checks: []core.Check{
			check("lint", "completed", "failure"), testOK, check("instrumented", "in_progress", ""),
		}}}}
	s5Pending = crCase{scen: "S5", repo: "sample-app", number: 3, title: "Increase home screen padding",
		header: core.Header{Criteria: sampleApp, CI: core.CI{State: core.CIPending, Checks: []core.Check{
			lintOK, testOK, check("instrumented", "in_progress", ""),
		}}}}
	s6Notes = crCase{scen: "S6", repo: "sample-app", number: 5, title: "Show profile initials",
		header: core.Header{CI: passing, Criteria: func() core.Criteria {
			c := sampleApp
			c.Notes = []string{"This change request edits .agent-code-review/config.json. The edit was not applied: criteria are read from the target branch."}
			return c
		}()}}
	s6Swift = crCase{scen: "S6", repo: "kmp-shared", number: 1, title: "Greet the user by name on iOS",
		header: core.Header{CI: passing, Criteria: kmpShared,
			NotVerified: []core.UnverifiedFile{{Path: "iosApp/iosApp/ContentView.swift", Reason: "unsupported-language", Detail: "no pack covers swift"}}}}
	ciUnknown = crCase{scen: "S4", repo: "feature-lib", number: 1, title: "Regenerate the icon catalog",
		header: core.Header{Criteria: featureLib, NotVerified: featureLibNotVerified,
			CI: core.CI{State: core.CIUnknown, Reason: "forbidden", Detail: "Resource not accessible by personal access token", Checks: []core.Check{}}}}
)

func TestGoldenReports(t *testing.T) {
	for name, c := range map[string]crCase{
		"s1-report":         s1,
		"s3-report":         s3,
		"s4-report":         s4,
		"s5-failing-report": s5Failing,
		"s5-pending-report": s5Pending,
		"s6-criteria-notes": s6Notes,
		"s6-swift-report":   s6Swift,
		"ci-unknown-report": ciUnknown,
	} {
		t.Run(name, func(t *testing.T) {
			golden(t, name, Report(version.Version, c.document(t)))
		})
	}
}

// s2 is the cascade of scenario S2 as its finished state example lists
// it, with the findings of each change request from its judgement.
func s2(t *testing.T) (cascade.Result, []Reviewed) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "contracts", "state", "valid", "s2-cascade-finalized.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		ChangeRequests []core.Entry `json:"changeRequests"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	k := func(s string) core.Key { return mustKey(t, sandboxKey+s) }
	res := cascade.Result{
		Entries: state.ChangeRequests,
		Order:   []core.Key{k("feature-lib#1"), k("kmp-shared#2"), k("core-lib#3"), k("sample-app#2"), k("sample-app#7")},
		NotFollowed: []cascade.Line{{CR: k("sample-app#7"), Line: 9,
			Text: "- https://github.com/acr-plugin-sandbox/kmp-shared/pull/1"}},
	}
	s2Of := func(repo string, number int, title string, criteria core.Criteria) crCase {
		return crCase{scen: "S2", repo: repo, number: number, title: title, header: core.Header{CI: passing, Criteria: criteria}}
	}
	feature := s4
	feature.scen = "S2"
	feature.rec = "S4"
	cases := []crCase{
		feature,
		s2Of("kmp-shared", 2, "Add a session countdown", kmpShared),
		s2Of("core-lib", 3, "Replace legacyToken with expiresAt", coreLib),
		s2Of("sample-app", 2, "Prefetch the session on startup", sampleApp),
		s2Of("sample-app", 7, "Show when the session expires", sampleApp),
	}
	var reviewed []Reviewed
	for _, c := range cases {
		d := c.document(t)
		reviewed = append(reviewed, Reviewed{Doc: d, Report: filepath.ToSlash(filepath.Join("work", c.repo+"-"+itoa(c.number), "report.txt"))})
	}
	return res, reviewed
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestGoldenCascade(t *testing.T) {
	res, reviewed := s2(t)
	summary, err := Cascade(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "s2-cascade", summary)
	final, err := Final(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "s2-final", final)

	res, reviewed = testCascade(t, true)
	summary, err = Cascade(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "incomplete-cascade", summary)
}
