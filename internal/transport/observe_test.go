package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// observed collects the events of a client.
type observed struct{ events []Event }

func (o *observed) observe(e Event) { o.events = append(o.events, e) }

func TestObserveOneEventPerRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{}")) }))
	defer srv.Close()
	var o observed
	c, _ := clientWithClock(t, srv.URL+"/api", Config{Token: staticToken(testToken), Observe: o.observe})
	if _, err := c.Get(context.Background(), "/repos/o/r/pulls/7/files?per_page=100&access_token=x", ""); err != nil {
		t.Fatal(err)
	}
	want := []Event{{Method: "GET", Path: "/api/repos/o/r/pulls/7/files", Status: 200, Attempts: 1, Outcome: OutcomeOK}}
	if !reflect.DeepEqual(o.events, want) {
		t.Errorf("events = %+v, want %+v", o.events, want)
	}
}

func TestObserveRetriesAndDuration(t *testing.T) {
	srv, _ := scriptedServer(t, step{status: 503}, step{status: 200})
	var o observed
	c, clock := clientWithClock(t, srv.URL, Config{Observe: o.observe})
	if _, err := c.Get(context.Background(), "/x", ""); err != nil {
		t.Fatal(err)
	}
	if len(clock.sleeps) != 1 {
		t.Fatalf("sleeps = %v", clock.sleeps)
	}
	want := []Event{
		{Method: "GET", Path: "/x", Status: 503, Attempts: 1, Outcome: OutcomeRetry},
		{Method: "GET", Path: "/x", Status: 200, Attempts: 2, Outcome: OutcomeOK, Duration: clock.sleeps[0]},
	}
	if !reflect.DeepEqual(o.events, want) {
		t.Errorf("events = %+v, want %+v", o.events, want)
	}
}

func TestObserveFailures(t *testing.T) {
	tests := []struct {
		name   string
		steps  []step
		cfg    Config
		status int
		out    Outcome
	}{
		{"not found", []step{{status: 404}}, Config{}, 404, OutcomeError},
		{"server error after the retries", []step{{status: 500}}, Config{MaxRetries: -1}, 500, OutcomeError},
		{"rate limit too far away", []step{{status: 429, header: map[string]string{"Retry-After": "3600"}}}, Config{MaxWait: time.Minute}, 429, OutcomeRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := scriptedServer(t, tt.steps...)
			var o observed
			tt.cfg.Observe = o.observe
			c, _ := clientWithClock(t, srv.URL, tt.cfg)
			if _, err := c.Get(context.Background(), "/x?q=1", ""); err == nil {
				t.Fatal("no error")
			}
			last := o.events[len(o.events)-1]
			if last.Status != tt.status || last.Outcome != tt.out || last.Path != "/x" {
				t.Errorf("last event = %+v, want status %d outcome %s", last, tt.status, tt.out)
			}
		})
	}
}

func TestObserveNetworkErrorAndTokenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	var o observed
	c, _ := clientWithClock(t, url, Config{MaxRetries: -1, Observe: o.observe})
	if _, err := c.Get(context.Background(), "/x", ""); err == nil {
		t.Fatal("no error")
	}
	if len(o.events) != 1 || o.events[0].Status != 0 || o.events[0].Outcome != OutcomeError || o.events[0].Attempts != 1 {
		t.Errorf("events = %+v", o.events)
	}

	o = observed{}
	failing := func(context.Context) (string, error) { return "", context.DeadlineExceeded }
	c, _ = clientWithClock(t, url, Config{Token: failing, Observe: o.observe})
	if _, err := c.Get(context.Background(), "/x", ""); err == nil {
		t.Fatal("no error")
	}
	if len(o.events) != 1 || o.events[0].Attempts != 0 || o.events[0].Outcome != OutcomeError {
		t.Errorf("events = %+v", o.events)
	}
}

func TestObserveNotModifiedIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	var o observed
	c, _ := clientWithClock(t, srv.URL, Config{Observe: o.observe})
	for range 2 {
		if _, err := c.Get(context.Background(), "/x", ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(o.events) != 2 || o.events[1].Status != 304 || o.events[1].Outcome != OutcomeOK {
		t.Errorf("events = %+v", o.events)
	}
}

func TestObserveEveryPage(t *testing.T) {
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", `<`+srvURL+`/list?page=2>; rel="next"`)
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	srvURL = srv.URL
	var o observed
	c, _ := clientWithClock(t, srv.URL, Config{Observe: o.observe})
	if _, err := c.GetPages(context.Background(), "/list", "", 5); err != nil {
		t.Fatal(err)
	}
	if len(o.events) != 2 {
		t.Fatalf("events = %+v", o.events)
	}
	for _, e := range o.events {
		if strings.Contains(e.Path, "?") || e.Path != "/list" {
			t.Errorf("path %q has a query", e.Path)
		}
	}
}
