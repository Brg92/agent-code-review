package packs

import (
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

func glob(t *testing.T, s string) rules.Glob {
	t.Helper()
	g, err := rules.CompileGlob(s)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// platformPack is an in-memory pack with a platform file dimension.
func platformPack(t *testing.T, id string, priority int, def string, entries ...string) *Pack {
	t.Helper()
	d := FileDimension{ID: "platform", Values: []string{"common", "android", "ios"}, Default: def}
	for i := 0; i+1 < len(entries); i += 2 {
		d.Map = append(d.Map, FileMapEntry{Path: glob(t, entries[i]), Value: entries[i+1]})
	}
	return &Pack{ID: id, Priority: priority, FileDimensions: []FileDimension{d}}
}

func TestResolveFileDimensions(t *testing.T) {
	android := platformPack(t, "android", 0, "android")
	kmp := platformPack(t, "kmp", 10, "common",
		"**/src/commonMain/**", "common", "**/src/androidMain/**", "android", "**/src/ios*Main/**", "ios")
	tests := []struct {
		name   string
		active []*Pack
		path   string
		want   string
	}{
		{"android alone: default", []*Pack{android}, "app/src/main/A.kt", "android"},
		{"kmp wins over android", []*Pack{android, kmp}, "shared/src/commonMain/kotlin/A.kt", "common"},
		{"kmp wins in any order", []*Pack{kmp, android}, "shared/src/androidMain/kotlin/A.kt", "android"},
		{"ios source set", []*Pack{android, kmp}, "shared/src/iosArm64Main/kotlin/A.kt", "ios"},
		{"no entry: default of the winner", []*Pack{android, kmp}, "build.gradle.kts", "common"},
		{"first matching entry wins", []*Pack{platformPack(t, "p", 0, "ios", "**/a/**", "android", "**/a/**", "common")}, "x/a/B.kt", "android"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveFileDimensions(tt.active, tt.path)
			if got["platform"] != tt.want || len(got) != 1 {
				t.Errorf("dimensions = %v, want platform %s", got, tt.want)
			}
		})
	}
	if got := ResolveFileDimensions([]*Pack{{ID: "none"}}, "A.kt"); len(got) != 0 {
		t.Errorf("a pack without file dimensions: %v", got)
	}
}

func TestCheckFileDimensions(t *testing.T) {
	a := platformPack(t, "a", 5, "common", "**/x/**", "ios")
	same := platformPack(t, "b", 5, "common", "**/x/**", "ios")
	otherMap := platformPack(t, "c", 5, "common", "**/x/**", "android")
	otherDefault := platformPack(t, "d", 5, "ios", "**/x/**", "ios")
	higher := platformPack(t, "e", 6, "android")
	tests := []struct {
		name   string
		active []*Pack
		want   string
	}{
		{"one pack", []*Pack{a}, ""},
		{"equal priority, same mapping", []*Pack{a, same}, ""},
		{"equal priority, other map", []*Pack{a, otherMap}, "platform"},
		{"equal priority, other default", []*Pack{a, otherDefault}, "platform"},
		{"different priorities may differ", []*Pack{a, higher}, ""},
		{"only the top priority counts", []*Pack{higher, a, otherMap}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkFileDimensions(tt.active)
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("error = %v", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("error = %v, want one that mentions %q", err, tt.want)
			}
		})
	}
}
