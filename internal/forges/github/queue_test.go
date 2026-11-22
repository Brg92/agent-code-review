package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

var _ base.Queue = (*Provider)(nil)

// queueQuery is what ReviewQueue asks the search API for.
func queueQuery(limit string) map[string]string {
	return map[string]string{"q": "is:pr is:open review-requested:@me archived:false", "sort": "updated", "order": "desc", "per_page": limit}
}

// recordedQueueBody is the answer the sandbox recorded for the queue, which
// the recorder asked for with another query (it adds the organization).
func recordedQueueBody(t *testing.T) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("..", "..", "..", "testdata", "github", "queue", "*.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("recorded queue: %v, %v", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	return string(rec.Body)
}

func TestReviewQueueReadsTheRecordedAnswer(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, "/search/issues", queueQuery("30"), nil, recordedQueueBody(t))

	got, err := s.ReviewQueue(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 13 || got.Incomplete || len(got.Items) != 13 {
		t.Fatalf("ReviewQueue() = total %d, incomplete %v, %d items", got.Total, got.Incomplete, len(got.Items))
	}
	first := got.Items[0]
	want := base.QueueItem{
		Key:       core.Key{Forge: core.GitHub, Host: "github.com", Project: sandbox.Owner + "/sample-app", Number: 7},
		Title:     "Show when the session expires",
		Author:    "sandbox-author",
		UpdatedAt: time.Date(2026, 11, 2, 18, 23, 44, 0, time.UTC),
	}
	if first != want {
		t.Errorf("first item = %+v, want %+v", first, want)
	}
	if reqs := s.Replay.Requests(); len(reqs) != 1 || !strings.HasPrefix(reqs[0], "GET /search/issues?") {
		t.Errorf("requests = %v, want one search", reqs)
	}
}

func TestReviewQueueKeepsTheLimit(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, "/search/issues", queueQuery("2"), nil, recordedQueueBody(t))

	got, err := s.ReviewQueue(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Total != 13 {
		t.Errorf("ReviewQueue(2) = %d items of %d, want 2 of 13", len(got.Items), got.Total)
	}
}

func TestReviewQueueReadsOnePageOnly(t *testing.T) {
	s := onSynthetic(t, nil)
	link := `<` + s.Replay.URL() + `/search/issues?page=2>; rel="next"`
	s.route(200, "/search/issues", queueQuery("30"), map[string]string{"Link": link}, `{"total_count":0,"incomplete_results":false,"items":[]}`)

	if _, err := s.ReviewQueue(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	if n := s.Replay.Count("GET", "/search/issues"); n != 1 {
		t.Errorf("%d searches, want 1: the queue is the first page", n)
	}
}

func TestReviewQueueReportsAnIncompleteSearch(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, "/search/issues", queueQuery("30"), nil, `{"total_count":0,"incomplete_results":true,"items":[]}`)

	got, err := s.ReviewQueue(context.Background(), 30)
	if err != nil || !got.Incomplete {
		t.Errorf("ReviewQueue() = %+v, %v, want Incomplete", got, err)
	}
}

func TestReviewQueueRefusesALimitOutsideTheRange(t *testing.T) {
	s := onSynthetic(t, nil)
	for _, limit := range []int{0, -1, 101} {
		if _, err := s.ReviewQueue(context.Background(), limit); err == nil {
			t.Errorf("ReviewQueue(%d) succeeded", limit)
		}
	}
	if reqs := s.Replay.Requests(); len(reqs) != 0 {
		t.Errorf("requests = %v, want none", reqs)
	}
}

func TestReviewQueueSkipsWhatItCannotKey(t *testing.T) {
	body := `{"total_count":3,"incomplete_results":false,"items":[
	  {"number":1,"title":"ok","draft":true,"updated_at":"2026-01-02T03:04:05Z","user":{"login":"ann"},"repository_url":"https://api.github.com/repos/acme/app"},
	  {"number":2,"title":"no repository","updated_at":"2026-01-02T03:04:05Z","user":{"login":"ann"},"repository_url":"https://api.github.com/user"},
	  {"number":0,"title":"no number","updated_at":"2026-01-02T03:04:05Z","user":{"login":"ann"},"repository_url":"https://api.github.com/repos/acme/app"}]}`
	s := onSynthetic(t, nil)
	s.route(200, "/search/issues", queueQuery("30"), nil, body)

	got, err := s.ReviewQueue(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Key.String() != "github:github.com/acme/app#1" || !got.Items[0].Draft {
		t.Errorf("items = %+v, want only acme/app#1, a draft", got.Items)
	}
	if got.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", got.Skipped)
	}
}

func TestReviewQueueErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header map[string]string
		body   string
		want   error
	}{
		{"no permission", 403, map[string]string{"X-Accepted-GitHub-Permissions": "issues=read"}, `{"message":"forbidden"}`, base.ErrForbidden},
		{"refused token", 401, nil, `{"message":"Bad credentials"}`, base.ErrForbidden},
		{"validation failed", 422, nil, `{"message":"Validation Failed"}`, base.ErrTransport},
		{"server error", 500, nil, `{"message":"oops"}`, base.ErrTransport},
		{"unreadable", 200, nil, `not json`, base.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.route(tt.status, "/search/issues", queueQuery("30"), tt.header, tt.body)
			_, err := s.ReviewQueue(context.Background(), 30)
			if !errors.Is(err, tt.want) {
				t.Errorf("ReviewQueue() error = %v, want kind %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), canary) {
				t.Errorf("the error shows the token: %v", err)
			}
		})
	}
}

func TestReviewQueueQueryIsFixed(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, "/search/issues", queueQuery("30"), nil, `{"total_count":0,"incomplete_results":false,"items":[]}`)
	if _, err := s.ReviewQueue(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	_, raw, _ := strings.Cut(s.Replay.Requests()[0], "?")
	q, err := url.ParseQuery(raw)
	if err != nil || q.Get("q") != "is:pr is:open review-requested:@me archived:false" {
		t.Errorf("query = %q, %v", raw, err)
	}
}
