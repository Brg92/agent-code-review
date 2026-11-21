package render_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/render"
	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
	"github.com/Brg92/agent-code-review/internal/testutil/productflow"
	"github.com/Brg92/agent-code-review/internal/version"
)

// The goldens of this file are made from the documents of the product
// flow: the change requests of the sandbox scenarios as review collects
// them from the recordings, finalized with their scenario judgement. A
// case may edit the judgement or the header, to render what the
// recordings do not hold.

var repoRoot = filepath.Join("..", "..")

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

func check(name, status, conclusion string) core.Check {
	c := core.Check{Name: name, Status: status}
	if conclusion != "" {
		c.Conclusion = &conclusion
	}
	return c
}

// crCase is a sandbox change request rendered as the product flow makes it.
type crCase struct {
	scen, repo string
	number     int
	judgement  func(j *core.Judgement)
	header     func(h *core.Header)
}

func (c crCase) document(t *testing.T) core.Document {
	t.Helper()
	return c.of(t, productflow.Collect(t, c.scen, c.repo, c.number))
}

// of makes the document of the case from a flow that reached it.
func (c crCase) of(t *testing.T, f productflow.Flow) core.Document {
	t.Helper()
	d := f.Of(t, c.repo, c.number).Document(t, c.judgement, c.header)
	checkDocument(t, d)
	return d
}

const s1Dir = "app/src/main/java/com/example/sample/"

var (
	lintOK = check("lint", "completed", "success")
	testOK = check("test", "completed", "success")

	s1 = crCase{scen: "S1", repo: "sample-app", number: 2,
		judgement: func(j *core.Judgement) {
			// The same hard-coded text twice in existing code: one grouped entry.
			for _, line := range []int{14, 22} {
				j.PreExisting = append(j.PreExisting, core.JudgedFinding{
					Rule: "hardcoded-string", Severity: core.Minor, Path: s1Dir + "ui/HomeViewModel.kt", Side: core.Right, Line: line,
					Title: "Greeting hard-coded in the ViewModel", Why: "The text cannot be translated.",
					Fix: "Move it to a string resource and expose its id.", References: []core.Reference{},
				})
			}
		}}
	s3 = crCase{scen: "S3", repo: "core-lib", number: 1}
	s4 = crCase{scen: "S4", repo: "feature-lib", number: 1}

	// The checks of the recordings are all green: the cases that render a
	// failing, a pending or an unknown state of the checks set it.
	s5Failing = crCase{scen: "S5", repo: "sample-app", number: 3,
		header: func(h *core.Header) {
			h.CI = core.CI{State: core.CIFailing, Checks: []core.Check{
				check("lint", "completed", "failure"), testOK, check("instrumented", "in_progress", ""),
			}}
		}}
	s5Pending = crCase{scen: "S5", repo: "sample-app", number: 3,
		header: func(h *core.Header) {
			h.CI = core.CI{State: core.CIPending, Checks: []core.Check{lintOK, testOK, check("instrumented", "in_progress", "")}}
		}}
	s6Notes   = crCase{scen: "S6", repo: "sample-app", number: 5}
	s6Swift   = crCase{scen: "S6", repo: "kmp-shared", number: 1}
	ciUnknown = crCase{scen: "S4", repo: "feature-lib", number: 1,
		header: func(h *core.Header) {
			h.CI = core.CI{State: core.CIUnknown, Reason: "forbidden", Detail: "Resource not accessible by personal access token", Checks: []core.Check{}}
		}}
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
			render.Golden(t, name, render.Report(version.Version, c.document(t)))
		})
	}
}

// s2 is the cascade of scenario S2 as its finished state example lists
// it, with the document of each change request from the product flow.
func s2(t *testing.T) (cascade.Result, []render.Reviewed) {
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
	flow := productflow.Collect(t, "S2", "sample-app", 7)
	byRepo := func(repo string, number int) core.Key { return flow.Of(t, repo, number).CR }
	res := cascade.Result{
		Entries: state.ChangeRequests,
		Order:   []core.Key{byRepo("feature-lib", 1), byRepo("kmp-shared", 2), byRepo("core-lib", 3), byRepo("sample-app", 2), byRepo("sample-app", 7)},
		NotFollowed: []cascade.Line{{CR: byRepo("sample-app", 7), Line: 9,
			Text: "- https://github.com/acr-plugin-sandbox/kmp-shared/pull/1"}},
	}
	var reviewed []render.Reviewed
	for _, k := range res.Order {
		c := crCase{scen: "S2", repo: k.Project[len("acr-plugin-sandbox/"):], number: k.Number}
		reviewed = append(reviewed, render.Reviewed{Doc: c.of(t, flow), Report: filepath.ToSlash(filepath.Join("work", c.repo+"-"+itoa(c.number), "report.txt"))})
	}
	return res, reviewed
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestGoldenCascade(t *testing.T) {
	res, reviewed := s2(t)
	summary, err := render.Cascade(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	render.Golden(t, "s2-cascade", summary)
	final, err := render.Final(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	render.Golden(t, "s2-final", final)
}
