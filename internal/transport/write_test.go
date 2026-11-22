package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type seenRequest struct {
	method, path, query, body, contentType, accept, auth string
}

// writeServer records the requests it gets and answers with the statuses
// it is given, the last one repeating.
type writeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []seenRequest
}

func newWriteServer(t *testing.T, answer func(n int, w http.ResponseWriter)) *writeServer {
	t.Helper()
	s := &writeServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, seenRequest{r.Method, r.URL.Path, r.URL.RawQuery, string(body), r.Header.Get("Content-Type"), r.Header.Get("Accept"), r.Header.Get("Authorization")})
		n := len(s.requests)
		s.mu.Unlock()
		answer(n, w)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *writeServer) seen() []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenRequest(nil), s.requests...)
}

var allowAll = []Endpoint{
	{Method: http.MethodPost, Path: "/repos/*/*/issues"},
	{Method: http.MethodPatch, Path: "/repos/*/*/issues/*"},
}

func created(_ int, w http.ResponseWriter) {
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"number":12}`))
}

func TestPostAndPatchSendTheBody(t *testing.T) {
	srv := newWriteServer(t, created)
	c := newTestClient(t, srv.URL+"/api", Config{Token: staticToken(testToken), Allow: allowAll})
	resp, err := c.Post(context.Background(), "/repos/o/r/issues", "application/vnd.test+json", []byte(`{"title":"t"}`))
	if err != nil || resp.StatusCode != http.StatusCreated || string(resp.Body) != `{"number":12}` {
		t.Fatalf("Post = %+v, %v", resp, err)
	}
	if _, err := c.Patch(context.Background(), "/repos/o/r/issues/12", "", []byte(`{"body":"b"}`)); err != nil {
		t.Fatal(err)
	}
	want := []seenRequest{
		{"POST", "/api/repos/o/r/issues", "", `{"title":"t"}`, "application/json", "application/vnd.test+json", "Bearer " + testToken},
		{"PATCH", "/api/repos/o/r/issues/12", "", `{"body":"b"}`, "application/json", "", "Bearer " + testToken},
	}
	got := srv.seen()
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("requests = %+v, want %+v", got, want)
	}
}

func TestWritesToOtherEndpointsNeverReachTheNetwork(t *testing.T) {
	srv := newWriteServer(t, created)
	for name, cfg := range map[string]Config{"no endpoints": {}, "other endpoints": {Allow: allowAll}} {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, srv.URL, cfg)
			for _, call := range []func() (*Response, error){
				func() (*Response, error) {
					return c.Post(context.Background(), "/repos/o/r/pulls/1/merge", "", []byte("{}"))
				},
				func() (*Response, error) {
					return c.Patch(context.Background(), "/repos/o/r/pulls/1", "", []byte("{}"))
				},
				func() (*Response, error) {
					return c.Post(context.Background(), "/repos/o/r/issues/3", "", []byte("{}"))
				},
			} {
				var na *NotAllowedError
				if _, err := call(); !errors.Is(err, ErrNotAllowed) || !errors.As(err, &na) {
					t.Errorf("error = %v, want a NotAllowedError", err)
				}
			}
		})
	}
	if n := len(srv.seen()); n != 0 {
		t.Errorf("%d requests reached the server", n)
	}
}

func TestWriteRefusesABadPath(t *testing.T) {
	c := newTestClient(t, "https://example.invalid", Config{Allow: allowAll})
	for _, p := range []string{"repos/o/r/issues", "//x/y", "/a b\x7f"} {
		if _, err := c.Post(context.Background(), p, "", nil); err == nil {
			t.Errorf("Post(%q) succeeded", p)
		}
	}
}

func TestWriteErrorsNameTheMethodAndHideTheToken(t *testing.T) {
	srv := newWriteServer(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed","echo":"` + testToken + `"}`))
	})
	c := newTestClient(t, srv.URL, Config{Token: staticToken(testToken), Allow: allowAll})
	_, err := c.Post(context.Background(), "/repos/o/r/issues", "", []byte(`{}`))
	var se *StatusError
	if !errors.As(err, &se) || se.StatusCode != 422 || se.Method != http.MethodPost {
		t.Fatalf("error = %v, want a 422 StatusError of a POST", err)
	}
	if !strings.Contains(err.Error(), "POST") || strings.Contains(err.Error(), testToken) {
		t.Errorf("message = %q: want the method, never the token", err)
	}
	if got := string(se.Body); !strings.Contains(got, "Validation Failed") || strings.Contains(got, testToken) {
		t.Errorf("Body = %q: want the answer of the platform, without the token", got)
	}
}

func TestWritesAreNotRetriedWhenTheOutcomeIsUnknown(t *testing.T) {
	for _, status := range []int{500, 502, 503} {
		srv := newWriteServer(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(status) })
		var events []Event
		c := newTestClient(t, srv.URL, Config{Allow: allowAll, Observe: func(e Event) { events = append(events, e) }})
		_, err := c.Post(context.Background(), "/repos/o/r/issues", "", []byte(`{}`))
		var se *StatusError
		if !errors.As(err, &se) || se.StatusCode != status {
			t.Errorf("status %d: error = %v", status, err)
		}
		if n := len(srv.seen()); n != 1 {
			t.Errorf("status %d: %d requests, want 1: a write may have been done", status, n)
		}
		if len(events) != 1 || events[0].Method != http.MethodPost || events[0].Outcome != OutcomeError {
			t.Errorf("events = %+v, want one error event of a POST", events)
		}
	}
}

func TestWriteNetworkFailureIsNotRetried(t *testing.T) {
	srv := newWriteServer(t, created)
	url := srv.URL
	srv.Close()
	var sleeps int
	c := newTestClient(t, url, Config{Allow: allowAll, Sleep: func(context.Context, time.Duration) error { sleeps++; return nil }})
	_, err := c.Post(context.Background(), "/repos/o/r/issues", "", []byte(`{}`))
	var ne *NetworkError
	if !errors.As(err, &ne) || sleeps != 0 || !strings.Contains(err.Error(), "POST") {
		t.Errorf("error = %v, sleeps %d, want one NetworkError of a POST, not retried", err, sleeps)
	}
}

func TestWriteRetriesAfterARateLimitWithTheSameBody(t *testing.T) {
	srv := newWriteServer(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		created(n, w)
	})
	var slept []time.Duration
	c := newTestClient(t, srv.URL, Config{Allow: allowAll, Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }})
	resp, err := c.Post(context.Background(), "/repos/o/r/issues", "", []byte(`{"title":"t"}`))
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("Post = %v, %v", resp, err)
	}
	got := srv.seen()
	if len(got) != 2 || got[0].body != `{"title":"t"}` || got[1].body != `{"title":"t"}` || len(slept) != 1 || slept[0] != 2*time.Second {
		t.Errorf("requests = %+v, slept %v, want the same body twice after 2s", got, slept)
	}
}

func TestWriteRateLimitBeyondTheLongestWaitIsAnError(t *testing.T) {
	srv := newWriteServer(t, func(_ int, w http.ResponseWriter) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c := newTestClient(t, srv.URL, Config{Allow: allowAll, MaxWait: time.Minute})
	_, err := c.Patch(context.Background(), "/repos/o/r/issues/1", "", []byte(`{}`))
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.Method != http.MethodPatch || !strings.Contains(err.Error(), "PATCH") {
		t.Errorf("error = %v, want a RateLimitError of a PATCH", err)
	}
	if n := len(srv.seen()); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

func TestWriteRateLimitRetriesAreBounded(t *testing.T) {
	srv := newWriteServer(t, func(_ int, w http.ResponseWriter) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c := newTestClient(t, srv.URL, Config{Allow: allowAll, MaxRetries: 2, Sleep: func(context.Context, time.Duration) error { return nil }})
	_, err := c.Post(context.Background(), "/repos/o/r/issues", "", []byte(`{}`))
	var rl *RateLimitError
	if !errors.As(err, &rl) || len(srv.seen()) != 3 {
		t.Errorf("error = %v after %d requests, want a RateLimitError after 3", err, len(srv.seen()))
	}
}

func TestWriteStopsWhenTheContextIsDone(t *testing.T) {
	srv := newWriteServer(t, created)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := newTestClient(t, srv.URL, Config{Allow: allowAll})
	if _, err := c.Post(ctx, "/repos/o/r/issues", "", []byte(`{}`)); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestWriteTokenErrorIsAnError(t *testing.T) {
	c := newTestClient(t, "https://example.invalid", Config{Allow: allowAll, Token: func(context.Context) (string, error) { return "", errors.New("no token") }})
	if _, err := c.Post(context.Background(), "/repos/o/r/issues", "", []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("error = %v, want the token failure", err)
	}
}

func TestWriteBodyOverTheLimitIsAnError(t *testing.T) {
	srv := newWriteServer(t, func(_ int, w http.ResponseWriter) { _, _ = w.Write([]byte(strings.Repeat("x", 100))) })
	c := newTestClient(t, srv.URL, Config{Allow: allowAll, MaxBodyBytes: 10})
	if _, err := c.Post(context.Background(), "/repos/o/r/issues", "", []byte(`{}`)); err == nil {
		t.Error("an answer over the limit was accepted")
	}
}

func TestGraphQLQueryThroughTheClient(t *testing.T) {
	srv := newWriteServer(t, func(_ int, w http.ResponseWriter) { _, _ = w.Write([]byte(`{"data":{}}`)) })
	c := newTestClient(t, srv.URL, Config{Allow: []Endpoint{{Method: http.MethodPost, Path: "/graphql", ReadOnlyQuery: true}}})
	if _, err := c.Post(context.Background(), "/graphql", "", []byte(`{"query":"{ viewer { login } }"}`)); err != nil {
		t.Fatal(err)
	}
	_, err := c.Post(context.Background(), "/graphql", "", []byte(`{"query":"mutation { x }"}`))
	var na *NotAllowedError
	if !errors.As(err, &na) || na.Reason == "" {
		t.Errorf("error = %v, want a NotAllowedError with its reason", err)
	}
	if n := len(srv.seen()); n != 1 {
		t.Errorf("%d requests reached the server, want only the query", n)
	}
}
