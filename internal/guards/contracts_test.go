package guards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
)

const (
	draft2020 = "https://json-schema.org/draft/2020-12/schema"
	schemaIDs = "https://github.com/Brg92/agent-code-review/schemas/"
)

// moreValidExamples lists files outside testdata/contracts that must match
// a schema.
var moreValidExamples = map[string][]string{
	"judgement":   {"testdata/scenarios/*/judgements/*.json"},
	"lexer-table": {"packs/*/lexer/*.json"},
}

func schemaFiles(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, "schemas", "*.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no schemas in schemas/")
	}
	byName := map[string]string{}
	for _, f := range files {
		byName[strings.TrimSuffix(filepath.Base(f), ".schema.json")] = f
	}
	return byName
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

// undescribed returns the JSON pointers of properties and definitions in a
// schema that have no description.
func undescribed(schema any, at string) []string {
	s, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	var missing []string
	for _, group := range []string{"properties", "$defs"} {
		members, _ := s[group].(map[string]any)
		for name, member := range members {
			here := at + "/" + group + "/" + name
			if m, ok := member.(map[string]any); !ok || m["description"] == nil {
				missing = append(missing, here)
			}
			missing = append(missing, undescribed(member, here)...)
		}
	}
	for _, k := range []string{"items", "additionalProperties"} {
		missing = append(missing, undescribed(s[k], at+"/"+k)...)
	}
	sort.Strings(missing)
	return missing
}

func TestSchemasFollowConventions(t *testing.T) {
	defs := map[string]any{}
	defOwner := map[string]string{}
	for name, file := range schemaFiles(t) {
		s := readJSON(t, file)
		if s["$schema"] != draft2020 {
			t.Errorf("%s: $schema = %v, want %s", name, s["$schema"], draft2020)
		}
		if want := schemaIDs + name + ".schema.json"; s["$id"] != want {
			t.Errorf("%s: $id = %v, want %s", name, s["$id"], want)
		}
		if s["type"] != "object" || s["additionalProperties"] != false {
			t.Errorf("%s: the root must be an object with additionalProperties false", name)
		}
		props, _ := s["properties"].(map[string]any)
		version, _ := props["schema"].(map[string]any)
		required, _ := s["required"].([]any)
		if version["const"] != 1.0 || len(required) == 0 || required[0] != "schema" {
			t.Errorf("%s: needs a required \"schema\" property, listed first, with const 1", name)
		}
		if s["description"] == nil {
			t.Errorf("%s: no description", name)
		}
		for _, p := range undescribed(s, "") {
			t.Errorf("%s: %s has no description", name, p)
		}
		own, _ := s["$defs"].(map[string]any)
		for def, body := range own {
			if prev, ok := defs[def]; ok && !reflect.DeepEqual(prev, body) {
				t.Errorf("%s: $defs/%s differs from the one in %s", name, def, defOwner[def])
			}
			defs[def], defOwner[def] = body, name
		}
	}
}

func compileSchema(t *testing.T, file string) *jsonschema.Schema {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	s, err := jsonschema.Compile(data)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return s
}

func globAll(t *testing.T, patterns ...string) []string {
	t.Helper()
	var files []string
	for _, p := range patterns {
		matches, err := filepath.Glob(filepath.Join(repoRoot, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	return files
}

func TestContractExamples(t *testing.T) {
	schemas := schemaFiles(t)
	for name, file := range schemas {
		t.Run(name, func(t *testing.T) {
			s := compileSchema(t, file)
			valid := globAll(t, append([]string{"testdata/contracts/" + name + "/valid/*.json"}, moreValidExamples[name]...)...)
			invalid := globAll(t, "testdata/contracts/"+name+"/invalid/*.json")
			if len(valid) == 0 || len(invalid) == 0 {
				t.Fatalf("want at least one valid and one invalid example, got %d and %d", len(valid), len(invalid))
			}
			for _, f := range valid {
				data, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				issues, err := s.Validate(data)
				if err != nil {
					t.Errorf("%s: %v", f, err)
				}
				for _, i := range issues {
					t.Errorf("%s: %s", f, i)
				}
			}
			for _, f := range invalid {
				data, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				issues, err := s.Validate(data)
				if err == nil && len(issues) == 0 {
					t.Errorf("%s: matches the schema, want at least one issue", f)
				}
				for _, i := range issues {
					t.Logf("%s: %s", filepath.Base(f), i)
				}
			}
		})
	}
	dirs, err := filepath.Glob(filepath.Join(repoRoot, "testdata", "contracts", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if _, ok := schemas[filepath.Base(d)]; !ok {
			t.Errorf("%s has no schema", d)
		}
	}
}

// TestInvalidOverlayNamesEveryField checks scenario S6 B: the error names
// each invalid field of the overlay, not only the first one.
func TestInvalidOverlayNamesEveryField(t *testing.T) {
	s := compileSchema(t, filepath.Join(repoRoot, "schemas", "config.schema.json"))
	data, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "contracts", "config", "invalid", "s6-invalid-overlay.json"))
	if err != nil {
		t.Fatal(err)
	}
	issues, err := s.Validate(data)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, i := range issues {
		got = append(got, i.Path+" "+i.Keyword)
	}
	want := []string{"/profile enum", "/schema const", "/variants type"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("issues = %q, want %q", got, want)
	}
}

// TestJudgementRulesAreCataloged keeps the rule ids of the scenario
// judgements in step with the example rules catalog.
func TestJudgementRulesAreCataloged(t *testing.T) {
	catalog := readJSON(t, filepath.Join(repoRoot, "testdata", "contracts", "rules-catalog", "valid", "android.json"))
	known := map[string]bool{}
	rules, _ := catalog["rules"].([]any)
	for _, r := range rules {
		rule, _ := r.(map[string]any)
		id, _ := rule["id"].(string)
		known[id] = true
	}
	for _, f := range globAll(t, moreValidExamples["judgement"]...) {
		j := readJSON(t, f)
		for _, group := range []string{"findings", "preExisting"} {
			list, _ := j[group].([]any)
			for _, item := range list {
				finding, _ := item.(map[string]any)
				if id, _ := finding["rule"].(string); !known[id] {
					t.Errorf("%s: rule %q is not in the rules catalog", f, id)
				}
			}
		}
	}
}
