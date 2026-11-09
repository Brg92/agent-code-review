package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
	"github.com/Brg92/agent-code-review/internal/testutil/scenario"
)

// recordedFiles is a core.Files over the recorded contents responses of
// one scenario. A request that was not recorded fails the test, except
// for the directories in missing, which the sandbox repositories do not
// have and the recorder did not ask for.
type recordedFiles struct {
	t        *testing.T
	scenario string
	missing  map[string]bool
}

func (f recordedFiles) lookup(project core.Key, ref, p string) (sandbox.Recorded, error) {
	f.t.Helper()
	repo := strings.TrimPrefix(project.Project, sandbox.Owner+"/")
	rec := sandbox.Contents(f.t, f.scenario, repo, p, ref)
	if !rec.Recorded {
		if f.missing[p] {
			return sandbox.Recorded{Recorded: true, Status: 404}, nil
		}
		f.t.Errorf("%s: no recorded response for %s at %s of %s", f.scenario, p, ref, repo)
		return rec, errors.New("not recorded")
	}
	return rec, nil
}

func (f recordedFiles) Get(_ context.Context, project core.Key, ref, p string) ([]byte, bool, error) {
	rec, err := f.lookup(project, ref, p)
	if err != nil || rec.Status == 404 {
		return nil, false, err
	}
	if rec.File == nil {
		return nil, false, fmt.Errorf("%s is a directory", p)
	}
	return rec.File, true, nil
}

func (f recordedFiles) List(_ context.Context, project core.Key, ref, dir string) ([]core.DirEntry, error) {
	rec, err := f.lookup(project, ref, dir)
	if err != nil || rec.Status == 404 {
		return nil, err
	}
	var out []core.DirEntry
	for _, e := range rec.Dir {
		out = append(out, core.DirEntry{Name: e.Name, Dir: e.Dir})
	}
	return out, nil
}

// allRules are the rules of every shipped pack and of the core.
func allRules(t *testing.T) []*rules.Rule {
	t.Helper()
	set, err := packs.Load(filepath.Join("..", "..", "packs"))
	if err != nil {
		t.Fatal(err)
	}
	catalogs := []*rules.Catalog{rules.Core()}
	for _, p := range set.Packs {
		catalogs = append(catalogs, p.Rules)
	}
	seen := map[string]bool{}
	var out []*rules.Rule
	for _, c := range catalogs {
		for _, r := range c.Rules {
			if !seen[r.ID] {
				seen[r.ID] = true
				out = append(out, r)
			}
		}
	}
	return out
}

func readRecorded(t *testing.T, scen, repo string, number int, ref string, missing ...string) (core.RepoCriteria, error) {
	t.Helper()
	var changed []string
	for _, f := range sandbox.PullFiles(t, scen, repo, number) {
		changed = append(changed, f.Filename)
	}
	files := recordedFiles{t: t, scenario: scen, missing: map[string]bool{}}
	for _, m := range missing {
		files.missing[m] = true
	}
	return core.ReadCriteria(context.Background(), files, core.CriteriaInput{
		Project: scenario.Key(t, repo, number), Ref: ref, Rules: allRules(t), Changed: changed,
	})
}

// sameAsExample checks the criteria, once the caller's part is set,
// against a valid repo-criteria contract example and its schema.
func sameAsExample(t *testing.T, c core.RepoCriteria, example string) {
	t.Helper()
	got, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	schemaData, err := os.ReadFile(filepath.Join("..", "..", "schemas", "repo-criteria.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := jsonschema.Compile(schemaData)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := schema.Validate(got)
	if err != nil || len(issues) > 0 {
		t.Errorf("criteria do not match the schema: %v %v", err, issues)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "repo-criteria", "valid", example))
	if err != nil {
		t.Fatal(err)
	}
	if string(got)+"\n" != string(want) {
		t.Errorf("criteria differ from %s:\n%s", example, got)
	}
}

// packNoise is the noise of the android pack, which the caller adds.
var packNoise = []string{"gradle/verification-metadata.xml"}

func TestRecordedCriteriaS1Neutral(t *testing.T) {
	c, err := readRecorded(t, "S1", "sample-app", 2, "main")
	if err != nil {
		t.Fatal(err)
	}
	c.Noise = append(c.Noise, packNoise...)
	sameAsExample(t, c, "sample-app-overlay.json")
}

// TestRecordedCriteriaS6A checks that the overlay the pull request edits
// is not applied: the criteria are main's, and a note says so.
func TestRecordedCriteriaS6A(t *testing.T) {
	c, err := readRecorded(t, "S6", "sample-app", 5, "main")
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != core.SourceOverlay || len(c.DisabledRules) != 0 || !reflect.DeepEqual(c.Variants, []string{"free", "paid"}) {
		t.Errorf("criteria = %+v, want main's overlay", c)
	}
	want := []string{"Criteria changed in this change request are not applied: .agent-code-review/config.json. The criteria of main are used."}
	if !reflect.DeepEqual(c.Notes, want) {
		t.Errorf("notes = %q, want %q", c.Notes, want)
	}
}

// TestRecordedCriteriaS6B checks that an invalid overlay on the target
// branch stops the review, naming the file and every invalid field.
func TestRecordedCriteriaS6B(t *testing.T) {
	_, err := readRecorded(t, "S6", "sample-app", 6, "chore/criteria-s6-invalid-overlay-base")
	var ce *core.CriteriaError
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want a *core.CriteriaError", err)
	}
	var fields []string
	for _, f := range ce.Errors {
		fields = append(fields, f.Pointer)
	}
	if ce.File != core.ConfigFile || !reflect.DeepEqual(fields, []string{"/profile", "/schema", "/variants"}) {
		t.Errorf("error in %s at %q", ce.File, fields)
	}
}

func TestRecordedCriteriaS6CLegacy(t *testing.T) {
	c, err := readRecorded(t, "S6", "core-lib", 2, "main", ".github/skills/core-lib-code-review/references")
	if err != nil {
		t.Fatal(err)
	}
	c.Packs = []string{"android"}
	c.Noise = append(c.Noise, packNoise...)
	sameAsExample(t, c, "core-lib-legacy.json")
}

func TestRecordedCriteriaS6DDefault(t *testing.T) {
	c, err := readRecorded(t, "S6", "kmp-shared", 1, "main")
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != core.SourceDefault || c.Profile != "" || len(c.Files) != 0 || len(c.Rules) != 0 {
		t.Errorf("criteria = %+v, want the defaults", c)
	}
	if want := []string{"No repository criteria on main: only the pack and core rules apply."}; !reflect.DeepEqual(c.Notes, want) {
		t.Errorf("notes = %q, want %q", c.Notes, want)
	}
}
