package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func examples(t *testing.T, schema, kind string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "contracts", schema, kind, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no %s %s examples: %v", kind, schema, err)
	}
	return files
}

func TestValidPlanExamplesRoundTrip(t *testing.T) {
	for _, path := range examples(t, "plan", "valid") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			p, err := DecodePlan(want)
			if err != nil {
				t.Fatal(err)
			}
			got, err := EncodePlan(p)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("EncodePlan = %v\n%s\nwant the example:\n%s", err, got, want)
			}
		})
	}
}

func TestInvalidPlanExamplesDoNotDecode(t *testing.T) {
	for _, path := range examples(t, "plan", "invalid") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if p, err := DecodePlan(data); err == nil {
				t.Errorf("DecodePlan = %+v, want an error", p)
			} else {
				t.Log(err)
			}
		})
	}
}

func TestValidPublishedExamplesRoundTrip(t *testing.T) {
	for _, path := range examples(t, "published", "valid") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			p, err := DecodePublished(want)
			if err != nil {
				t.Fatal(err)
			}
			got, err := EncodePublished(p)
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("EncodePublished = %v\n%s\nwant the example:\n%s", err, got, want)
			}
		})
	}
}

func TestInvalidPublishedExamplesDoNotDecode(t *testing.T) {
	for _, path := range examples(t, "published", "invalid") {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if p, err := DecodePublished(data); err == nil {
				t.Errorf("DecodePublished = %+v, want an error", p)
			} else {
				t.Log(err)
			}
		})
	}
}

func TestChoice(t *testing.T) {
	tests := []struct {
		in               string
		ok, comments, is bool
	}{
		{"comments", true, true, false},
		{"issues", true, false, true},
		{"both", true, true, true},
		{"", false, false, false},
		{"all", false, false, false},
		{"Comments", false, false, false},
	}
	for _, tt := range tests {
		c, err := ParseChoice(tt.in)
		if (err == nil) != tt.ok {
			t.Errorf("ParseChoice(%q) error = %v", tt.in, err)
			continue
		}
		if tt.ok && (c.WantsComments() != tt.comments || c.WantsIssues() != tt.is) {
			t.Errorf("%q wants comments %v issues %v, want %v %v", tt.in, c.WantsComments(), c.WantsIssues(), tt.comments, tt.is)
		}
	}
}

func TestPlanStatuses(t *testing.T) {
	for _, s := range planStatuses {
		if !s.Valid() || !s.ValidInPlan() || !s.Final() {
			t.Errorf("%s: valid %v in plan %v final %v", s, s.Valid(), s.ValidInPlan(), s.Final())
		}
	}
	for _, s := range []PlanStatus{StatusPosted, StatusPostedUnassigned} {
		if !s.Valid() || s.ValidInPlan() || !s.Final() {
			t.Errorf("%s: valid %v in plan %v final %v", s, s.Valid(), s.ValidInPlan(), s.Final())
		}
	}
	if !StatusFailed.Valid() || StatusFailed.Final() {
		t.Error("failed is a status, and not a final one")
	}
	if PlanStatus("done").Valid() || PlanStatus("").Final() {
		t.Error("an unknown status is nothing")
	}
	var s PlanStatus
	if err := s.UnmarshalText([]byte("nope")); err == nil {
		t.Error("UnmarshalText accepted nope")
	}
	if _, err := PlanStatus("nope").MarshalText(); err == nil {
		t.Error("MarshalText accepted nope")
	}
	if b, err := StatusOwnCR.MarshalText(); err != nil || string(b) != "own-cr" {
		t.Errorf("MarshalText = %q, %v", b, err)
	}
	if err := s.UnmarshalText([]byte("own-cr")); err != nil || s != StatusOwnCR {
		t.Errorf("UnmarshalText = %v, %q", err, s)
	}
}

func validPlan() Plan {
	k, _ := ParseKey("github:github.com/acme/app#7")
	parent, _ := ParseKey("github:github.com/acme/lib#3")
	return Plan{Schema: 1, RunID: "20261102-090000-s1", CR: k, HeadSHA: strings.Repeat("a", 40), CreatedAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
		Publish: Both, Skipped: []PlanSkip{},
		Actions: []Action{
			{ID: 1, Kind: ActionReviewComment, Severity: Blocker, Rules: []string{"r"}, Body: "b", FindingID: FindingFingerprint("r", "a/B.kt", "x", 0), Path: "a/B.kt", Side: Right, Line: 3},
			{ID: 2, Kind: ActionSummaryComment, Body: "ok", Fingerprint: OKFingerprint(strings.Repeat("a", 40))},
			{ID: 3, Kind: ActionLabelCreate, Project: "github:github.com/acme/app", Label: &Label{Name: "review", Color: "5319e7"}},
			{ID: 4, Kind: ActionIssueCreate, Severity: Major, Rules: []string{"r"}, Body: "b", Fingerprint: "acr:v1:issue:acme/app#7:major", Project: "github:github.com/acme/app", Title: "t", Assignee: "me"},
			{ID: 5, Kind: ActionIssueUpdate, Severity: Minor, Rules: []string{"r"}, Body: "b", Fingerprint: "acr:v1:issue:acme/app#7:minor", Project: "github:github.com/acme/app", Number: 4, Title: "t"},
			{ID: 6, Kind: ActionRelatedTick, Parent: &parent, Entry: &k},
		}}
}

func TestPlanValidate(t *testing.T) {
	if err := validPlan().Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*Plan)
		want   string
	}{
		{"schema", func(p *Plan) { p.Schema = 2 }, "schema is 2"},
		{"run id", func(p *Plan) { p.RunID = "x" }, "run id"},
		{"cr", func(p *Plan) { p.CR = Key{} }, "missing cr"},
		{"head", func(p *Plan) { p.HeadSHA = "abc" }, "headSha"},
		{"time", func(p *Plan) { p.CreatedAt = time.Time{} }, "createdAt"},
		{"choice", func(p *Plan) { p.Publish = "all" }, "publish"},
		{"null actions", func(p *Plan) { p.Actions = nil }, "never null"},
		{"null skipped", func(p *Plan) { p.Skipped = nil }, "never null"},
		{"kind", func(p *Plan) { p.Actions[0].Kind = "approve" }, "unknown kind"},
		{"id order", func(p *Plan) { p.Actions[1].ID = 5 }, "ids run from 1"},
		{"comment id", func(p *Plan) { p.Actions[0].FindingID = "mr-review:v1:r:a:1" }, "findingId"},
		{"comment severity", func(p *Plan) { p.Actions[0].Severity = Info }, "severity"},
		{"comment rules", func(p *Plan) { p.Actions[0].Rules = nil }, "rules"},
		{"comment path", func(p *Plan) { p.Actions[0].Path = "../x" }, "path"},
		{"comment side", func(p *Plan) { p.Actions[0].Side = "" }, "side"},
		{"comment line", func(p *Plan) { p.Actions[0].Line = 0 }, "line"},
		{"comment start", func(p *Plan) { p.Actions[0].StartLine = 9 }, "startLine"},
		{"summary", func(p *Plan) { p.Actions[1].Fingerprint = "" }, "fingerprint"},
		{"label project", func(p *Plan) { p.Actions[2].Project = "x" }, "project"},
		{"label", func(p *Plan) { p.Actions[2].Label = nil }, "label"},
		{"label colour", func(p *Plan) { p.Actions[2].Label.Color = "purple" }, "colour"},
		{"issue title", func(p *Plan) { p.Actions[3].Title = "" }, "title"},
		{"issue number", func(p *Plan) { p.Actions[4].Number = 0 }, "number"},
		{"tick parent", func(p *Plan) { p.Actions[5].Parent = nil }, "parent"},
		{"tick entry", func(p *Plan) { p.Actions[5].Entry = &Key{} }, "entry"},
		{"skipped status", func(p *Plan) { p.Skipped = []PlanSkip{{Status: StatusPosted}} }, "cannot be skipped"},
		{"skipped kind", func(p *Plan) { p.Skipped = []PlanSkip{{Status: StatusOwnCR, Kind: "x"}} }, "unknown kind"},
		{"skipped finding", func(p *Plan) { p.Skipped = []PlanSkip{{Status: StatusAlreadyPosted, FindingID: "x"}} }, "fingerprint"},
		{"skipped line", func(p *Plan) { p.Skipped = []PlanSkip{{Status: StatusAlreadyPosted, Line: -1}} }, "negative"},
		{"skipped path", func(p *Plan) { p.Skipped = []PlanSkip{{Status: StatusAlreadyPosted, Path: "/x"}} }, "path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validPlan()
			tt.change(&p)
			err := p.Validate()
			var pe *PlanError
			if !errors.As(err, &pe) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate = %v, want a *PlanError that mentions %q", err, tt.want)
			}
			if _, err := EncodePlan(p); err == nil {
				t.Error("EncodePlan wrote an invalid plan")
			}
		})
	}
}

func TestDecodePlanRefusesWhatIsNotAPlan(t *testing.T) {
	good, err := EncodePlan(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(good, &generic); err != nil {
		t.Fatal(err)
	}
	generic["createdAt"] = "yesterday"
	badTime, _ := json.Marshal(generic)
	for name, data := range map[string][]byte{
		"not json":      []byte("{"),
		"trailing data": append(append([]byte{}, good...), []byte("{}")...),
		"unknown field": bytes.Replace(good, []byte(`"schema": 1`), []byte(`"schema": 1, "approve": true`), 1),
		"bad time":      badTime,
	} {
		if _, err := DecodePlan(data); err == nil {
			t.Errorf("%s: DecodePlan succeeded", name)
		}
	}
}

func TestHashIsTheSHA256OfTheBytes(t *testing.T) {
	if got, want := Hash([]byte("abc")), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"; got != want {
		t.Errorf("Hash(abc) = %s, want %s", got, want)
	}
}

func TestParseProjectKey(t *testing.T) {
	k, err := ParseProjectKey("github:github.com/acme/app")
	if err != nil || k.Project != "acme/app" || k.Number != 0 || k.ProjectKey() != "github:github.com/acme/app" {
		t.Errorf("ParseProjectKey = %+v, %v", k, err)
	}
	for _, s := range []string{"", "github:github.com/acme/app#1", "github:github.com/acme", "acme/app", "github:github.com/acme/../app"} {
		if _, err := ParseProjectKey(s); err == nil {
			t.Errorf("ParseProjectKey(%q) succeeded", s)
		}
	}
}

func TestPublishedValidate(t *testing.T) {
	k, _ := ParseKey("github:github.com/acme/app#7")
	good := func() Published {
		return Published{Schema: 1, RunID: "20261102-090000-s1", CR: k, PlanSHA256: strings.Repeat("a", 64), UpdatedAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
			Outcomes: []Outcome{{ID: 1, Status: StatusPosted}, {ID: 2, Status: StatusFailed, Detail: "x"}}}
	}
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*Published)
	}{
		{"schema", func(p *Published) { p.Schema = 0 }},
		{"run", func(p *Published) { p.RunID = "" }},
		{"cr", func(p *Published) { p.CR = Key{} }},
		{"hash", func(p *Published) { p.PlanSHA256 = "x" }},
		{"time", func(p *Published) { p.UpdatedAt = time.Time{} }},
		{"null", func(p *Published) { p.Outcomes = nil }},
		{"blocked", func(p *Published) { p.Blocked = StatusRejected }},
		{"id", func(p *Published) { p.Outcomes[0].ID = 0 }},
		{"twice", func(p *Published) { p.Outcomes[1].ID = 1 }},
		{"status", func(p *Published) { p.Outcomes[0].Status = StatusIgnoredUntrusted }},
		{"no anchor", func(p *Published) { p.Outcomes[0].Status = StatusNoAnchor }},
		{"number", func(p *Published) { p.Outcomes[0].Number = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := good()
			tt.change(&p)
			if err := p.Validate(); err == nil {
				t.Error("Validate succeeded")
			}
			if _, err := EncodePublished(p); err == nil {
				t.Error("EncodePublished wrote it")
			}
		})
	}
	data, _ := EncodePublished(good())
	for name, bad := range map[string][]byte{
		"not json":      []byte("["),
		"trailing data": append(append([]byte{}, data...), '1'),
		"bad time":      bytes.Replace(data, []byte("2026-11-02T09:00:00Z"), []byte("then"), 1),
	} {
		if _, err := DecodePublished(bad); err == nil {
			t.Errorf("%s: DecodePublished succeeded", name)
		}
	}
}
