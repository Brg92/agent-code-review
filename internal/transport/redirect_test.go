package transport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSameHostRedirectKeepsAuthorization(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusMovedPermanently)
			return
		}
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("moved"))
	}))
	defer srv.Close()

	resp, err := newTestClient(t, srv.URL, Config{Token: staticToken(testToken)}).Get(context.Background(), "/old", "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(resp.Body) != "moved" || !strings.HasSuffix(resp.URL, "/new") {
		t.Errorf("resp = %q from %s", resp.Body, resp.URL)
	}
	if auth != "Bearer "+testToken {
		t.Errorf("Authorization after same-host redirect = %q", auth)
	}
}

func TestCrossHostRedirectIsRefused(t *testing.T) {
	var otherHits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherHits.Add(1)
	}))
	defer other.Close()

	var sameHits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-other":
			http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
		case "/to-alias":
			// Same address, different host name: still another host.
			http.Redirect(w, r, strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)+"/steal", http.StatusFound)
		case "/steal":
			sameHits.Add(1)
		}
	}))
	defer srv.Close()

	c, clock := clientWithClock(t, srv.URL, Config{Token: staticToken(testToken)})
	for _, path := range []string{"/to-other", "/to-alias"} {
		t.Run(path, func(t *testing.T) {
			_, err := c.Get(context.Background(), path, "")
			if !errors.Is(err, ErrCrossHostRedirect) {
				t.Fatalf("error = %v, want ErrCrossHostRedirect", err)
			}
			assertNoSecret(t, err, testToken)
		})
	}
	if len(clock.sleeps) != 0 {
		t.Errorf("refused redirects were retried: sleeps %v", clock.sleeps)
	}
	if otherHits.Load() != 0 || sameHits.Load() != 0 {
		t.Errorf("redirect target reached: other %d, alias %d", otherHits.Load(), sameHits.Load())
	}
}

func TestCheckRedirect(t *testing.T) {
	mk := func(raw string) *http.Request {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	first := mk("https://api.example.invalid/a")
	tests := []struct {
		name string
		next string
		via  int
		want error
	}{
		{"same host", "https://api.example.invalid/b", 1, nil},
		{"host case", "https://API.example.invalid/b", 1, nil},
		{"other host", "https://evil.example.invalid/b", 1, ErrCrossHostRedirect},
		{"other port", "https://api.example.invalid:8443/b", 1, ErrCrossHostRedirect},
		{"downgrade", "http://api.example.invalid/b", 1, ErrCrossHostRedirect},
		{"too many", "https://api.example.invalid/b", maxRedirects, errTooManyRedirects},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			via := make([]*http.Request, tt.via)
			for i := range via {
				via[i] = first
			}
			if err := checkRedirect(mk(tt.next), via); !errors.Is(err, tt.want) {
				t.Errorf("checkRedirect = %v, want %v", err, tt.want)
			}
		})
	}
}
