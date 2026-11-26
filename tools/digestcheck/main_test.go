package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	digestA = strings.Repeat("a", 64)
	digestB = strings.Repeat("b", 64)
)

// collect writes digest files the way the download of the CI artifacts lays
// them out: one directory per artifact.
func collect(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runTool(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRunAgrees(t *testing.T) {
	root := collect(t, map[string]string{
		"digest-linux/digest.txt":   digestA + "\n",
		"digest-macos/digest.txt":   digestA + "\n",
		"digest-windows/digest.txt": digestA + "\n",
		"digest-linux/other.txt":    "not a digest",
	})
	code, stdout, stderr := runTool("-n", "3", root)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "3 digests") || !strings.Contains(stdout, digestA) {
		t.Errorf("stdout = %q, want the count and the digest", stdout)
	}
}

func TestRunFails(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		n     string
		want  string
	}{
		{"one differs", map[string]string{
			"l/digest.txt": digestA + "\n", "m/digest.txt": digestA + "\n", "w/digest.txt": digestB + "\n",
		}, "3", "differ"},
		{"one is missing", map[string]string{
			"l/digest.txt": digestA + "\n", "m/digest.txt": digestA + "\n",
		}, "3", "found 2 digest files, want 3"},
		{"one too many", map[string]string{
			"l/digest.txt": digestA + "\n", "m/digest.txt": digestA + "\n",
		}, "1", "found 2 digest files, want 1"},
		{"empty file", map[string]string{"l/digest.txt": "", "m/digest.txt": ""}, "2", "not a SHA-256"},
		{"upper case", map[string]string{
			"l/digest.txt": strings.ToUpper(digestA) + "\n", "m/digest.txt": strings.ToUpper(digestA) + "\n",
		}, "2", "not a SHA-256"},
		{"two lines", map[string]string{
			"l/digest.txt": digestA + "\n" + digestA + "\n", "m/digest.txt": digestA + "\n",
		}, "2", "not a SHA-256"},
		{"no newline", map[string]string{"l/digest.txt": digestA, "m/digest.txt": digestA + "\n"}, "2", "not a SHA-256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runTool("-n", tt.n, collect(t, tt.files))
			if code != 1 || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit %d, stderr %q, want 1 and %q", code, stderr, tt.want)
			}
		})
	}
}

func TestRunListsEveryDigestWhenTheyDiffer(t *testing.T) {
	root := collect(t, map[string]string{"linux/digest.txt": digestA + "\n", "windows/digest.txt": digestB + "\n"})
	_, _, stderr := runTool("-n", "2", root)
	for _, want := range []string{"linux", digestA, "windows", digestB} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q does not mention %q", stderr, want)
		}
	}
}

func TestRunUsageErrors(t *testing.T) {
	root := collect(t, nil)
	tests := [][]string{
		nil,
		{root},
		{"-n", "0", root},
		{"-n", "x", root},
		{"-n", "2"},
		{"-n", "2", root, "extra"},
		{"-bogus"},
	}
	for _, args := range tests {
		if code, _, stderr := runTool(args...); code != 2 || stderr == "" {
			t.Errorf("run(%v): exit %d, stderr %q, want 2 and a message", args, code, stderr)
		}
	}
	if code, _, _ := runTool("-h"); code != 0 {
		t.Errorf("-h: exit %d, want 0", code)
	}
	if code, _, stderr := runTool("-n", "2", filepath.Join(root, "missing")); code != 1 || stderr == "" {
		t.Errorf("missing directory: exit %d, stderr %q, want 1", code, stderr)
	}
}
