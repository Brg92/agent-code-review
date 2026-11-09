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
		{"checklists are generated from the catalog", "mobile", set("checklists", []any{}), nil, "unknown field"},
		{"missing template", "mobile", set("templates", []any{"templates/none.md"}), nil, "none.md"},
		{"profile detection of another profile", "mobile", set("profileDetect", []any{map[string]any{"profile": "kmp", "path": "x"}}), nil, "kmp"},
		{"default profile not supported", "mobile", set("defaultProfile", "kmp"), nil, "kmp"},
		{"unknown profile", "mobile", set("profiles", []any{"app", "service"}), nil, "service"},
		{"kmp is a stack, not a profile", "mobile", set("profiles", []any{"app", "kmp"}), nil, "unknown profile \"kmp\""},
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

// toolkit is a dimension of a test pack.
func toolkit(edit func(d map[string]any)) any {
	d := map[string]any{
		"id": "ui-toolkit", "values": []any{"views", "compose", "both"}, "default": "both", "combined": "both",
		"detect": []any{
			map[string]any{"value": "compose", "path": "**/build.gradle.kts", "contains": "androidx.compose"},
			map[string]any{"value": "views", "path": "**/build.gradle.kts", "contains": "viewBinding"},
		},
	}
	if edit != nil {
		edit(d)
	}
	return []any{d}
}

func TestLoadPackDimensions(t *testing.T) {
	dir := copyPack(t, "mobile", "mobile", func(m map[string]any) { m["dimensions"] = toolkit(nil) })
	p, err := LoadPack(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Dimensions) != 1 {
		t.Fatalf("dimensions %+v", p.Dimensions)
	}
	d := p.Dimensions[0]
	if d.ID != "ui-toolkit" || !reflect.DeepEqual(d.Values, []string{"views", "compose", "both"}) || d.Default != "both" || d.Combined != "both" {
		t.Errorf("dimension %+v", d)
	}
	if len(d.Detect) != 2 || d.Detect[0].Value != "compose" || d.Detect[0].Path.String() != "**/build.gradle.kts" || d.Detect[0].Contains != "androidx.compose" {
		t.Errorf("signs %+v", d.Detect)
	}
	if p, err = LoadPack(filepath.Join(testPacks, "web")); err != nil || len(p.Dimensions) != 0 {
		t.Errorf("a pack without dimensions: %+v, %v", p.Dimensions, err)
	}
}

func TestLoadPackRejectsDimensions(t *testing.T) {
	tests := []struct {
		name string
		edit func(d map[string]any)
		want string
	}{
		{"profile is reserved", func(d map[string]any) { d["id"] = "profile" }, "profile"},
		{"id not lowercase", func(d map[string]any) { d["id"] = "UiToolkit" }, "UiToolkit"},
		{"no values", func(d map[string]any) { d["values"] = []any{} }, "values"},
		{"repeated value", func(d map[string]any) { d["values"] = []any{"views", "views"} }, "views"},
		{"value not lowercase", func(d map[string]any) { d["values"] = []any{"Views", "both"} }, "Views"},
		{"default not a value", func(d map[string]any) { d["default"] = "xml" }, "default"},
		{"missing default", func(d map[string]any) { delete(d, "default") }, "default"},
		{"combined not a value", func(d map[string]any) { d["combined"] = "xml" }, "combined"},
		{"sign of another value", func(d map[string]any) { d["detect"].([]any)[0].(map[string]any)["value"] = "xml" }, "xml"},
		{"sign without path", func(d map[string]any) { delete(d["detect"].([]any)[0].(map[string]any), "path") }, "path"},
		{"sign without value", func(d map[string]any) { delete(d["detect"].([]any)[0].(map[string]any), "value") }, "value"},
		{"bad glob", func(d map[string]any) { d["detect"].([]any)[0].(map[string]any)["path"] = "/a" }, "glob"},
		{"unknown field", func(d map[string]any) { d["weight"] = 1 }, "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyPack(t, "mobile", "mobile", func(m map[string]any) { m["dimensions"] = toolkit(tt.edit) })
			_, err := LoadPack(dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one that mentions %q", err, tt.want)
			}
		})
	}
	dir := copyPack(t, "mobile", "mobile", func(m map[string]any) {
		m["dimensions"] = append(toolkit(nil).([]any), toolkit(nil).([]any)...)
	})
	if _, err := LoadPack(dir); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("a dimension listed twice: error %v", err)
	}
}

// platformDim is a per-file dimension of a test pack.
func platformDim(edit func(d map[string]any)) any {
	d := map[string]any{
		"id": "platform", "values": []any{"common", "android", "ios"}, "default": "common",
		"map": []any{
			map[string]any{"path": "**/src/androidMain/**", "value": "android"},
			map[string]any{"path": "**/src/ios*Main/**", "value": "ios"},
		},
	}
	if edit != nil {
		edit(d)
	}
	return []any{d}
}

func TestLoadPackFileDimensionsAndPriority(t *testing.T) {
	dir := copyPack(t, "mobile", "mobile", func(m map[string]any) {
		m["fileDimensions"] = platformDim(nil)
		m["priority"] = 10
	})
	p, err := LoadPack(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Priority != 10 || len(p.FileDimensions) != 1 {
		t.Fatalf("priority %d, file dimensions %+v", p.Priority, p.FileDimensions)
	}
	d := p.FileDimensions[0]
	if d.ID != "platform" || !reflect.DeepEqual(d.Values, []string{"common", "android", "ios"}) || d.Default != "common" {
		t.Errorf("file dimension %+v", d)
	}
	if len(d.Map) != 2 || d.Map[1].Path.String() != "**/src/ios*Main/**" || d.Map[1].Value != "ios" {
		t.Errorf("map %+v", d.Map)
	}
	if p, err = LoadPack(filepath.Join(testPacks, "web")); err != nil || p.Priority != 0 || len(p.FileDimensions) != 0 {
		t.Errorf("a pack without them: %d %+v, %v", p.Priority, p.FileDimensions, err)
	}
}

func TestLoadPackRejectsFileDimensions(t *testing.T) {
	entry := func(d map[string]any, i int) map[string]any { return d["map"].([]any)[i].(map[string]any) }
	tests := []struct {
		name string
		edit func(d map[string]any)
		want string
	}{
		{"profile is reserved", func(d map[string]any) { d["id"] = "profile" }, "profile"},
		{"id not lowercase", func(d map[string]any) { d["id"] = "Platform" }, "Platform"},
		{"no values", func(d map[string]any) { d["values"] = []any{} }, "values"},
		{"repeated value", func(d map[string]any) { d["values"] = []any{"common", "common"} }, "common"},
		{"default not a value", func(d map[string]any) { d["default"] = "web" }, "default"},
		{"missing default", func(d map[string]any) { delete(d, "default") }, "default"},
		{"entry of another value", func(d map[string]any) { entry(d, 0)["value"] = "web" }, "web"},
		{"entry without path", func(d map[string]any) { delete(entry(d, 0), "path") }, "path"},
		{"entry without value", func(d map[string]any) { delete(entry(d, 0), "value") }, "value"},
		{"bad glob", func(d map[string]any) { entry(d, 0)["path"] = "/a" }, "glob"},
		{"unknown field", func(d map[string]any) { d["detect"] = []any{} }, "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyPack(t, "mobile", "mobile", func(m map[string]any) { m["fileDimensions"] = platformDim(tt.edit) })
			_, err := LoadPack(dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one that mentions %q", err, tt.want)
			}
		})
	}
	other := []struct {
		name string
		edit func(m map[string]any)
		want string
	}{
		{"listed twice", func(m map[string]any) {
			m["fileDimensions"] = append(platformDim(nil).([]any), platformDim(nil).([]any)...)
		}, "twice"},
		{"same id as a repository dimension", func(m map[string]any) {
			m["fileDimensions"] = platformDim(func(d map[string]any) { d["id"] = "ui-toolkit" })
			m["dimensions"] = toolkit(nil)
		}, "ui-toolkit"},
		{"priority is not an integer", func(m map[string]any) { m["priority"] = 1.5 }, "priority"},
	}
	for _, tt := range other {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyPack(t, "mobile", "mobile", tt.edit)
			_, err := LoadPack(dir)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one that mentions %q", err, tt.want)
			}
		})
	}
}

// TestMatchedRulesGiveTheTextOfTheirFindings checks that every rule of the
// shipped packs that is matched writes its findings itself: the why and the
// fix come from the catalog. A rule that needs judgement gives them too,
// because the judgement confirms its signals without writing text.
func TestMatchedRulesGiveTheTextOfTheirFindings(t *testing.T) {
	for _, id := range []string{"android", "kotlin-multiplatform"} {
		p, err := LoadPack(filepath.Join(packsDir, id))
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range p.Rules.Rules {
			if r.Matched() && (r.Why == "" || r.Fix == "") {
				t.Errorf("%s: rule %s is matched and has no why or fix", id, r.ID)
			}
		}
	}
}

func TestLoadPackSymbols(t *testing.T) {
	symbols := map[string]any{
		"packageKeyword": "package", "declarations": []any{map[string]any{"word": "fun", "kind": "function"}},
		"hidingModifiers": []any{"private"}, "publicModifiers": []any{"public"}, "publicByDefault": true,
		"annotation": "@", "qualifiers": []any{"."}, "parameterSeparator": ",",
		"body":           map[string]any{"open": "{", "close": "}"},
		"parameters":     map[string]any{"open": "(", "close": ")"},
		"typeParameters": map[string]any{"open": "<", "close": ">"},
	}
	dir := copyPack(t, "mobile", "mobile", func(m map[string]any) {
		m["languages"].([]any)[0].(map[string]any)["symbols"] = symbols
	})
	p, err := LoadPack(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := p.Languages[0].Symbols; s == nil || s.Declarations["fun"] != "function" || !s.PublicByDefault {
		t.Errorf("kotlin symbols = %+v", s)
	}
	if p.Languages[1].Symbols != nil {
		t.Errorf("java declares no symbols, got %+v", p.Languages[1].Symbols)
	}
	symbols["declarations"] = []any{}
	dir = copyPack(t, "mobile", "mobile", func(m map[string]any) {
		m["languages"].([]any)[0].(map[string]any)["symbols"] = symbols
	})
	if _, err := LoadPack(dir); err == nil || !strings.Contains(err.Error(), "kotlin") || !strings.Contains(err.Error(), "declarations") {
		t.Errorf("broken symbols: error %v", err)
	}
}
