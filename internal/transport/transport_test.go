package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Brg92/agent-code-review/internal/version"
)

const testToken = "test-token-value-0123"

func staticToken(token string) TokenSource {
	return func(context.Context) (string, error) { return token, nil }
}

func newTestClient(t *testing.T, baseURL string, cfg Config) *Client {
	t.Helper()
	cfg.BaseURL = baseURL
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestGetSendsHeaders(t *testing.T) {
	var got http.Header
	var gotPath, gotQuery, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		gotPath, gotQuery, gotMethod = r.URL.Path, r.URL.RawQuery, r.Method
		w.Header().Set("X-Answer", "42")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL+"/api/v3/", Config{Token: staticToken(testToken)})
	resp, err := c.Get(context.Background(), "/repos/o/r?per_page=100", "application/vnd.test+json")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.StatusCode != http.StatusOK || string(resp.Body) != `{"ok":true}` || resp.Header.Get("X-Answer") != "42" {
		t.Errorf("resp = %d %q %v", resp.StatusCode, resp.Body, resp.Header)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v3/repos/o/r" || gotQuery != "per_page=100" {
		t.Errorf("path = %q query = %q", gotPath, gotQuery)
	}
	checks := map[string]string{
		"Authorization": "Bearer " + testToken,
		"User-Agent":    "agent-code-review/" + version.Version,
		"Accept":        "application/vnd.test+json",
	}
	for name, want := range checks {
		if got.Get(name) != want {
			t.Errorf("%s = %q, want %q", name, got.Get(name), want)
		}
	}
}

func TestGetWithoutToken(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Values("Authorization")
	}))
	defer srv.Close()

	for name, src := range map[string]TokenSource{"nil source": nil, "empty token": staticToken("")} {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, srv.URL, Config{Token: src})
			if _, err := c.Get(context.Background(), "/x", ""); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if len(auth) != 0 {
				t.Errorf("Authorization = %v, want none", auth)
			}
		})
	}
}

func TestNewRejectsInvalidBaseURL(t *testing.T) {
	for _, base := range []string{"", "://x", "ftp://host", "https://", "https://user:pw@host", "https://host?q=1", "https://host#f", "/relative"} {
		if _, err := New(Config{BaseURL: base}); err == nil {
			t.Errorf("New(%q) = nil error, want error", base)
		}
	}
}

func TestGetRejectsInvalidPath(t *testing.T) {
	c := newTestClient(t, "https://example.invalid", Config{})
	for _, p := range []string{"", "repos", "https://other.invalid/x", "//other.invalid/x"} {
		if _, err := c.Get(context.Background(), p, ""); err == nil {
			t.Errorf("Get(%q) = nil error, want error", p)
		}
	}
}
