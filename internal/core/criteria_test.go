package core

import (
	"context"
	"errors"
	"fmt"
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
		ConfigFile: `{"schema":1,"profile":"app","packs":["android"],"variants":["free","paid"],"noise":["docs/**"],"cascade":{"maxDepth":1},` +
			`"sharedLibraries":["gitlab:gitlab.example.com/acme/libs/core"]}`,
		CriteriaFile: "# Criteria\n\n- Prose.\n\n## Project rules\n\n- `app-tracker` — **MAJOR**. Analytics go through the tracker.\n",
	})
	c := readCriteria(t, files)
	depth := 1
	want := RepoCriteria{
		Schema: 1, Project: "github:github.com/acme/sample-app", Ref: "main", Source: SourceOverlay,
		Profile: "app", Packs: []string{"android"}, Variants: []string{"free", "paid"}, Dimensions: map[string]string{},
		DisabledRules: []string{}, RuleOptions: map[string]map[string]int{},
		Rules: []RepositoryRule{{ID: "app-tracker", Source: RepositorySource, Severity: Major, Text: "Analytics go through the tracker.", File: CriteriaFile, Line: 7}},
		Noise: []string{"docs/**"}, PublicAPI: []string{}, Consumers: []string{}, SharedLibraries: []string{"gitlab:gitlab.example.com/acme/libs/core"},
		Cascade: CascadeConfig{MaxDepth: &depth},
		Files:   []string{ConfigFile, CriteriaFile}, Notes: []string{},
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

const (
	legacySkill   = ".github/skills/app-code-review/SKILL.md"
	legacyRefA    = ".github/skills/app-code-review/references/a.md"
	legacyRefB    = ".github/skills/app-code-review/references/b.md"
	legacyMRRules = ".github/mr-review.json"
)

func TestReadCriteriaLegacy(t *testing.T) {
	files := newMemFiles(t, map[string]string{
		legacySkill: "---\nname: app-code-review\n---\n# App review\n\n- Prose.\n\n## Project rules\n\n- `app-tracker` — **MAJOR**. Use the tracker.\n",
		legacyRefB:  "# B\n\n## Naming\n\n- `app-naming` — **MINOR**. Names say what they hold.\n",
		legacyRefA:  "# A\n\n## Threads\n\n- `app-main-thread` — **BLOCKER**. No disk access on the main thread.\n",
		".github/skills/app-code-review/references/notes.txt": "- `app-not-markdown` — **MAJOR**. Not read.\n",
		".github/skills/other-skill/SKILL.md":                 "- `other-rule` — **MAJOR**. Not a code-review skill.\n",
		legacyMRRules: `{"uiToolkit":"compose","kind":"library","lineLength":120,"variants":["free","paid"],` +
			`"publicApi":["src/main/java/com/acme/api/**"],"consumers":["acme/sample-app"],` +
			`"approvals":{"enabled":false},"sharedLibraries":["acme/core-lib","acme/libs/feature"],"reviewers":["someone"]}`,
	})
	c := readCriteria(t, files)
	if c.Source != SourceLegacy || c.Profile != "library" || !reflect.DeepEqual(c.Variants, []string{"free", "paid"}) {
		t.Errorf("source %s, profile %q, variants %q", c.Source, c.Profile, c.Variants)
	}
	if want := []string{legacySkill, legacyRefA, legacyRefB, legacyMRRules}; !reflect.DeepEqual(c.Files, want) {
		t.Errorf("files = %q, want %q", c.Files, want)
	}
	var ids []string
	for _, r := range c.Rules {
		ids = append(ids, r.ID)
	}
	if want := []string{"app-tracker", "app-main-thread", "app-naming"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("rules %q, want %q", ids, want)
	}
	if !reflect.DeepEqual(c.Dimensions, map[string]string{"ui-toolkit": "compose"}) ||
		!reflect.DeepEqual(c.RuleOptions, map[string]map[string]int{"line-too-long": {"max": 120}}) {
		t.Errorf("dimensions %v, rule options %v", c.Dimensions, c.RuleOptions)
	}
	if !reflect.DeepEqual(c.PublicAPI, []string{"src/main/java/com/acme/api/**"}) || !reflect.DeepEqual(c.Consumers, []string{"github:github.com/acme/sample-app"}) {
		t.Errorf("public API %q, consumers %q", c.PublicAPI, c.Consumers)
	}
	if want := []string{"github:github.com/acme/core-lib", "github:github.com/acme/libs/feature"}; !reflect.DeepEqual(c.SharedLibraries, want) {
		t.Errorf("shared libraries %q, want %q", c.SharedLibraries, want)
	}
	want := []string{
		"Legacy criteria are in use: .github/skills and .github/mr-review.json.",
		"Repository criteria ignored: `approvals` (.github/mr-review.json) — the plugin never approves",
		"Repository criteria ignored: `reviewers` (.github/mr-review.json) — not a setting of .github/mr-review.json",
	}
	if !reflect.DeepEqual(c.Notes, want) {
		t.Errorf("notes =\n%q\nwant\n%q", c.Notes, want)
	}
}

func TestReadCriteriaLegacyParts(t *testing.T) {
	skills := readCriteria(t, newMemFiles(t, map[string]string{legacySkill: "- `app-tracker` — **MAJOR**. Text.\n"}))
	if skills.Source != SourceLegacy || skills.Notes[0] != "Legacy criteria are in use: .github/skills." {
		t.Errorf("skills alone: %s, %q", skills.Source, skills.Notes)
	}
	config := readCriteria(t, newMemFiles(t, map[string]string{legacyMRRules: `{"profile":"library"}`}))
	if config.Source != SourceLegacy || config.Profile != "library" || config.Notes[0] != "Legacy criteria are in use: .github/mr-review.json." {
		t.Errorf("mr-review.json alone, with profile: %s, %q, %q", config.Source, config.Profile, config.Notes)
	}
	empty := readCriteria(t, newMemFiles(t, map[string]string{legacyRefA: "- `app-rule` — **MAJOR**. Text.\n"}))
	want := []string{"Legacy criteria are in use: .github/skills.",
		"Repository criteria ignored: `app-code-review` (.github/skills/app-code-review) — it has no SKILL.md"}
	if empty.Source != SourceLegacy || !reflect.DeepEqual(empty.Notes, want) || len(empty.Rules) != 0 {
		t.Errorf("skill without SKILL.md: %s, %q, %+v", empty.Source, empty.Notes, empty.Rules)
	}
}

func TestReadCriteriaLegacyConfigErrors(t *testing.T) {
	tests := []struct{ name, data, pointer string }{
		{"not JSON", `{`, ""},
		{"ui toolkit", `{"uiToolkit":"flutter"}`, "/uiToolkit"},
		{"kind", `{"kind":"service"}`, "/kind"},
		{"kind and profile disagree", `{"kind":"app","profile":"library"}`, "/profile"},
		{"line length", `{"lineLength":"120"}`, "/lineLength"},
		{"variants", `{"variants":"free"}`, "/variants"},
		{"public API", `{"publicApi":["/abs"]}`, "/publicApi/0"},
		{"consumer", `{"consumers":["sample-app"]}`, "/consumers/0"},
		{"shared library", `{"sharedLibraries":["core-lib"]}`, "/sharedLibraries/0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadCriteria(context.Background(), newMemFiles(t, map[string]string{legacyMRRules: tt.data}), CriteriaInput{Project: testProject, Ref: "main"})
			var ce *CriteriaError
			if !errors.As(err, &ce) || ce.File != legacyMRRules || len(ce.Errors) != 1 || ce.Errors[0].Pointer != tt.pointer {
				t.Errorf("error %v, want one at %s %q", err, legacyMRRules, tt.pointer)
			}
		})
	}
}

func TestReadCriteriaBothLayouts(t *testing.T) {
	files := newMemFiles(t, map[string]string{
		ConfigFile:    `{"schema":1,"profile":"app"}`,
		legacySkill:   "- `app-legacy` — **MAJOR**. Not read.\n",
		legacyMRRules: `{"kind":"library"}`,
	})
	c := readCriteria(t, files)
	if c.Source != SourceOverlay || c.Profile != "app" || len(c.Rules) != 0 || !reflect.DeepEqual(c.Files, []string{ConfigFile}) {
		t.Errorf("criteria = %+v, want the neutral layout alone", c)
	}
	want := []string{"Legacy criteria ignored: .agent-code-review/ is present, so .github/skills and .github/mr-review.json are not read."}
	if !reflect.DeepEqual(c.Notes, want) {
		t.Errorf("notes = %q, want %q", c.Notes, want)
	}
	// Finding the legacy layout fetches .github/mr-review.json, as a real
	// run does, but reads no skill file.
	for _, p := range files.read {
		if strings.HasPrefix(p, ".github/skills/app-code-review/") {
			t.Errorf("read %s although the neutral layout is present", p)
		}
	}
}

func TestReadCriteriaDisabledRules(t *testing.T) {
	files := newMemFiles(t, map[string]string{
		ConfigFile:   `{"schema":1,"disabledRules":["app-tracker","no-double-bang","logic-defect","app-unknown"]}`,
		CriteriaFile: "- `app-tracker` — **MAJOR**. Use the tracker.\n",
	})
	c := readCriteria(t, files)
	if !reflect.DeepEqual(c.DisabledRules, []string{"app-tracker"}) {
		t.Errorf("disabled rules = %q, want only the repository rule", c.DisabledRules)
	}
	want := []string{
		"Repository criteria ignored: `no-double-bang` in disabledRules (.agent-code-review/config.json) — pack and core rules always apply",
		"Repository criteria ignored: `logic-defect` in disabledRules (.agent-code-review/config.json) — pack and core rules always apply",
		"Repository criteria ignored: `app-unknown` in disabledRules (.agent-code-review/config.json) — no repository rule has this id",
	}
	if !reflect.DeepEqual(c.Notes, want) {
		t.Errorf("notes =\n%q\nwant\n%q", c.Notes, want)
	}
}

func TestReadCriteriaRuleOptions(t *testing.T) {
	files := newMemFiles(t, map[string]string{
		ConfigFile: `{"schema":1,"ruleOptions":{"line-too-long":{"max":120,"min":3},"no-double-bang":{"max":1},"app-rule":{"max":1}}}`,
	})
	c := readCriteria(t, files)
	if want := map[string]map[string]int{"line-too-long": {"max": 120}}; !reflect.DeepEqual(c.RuleOptions, want) {
		t.Errorf("rule options = %v, want %v", c.RuleOptions, want)
	}
	want := []string{
		"Repository criteria ignored: `app-rule.max` in ruleOptions (.agent-code-review/config.json) — no pack or core rule has this id",
		"Repository criteria ignored: `line-too-long.min` in ruleOptions (.agent-code-review/config.json) — the rule has no option min",
		"Repository criteria ignored: `no-double-bang.max` in ruleOptions (.agent-code-review/config.json) — the rule has no option max",
	}
	if !reflect.DeepEqual(c.Notes, want) {
		t.Errorf("notes =\n%q\nwant\n%q", c.Notes, want)
	}
	for _, v := range []int{59, 201} {
		files := newMemFiles(t, map[string]string{ConfigFile: fmt.Sprintf(`{"schema":1,"ruleOptions":{"line-too-long":{"max":%d}}}`, v)})
		c := readCriteria(t, files)
		note := fmt.Sprintf("Repository criteria ignored: `line-too-long.max` in ruleOptions (.agent-code-review/config.json) — %d is outside 60-200", v)
		if len(c.RuleOptions) != 0 || !reflect.DeepEqual(c.Notes, []string{note}) {
			t.Errorf("max %d: options %v, notes %q", v, c.RuleOptions, c.Notes)
		}
	}
	legacy := readCriteria(t, newMemFiles(t, map[string]string{legacyMRRules: `{"lineLength":300}`}))
	note := "Repository criteria ignored: `lineLength` (.github/mr-review.json) — 300 is outside 60-200"
	if len(legacy.RuleOptions) != 0 || !reflect.DeepEqual(legacy.Notes[1:], []string{note}) {
		t.Errorf("legacy lineLength 300: options %v, notes %q", legacy.RuleOptions, legacy.Notes)
	}
}
