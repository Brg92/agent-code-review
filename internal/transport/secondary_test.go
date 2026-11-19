package transport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// bodyStep is a scripted response with a body of its own.
type bodyStep struct {
	status int
	header map[string]string
	body   string
}

func bodyServer(t *testing.T, steps ...bodyStep) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := min(int(n.Add(1))-1, len(steps)-1)
		for k, v := range steps[i].header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(steps[i].status)
		_, _ = w.Write([]byte(steps[i].body))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func secondaryPolicy() Policy {
	return Policy{
		RemainingHeader: "X-Left", ResetHeader: "X-Reset",
		ResetMargin:   time.Second,
		SecondaryWait: time.Minute, SecondaryRetries: 1,
		Secondary: func(r *Response) bool { return strings.Contains(string(r.Body), "secondary rate limit") },
	}
}

func TestSecondaryLimitPolicy(t *testing.T) {
	s := time.Second
	reset := strconv.FormatInt(epoch.Add(20*s).Unix(), 10)
	farReset := strconv.FormatInt(epoch.Add(time.Hour).Unix(), 10)
	const secondary = `{"message":"You have exceeded a secondary rate limit."}`
	cfg := Config{Policy: secondaryPolicy(), MaxWait: time.Minute, MaxRetries: 3}
	tests := []struct {
		name     string
		steps    []bodyStep
		requests int32
		sleeps   []time.Duration
		wantRate bool
		retryAt  time.Time
	}{
		{name: "primary waits for the reset plus the margin", steps: []bodyStep{{status: 403, header: map[string]string{"X-Left": "0", "X-Reset": reset}}, {status: 200}},
			requests: 2, sleeps: []time.Duration{21 * s}},
		{name: "primary beyond the maximum wait fails at once", steps: []bodyStep{{status: 429, header: map[string]string{"X-Left": "0", "X-Reset": farReset}}},
			requests: 1, wantRate: true, retryAt: epoch.Add(time.Hour + s)},
		{name: "primary reset just inside the maximum wait", steps: []bodyStep{{status: 403, header: map[string]string{"X-Left": "0", "X-Reset": strconv.FormatInt(epoch.Add(59*s).Unix(), 10)}}, {status: 200}},
			requests: 2, sleeps: []time.Duration{60 * s}},
		{name: "primary reset one second too far", steps: []bodyStep{{status: 403, header: map[string]string{"X-Left": "0", "X-Reset": strconv.FormatInt(epoch.Add(60*s).Unix(), 10)}}},
			requests: 1, wantRate: true, retryAt: epoch.Add(61 * s)},
		{name: "secondary by body waits a minute, retries once", steps: []bodyStep{{status: 403, body: secondary}, {status: 200}},
			requests: 2, sleeps: []time.Duration{time.Minute}},
		{name: "secondary fails after its one retry", steps: []bodyStep{{status: 403, body: secondary}},
			requests: 2, sleeps: []time.Duration{time.Minute}, wantRate: true},
		{name: "secondary on 429 without hints waits a minute", steps: []bodyStep{{status: 429}, {status: 200}},
			requests: 2, sleeps: []time.Duration{time.Minute}},
		{name: "secondary honours Retry-After", steps: []bodyStep{{status: 403, header: map[string]string{"Retry-After": "7"}, body: secondary}, {status: 200}},
			requests: 2, sleeps: []time.Duration{7 * s}},
		{name: "secondary Retry-After beyond the maximum wait", steps: []bodyStep{{status: 403, header: map[string]string{"Retry-After": "61"}, body: secondary}},
			requests: 1, wantRate: true, retryAt: epoch.Add(61 * s)},
		{name: "a plain 403 is final", steps: []bodyStep{{status: 403, body: `{"message":"Resource not accessible"}`}},
			requests: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, n := bodyServer(t, tt.steps...)
			c, clock := clientWithClock(t, srv.URL, cfg)
			_, err := c.Get(context.Background(), "/x", "")
			if n.Load() != tt.requests {
				t.Errorf("requests = %d, want %d", n.Load(), tt.requests)
			}
			if !reflect.DeepEqual(clock.sleeps, tt.sleeps) {
				t.Errorf("sleeps = %v, want %v", clock.sleeps, tt.sleeps)
			}
			var rl *RateLimitError
			isRate := errors.As(err, &rl)
			if isRate != tt.wantRate {
				t.Fatalf("error = %T %v, want rate limit %v", err, err, tt.wantRate)
			}
			if isRate && !tt.retryAt.IsZero() && !rl.RetryAt.Equal(tt.retryAt) {
				t.Errorf("RetryAt = %v, want %v", rl.RetryAt, tt.retryAt)
			}
		})
	}
}

func TestSecondaryRetriesAreCountedApart(t *testing.T) {
	// A primary wait does not use up the one retry of a secondary limit.
	reset := strconv.FormatInt(epoch.Add(2*time.Second).Unix(), 10)
	srv, n := bodyServer(t,
		bodyStep{status: 403, header: map[string]string{"X-Left": "0", "X-Reset": reset}},
		bodyStep{status: 403, body: "secondary rate limit"},
		bodyStep{status: 200})
	c, _ := clientWithClock(t, srv.URL, Config{Policy: secondaryPolicy(), MaxWait: time.Minute})
	if _, err := c.Get(context.Background(), "/x", ""); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if n.Load() != 3 {
		t.Errorf("requests = %d, want 3", n.Load())
	}
}

func TestStatusErrorKeepsHeaders(t *testing.T) {
	srv, _ := bodyServer(t, bodyStep{status: 403, header: map[string]string{"X-Accepted-GitHub-Permissions": "actions=read"}, body: "nope"})
	c := newTestClient(t, srv.URL, Config{})
	_, err := c.Get(context.Background(), "/x", "")
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("error = %T %v, want *StatusError", err, err)
	}
	if got := se.Header.Get("X-Accepted-GitHub-Permissions"); got != "actions=read" {
		t.Errorf("Header = %v, want the response headers", se.Header)
	}
	if strings.Contains(se.Error(), "actions=read") {
		t.Errorf("Error() %q must not print headers", se.Error())
	}
}
