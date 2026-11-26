package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runTool(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRunWritesTheTree(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tree")
	code, stdout, stderr := runTool(t, "-src", repoRoot, "-out", out)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "0.0.0-dev") || !strings.Contains(stdout, out) {
		t.Errorf("stdout = %q, want the version and the directory", stdout)
	}
	want := treeDigest(buildReal(t, func(o *options) { o.Version = "0.0.0-dev" }))
	if !strings.Contains(stdout, "sha256 "+want) {
		t.Errorf("stdout = %q, want the digest of the tree, sha256 %s", stdout, want)
	}
	data, err := os.ReadFile(filepath.Join(out, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil || m["version"] != "0.0.0-dev" {
		t.Errorf("plugin.json = %s (%v), want the default version", data, err)
	}

	code, _, stderr = runTool(t, "-src", repoRoot, "-out", out)
	if code != 1 || !strings.Contains(stderr, "not empty") {
		t.Errorf("second run: exit %d, stderr %q, want 1 and a refusal", code, stderr)
	}
}

func TestRunTakesAVersion(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tree")
	if code, _, stderr := runTool(t, "-src", repoRoot, "-out", out, "-version", "1.4.0-rc.1"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	data, _ := os.ReadFile(filepath.Join(out, ".claude-plugin", "plugin.json"))
	if !strings.Contains(string(data), `"version": "1.4.0-rc.1"`) {
		t.Errorf("claude manifest = %s", data)
	}
}

func TestRunUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no output directory", nil},
		{"unknown flag", []string{"-out", "x", "-bogus"}},
		{"no way to turn MCP on", []string{"-out", "x", "-mcp"}},
		{"argument", []string{"-out", "x", "extra"}},
		{"bad version", []string{"-out", "x", "-version", "one"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code, _, stderr := runTool(t, tt.args...); code != 2 || stderr == "" {
				t.Errorf("exit %d, stderr %q, want 2 and a message", code, stderr)
			}
		})
	}
	if code, _, _ := runTool(t, "-h"); code != 0 {
		t.Errorf("-h: exit %d, want 0", code)
	}
}

func TestRunFailures(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tree")
	if code, _, stderr := runTool(t, "-src", t.TempDir(), "-out", out); code != 1 || stderr == "" {
		t.Errorf("empty source: exit %d, stderr %q, want 1", code, stderr)
	}

	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "docs", "release-allowlist.txt"), []byte("nothing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runTool(t, "-src", src, "-out", out); code != 1 || stderr == "" {
		t.Errorf("no plugin sources: exit %d, stderr %q, want 1", code, stderr)
	}

	// A file where the output directory should be.
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runTool(t, "-src", repoRoot, "-out", filepath.Join(file, "tree")); code != 1 {
		t.Errorf("output below a file: exit %d, want 1", code)
	}
}
