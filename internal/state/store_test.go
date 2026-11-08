package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
)

// save saves s into dir and checks the file against the state schema.
func save(t *testing.T, dir string, s *State) []byte {
	t.Helper()
	if err := Save(dir, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	schemaData, err := os.ReadFile(filepath.Join("..", "..", "schemas", "state.schema.json"))
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
		t.Errorf("saved state does not match the schema: %s", i)
	}
	return data
}

func TestSaveWritesStableJSON(t *testing.T) {
	s := newState(t)
	s.ChangeRequests[0].Title = "Fix <a> & <b>"
	data := save(t, t.TempDir(), s)
	want := `{
  "schema": 1,
  "runId": "20261102T101500Z-0a1b2c3d",
  "forge": "github",
  "root": "github:github.com/acr-plugin-sandbox/sample-app#7",
  "phase": "resolved",
  "cascade": {
    "maxDepth": 3,
    "maxChangeRequests": 10
  },
  "createdAt": "2026-11-02T10:15:00Z",
  "updatedAt": "2026-11-02T10:15:00Z",
  "changeRequests": [
    {
      "cr": "github:github.com/acr-plugin-sandbox/sample-app#7",
      "depth": 0,
      "status": "pending",
      "title": "Fix <a> & <b>"
    },
    {
      "cr": "github:github.com/acr-plugin-sandbox/core-lib#3",
      "depth": 1,
      "parent": "github:github.com/acr-plugin-sandbox/sample-app#7",
      "status": "pending"
    },
    {
      "cr": "github:github.com/acr-plugin-sandbox/feature-lib#2",
      "depth": 1,
      "parent": "github:github.com/acr-plugin-sandbox/sample-app#7",
      "status": "skipped",
      "skipReason": "already-done"
    }
  ]
}
`
	if string(data) != want {
		t.Errorf("saved state =\n%s\nwant\n%s", data, want)
	}
}

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	s := newState(t)
	s.ChangeRequests[0].HeadSHA = strings.Repeat("0f", 20)
	s.ChangeRequests[0].Title = "Show when the session expires"
	save(t, dir, s)
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, s) {
		t.Errorf("Load() =\n%+v\nwant\n%+v", got, s)
	}
}

func TestSaveEmptyCascade(t *testing.T) {
	s := newState(t)
	s.ChangeRequests = nil
	data := save(t, t.TempDir(), s)
	if !strings.Contains(string(data), `"changeRequests": []`) {
		t.Errorf("nil change requests saved as\n%s", data)
	}
}

func TestSaveReplacesAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s := newState(t)
	save(t, dir, s)
	for i := range s.ChangeRequests[:2] {
		s.ChangeRequests[i].Status = core.Collected
	}
	if err := s.Advance(Collected, t1); err != nil {
		t.Fatal(err)
	}
	save(t, dir, s)
	got, err := Load(dir)
	if err != nil || got.Phase != Collected {
		t.Fatalf("Load() = %+v, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != FileName {
		t.Errorf("work dir holds %v, want only %s", entries, FileName)
	}
}

func TestSaveRefusesInvalidState(t *testing.T) {
	dir := t.TempDir()
	s := newState(t)
	s.ChangeRequests[2].SkipReason = ""
	var ce *ConsistencyError
	if err := Save(dir, s); !errors.As(err, &ce) {
		t.Errorf("Save() = %v, want a *ConsistencyError", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("Save() wrote %v", entries)
	}
}

func TestSaveFailures(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := Save(missing, newState(t)); err == nil {
		t.Error("Save() into a missing directory succeeded")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, newState(t)); err == nil {
		t.Error("Save() over a directory succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("failed Save() left %v", entries)
	}
}

func TestResume(t *testing.T) {
	dir := t.TempDir()
	s := newState(t)
	for i := range s.ChangeRequests[:2] {
		s.ChangeRequests[i].Status = core.Collected
	}
	if err := s.Advance(Collected, t1); err != nil {
		t.Fatal(err)
	}
	save(t, dir, s)

	resumed, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var te *TransitionError
	if err := resumed.Advance(Finalized, t1); !errors.As(err, &te) {
		t.Errorf("resumed run skipped a phase: %v", err)
	}
	for i := range resumed.ChangeRequests[:2] {
		resumed.ChangeRequests[i].Status = core.Judged
	}
	if err := resumed.Advance(Judged, t1); err != nil {
		t.Fatal(err)
	}
	save(t, dir, resumed)
	again, err := Load(dir)
	if err != nil || again.Phase != Judged {
		t.Errorf("Load() after resume = %+v, %v", again, err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(t.TempDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load() = %v, want fs.ErrNotExist", err)
	}
}

func TestLoadRejects(t *testing.T) {
	valid := string(save(t, t.TempDir(), newState(t)))
	replace := func(old, new string) string {
		if !strings.Contains(valid, old) {
			t.Fatalf("%q is not in the saved state", old)
		}
		return strings.Replace(valid, old, new, 1)
	}
	tests := []struct {
		name, data, wantErr string
	}{
		{"empty", "", "unexpected end"},
		{"invalid JSON", valid[:len(valid)/2], "unexpected end"},
		{"not an object", "[]", "cannot unmarshal"},
		{"trailing data", valid + "{}", "after top-level value"},
		{"invalid UTF-8", replace("pending", "pend\xffing"), "UTF-8"},
		{"missing schema", replace(`"schema": 1,`, ""), "missing schema"},
		{"schema as string", replace(`"schema": 1`, `"schema": "1"`), "cannot unmarshal"},
		{"unknown version", replace(`"schema": 1`, `"schema": 2`), "unsupported schema version 2"},
		{"unknown field", replace(`"schema": 1,`, `"schema": 1, "extra": true,`), `unknown field "extra"`},
		{"unknown entry field", replace(`"depth": 0,`, `"depth": 0, "verdict": "OK",`), `unknown field "verdict"`},
		{"unknown phase", replace(`"resolved"`, `"approved"`), "unknown phase"},
		{"invalid key", replace(`"root": "github:github.com/acr-plugin-sandbox/`, `"root": "github:github.com/`), "invalid change request key"},
		{"timestamp with offset", replace(`"createdAt": "2026-11-02T10:15:00Z"`, `"createdAt": "2026-11-02T11:15:00+01:00"`), "createdAt"},
		{"timestamp out of range", replace(`"createdAt": "2026-11-02T10:15:00Z"`, `"createdAt": "2026-13-02T10:15:00Z"`), "createdAt"},
		{"missing timestamp", replace(`"updatedAt": "2026-11-02T10:15:00Z",`, ""), "updatedAt"},
		{"null change requests", replace(valid[strings.Index(valid, `"changeRequests"`):len(valid)-2], `"changeRequests": null`), "changeRequests"},
		{"missing change requests", replace(",\n  "+valid[strings.Index(valid, `"changeRequests"`):len(valid)-2], ""), "changeRequests"},
		{"inconsistent", replace(`"skipReason": "already-done"`, `"skipReason": "cycle", "title": "x", "headSha": "XYZ"`), "headSha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(dir)
			var ie *InvalidStateError
			if !errors.As(err, &ie) {
				t.Fatalf("Load() = %v, want an *InvalidStateError", err)
			}
			if ie.Path != filepath.Join(dir, FileName) {
				t.Errorf("error path %q", ie.Path)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadWrapsConsistencyErrors(t *testing.T) {
	dir := t.TempDir()
	data := strings.Replace(string(save(t, dir, newState(t))), `"phase": "resolved"`, `"phase": "judged"`, 1)
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	var ce *ConsistencyError
	if !errors.As(err, &ce) || ce.Index != 0 {
		t.Errorf("Load() = %v, want a wrapped *ConsistencyError for entry 0", err)
	}
}
