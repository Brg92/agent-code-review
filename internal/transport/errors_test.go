package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// assertNoSecret fails if any rendering of err contains secret.
func assertNoSecret(t *testing.T, err error, secrets ...string) {
	t.Helper()
	for _, s := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		for _, secret := range secrets {
			if strings.Contains(s, secret) {
				t.Errorf("error leaks %q: %s", secret, s)
			}
		}
	}
}

func TestStatusErrorIsTypedAndRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"message":"Not Found","echo":%q}`, r.Header.Get("Authorization"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, Config{Token: staticToken(testToken)})
	_, err := c.Get(context.Background(), "/repos/o/r?access_token=query-secret-77&page=2", "")
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("error = %T %v, want *StatusError", err, err)
	}
	if se.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", se.StatusCode)
	}
	if !strings.Contains(se.Error(), "404") || !strings.Contains(se.Error(), "/repos/o/r") || !strings.Contains(se.Error(), "page=2") {
		t.Errorf("Error() = %q, want status, path and harmless query", se.Error())
	}
	if !strings.Contains(se.Error(), redacted) {
		t.Errorf("Error() = %q, want the redaction marker", se.Error())
	}
	assertNoSecret(t, err, testToken, "query-secret-77")
}

func TestStatusErrorTruncatesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(strings.Repeat("x", 10_000)))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, Config{}).Get(context.Background(), "/x", "")
	if err == nil || len(err.Error()) > 600 {
		t.Errorf("error length = %d, want a short message", len(fmt.Sprint(err)))
	}
}

func TestNetworkErrorIsTypedAndRedacted(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	c := newTestClient(t, url, Config{Token: staticToken(testToken)})
	_, err := c.Get(context.Background(), "/x?token=query-secret-77", "")
	var ne *NetworkError
	if !errors.As(err, &ne) {
		t.Fatalf("error = %T %v, want *NetworkError", err, err)
	}
	if ne.Timeout() {
		t.Error("Timeout() = true for a refused connection")
	}
	assertNoSecret(t, err, testToken, "query-secret-77")
}

func TestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	c := newTestClient(t, srv.URL, Config{Timeout: 50 * time.Millisecond, Token: staticToken(testToken)})
	start := time.Now()
	_, err := c.Get(context.Background(), "/slow", "")
	var ne *NetworkError
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("error = %v, want a timeout NetworkError", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Get took %v, want it bounded by the timeout", elapsed)
	}
	assertNoSecret(t, err, testToken)
}

func TestTokenSourceErrorIsWrapped(t *testing.T) {
	boom := errors.New("keychain locked")
	c := newTestClient(t, "https://example.invalid", Config{Token: func(context.Context) (string, error) { return "", boom }})
	if _, err := c.Get(context.Background(), "/x", ""); !errors.Is(err, boom) {
		t.Errorf("error = %v, want it to wrap the token source error", err)
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://h/p?page=2", "https://h/p?page=2"},
		{"https://h/p?access_token=abc&page=2", "https://h/p?access_token=" + redacted + "&page=2"},
		{"https://h/p?private_token=a&client_secret=b&api_key=c&sig=d&password=e", "https://h/p?api_key=" + redacted + "&client_secret=" + redacted + "&password=" + redacted + "&private_token=" + redacted + "&sig=" + redacted},
		{"https://user:pw@h/p", "https://h/p"},
		{"https://h/p#frag", "https://h/p"},
		{"::bad", redacted},
	}
	for _, tt := range tests {
		if got := redactURL(tt.in); got != tt.want {
			t.Errorf("redactURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("y", 101)))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, Config{MaxBodyBytes: 100})
	if _, err := c.Get(context.Background(), "/big", ""); err == nil || !strings.Contains(err.Error(), "exceeds 100 bytes") {
		t.Errorf("error = %v, want a body limit error", err)
	}
	c = newTestClient(t, srv.URL, Config{MaxBodyBytes: 101})
	if resp, err := c.Get(context.Background(), "/big", ""); err != nil || len(resp.Body) != 101 {
		t.Errorf("Get = %v, want the full body at the limit", err)
	}
}

func TestBodyMessageCutsOnRuneBoundary(t *testing.T) {
	msg := bodyMessage([]byte(strings.Repeat("é", 150)), nil)
	if !strings.HasSuffix(msg, "…") || strings.ContainsRune(msg, '�') {
		t.Errorf("bodyMessage = %q, want a clean cut", msg)
	}
	if got := bodyMessage([]byte("a\x00b\n\tc"), nil); got != "a b c" {
		t.Errorf("bodyMessage = %q, want control characters folded", got)
	}
}
