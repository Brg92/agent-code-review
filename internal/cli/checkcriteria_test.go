package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/github"
)

// offline is an environment that fails the test if the command reads a
// token, asks git or reaches for the platform.
func offline(t *testing.T) env {
	t.Helper()
	vars := map[string]string{"ACR_PACKS_DIR": packsDir(t), "ACR_WORKDIR": t.TempDir()}
	return env{
		getenv: func(k string) string {
			if strings.Contains(k, "TOKEN") {
				t.Errorf("the command read %s", k)
			}
			return vars[k]
		},
		git: auth.RunnerFunc(func(_ context.Context, _ string, args ...string) (string, string, error) {
			t.Errorf("the command ran git %v", args)
			return "", "", errors.New("no git")
		}),
		newReader: func(string, github.Resolver, github.Options) (base.ForgeProvider, error) {
			t.Error("the command reached for the platform")
			return nil, errors.New("offline")
		},
		userConfigDir: func() (string, error) { return filepath.Join(t.TempDir(), "config"), nil },
		userCacheDir:  func() (string, error) { return t.TempDir(), nil },
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "criteria", name))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func golden(t *testing.T, name, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "criteria", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "<dir>", dir)
}

func TestCheckCriteriaGoldens(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"valid", exitOK},
		{"none", exitOK},
		{"legacy", exitValidation},
		{"ignored", exitValidation},
		{"invalid-config", exitValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := fixture(t, tt.name)
			e := offline(t)
			e.random = strings.NewReader("\x0a\x1b\x2c\x3d")
			got := runWith(t, e, "check-criteria", dir)
			if got.code != tt.code || got.stderr != "" {
				t.Errorf("code %d, want %d\nstderr: %s", got.code, tt.code, got.stderr)
			}
			if want := golden(t, tt.name, dir); got.stdout != want {
				t.Errorf("stdout =\n%s\nwant\n%s", got.stdout, want)
			}
		})
	}
}

func TestCheckCriteriaDefaultsToTheCurrentDirectory(t *testing.T) {
	e := offline(t)
	dir := fixture(t, "valid")
	t.Chdir(dir)
	got := runWith(t, e, "check-criteria")
	if got.code != exitOK || !strings.Contains(got.stdout, "Repository rules: 2") {
		t.Errorf("code %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	// The directory is shown whole, whatever way it was given.
	if !strings.Contains(got.stdout, "Repository criteria in "+dir+"\n") {
		t.Errorf("the directory is not shown:\n%s", got.stdout)
	}
}

func TestCheckCriteriaAcceptsARelativeDirectory(t *testing.T) {
	e := offline(t)
	t.Chdir(filepath.Join(fixture(t, "valid"), ".."))
	got := runWith(t, e, "check-criteria", "valid")
	if got.code != exitOK {
		t.Errorf("code %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

func TestCheckCriteriaErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *env)
		args  []string
		code  int
		want  string
	}{
		{"not a directory", nil, []string{"check-criteria", filepath.Join(fixture(t, "valid"), ".agent-code-review", "config.json")}, exitUsage, "not a directory"},
		{"missing directory", nil, []string{"check-criteria", filepath.Join(fixture(t, "valid"), "nothing")}, exitUsage, "not a directory"},
		{"two directories", nil, []string{"check-criteria", "a", "b"}, exitUsage, "usage: agent-code-review check-criteria"},
		{"unknown flag", nil, []string{"check-criteria", "--all"}, exitUsage, "usage: agent-code-review check-criteria"},
		{"no packs", func(e *env) {
			vars := map[string]string{"ACR_PACKS_DIR": t.TempDir()}
			e.getenv = func(k string) string { return vars[k] }
		}, []string{"check-criteria", fixture(t, "valid")}, exitUnexpected, "could not load the packs"},
		{"invalid user config", func(e *env) {
			dir := t.TempDir()
			path := filepath.Join(dir, "agent-code-review", "config.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"schema":1,"keepDays":"x"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			e.userConfigDir = func() (string, error) { return dir, nil }
		}, []string{"check-criteria", fixture(t, "valid")}, exitValidation, "user configuration is not valid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := offline(t)
			if tt.setup != nil {
				tt.setup(&e)
			}
			got := runWith(t, e, tt.args...)
			if got.code != tt.code || got.stdout != "" || !strings.Contains(got.stderr, tt.want) {
				t.Errorf("code %d, want %d\nstdout: %s\nstderr: %s", got.code, tt.code, got.stdout, got.stderr)
			}
		})
	}
}

func TestCheckCriteriaIsListedInTheUsage(t *testing.T) {
	if !strings.Contains(usage, "check-criteria [<dir>]") {
		t.Error("the usage does not list check-criteria")
	}
}
