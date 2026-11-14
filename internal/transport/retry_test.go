package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var epoch = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// fakeClock records sleeps and advances a fixed clock instead of waiting.
type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	f.sleeps = append(f.sleeps, d)
	f.now = f.now.Add(d)
	return ctx.Err()
}

// scripted answers each request with the next step; the last repeats.
type step struct {
	status int
	header map[string]string
}

func scriptedServer(t *testing.T, steps ...step) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		if i >= len(steps) {
			i = len(steps) - 1
		}
		for k, v := range steps[i].header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(steps[i].status)
		_, _ = w.Write([]byte("body " + strconv.Itoa(i)))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func clientWithClock(t *testing.T, url string, cfg Config) (*Client, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: epoch}
	cfg.Now, cfg.Sleep = clock.Now, clock.Sleep
	return newTestClient(t, url, cfg), clock
}

var testPolicy = Policy{RemainingHeader: "X-Left", ResetHeader: "X-Reset"}

func TestRetries(t *testing.T) {
	s := time.Second
	reset := strconv.FormatInt(epoch.Add(20*s).Unix(), 10)
	farReset := strconv.FormatInt(epoch.Add(time.Hour).Unix(), 10)
	tests := []struct {
		name      string
		cfg       Config
		steps     []step
		requests  int32
		sleeps    []time.Duration
		wantErr   any
		wantRetry time.Time
	}{
		{name: "5xx then success", steps: []step{{status: 502}, {status: 503}, {status: 200}}, requests: 3, sleeps: []time.Duration{s, 2 * s}},
		{name: "5xx exhausts retries", steps: []step{{status: 500}}, requests: 4, sleeps: []time.Duration{s, 2 * s, 4 * s}, wantErr: &StatusError{}},
		{name: "custom retries", cfg: Config{MaxRetries: 1}, steps: []step{{status: 500}}, requests: 2, sleeps: []time.Duration{s}, wantErr: &StatusError{}},
		{name: "no retries", cfg: Config{MaxRetries: -1}, steps: []step{{status: 500}}, requests: 1, wantErr: &StatusError{}},
		{name: "backoff cap", cfg: Config{MaxRetries: 3, BaseBackoff: 3 * s, MaxBackoff: 5 * s}, steps: []step{{status: 500}}, requests: 4, sleeps: []time.Duration{3 * s, 5 * s, 5 * s}, wantErr: &StatusError{}},
		{name: "4xx is final", steps: []step{{status: 404}}, requests: 1, wantErr: &StatusError{}},
		{name: "5xx honours Retry-After", steps: []step{{status: 503, header: map[string]string{"Retry-After": "9"}}, {status: 200}}, requests: 2, sleeps: []time.Duration{9 * s}},
		{name: "429 Retry-After seconds", steps: []step{{status: 429, header: map[string]string{"Retry-After": "7"}}, {status: 200}}, requests: 2, sleeps: []time.Duration{7 * s}},
		{name: "429 Retry-After date", steps: []step{{status: 429, header: map[string]string{"Retry-After": epoch.Add(10 * s).Format(http.TimeFormat)}}, {status: 200}}, requests: 2, sleeps: []time.Duration{10 * s}},
		{name: "429 past date", steps: []step{{status: 429, header: map[string]string{"Retry-After": epoch.Add(-10 * s).Format(http.TimeFormat)}}, {status: 200}}, requests: 2, sleeps: []time.Duration{0}},
		{name: "429 bad Retry-After uses backoff", steps: []step{{status: 429, header: map[string]string{"Retry-After": "soon"}}, {status: 200}}, requests: 2, sleeps: []time.Duration{s}},
		{name: "429 without hints uses backoff", steps: []step{{status: 429}, {status: 200}}, requests: 2, sleeps: []time.Duration{s}},
		{name: "403 with Retry-After is rate limit", steps: []step{{status: 403, header: map[string]string{"Retry-After": "3"}}, {status: 200}}, requests: 2, sleeps: []time.Duration{3 * s}},
		{name: "policy reset header", cfg: Config{Policy: testPolicy}, steps: []step{{status: 403, header: map[string]string{"X-Left": "0", "X-Reset": reset}}, {status: 200}}, requests: 2, sleeps: []time.Duration{20 * s}},
		{name: "policy remaining not zero is a plain 403", cfg: Config{Policy: testPolicy}, steps: []step{{status: 403, header: map[string]string{"X-Left": "12", "X-Reset": reset}}}, requests: 1, wantErr: &StatusError{}},
		{name: "default policy ignores platform headers", steps: []step{{status: 403, header: map[string]string{"X-Left": "0", "X-Reset": reset}}}, requests: 1, wantErr: &StatusError{}},
		{name: "reset beyond max wait", cfg: Config{Policy: testPolicy}, steps: []step{{status: 429, header: map[string]string{"X-Left": "0", "X-Reset": farReset}}}, requests: 1, wantErr: &RateLimitError{}, wantRetry: epoch.Add(time.Hour)},
		{name: "Retry-After beyond max wait", cfg: Config{MaxWait: 5 * s}, steps: []step{{status: 429, header: map[string]string{"Retry-After": "6"}}}, requests: 1, wantErr: &RateLimitError{}, wantRetry: epoch.Add(6 * s)},
		{name: "rate limit exhausts retries", steps: []step{{status: 429, header: map[string]string{"Retry-After": "1"}}}, requests: 4, sleeps: []time.Duration{s, s, s}, wantErr: &RateLimitError{}, wantRetry: epoch.Add(4 * s)},
		{name: "5xx Retry-After beyond max wait", cfg: Config{MaxWait: 5 * s}, steps: []step{{status: 503, header: map[string]string{"Retry-After": "60"}}}, requests: 1, wantErr: &StatusError{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, n := scriptedServer(t, tt.steps...)
			c, clock := clientWithClock(t, srv.URL, tt.cfg)
			resp, err := c.Get(context.Background(), "/x", "")
			if n.Load() != tt.requests {
				t.Errorf("requests = %d, want %d", n.Load(), tt.requests)
			}
			if !reflect.DeepEqual(clock.sleeps, tt.sleeps) {
				t.Errorf("sleeps = %v, want %v", clock.sleeps, tt.sleeps)
			}
			switch want := tt.wantErr.(type) {
			case nil:
				if err != nil || resp.StatusCode != 200 {
					t.Errorf("Get = %v, want success", err)
				}
			case *StatusError:
				if !errors.As(err, &want) {
					t.Errorf("error = %T %v, want *StatusError", err, err)
				}
			case *RateLimitError:
				if !errors.As(err, &want) {
					t.Fatalf("error = %T %v, want *RateLimitError", err, err)
				}
				if !want.RetryAt.Equal(tt.wantRetry) {
					t.Errorf("RetryAt = %v, want %v", want.RetryAt, tt.wantRetry)
				}
				if !strings.Contains(want.Error(), "rate limited") {
					t.Errorf("Error() = %q", want.Error())
				}
			}
		})
	}
}

// flakyTripper fails the first n round trips with a network error.
type flakyTripper struct {
	fails int
	calls int
}

func (f *flakyTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls++
	if f.calls <= f.fails {
		return nil, errors.New("connection reset by peer")
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
}

func TestNetworkErrorsAreRetried(t *testing.T) {
	for _, tt := range []struct {
		fails   int
		calls   int
		wantErr bool
	}{{2, 3, false}, {9, 4, true}} {
		ft := &flakyTripper{fails: tt.fails}
		c, clock := clientWithClock(t, "https://api.example.invalid", Config{base: ft})
		_, err := c.Get(context.Background(), "/x", "")
		var ne *NetworkError
		if (err != nil) != tt.wantErr || (tt.wantErr && !errors.As(err, &ne)) {
			t.Errorf("fails %d: error = %v", tt.fails, err)
		}
		if ft.calls != tt.calls || len(clock.sleeps) != tt.calls-1 {
			t.Errorf("fails %d: calls = %d sleeps = %v", tt.fails, ft.calls, clock.sleeps)
		}
	}
}

func TestCancelledContextStopsRetries(t *testing.T) {
	srv, n := scriptedServer(t, step{status: 500})
	ctx, cancel := context.WithCancel(context.Background())
	c := newTestClient(t, srv.URL, Config{Sleep: func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}})
	if _, err := c.Get(ctx, "/x", ""); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if n.Load() != 1 {
		t.Errorf("requests = %d, want 1", n.Load())
	}
	if _, err := c.Get(ctx, "/x", ""); !errors.Is(err, context.Canceled) {
		t.Errorf("error with cancelled context = %v, want context.Canceled", err)
	}
	if n.Load() != 1 {
		t.Errorf("requests after cancel = %d, want 1", n.Load())
	}
}

func TestSleepContextWaitsAndCancels(t *testing.T) {
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Errorf("sleepContext = %v", err)
	}
	if err := sleepContext(context.Background(), 0); err != nil {
		t.Errorf("sleepContext(0) = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepContext(cancelled) = %v", err)
	}
}
