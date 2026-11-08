package packs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var testPacks = filepath.Join("..", "..", "testdata", "packs")

// copyPack copies a test pack into a new directory named dirName, applies
// edit to its manifest and returns the new pack directory.
func copyPack(t *testing.T, id, dirName string, edit func(m map[string]any)) string {
	t.Helper()
	src := filepath.Join(testPacks, id)
	dst := filepath.Join(t.TempDir(), dirName)
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		manifest := filepath.Join(dst, "manifest.json")
		var m map[string]any
		data, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		edit(m)
		out, _ := json.Marshal(m)
		if err := os.WriteFile(manifest, out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPack(t *testing.T) {
	p, err := LoadPack(filepath.Join(testPacks, "mobile"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "mobile" || p.Name != "Mobile" || !reflect.DeepEqual(p.Profiles, []string{"app", "library"}) || p.DefaultProfile != "app" {
		t.Errorf("pack = %+v", p)
	}
	var langs []string
	for _, l := range p.Languages {
		if l.Table == nil || l.Table.Language != l.ID {
			t.Errorf("language %s: table %+v", l.ID, l.Table)
		}
		langs = append(langs, l.ID+" "+strings.Join(l.Extensions, ","))
	}
	if want := []string{"kotlin .kt,.kts", "java .java"}; !reflect.DeepEqual(langs, want) {
		t.Errorf("languages %q, want %q", langs, want)
	}
	if p.Rules.Pack != "mobile" || len(p.Rules.Rules) != 2 {
		t.Errorf("rules %+v", p.Rules)
	}
	if len(p.Checklists) != 2 || p.Checklists[1].Profile != "library" || p.Checklists[1].Path != filepath.Join(p.Dir, "checklists", "library.md") {
		t.Errorf("checklists %+v", p.Checklists)
	}
	if len(p.Templates) != 1 || p.Templates[0] != filepath.Join(p.Dir, "templates", "summary.md") {
		t.Errorf("templates %+v", p.Templates)
	}
}

func TestLoadPackDefaultProfileIsTheFirstProfile(t *testing.T) {
	p, err := LoadPack(filepath.Join(testPacks, "web"))
	if err != nil {
		t.Fatal(err)
	}
	if p.DefaultProfile != "app" || len(p.ProfileDetect) != 0 {
		t.Errorf("default profile %q, profileDetect %+v", p.DefaultProfile, p.ProfileDetect)
	}
}

func TestLoadPackRejects(t *testing.T) {
	set := func(key string, v any) func(map[string]any) { return func(m map[string]any) { m[key] = v } }
	language := func(i int, key string, v any) func(map[string]any) {
		return func(m map[string]any) { m["languages"].([]any)[i].(map[string]any)[key] = v }
	}
	tests := []struct {
		name, dir string
		edit      func(map[string]any)
		files     map[string]string // extra files to write, relative to the pack
		want      string
	}{
		{"id is not the directory", "other", nil, nil, "directory"},
		{"unknown field", "mobile", set("lexer", "lexer/kotlin.json"), nil, "unknown field"},
		{"wrong version", "mobile", set("schema", 2), nil, "schema"},
		{"missing required field", "mobile", func(m map[string]any) { delete(m, "noise") }, nil, "noise"},
		{"path leaves the pack", "mobile", set("rules", "../rules.json"), nil, "rules"},
		{"missing rules file", "mobile", set("rules", "missing.json"), nil, "missing.json"},
		{"missing lexer table", "mobile", language(0, "lexer", "lexer/swift.json"), nil, "swift.json"},
		{"lexer table of another language", "mobile", language(1, "lexer", "lexer/kotlin.json"), nil, "language"},
		{"extension in two languages", "mobile", language(1, "extensions", []any{".java", ".kt"}), nil, ".kt"},
		{"no extensions", "mobile", language(1, "extensions", []any{}), nil, "extensions"},
		{"missing checklist", "mobile", set("checklists", []any{map[string]any{"profile": "app", "path": "checklists/none.md"}}), nil, "none.md"},
		{"checklist of another profile", "mobile", set("checklists", []any{map[string]any{"profile": "kmp", "path": "checklists/app.md"}}), nil, "kmp"},
		{"missing template", "mobile", set("templates", []any{"templates/none.md"}), nil, "none.md"},
		{"profile detection of another profile", "mobile", set("profileDetect", []any{map[string]any{"profile": "kmp", "path": "x"}}), nil, "kmp"},
		{"default profile not supported", "mobile", set("defaultProfile", "kmp"), nil, "kmp"},
		{"unknown profile", "mobile", set("profiles", []any{"app", "service"}), nil, "service"},
		{"bad glob", "mobile", set("noise", []any{"/build"}), nil, "glob"},
		{"catalog of another pack", "mobile", nil, map[string]string{"rules.json": `{"schema":1,"pack":"web","rules":[]}`}, "pack"},
		{"rule of a language the pack lacks", "mobile", nil, map[string]string{"rules.json": `{"schema":1,"pack":"mobile","rules":[` +
			`{"id":"r","title":"T","severity":"MAJOR","description":"D","languages":["swift"],"tags":[]}]}`}, "swift"},
		{"broken rules catalog", "mobile", nil, map[string]string{"rules.json": `{`}, "rules"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyPack(t, "mobile", tt.dir, tt.edit)
			for rel, content := range tt.files {
				writeFile(t, filepath.Join(dir, rel), content)
			}
			_, err := LoadPack(dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one that mentions %q", err, tt.want)
			}
		})
	}
	if _, err := LoadPack(filepath.Join(t.TempDir(), "none")); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Errorf("a directory without a manifest: error %v", err)
	}
}
