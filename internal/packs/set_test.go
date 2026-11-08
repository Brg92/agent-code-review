package packs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

func TestLoad(t *testing.T) {
	extra := filepath.Dir(copyPack(t, "web", "web2", func(m map[string]any) { m["id"] = "web2" }))
	writeFile(t, filepath.Join(extra, "web2", "rules.json"), `{"schema":1,"pack":"web2","rules":[]}`)
	writeFile(t, filepath.Join(extra, "README.md"), "Not a pack.\n")
	set, err := Load(testPacks, extra)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range set.Packs {
		ids = append(ids, p.ID)
	}
	if strings.Join(ids, " ") != "mobile web web2" {
		t.Errorf("packs %q, want built-in packs in name order, then the extra ones", ids)
	}
	if len(set.Rules) != len(rules.Core().Rules)+2+1 || set.Rules[0].ID != rules.Core().Rules[0].ID {
		t.Errorf("%d rules, first %s; want the core rules first, then the packs'", len(set.Rules), set.Rules[0].ID)
	}
}

func TestLoadRejects(t *testing.T) {
	dupRule := `{"schema":1,"pack":"web","rules":[{"id":"%s","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[]}]}`
	tests := []struct {
		name  string
		setup func(t *testing.T) []string
		want  string
	}{
		{"same pack id twice", func(t *testing.T) []string {
			return []string{testPacks, filepath.Dir(copyPack(t, "web", "web", nil))}
		}, `"web"`},
		{"rule id of two packs", func(t *testing.T) []string {
			dir := copyPack(t, "web", "other", func(m map[string]any) { m["id"] = "other" })
			writeFile(t, filepath.Join(dir, "rules.json"), strings.Replace(strings.Replace(dupRule, "%s", "global-scope", 1), `"web"`, `"other"`, 1))
			return []string{filepath.Join(testPacks), filepath.Dir(dir)}
		}, "global-scope"},
		{"rule id of the core", func(t *testing.T) []string {
			dir := copyPack(t, "web", "web", nil)
			writeFile(t, filepath.Join(dir, "rules.json"), strings.Replace(dupRule, "%s", "uses-removed-api", 1))
			return []string{filepath.Dir(dir)}
		}, "uses-removed-api"},
		{"directory without a manifest", func(t *testing.T) []string {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
				t.Fatal(err)
			}
			return []string{root}
		}, "manifest"},
		{"missing directory", func(t *testing.T) []string {
			return []string{filepath.Join(t.TempDir(), "none")}
		}, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(tt.setup(t)...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one that mentions %s", err, tt.want)
			}
		})
	}
}

// TestLoadRealPacks loads the packs shipped in packs/ once they have
// manifests.
func TestLoadRealPacks(t *testing.T) {
	root := filepath.Join("..", "..", "packs")
	manifests, err := filepath.Glob(filepath.Join(root, "*", ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) == 0 {
		t.Skip("no pack in packs/ has a manifest yet")
	}
	if _, err := Load(root); err != nil {
		t.Fatal(err)
	}
}
