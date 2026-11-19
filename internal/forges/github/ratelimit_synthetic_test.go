package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// Synthetic: no recording has a rate limit or a server error. The clock is
// injected, so nothing waits.

const okPull = `{"title":"T","body":"b","state":"open","head":{"sha":"h"},"base":{"sha":"b","ref":"main"},"user":{"login":"u"}}`

func at(d time.Duration) string { return strconv.FormatInt(epoch.Add(d).Unix(), 10) }

func primary(reset time.Duration) map[string]string {
	return map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": at(reset)}
}

func TestRateLimitPolicy(t *testing.T) {
	s := time.Second
	secondary := `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`
	tests := []struct {
		name     string
		steps    sandbox.Route
		requests int
		sleeps   []time.Duration
		want     base.Kind // "" = success
	}{
		{"primary within the maximum wait", sandbox.Route{Status: 403, Header: primary(30 * s), Body: `{"message":"API rate limit exceeded"}`, Then: []sandbox.Route{{Status: 200, Body: okPull}}},
			2, []time.Duration{31 * s}, ""},
		{"primary on a 429", sandbox.Route{Status: 429, Header: primary(10 * s), Then: []sandbox.Route{{Status: 200, Body: okPull}}},
			2, []time.Duration{11 * s}, ""},
		{"primary beyond the maximum wait", sandbox.Route{Status: 403, Header: primary(120 * s), Body: `{"message":"API rate limit exceeded"}`},
			1, nil, base.ErrRateLimited},
		{"primary one second beyond the maximum wait", sandbox.Route{Status: 403, Header: primary(60 * s)},
			1, nil, base.ErrRateLimited},
		{"secondary with Retry-After", sandbox.Route{Status: 403, Header: map[string]string{"Retry-After": "5"}, Body: secondary, Then: []sandbox.Route{{Status: 200, Body: okPull}}},
			2, []time.Duration{5 * s}, ""},
		{"secondary without Retry-After waits a minute", sandbox.Route{Status: 403, Body: secondary, Then: []sandbox.Route{{Status: 200, Body: okPull}}},
			2, []time.Duration{time.Minute}, ""},
		{"secondary is retried once, then fails", sandbox.Route{Status: 403, Body: secondary},
			2, []time.Duration{time.Minute}, base.ErrRateLimited},
		{"secondary on a 429 without hints", sandbox.Route{Status: 429, Then: []sandbox.Route{{Status: 200, Body: okPull}}},
			2, []time.Duration{time.Minute}, ""},
		{"secondary Retry-After beyond the maximum wait", sandbox.Route{Status: 429, Header: map[string]string{"Retry-After": "61"}},
			1, nil, base.ErrRateLimited},
		{"a plain 403 is forbidden, not a limit", sandbox.Route{Status: 403, Body: `{"message":"Resource not accessible"}`},
			1, nil, base.ErrForbidden},
		{"5xx then success", sandbox.Route{Status: 502, Then: []sandbox.Route{{Status: 200, Body: okPull}}},
			2, []time.Duration{s}, ""},
		{"5xx after the retries", sandbox.Route{Status: 502, Body: `{"message":"Bad gateway"}`},
			4, []time.Duration{s, 2 * s, 4 * s}, base.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			tt.steps.Path = repo + "/pulls/7"
			s.Replay.Add(tt.steps)
			cr, err := s.ChangeRequest(context.Background(), synthKey)
			if n := s.Replay.Count("GET", repo+"/pulls/7"); n != tt.requests {
				t.Errorf("requests = %d, want %d", n, tt.requests)
			}
			if got := s.Clock.sleeps; len(got) != len(tt.sleeps) || (len(got) > 0 && !equalDurations(got, tt.sleeps)) {
				t.Errorf("sleeps = %v, want %v", got, tt.sleeps)
			}
			switch {
			case tt.want == "" && (err != nil || cr.HeadSHA != "h"):
				t.Errorf("ChangeRequest() = %+v, %v, want success", cr, err)
			case tt.want != "" && (!isKind(err, tt.want) || cr != (core.ChangeRequest{})):
				t.Errorf("ChangeRequest() = %+v, %v, want %v", cr, err, tt.want)
			}
		})
	}
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRateLimitParametersAreOptions(t *testing.T) {
	s := onSynthetic(t, func(o *Options) {
		o.Limits = Limits{MaxWait: 5 * time.Second, ResetMargin: 2 * time.Second, SecondaryWait: 3 * time.Second, SecondaryRetries: 2}
	})
	s.Replay.Add(sandbox.Route{Path: repo + "/pulls/7", Status: 403, Header: primary(1 * time.Second),
		Then: []sandbox.Route{{Status: 429}, {Status: 429}, {Status: 429}}})
	_, err := s.ChangeRequest(context.Background(), synthKey)
	// primary 1+2s, then two secondary retries of 3s, then the third 429 fails.
	if !isKind(err, base.ErrRateLimited) || !equalDurations(s.Clock.sleeps, []time.Duration{3 * time.Second, 3 * time.Second, 3 * time.Second}) {
		t.Errorf("err = %v, sleeps = %v", err, s.Clock.sleeps)
	}
	if n := s.Replay.Count("GET", repo+"/pulls/7"); n != 4 {
		t.Errorf("requests = %d, want 4", n)
	}
}

func TestRequestsAreSequential(t *testing.T) {
	var inFlight, peak, total atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		total.Add(1)
		time.Sleep(5 * time.Millisecond)
		_, _ = w.Write([]byte(okPull))
	}))
	defer srv.Close()
	p, err := newProvider("github.com", srv.URL, &tokens{token: canary}, testOptions(&clock{now: epoch}))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.ChangeRequest(context.Background(), inProject(key("app", i+1), "acme/app")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() != 1 || total.Load() != 8 {
		t.Errorf("peak concurrency = %d over %d requests, want 1: requests are never parallel", peak.Load(), total.Load())
	}
}
