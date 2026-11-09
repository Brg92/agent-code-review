package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDecodeDocumentRoundTrip decodes every valid findings example of the
// contracts and encodes it again: nothing is lost or added.
func TestDecodeDocumentRoundTrip(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "contracts", "findings", "valid", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := DecodeDocument(data)
			if err != nil {
				t.Fatal(err)
			}
			again, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var want, got any
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(again, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round trip changed the document:\n%s", again)
			}
		})
	}
}

func TestDecodeDocumentRejects(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "findings", "valid", "s4-not-verified.json"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, data, want string }{
		{"unknown field", strings.Replace(string(data), `"title"`, `"name":"x","title"`, 1), "unknown field"},
		{"trailing data", string(data) + "{}", "trailing"},
		{"bad key", strings.Replace(string(data), `"github:github.com/`, `"github.com/`, 1), "cr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeDocument([]byte(tt.data)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %v, want one that mentions %q", err, tt.want)
			}
		})
	}
}

func TestResultDocument(t *testing.T) {
	j := testJudgement()
	j.Signals = append(j.Signals, SignalDecision{ID: "s1", Decision: Dismiss, Reason: "the catch logs through a wrapper"})
	res, err := Finalize(testReview(t), j)
	if err != nil {
		t.Fatal(err)
	}
	h := Header{
		Title: "Add the cache", BaseRef: "main", Files: []string{"app/A.kt", "app/New.kt", "app/Old.kt"},
		CI:       CI{State: CINone},
		Criteria: Criteria{Source: "default", Profile: "app", ProfileReason: "default profile of android", Packs: []string{"android"}},
	}
	doc := res.Document(h)
	if doc.Schema != 1 || doc.CR != res.CR || doc.HeadSHA != testHead || doc.Verdict != res.Verdict || doc.Title != h.Title || doc.BaseRef != "main" {
		t.Errorf("header of the document: %+v", doc)
	}
	if want := []Dismissal{{Rule: "empty-catch", Path: "app/A.kt", Line: 2, Reason: "the catch logs through a wrapper"}}; !reflect.DeepEqual(doc.SignalsDismissed, want) {
		t.Errorf("dismissed %+v, want %+v", doc.SignalsDismissed, want)
	}
	if !reflect.DeepEqual(doc.Questions, j.Questions) || !reflect.DeepEqual(doc.Notes, j.Notes) || !reflect.DeepEqual(doc.AgentNotVerified, j.NotVerified) {
		t.Errorf("report data not kept: %+v %+v %+v", doc.Questions, doc.Notes, doc.AgentNotVerified)
	}
	if !reflect.DeepEqual(doc.Findings, res.Findings) || !reflect.DeepEqual(doc.PreExisting, res.PreExisting) {
		t.Error("findings not kept")
	}
}

// TestResultDocumentListsAreArrays checks that empty lists encode as
// arrays, as the findings contract requires, not null.
func TestResultDocumentListsAreArrays(t *testing.T) {
	data, err := json.Marshal(Result{CR: testReview(t).CR, Verdict: OK}.Document(Header{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "null") {
		t.Errorf("null in %s", data)
	}
}
