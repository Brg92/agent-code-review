package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const module = "example.com/mod"

func TestParseProfile(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    map[string]counts
		wantErr string
	}{
		{
			name: "set mode, one package",
			in: "mode: set\n" +
				"example.com/mod/internal/a/a.go:3.10,5.2 2 1\n" +
				"example.com/mod/internal/a/a.go:7.10,9.2 3 0\n",
			want: map[string]counts{"example.com/mod/internal/a": {statements: 5, covered: 2}},
		},
		{
			name: "atomic mode, two packages, CRLF and blank lines",
			in: "mode: atomic\r\n" +
				"example.com/mod/internal/a/a.go:3.10,5.2 2 7\r\n" +
				"\r\n" +
				"example.com/mod/internal/b/x.go:1.1,2.2 4 0\r\n" +
				"example.com/mod/internal/b/y.go:1.1,2.2 1 3\r\n",
			want: map[string]counts{
				"example.com/mod/internal/a": {statements: 2, covered: 2},
				"example.com/mod/internal/b": {statements: 5, covered: 1},
			},
		},
		{
			name: "a block listed twice counts once and is covered if any run covered it",
			in: "mode: count\n" +
				"example.com/mod/internal/a/a.go:3.10,5.2 2 0\n" +
				"example.com/mod/internal/a/a.go:3.10,5.2 2 4\n" +
				"example.com/mod/internal/a/a.go:6.1,8.2 1 0\n",
			want: map[string]counts{"example.com/mod/internal/a": {statements: 3, covered: 2}},
		},
		{
			name: "a second mode line from concatenated profiles is ignored",
			in: "mode: set\n" +
				"example.com/mod/internal/a/a.go:3.10,5.2 2 1\n" +
				"mode: set\n" +
				"example.com/mod/internal/b/b.go:3.10,5.2 1 0\n",
			want: map[string]counts{
				"example.com/mod/internal/a": {statements: 2, covered: 2},
				"example.com/mod/internal/b": {statements: 1, covered: 0},
			},
		},
		{name: "empty", in: "", wantErr: "missing mode line"},
		{name: "no mode line", in: "example.com/mod/a.go:1.1,2.2 1 1\n", wantErr: "missing mode line"},
		{name: "unknown mode", in: "mode: bogus\n", wantErr: `unknown mode "bogus"`},
		{name: "no colon", in: "mode: set\nnot-a-block 1 1\n", wantErr: "line 2"},
		{name: "two fields", in: "mode: set\nexample.com/mod/a.go:1.1,2.2 1\n", wantErr: "line 2"},
		{name: "bad statements", in: "mode: set\nexample.com/mod/a.go:1.1,2.2 x 1\n", wantErr: "line 2"},
		{name: "negative count", in: "mode: set\nexample.com/mod/a.go:1.1,2.2 1 -1\n", wantErr: "line 2"},
		{name: "bad range", in: "mode: set\nexample.com/mod/a.go:1.1-2.2 1 1\n", wantErr: "line 2"},
		{name: "file without directory", in: "mode: set\na.go:1.1,2.2 1 1\n", wantErr: "line 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProfile(strings.NewReader(tt.in))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseProfile() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProfile() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseProfile() = %v, want %v", got, tt.want)
			}
			for pkg, want := range tt.want {
				if got[pkg] != want {
					t.Errorf("package %s = %+v, want %+v", pkg, got[pkg], want)
				}
			}
		})
	}
}

func TestSelectPackages(t *testing.T) {
	all := map[string]counts{
		module + "/cmd/tool":                  {statements: 1},
		module + "/internal/a":                {statements: 1},
		module + "/internal/a/sub":            {statements: 1},
		module + "/internal/guards":           {statements: 1},
		module + "/internal/guardsextra":      {statements: 1},
		module + "/internal/testutil/fixture": {statements: 1},
		module + "/tools/gen":                 {statements: 1},
		"other.example/internal/x":            {statements: 1},
	}
	got := selectPackages(all, module, []string{"internal"}, []string{"internal/testutil", "internal/guards"})
	want := []string{"internal/a", "internal/a/sub", "internal/guardsextra"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("selectPackages() = %v, want %v", got, want)
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name   string
		counts counts
		min    float64
		pass   bool
	}{
		{"exactly at threshold", counts{statements: 10, covered: 9}, 90, true},
		{"just below threshold", counts{statements: 1000, covered: 899}, 90, false},
		{"above threshold", counts{statements: 4, covered: 4}, 90, true},
		{"nothing covered", counts{statements: 3, covered: 0}, 90, false},
		{"no statements", counts{}, 90, true},
		{"zero threshold", counts{statements: 3, covered: 0}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.counts.passes(tt.min); got != tt.pass {
				t.Errorf("%+v.passes(%v) = %v, want %v", tt.counts, tt.min, got, tt.pass)
			}
		})
	}
}

func TestPercentIsTruncatedNotRounded(t *testing.T) {
	tests := []struct {
		counts counts
		want   string
	}{
		{counts{statements: 10000, covered: 8999}, "89.9%"},
		{counts{statements: 3, covered: 2}, "66.6%"},
		{counts{statements: 1, covered: 1}, "100.0%"},
		{counts{}, "-"},
	}
	for _, tt := range tests {
		if got := tt.counts.percent(); got != tt.want {
			t.Errorf("%+v.percent() = %q, want %q", tt.counts, got, tt.want)
		}
	}
}

func TestModulePath(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", "module example.com/mod\n\ngo 1.26.0\n", "example.com/mod", false},
		{"quoted with comment and CRLF", "// comment\r\nmodule \"example.com/mod\" // trailing\r\n", "example.com/mod", false},
		{"missing", "go 1.26.0\n", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := modulePath(strings.NewReader(tt.in))
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("modulePath() = %q, %v; want %q, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

// fixture writes a go.mod and a cover profile into a temp dir and returns their paths.
func fixture(t *testing.T, profile string) (gomod, cover string) {
	t.Helper()
	dir := t.TempDir()
	gomod = filepath.Join(dir, "go.mod")
	cover = filepath.Join(dir, "coverage.out")
	if err := os.WriteFile(gomod, []byte("module "+module+"\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cover, []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	return gomod, cover
}

const mixedProfile = "mode: atomic\n" +
	"example.com/mod/internal/good/g.go:1.1,2.2 19 1\n" +
	"example.com/mod/internal/good/g.go:3.1,4.2 1 0\n" +
	"example.com/mod/internal/bad/b.go:1.1,2.2 8 1\n" +
	"example.com/mod/internal/bad/b.go:3.1,4.2 2 0\n" +
	"example.com/mod/internal/testutil/t/t.go:1.1,2.2 5 0\n" +
	"example.com/mod/internal/guards/x.go:1.1,2.2 5 0\n" +
	"example.com/mod/cmd/tool/main.go:1.1,2.2 5 0\n"

func TestRun(t *testing.T) {
	gomod, cover := fixture(t, mixedProfile)
	tests := []struct {
		name       string
		args       []string
		code       int
		wantOut    []string
		notWantOut []string
		wantErr    string
	}{
		{
			name:       "defaults fail on the package below 90%",
			args:       []string{"-gomod", gomod, cover},
			code:       1,
			wantOut:    []string{"internal/good", "95.0%", "ok", "internal/bad", "80.0%", "FAIL"},
			notWantOut: []string{"internal/testutil", "internal/guards", "cmd/tool"},
			wantErr:    "1 package(s) below 90.0%: internal/bad",
		},
		{
			name:    "lower threshold passes",
			args:    []string{"-gomod", gomod, "-min", "80", cover},
			code:    0,
			wantOut: []string{"internal/bad", "80.0%", "ok"},
		},
		{
			name:    "custom exclusions",
			args:    []string{"-gomod", gomod, "-exclude", "internal/bad,internal/testutil,internal/guards", cover},
			code:    0,
			wantOut: []string{"internal/good"},
		},
		{
			name:    "empty exclusions gate every internal package",
			args:    []string{"-gomod", gomod, "-exclude", "", cover},
			code:    1,
			wantOut: []string{"internal/testutil/t", "internal/guards"},
			wantErr: "3 package(s) below 90.0%: internal/bad, internal/guards, internal/testutil/t",
		},
		{
			name:    "custom include",
			args:    []string{"-gomod", gomod, "-include", "cmd", cover},
			code:    1,
			wantOut: []string{"cmd/tool", "0.0%"},
			wantErr: "cmd/tool",
		},
		{name: "no profile", args: []string{"-gomod", gomod}, code: 2, wantErr: "usage:"},
		{name: "two profiles", args: []string{"-gomod", gomod, cover, cover}, code: 2, wantErr: "usage:"},
		{name: "unknown flag", args: []string{"-x", cover}, code: 2, wantErr: "usage:"},
		{name: "threshold above 100", args: []string{"-gomod", gomod, "-min", "101", cover}, code: 2, wantErr: "-min"},
		{name: "negative threshold", args: []string{"-gomod", gomod, "-min", "-1", cover}, code: 2, wantErr: "-min"},
		{name: "missing profile file", args: []string{"-gomod", gomod, filepath.Join(t.TempDir(), "nope.out")}, code: 2, wantErr: "nope.out"},
		{name: "missing go.mod", args: []string{"-gomod", filepath.Join(t.TempDir(), "go.mod"), cover}, code: 2, wantErr: "go.mod"},
		{name: "nothing selected", args: []string{"-gomod", gomod, "-include", "pkg", cover}, code: 2, wantErr: "no package"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.code {
				t.Errorf("run(%v) = %d, want %d; stderr %q", tt.args, code, tt.code, stderr.String())
			}
			for _, w := range tt.wantOut {
				if !strings.Contains(stdout.String(), w) {
					t.Errorf("stdout lacks %q:\n%s", w, stdout.String())
				}
			}
			for _, w := range tt.notWantOut {
				if strings.Contains(stdout.String(), w) {
					t.Errorf("stdout contains %q:\n%s", w, stdout.String())
				}
			}
			if tt.wantErr != "" && !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantErr)
			}
			if tt.wantErr == "" && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunMalformedProfile(t *testing.T) {
	gomod, cover := fixture(t, "mode: set\ngarbage\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-gomod", gomod, cover}, &stdout, &stderr); code != 2 {
		t.Errorf("run() = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "line 2") {
		t.Errorf("stderr = %q, want the failing line", stderr.String())
	}
}

func TestTableIsSortedAndAligned(t *testing.T) {
	gomod, cover := fixture(t, mixedProfile)
	var stdout, stderr bytes.Buffer
	run([]string{"-gomod", gomod, cover}, &stdout, &stderr)
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want header and 2 rows:\n%s", len(lines), stdout.String())
	}
	if !strings.HasPrefix(lines[0], "package") || !strings.HasPrefix(lines[1], "internal/bad ") || !strings.HasPrefix(lines[2], "internal/good ") {
		t.Errorf("table not sorted by package:\n%s", stdout.String())
	}
	col := strings.Index(lines[0], "statements")
	for _, l := range lines[1:] {
		if len(l) <= col || l[col-1] != ' ' {
			t.Errorf("row %q not aligned with header column at %d", l, col)
		}
	}
}
