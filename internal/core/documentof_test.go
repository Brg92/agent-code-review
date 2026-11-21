package core_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
)

// TestHeaderOfWorkPackage: the findings document repeats what the work
// package knows about the change request: its title, target branch,
// changed files in diff order, files not verified, checks and criteria
// with their notes.
func TestHeaderOfWorkPackage(t *testing.T) {
	in := bigAndSmall(t)
	success := "success"
	in.CI = core.CI{State: core.CIPassing, Checks: []core.Check{{Name: "build", Status: "completed", Conclusion: &success}}}
	in.Criteria.Notes = []string{"Criteria changed in this change request are not applied."}
	in.NotVerified = []core.UnverifiedFile{{Path: "app/logo.png", Reason: "binary"}}
	wp, _, err := core.BuildWorkPackage(in)
	if err != nil {
		t.Fatal(err)
	}
	h := wp.Header()
	want := core.Header{
		Title: "Tidy the screens", BaseRef: "main",
		Files:       []string{"app/src/main/kotlin/Big.kt", "app/src/main/kotlin/Small.kt"},
		NotVerified: in.NotVerified,
		CI:          in.CI,
		Criteria: core.Criteria{
			Source: "overlay", Profile: "app", ProfileReason: "set in .agent-code-review/config.json", Packs: wp.Criteria.Packs,
			Dimensions: in.Dimensions, Notes: in.Criteria.Notes,
		},
	}
	if !reflect.DeepEqual(h, want) {
		t.Errorf("header %+v\nwant   %+v", h, want)
	}
}

// TestEncodeDocument: a findings document is written as the findings
// contract wants it and reads back the same.
func TestEncodeDocument(t *testing.T) {
	wp, review, err := core.BuildWorkPackage(bigAndSmall(t))
	if err != nil {
		t.Fatal(err)
	}
	res, err := core.Finalize(review, core.Judgement{
		Schema: 1, CR: wp.CR.String(), HeadSHA: wp.HeadSHA,
		Findings: []core.JudgedFinding{judgedFinding("app/src/main/kotlin/Small.kt", 5)},
		Signals:  dismissAll(wp),
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document(wp.Header())
	data, err := core.EncodeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "}\n") || strings.Contains(string(data), "\\u003c") {
		t.Errorf("not two-space JSON without HTML escapes and with a final newline:\n%s", data)
	}
	schemaData, err := os.ReadFile(filepath.Join("..", "..", "schemas", "findings.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := jsonschema.Compile(schemaData)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := schema.Validate(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range issues {
		t.Errorf("findings contract: %s", i)
	}
	back, err := core.DecodeDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, doc) {
		t.Errorf("read back %+v, want %+v", back, doc)
	}
}

func dismissAll(wp core.WorkPackage) []core.SignalDecision {
	var out []core.SignalDecision
	for _, s := range wp.Signals {
		out = append(out, core.SignalDecision{ID: s.ID, Decision: core.Dismiss, Reason: "synthetic"})
	}
	return out
}
