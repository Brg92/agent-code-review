package rules

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var contractDir = filepath.Join("..", "..", "testdata", "contracts", "rules-catalog")

func readFile(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLoadCatalog(t *testing.T) {
	c, err := LoadCatalog(readFile(t, filepath.Join(contractDir, "valid", "legacy-signals.json")))
	if err != nil {
		t.Fatal(err)
	}
	if c.Pack != "android" || len(c.Rules) != 18 {
		t.Fatalf("pack %q with %d rules", c.Pack, len(c.Rules))
	}
	byID := map[string]*Rule{}
	for _, r := range c.Rules {
		byID[r.ID] = r
	}
	if r := byID["no-double-bang"]; r == nil || !r.NeedsJudgement || r.Severity != "MAJOR" || !r.Matched() {
		t.Errorf("no-double-bang = %+v", r)
	}
	if r := byID["missing-tests"]; r == nil || r.Matched() {
		t.Errorf("missing-tests should have no match: %+v", r)
	}
	if r := byID["global-scope"]; r == nil || r.Title == "" || r.Description == "" || len(r.Languages) != 2 || len(r.Tags) != 1 {
		t.Errorf("global-scope = %+v", r)
	}
}

func TestLoadCatalogFindingText(t *testing.T) {
	c, err := LoadCatalog([]byte(catalogWith(`{"detector":"line-length","max":1}`)))
	if err != nil {
		t.Fatal(err)
	}
	if r := c.Rules[0]; r.Why != "W" || r.Fix != "F" {
		t.Errorf("why %q, fix %q", r.Why, r.Fix)
	}
	// A rule that needs judgement, or that is not matched, may leave the
	// text to the judgement.
	for name, data := range map[string]string{
		"needs judgement": strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `"why":"W","fix":"F",`, `"needsJudgement":true,`, 1),
		"not matched":     `{"schema":1,"pack":"test","rules":[{"id":"r","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[]}]}`,
	} {
		if _, err := LoadCatalog([]byte(data)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestLoadCatalogMatchesTheContract keeps the loader in step with the
// rules catalog schema.
func TestLoadCatalogMatchesTheContract(t *testing.T) {
	for _, kind := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join(contractDir, kind, "*.json"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no %s examples: %v", kind, err)
		}
		for _, f := range files {
			_, err := LoadCatalog(readFile(t, f))
			if kind == "valid" && err != nil {
				t.Errorf("%s: %v", f, err)
			}
			if kind == "invalid" && err == nil {
				t.Errorf("%s: loaded, want an error", f)
			}
		}
	}
}

// Change-request matches for the loader tests.
const (
	missingTest = `{"scope":"change-request","detector":"missing-test","sources":["**/src/main/**/*.kt"],"nameSuffixes":["ViewModel"],` +
		`"moduleSeparator":"src/main/","testRoots":["src/test"],"testName":"{name}Test","testExtensions":[".kt"]}`
	counterpart = `{"scope":"change-request","detector":"counterpart","groups":["androidMain","iosMain"],"groupPattern":"src/{group}/",` +
		`"stripSuffixes":[".android",".ios"]}`
)

func TestLoadCatalogChangeRequestScope(t *testing.T) {
	for name, m := range map[string]string{
		"missing-test":          missingTest,
		"counterpart":           counterpart,
		"counterpart, variants": strings.Replace(counterpart, `"groups":["androidMain","iosMain"]`, `"groupsFrom":"config.variants"`, 1),
		"counterpart, no strip": strings.Replace(counterpart, `,"stripSuffixes":[".android",".ios"]`, ``, 1),
	} {
		c, err := LoadCatalog([]byte(catalogWith(m)))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if r := c.Rules[0]; !r.Matched() || !r.OnChangeRequest() {
			t.Errorf("%s: matched %v, on the change request %v", name, r.Matched(), r.OnChangeRequest())
		}
	}
	c, err := LoadCatalog([]byte(catalogWith(`{"detector":"line-length","max":1}`)))
	if err != nil || c.Rules[0].OnChangeRequest() {
		t.Errorf("a file rule is on the change request: %v", err)
	}
}

// catalogWith returns a catalog with one rule whose match is the given
// JSON.
func catalogWith(match string) string {
	return `{"schema":1,"pack":"test","rules":[{"id":"r","title":"T","severity":"MAJOR","description":"D","why":"W","fix":"F",` +
		`"languages":["kotlin"],"tags":[],"match":` + match + `}]}`
}

func TestLoadCatalogRejects(t *testing.T) {
	tok := `{"patterns":[{"tokens":[%s]}]}`
	with := func(elems string) string { return catalogWith(strings.Replace(tok, "%s", elems, 1)) }
	tests := []struct{ name, data, want string }{
		{"not JSON", `{`, "decode"},
		{"trailing data", catalogWith(`{"detector":"line-length","max":1}`) + `{}`, "trailing"},
		{"unknown field", strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `"tags"`, `"tag":[],"tags"`, 1), "unknown field"},
		{"unknown nested field", with(`{"kind":"punct","txt":"("}`), "unknown field"},
		{"wrong version", strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `"schema":1`, `"schema":2`, 1), "schema"},
		{"no rules", `{"schema":1,"pack":"test"}`, "rules"},
		{"bad pack id", strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `"test"`, `"Test"`, 1), "pack"},
		{"no languages", strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `"languages":["kotlin"],`, ``, 1), "languages"},
		{"duplicate rule", strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `}]}`,
			`},{"id":"r","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[]}]}`, 1), "twice"},
		{"bad severity", strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `MAJOR`, `HIGH`, 1), "severity"},
		{"text and regexp", with(`{"text":"a","regexp":"a"}`), "at most one"},
		{"bad regexp", with(`{"regexp":"("}`), "regexp"},
		{"unknown kind", with(`{"kind":["punct","comment"]}`), "kind"},
		{"empty kind list", with(`{"kind":[]}`), "kind"},
		{"two at", with(`{"kind":"punct","at":true},{"kind":"punct","at":true}`), "at"},
		{"at false", with(`{"kind":"punct","at":false}`), "at"},
		{"at inside repeat", with(`{"repeat":[{"kind":"punct","at":true}]}`), "at"},
		{"gap and token", with(`{"kind":"punct","gap":{}}`), "one of"},
		{"min without repeat", with(`{"kind":"punct","min":1}`), "min"},
		{"repeat max below min", with(`{"repeat":[{"kind":"punct"}],"min":2,"max":1}`), "max"},
		{"negative gap", with(`{"gap":{"max":-1}}`), "gap"},
		{"one alternative", with(`{"anyOf":[[{"kind":"punct"}]]}`), "anyOf"},
		{"empty alternative", with(`{"anyOf":[[{"kind":"punct"}],[]]}`), "anyOf"},
		{"pattern language outside the rule", catalogWith(`{"patterns":[{"languages":["java"],"tokens":[{"kind":"punct"}]}]}`), "language"},
		{"no patterns", catalogWith(`{"lines":"all"}`), "patterns"},
		{"detector with patterns", catalogWith(`{"detector":"line-length","max":1,"patterns":[{"tokens":[{"kind":"punct"}]}]}`), "patterns"},
		{"unknown detector", catalogWith(`{"detector":"todo"}`), "detector"},
		{"line length without max", catalogWith(`{"detector":"line-length"}`), "max"},
		{"line length with words", catalogWith(`{"detector":"line-length","max":1,"types":["class"]}`), "types"},
		{"missing doc without body", catalogWith(`{"detector":"missing-doc","declarations":["fun"],"types":["class"],"annotation":"@","header":{"open":"(","close":")"}}`), "body"},
		{"bad lines", catalogWith(`{"detector":"line-length","max":1,"lines":"some"}`), "lines"},
		{"bad glob", catalogWith(`{"detector":"line-length","max":1,"files":{"include":["/a"]}}`), "glob"},
		{"bad tests mode", catalogWith(`{"detector":"line-length","max":1,"files":{"tests":"never"}}`), "tests"},
		{"empty include", catalogWith(`{"detector":"line-length","max":1,"files":{"include":[]}}`), "include"},
		{"blank why", strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `"why":"W"`, `"why":" "`, 1), "why"},
		{"blank fix", strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `"fix":"F"`, `"fix":""`, 1), "fix"},
		{"matched without why", strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `"why":"W",`, ``, 1), "why"},
		{"matched without fix", strings.Replace(catalogWith(`{"detector":"line-length","max":1}`), `"fix":"F",`, ``, 1), "fix"},
		{"unknown scope", catalogWith(`{"scope":"file","detector":"line-length","max":1}`), "scope"},
		{"change-request parameter on a file", catalogWith(`{"detector":"line-length","max":1,"groupPattern":"src/{group}/"}`), "groupPattern"},
		{"change-request without detector", catalogWith(`{"scope":"change-request"}`), "detector"},
		{"file detector on the change request", catalogWith(`{"scope":"change-request","detector":"line-length","max":1}`), "max"},
		{"unknown change-request detector", catalogWith(`{"scope":"change-request","detector":"todo"}`), "todo"},
		{"missing-test without sources", catalogWith(strings.Replace(missingTest, `"sources":["**/src/main/**/*.kt"],`, ``, 1)), "sources"},
		{"missing-test bad source glob", catalogWith(strings.Replace(missingTest, `**/src/main/**/*.kt`, `/a`, 1)), "glob"},
		{"missing-test suffix not a word", catalogWith(strings.Replace(missingTest, `"ViewModel"`, `"View-Model"`, 1)), "nameSuffixes"},
		{"missing-test separator without slash", catalogWith(strings.Replace(missingTest, `"src/main/"`, `"src/main"`, 1)), "moduleSeparator"},
		{"missing-test root with wildcard", catalogWith(strings.Replace(missingTest, `"src/test"`, `"src/*"`, 1)), "testRoots"},
		{"missing-test name without placeholder", catalogWith(strings.Replace(missingTest, `{name}Test`, `Test`, 1)), "testName"},
		{"missing-test extension without dot", catalogWith(strings.Replace(missingTest, `".kt"`, `"kt"`, 1)), "testExtensions"},
		{"missing-test with groups", catalogWith(strings.TrimSuffix(missingTest, "}") + `,"groups":["a","b"]}`), "counterpart"},
		{"counterpart with test roots", catalogWith(strings.TrimSuffix(counterpart, "}") + `,"testRoots":["src/test"]}`), "missing-test"},
		{"counterpart with both groups", catalogWith(strings.Replace(counterpart, `"groups"`, `"groupsFrom":"config.variants","groups"`, 1)), "exactly one"},
		{"counterpart without groups", catalogWith(strings.Replace(counterpart, `"groups":["androidMain","iosMain"],`, ``, 1)), "exactly one"},
		{"counterpart one group", catalogWith(strings.Replace(counterpart, `"androidMain",`, ``, 1)), "two"},
		{"counterpart repeated group", catalogWith(strings.Replace(counterpart, `"iosMain"`, `"androidMain"`, 1)), "twice"},
		{"counterpart unknown groupsFrom", catalogWith(strings.Replace(counterpart, `"groups":["androidMain","iosMain"]`, `"groupsFrom":"config.flavors"`, 1)), "groupsFrom"},
		{"counterpart pattern without placeholder", catalogWith(strings.Replace(counterpart, `src/{group}/`, `src/main/`, 1)), "groupPattern"},
		{"counterpart pattern twice", catalogWith(strings.Replace(counterpart, `src/{group}/`, `{group}/{group}/`, 1)), "groupPattern"},
		{"counterpart pattern without final slash", catalogWith(strings.Replace(counterpart, `src/{group}/`, `src/{group}`, 1)), "groupPattern"},
		{"counterpart suffix without dot", catalogWith(strings.Replace(counterpart, `".ios"`, `"ios"`, 1)), "stripSuffixes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadCatalog([]byte(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one that mentions %q", err, tt.want)
			}
		})
	}
}

// catalogWithField returns a catalog with one rule that has the given
// extra JSON members.
func catalogWithField(members string) string {
	return `{"schema":1,"pack":"test","rules":[{"id":"r","title":"T","severity":"MAJOR","description":"D","why":"W","fix":"F",` +
		`"languages":[],"tags":[],` + members + `}]}`
}

func TestLoadCatalogWhen(t *testing.T) {
	c, err := LoadCatalog([]byte(catalogWithField(`"when":{"profile":["library"],"ui-toolkit":["views","both"]}`)))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"profile": {"library"}, "ui-toolkit": {"views", "both"}}
	if got := c.Rules[0].When; !reflect.DeepEqual(got, want) {
		t.Errorf("when = %v, want %v", got, want)
	}
	c, err = LoadCatalog([]byte(catalogWithField(`"when":{}`)))
	if err != nil || len(c.Rules[0].When) != 0 {
		t.Errorf("an empty when: %v, %v", c, err)
	}
}

func TestLoadCatalogRejectsWhen(t *testing.T) {
	tests := []struct{ name, when, want string }{
		{"empty list", `{"ui-toolkit":[]}`, "ui-toolkit"},
		{"not a list", `{"ui-toolkit":"views"}`, "when"},
		{"unknown profile", `{"profile":["desktop"]}`, "desktop"},
		{"kmp is a stack, not a profile", `{"profile":["kmp"]}`, "kmp"},
		{"repeated value", `{"ui-toolkit":["views","views"]}`, "views"},
		{"value not lowercase", `{"ui-toolkit":["Views"]}`, "Views"},
		{"dimension not lowercase", `{"UiToolkit":["views"]}`, "UiToolkit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadCatalog([]byte(catalogWithField(`"when":` + tt.when)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one that mentions %q", err, tt.want)
			}
		})
	}
}

func TestLoadCatalogGroup(t *testing.T) {
	c, err := LoadCatalog([]byte(catalogWithField(`"group":"Error handling"`)))
	if err != nil || c.Rules[0].Group != "Error handling" {
		t.Errorf("group = %+v, %v", c.Rules[0], err)
	}
	for _, bad := range []string{`""`, `"  "`, `3`} {
		if _, err := LoadCatalog([]byte(catalogWithField(`"group":` + bad))); err == nil {
			t.Errorf("group %s loaded, want an error", bad)
		}
	}
}
