package core

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

// memFiles is an in-memory Files of one project at one ref. It fails the
// test on a read at another project or ref: criteria come only from the
// target branch.
type memFiles struct {
	t       *testing.T
	project Key
	ref     string
	files   map[string]string
	fail    map[string]error
	read    []string
}

func newMemFiles(t *testing.T, files map[string]string) *memFiles {
	return &memFiles{t: t, project: testProject, ref: "main", files: files}
}

var testProject = Key{Forge: GitHub, Host: "github.com", Project: "acme/sample-app", Number: 7}

func (m *memFiles) check(project Key, ref, p string) error {
	m.t.Helper()
	if project.Forge != m.project.Forge || project.Host != m.project.Host || project.Project != m.project.Project || ref != m.ref {
		m.t.Errorf("read %s at %s of %s, want only %s of %s", p, ref, project, m.ref, m.project)
	}
	m.read = append(m.read, p)
	return m.fail[p]
}

func (m *memFiles) Get(_ context.Context, project Key, ref, p string) ([]byte, bool, error) {
	if err := m.check(project, ref, p); err != nil {
		return nil, false, err
	}
	s, ok := m.files[p]
	return []byte(s), ok, nil
}

func (m *memFiles) List(_ context.Context, project Key, ref, dir string) ([]DirEntry, error) {
	if err := m.check(project, ref, dir+"/"); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []DirEntry
	for p := range m.files {
		rest, ok := strings.CutPrefix(p, dir+"/")
		if !ok {
			continue
		}
		name, _, sub := strings.Cut(rest, "/")
		if !seen[name] {
			seen[name] = true
			out = append(out, DirEntry{Name: name, Dir: sub})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func readCriteria(t *testing.T, files *memFiles, changed ...string) RepoCriteria {
	t.Helper()
	c, err := ReadCriteria(context.Background(), files, CriteriaInput{Project: files.project, Ref: files.ref, Rules: testRules(t), Changed: changed})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestKeyProjectKey(t *testing.T) {
	if got := testProject.ProjectKey(); got != "github:github.com/acme/sample-app" {
		t.Errorf("ProjectKey() = %q", got)
	}
}

func TestReadCriteriaNeutral(t *testing.T) {
	files := newMemFiles(t, map[string]string{
		ConfigFile:   `{"schema":1,"profile":"app","packs":["android"],"variants":["free","paid"],"noise":["docs/**"],"cascade":{"maxDepth":1}}`,
		CriteriaFile: "# Criteria\n\n- Prose.\n\n## Project rules\n\n- `app-tracker` — **MAJOR**. Analytics go through the tracker.\n",
	})
	c := readCriteria(t, files)
	depth := 1
	want := RepoCriteria{
		Schema: 1, Project: "github:github.com/acme/sample-app", Ref: "main", Source: SourceOverlay,
		Profile: "app", Packs: []string{"android"}, Variants: []string{"free", "paid"}, Dimensions: map[string]string{},
		DisabledRules: []string{}, RuleOptions: map[string]map[string]int{},
		Rules: []RepositoryRule{{ID: "app-tracker", Source: RepositorySource, Severity: Major, Text: "Analytics go through the tracker.", File: CriteriaFile, Line: 7}},
		Noise: []string{"docs/**"}, PublicAPI: []string{}, Consumers: []string{}, Cascade: CascadeConfig{MaxDepth: &depth},
		Files: []string{ConfigFile, CriteriaFile}, Notes: []string{},
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("criteria =\n%+v\nwant\n%+v", c, want)
	}
	only := readCriteria(t, newMemFiles(t, map[string]string{CriteriaFile: "- `app-tracker` — **MAJOR**. Text.\n"}))
	if only.Source != SourceOverlay || !reflect.DeepEqual(only.Files, []string{CriteriaFile}) || len(only.Rules) != 1 || only.Profile != "" {
		t.Errorf("criteria.md alone: %+v", only)
	}
}

func TestReadCriteriaDefault(t *testing.T) {
	c := readCriteria(t, newMemFiles(t, map[string]string{"README.md": "x"}))
	if c.Source != SourceDefault || len(c.Files) != 0 || len(c.Rules) != 0 || c.Profile != "" {
		t.Errorf("criteria = %+v, want the defaults", c)
	}
	if want := []string{"No repository criteria on main: only the pack and core rules apply."}; !reflect.DeepEqual(c.Notes, want) {
		t.Errorf("notes = %q, want %q", c.Notes, want)
	}
}

// TestReadCriteriaChangedByTheChangeRequest checks scenario S6 A on a
// synthetic tree: criteria files the change request edits are read from
// the target branch, and a note says the edit is not applied.
func TestReadCriteriaChangedByTheChangeRequest(t *testing.T) {
	files := newMemFiles(t, map[string]string{ConfigFile: `{"schema":1}`})
	c := readCriteria(t, files, "app/A.kt", ConfigFile, ".github/skills/app-code-review/SKILL.md", ".github/mr-review.json")
	want := "Criteria changed in this change request are not applied: .agent-code-review/config.json, .github/skills/app-code-review/SKILL.md, " +
		".github/mr-review.json. The criteria of main are used."
	if !reflect.DeepEqual(c.Notes, []string{want}) {
		t.Errorf("notes = %q, want %q", c.Notes, want)
	}
	if c := readCriteria(t, files, "app/A.kt", ".github/skills/other/SKILL.md"); len(c.Notes) != 0 {
		t.Errorf("notes for files that are not criteria: %q", c.Notes)
	}
}

func TestReadCriteriaErrors(t *testing.T) {
	bad := newMemFiles(t, map[string]string{ConfigFile: `{"schema":99}`})
	_, err := ReadCriteria(context.Background(), bad, CriteriaInput{Project: testProject, Ref: "main"})
	var ce *CriteriaError
	if !errors.As(err, &ce) || ce.File != ConfigFile {
		t.Errorf("invalid config: error %v, want a *CriteriaError for %s", err, ConfigFile)
	}
	down := errors.New("platform down")
	failing := newMemFiles(t, map[string]string{})
	failing.fail = map[string]error{CriteriaFile: down}
	if _, err := ReadCriteria(context.Background(), failing, CriteriaInput{Project: testProject, Ref: "main"}); !errors.Is(err, down) || !strings.Contains(err.Error(), CriteriaFile) {
		t.Errorf("read failure: error %v, want one that wraps it and names %s", err, CriteriaFile)
	}
}

// testCatalog is a pack catalog for the criteria tests: no-double-bang,
// and line-too-long, whose max is an option between 60 and 200.
const testCatalog = `{"schema":1,"pack":"android","rules":[` +
	`{"id":"no-double-bang","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[]},` +
	`{"id":"line-too-long","title":"T","severity":"MINOR","description":"Within {max}.","why":"W","fix":"F","languages":[],"tags":[],` +
	`"match":{"detector":"line-length","max":100},"options":{"max":{"min":60,"max":200}}}]}`

// testRules are the core rules and the rules of testCatalog.
func testRules(t *testing.T) []*rules.Rule {
	t.Helper()
	pack, err := rules.LoadCatalog([]byte(testCatalog))
	if err != nil {
		t.Fatal(err)
	}
	all, err := rules.Merge(rules.Core(), pack)
	if err != nil {
		t.Fatal(err)
	}
	return all
}
