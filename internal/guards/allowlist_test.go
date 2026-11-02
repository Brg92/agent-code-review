package guards

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseAllowlist(t *testing.T) {
	got := ParseAllowlist("# comment\n\nLICENSE\r\n  packs/**  \n#another\nbin/**")
	want := []string{"LICENSE", "packs/**", "bin/**"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseAllowlist = %v, want %v", got, want)
	}
}

func TestDisallowedPaths(t *testing.T) {
	patterns := []string{"LICENSE", "packs/**", "plugin-*.json"}
	tests := []struct {
		path    string
		allowed bool
	}{
		{"LICENSE", true},
		{"packs/android/manifest.json", true},
		{"plugin-a.json", true},
		{"LICENSE.txt", false},
		{"license", false},
		{"docs/LICENSE", false},
		{"packs", false},
		{"packs/", false},
		{"packsx/a", false},
		{"testdata/x.json", false},
		{"internal/a_test.go", false},
		{"packs/../internal/a.go", false},
		{"../packs/a", false},
		{"/packs/a", false},
		{`packs\a`, false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := DisallowedPaths(patterns, []string{tt.path})
			if allowed := len(got) == 0; allowed != tt.allowed {
				t.Errorf("DisallowedPaths(%q) = %v, want allowed = %v", tt.path, got, tt.allowed)
			}
		})
	}
}

func TestReleaseAllowlist(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "docs", "release-allowlist.txt"))
	if err != nil {
		t.Fatal(err)
	}
	patterns := ParseAllowlist(string(data))

	shipped := []string{
		"plugin.json",
		".claude-plugin/plugin.json",
		"com.github.copilot/plugin.json",
		"skills/review/SKILL.md",
		"agents/reviewer.md",
		"hooks/hooks.json",
		"packs/android/manifest.json",
		"schemas/judgement.schema.json",
		"bin/agent-code-review",
		"LICENSE",
		"README.md",
	}
	if bad := DisallowedPaths(patterns, shipped); len(bad) != 0 {
		t.Errorf("paths that must ship are rejected: %v", bad)
	}

	neverShipped := []string{
		"testdata/x.json",
		"internal/a_test.go",
		"cmd/agent-code-review/main.go",
		"tools/recorder/main.go",
		"docs/ci.md",
		"AGENTS.md",
		"go.mod",
	}
	got := DisallowedPaths(patterns, neverShipped)
	if !reflect.DeepEqual(got, neverShipped) {
		t.Errorf("DisallowedPaths = %v, want all of %v", got, neverShipped)
	}
}
