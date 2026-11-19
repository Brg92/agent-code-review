package github

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

func TestSearchIncompleteIsNeverNoUse(t *testing.T) {
	tests := []struct {
		scenario, repo, term string
	}{
		{"S2", "sample-app", "legacyToken"},
		{"S7", "kmp-shared", "deviceModel"},
	}
	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			rec := sandbox.SearchOf(t, tt.scenario, tt.repo, tt.term)
			if !rec.Recorded || !rec.Incomplete {
				t.Fatalf("recording: %+v", rec)
			}
			p, replay, _ := onReplay(t, tt.scenario)
			got, err := p.Search().Search(key(tt.repo, 1).ProjectKey(), "main", tt.term)
			if !errors.Is(err, base.ErrSearchUnavailable) || got != nil {
				t.Errorf("Search() = %v, %v, want ErrSearchUnavailable: an incomplete search proves nothing", got, err)
			}
			if replay.Count("GET", "/search/code") != 1 {
				t.Errorf("requests = %v", replay.Requests())
			}
		})
	}
}

func TestSearchOnAnotherBranchIsUnavailableWithoutSearching(t *testing.T) {
	p, replay, _ := onReplay(t, "S2")
	got, err := p.Search().Search(key("sample-app", 1).ProjectKey(), "release/2.0", "legacyToken")
	if !errors.Is(err, base.ErrSearchUnavailable) || got != nil {
		t.Errorf("Search() = %v, %v, want ErrSearchUnavailable", got, err)
	}
	if replay.Count("GET", "/search/code") != 0 {
		t.Errorf("a search was sent for a branch GitHub does not index: %v", replay.Requests())
	}
}

// Synthetic: the recorded searches are both incomplete.

const searchPath = "/search/code"

func searchQuery(term string) map[string]string {
	return map[string]string{"q": term + " repo:acme/app", "per_page": "100"}
}

func (s *synthetic) repoRoute() {
	s.route(200, repo, nil, nil, `{"default_branch":"main"}`)
}

func (s *synthetic) file(path, ref, content string) {
	enc := encodeBase64(content)
	s.route(200, repo+"/contents/"+path, map[string]string{"ref": ref}, nil, `{"type":"file","encoding":"base64","size":`+strconv.Itoa(len(content))+`,"content":"`+enc+`"}`)
}

func TestSearchReturnsTheMatchingLines(t *testing.T) {
	s := onSynthetic(t, nil)
	s.repoRoute()
	s.route(200, searchPath, searchQuery("greet"), nil,
		`{"total_count":2,"incomplete_results":false,"items":[{"path":"src/Main.kt"},{"path":"src/Greeter.kt"}]}`)
	s.file("src/Greeter.kt", "main", "package acme\r\n\r\nfun greet(n: String) = n\r\n// Greet again\r\n")
	s.file("src/Main.kt", "main", "fun main() = greet(\"x\")\n")
	got, err := s.Search().Search(synthKey.ProjectKey(), "main", "greet")
	want := []rules.Match{
		{Path: "src/Greeter.kt", Line: 3, Text: "fun greet(n: String) = n"},
		{Path: "src/Main.kt", Line: 1, Text: `fun main() = greet("x")`},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Search() = %+v, %v, want %+v", got, err, want)
	}
}

func TestSearchMatchesInAnyCaseOnlyWhenNoLineMatchesExactly(t *testing.T) {
	s := onSynthetic(t, nil)
	s.repoRoute()
	s.route(200, searchPath, searchQuery("Greet"), nil, `{"total_count":1,"incomplete_results":false,"items":[{"path":"src/A.kt"}]}`)
	s.file("src/A.kt", "main", "fun greet() {}\n")
	got, err := s.Search().Search(synthKey.ProjectKey(), "main", "Greet")
	if want := []rules.Match{{Path: "src/A.kt", Line: 1, Text: "fun greet() {}"}}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Search() = %+v, %v, want %+v", got, err, want)
	}
}

func TestSearchWithNoResultIsAnEmptyListWhenComplete(t *testing.T) {
	s := onSynthetic(t, nil)
	s.repoRoute()
	s.route(200, searchPath, searchQuery("nothing"), nil, `{"total_count":0,"incomplete_results":false,"items":[]}`)
	got, err := s.Search().Search(synthKey.ProjectKey(), "main", "nothing")
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("Search() = %#v, %v, want an empty list: a complete search found nothing", got, err)
	}
}

func TestSearchFailuresAreNeverEmptyResults(t *testing.T) {
	complete := func(items string, total int) string {
		return `{"total_count":` + strconv.Itoa(total) + `,"incomplete_results":false,"items":` + items + `}`
	}
	tests := []struct {
		name   string
		status int
		body   string
		file   string // content of src/A.kt at main; "" = no route
		want   base.Kind
	}{
		{"validation failed", 422, `{"message":"Validation Failed"}`, "", base.ErrSearchUnavailable},
		{"forbidden", 403, `{"message":"no"}`, "", base.ErrForbidden},
		{"server error", 500, `{}`, "", base.ErrTransport},
		{"more results than returned", 200, complete(`[{"path":"src/A.kt"}]`, 1500), "x term x", base.ErrSearchUnavailable},
		{"a result file that has no matching line", 200, complete(`[{"path":"src/A.kt"}]`, 1), "nothing here", base.ErrSearchUnavailable},
		{"a result file that cannot be read", 200, complete(`[{"path":"src/A.kt"}]`, 1), "", base.ErrNotFound},
		{"unreadable answer", 200, `not json`, "", base.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.repoRoute()
			s.route(tt.status, searchPath, searchQuery("term"), nil, tt.body)
			if tt.file != "" {
				s.file("src/A.kt", "main", tt.file)
			} else {
				s.route(404, repo+"/contents/src/A.kt", map[string]string{"ref": "main"}, nil, `{"message":"Not Found"}`)
			}
			got, err := s.Search().Search(synthKey.ProjectKey(), "main", "term")
			if !errors.Is(err, tt.want) || got != nil {
				t.Errorf("Search() = %v, %v, want %v and nothing else", got, err, tt.want)
			}
		})
	}
}

func TestSearchRefusesMoreFilesThanItReads(t *testing.T) {
	s := onSynthetic(t, func(o *Options) { o.MaxSearchFiles = 2 })
	s.repoRoute()
	s.route(200, searchPath, searchQuery("term"), nil,
		`{"total_count":3,"incomplete_results":false,"items":[{"path":"a"},{"path":"b"},{"path":"c"}]}`)
	got, err := s.Search().Search(synthKey.ProjectKey(), "main", "term")
	if !errors.Is(err, base.ErrSearchUnavailable) || got != nil {
		t.Errorf("Search() = %v, %v, want ErrSearchUnavailable", got, err)
	}
	for _, r := range s.Replay.Requests() {
		if strings.Contains(r, "/contents/") {
			t.Errorf("a file was read although the search was refused: %s", r)
		}
	}
}

func TestSearchOfAnInvalidProjectKey(t *testing.T) {
	s := onSynthetic(t, nil)
	for _, project := range []string{"", "acme/app", "github:github.com/acme"} {
		got, err := s.Search().Search(project, "main", "x")
		if !errors.Is(err, base.ErrNotFound) || got != nil {
			t.Errorf("Search(%q) = %v, %v, want ErrNotFound", project, got, err)
		}
	}
	if len(s.Replay.Requests()) != 0 {
		t.Errorf("requests = %v", s.Replay.Requests())
	}
}
