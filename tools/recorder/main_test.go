package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const fakeToken = "fake-token-value"

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

var validEnv = map[string]string{"ACR_GITHUB_TOKEN": fakeToken, "ACR_SANDBOX_LOGINS": "RealReviewer,real-author"}

func TestRunUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"no scenario", nil, validEnv, "usage:"},
		{"unknown scenario", []string{"S99"}, validEnv, `unknown scenario "S99"`},
		{"unknown flag", []string{"-x", "S1"}, validEnv, "usage:"},
		{"no token", []string{"S1"}, map[string]string{"ACR_SANDBOX_LOGINS": "a,b"}, "ACR_GITHUB_TOKEN is not set"},
		{"no logins", []string{"S1"}, map[string]string{"ACR_GITHUB_TOKEN": fakeToken}, "ACR_SANDBOX_LOGINS"},
		{"one login", []string{"S1"}, map[string]string{"ACR_GITHUB_TOKEN": fakeToken, "ACR_SANDBOX_LOGINS": "a"}, "ACR_SANDBOX_LOGINS"},
		{"empty login", []string{"S1"}, map[string]string{"ACR_GITHUB_TOKEN": fakeToken, "ACR_SANDBOX_LOGINS": "a, "}, "ACR_SANDBOX_LOGINS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, env(tt.env), &stdout, &stderr)
			if code != 2 || !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("run(%v) = %d, stderr %q; want 2 and %q", tt.args, code, stderr.String(), tt.want)
			}
			if strings.Contains(stdout.String()+stderr.String(), fakeToken) {
				t.Error("output contains the token")
			}
		})
	}
}

func TestEveryScenarioHasARecording(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "..", "testdata", "scenarios", "S*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no scenario directories: %v", err)
	}
	for _, dir := range dirs {
		name := filepath.Base(dir)
		if s, ok := scenarios[name]; !ok || len(s.targets) == 0 {
			t.Errorf("scenario %s has no targets to record", name)
		}
	}
}

func TestRunRecordsIntoScenarioDirectories(t *testing.T) {
	var (
		mu      sync.Mutex
		methods []string
	)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		if r.URL.Path == "/search/issues" {
			w.Write([]byte(`{"total_count":0,"items":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer api.Close()

	out := t.TempDir()
	stale := filepath.Join(out, "S3", "stale.json")
	keep := filepath.Join(out, "S3", "notes.txt")
	for _, f := range []string{stale, keep} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-api", api.URL, "-out", out, "S3", "queue"}, env(validEnv), &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, stderr %q", code, stderr.String())
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale recording was not removed: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("non-recording file was removed: %v", err)
	}
	for _, dir := range []string{"S3", "queue"} {
		files, _ := filepath.Glob(filepath.Join(out, dir, "*.json"))
		if len(files) == 0 {
			t.Errorf("no recordings in %s", dir)
		}
	}
	for _, m := range methods {
		if m != http.MethodGet {
			t.Errorf("request with method %s", m)
		}
	}
}
