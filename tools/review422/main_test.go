package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	fakeToken = "ghp_" + "0123456789abcdefghijklmnopqrstuvwxyz"
	head      = "76ae5f13ca1575b65fe1567c471e8a7b51a00af4"
)

type seen struct {
	mu       sync.Mutex
	requests []string
	review   map[string]any
	auth     string
}

// fakeGitHub answers the pull request, and the review with status and body.
func fakeGitHub(t *testing.T, status int, body string) (*httptest.Server, *seen) {
	t.Helper()
	s := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests = append(s.requests, r.Method+" "+r.URL.Path)
		s.auth = r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/pulls/9":
			_, _ = io.WriteString(w, `{"head":{"sha":"`+head+`"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/pulls/9/reviews":
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, &s.review)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func getenv(k string) string {
	if k == "ACR_GITHUB_TOKEN" {
		return fakeToken
	}
	return ""
}

func args(srv *httptest.Server, out string) []string {
	return []string{"-api", srv.URL, "-repo", "acme/widgets", "-pr", "9", "-path", "app/Main.kt", "-line", "1", "-out", out}
}

func TestCapturesTheBodyOfAnUnprocessableReview(t *testing.T) {
	answer := `{"message":"Unprocessable Entity","errors":["Pull request review thread line must be part of the diff for acme/widgets at ` + head + `"],"status":"422"}`
	srv, s := fakeGitHub(t, 422, answer)
	out := filepath.Join(t.TempDir(), "body.json")
	var stdout, stderr bytes.Buffer
	if code := run(args(srv, out), getenv, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if s.review["event"] != "COMMENT" || s.review["commit_id"] != head {
		t.Errorf("review %v: want the event COMMENT on the head %s", s.review, head)
	}
	comments, _ := s.review["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("%d comments, want 1", len(comments))
	}
	c, _ := comments[0].(map[string]any)
	if c["path"] != "app/Main.kt" || c["line"] != float64(1) || c["side"] != "RIGHT" {
		t.Errorf("comment %v: want app/Main.kt line 1 on the RIGHT", c)
	}
	if _, ok := s.review["body"]; ok {
		t.Errorf("the review has a text of its own: %v", s.review["body"])
	}
	if s.auth != "Bearer "+fakeToken {
		t.Errorf("authorization %q", s.auth)
	}
	saved, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, real := range []string{"acme", "widgets", head} {
		if strings.Contains(string(saved), real) {
			t.Errorf("the saved body still has %q: %s", real, saved)
		}
	}
	var doc struct {
		Message string
		Errors  []string
	}
	if err := json.Unmarshal(saved, &doc); err != nil || doc.Message != "Unprocessable Entity" || len(doc.Errors) != 1 {
		t.Errorf("saved body is not the answer, sanitised: %s (%v)", saved, err)
	}
	if !strings.Contains(string(saved), "o/r") {
		t.Errorf("owner/repo is not replaced by o/r: %s", saved)
	}
	for name, text := range map[string]string{"stdout": stdout.String(), "stderr": stderr.String(), "file": string(saved)} {
		if strings.Contains(text, fakeToken) {
			t.Errorf("the token is in %s", name)
		}
	}
}

func TestDoesNotSaveAnAcceptedReview(t *testing.T) {
	srv, _ := fakeGitHub(t, 200, `{"id":1}`)
	out := filepath.Join(t.TempDir(), "body.json")
	var stdout, stderr bytes.Buffer
	if code := run(args(srv, out), getenv, nil, &stdout, &stderr); code != 1 {
		t.Errorf("exit %d, want 1: GitHub accepted the review, so there is no 422 to keep", code)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a file was written for an accepted review")
	}
	if !strings.Contains(stderr.String(), "200") {
		t.Errorf("stderr %q does not say the status", stderr.String())
	}
}

func TestUsageErrors(t *testing.T) {
	srv, s := fakeGitHub(t, 422, `{}`)
	for name, a := range map[string][]string{
		"no arguments":    {},
		"no repository":   {"-api", srv.URL, "-pr", "9", "-path", "a", "-line", "1", "-out", "x"},
		"bad repository":  {"-api", srv.URL, "-repo", "acme", "-pr", "9", "-path", "a", "-line", "1", "-out", "x"},
		"no pull request": {"-api", srv.URL, "-repo", "acme/widgets", "-path", "a", "-line", "1", "-out", "x"},
		"no line":         {"-api", srv.URL, "-repo", "acme/widgets", "-pr", "9", "-path", "a", "-out", "x"},
		"no output":       {"-api", srv.URL, "-repo", "acme/widgets", "-pr", "9", "-path", "a", "-line", "1"},
	} {
		var stderr bytes.Buffer
		if code := run(a, getenv, nil, io.Discard, &stderr); code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
	if len(s.requests) != 0 {
		t.Errorf("a usage error sent %v", s.requests)
	}
}
