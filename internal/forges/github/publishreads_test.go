package github

import (
	"context"
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

var issuesQuery = map[string]string{"state": "all", "labels": "review", "per_page": "100"}

func TestIssuesListOpenAndClosedWithTheirAuthors(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo+"/issues", issuesQuery, nil, `[
	  {"number":3,"state":"open","title":"Review of #7: MAJOR findings","body":"b3","user":{"login":"Reviewer"},"author_association":"MEMBER"},
	  {"number":4,"state":"closed","title":"t4","body":null,"user":{"login":"stranger"},"author_association":"NONE"},
	  {"number":5,"state":"open","title":"a pull request","body":"x","user":{"login":"a"},"author_association":"OWNER","pull_request":{"url":"u"}},
	  {"number":6,"state":"open","title":"mine","body":"b6","user":{"login":"token-login"},"author_association":"NONE"},
	  {"number":7,"state":"open","title":"gone","body":"b7","user":null}]`)
	s.route(200, "/user", nil, nil, `{"login":"token-login"}`)
	got, err := s.Issues(context.Background(), synthKey, "review")
	want := []core.Issue{
		{Number: 3, Open: true, Author: "Reviewer", Trusted: true, Title: "Review of #7: MAJOR findings", Body: "b3"},
		{Number: 4, Open: false, Author: "stranger", Title: "t4"},
		{Number: 6, Open: true, Author: "token-login", Trusted: true, Title: "mine", Body: "b6"},
		{Number: 7, Open: true, Author: "ghost", Title: "gone", Body: "b7"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Issues = %+v, %v, want %+v", got, err, want)
	}
}

func TestIssuesReadEveryPage(t *testing.T) {
	s := onSynthetic(t, nil)
	next := map[string]string{"Link": `<https://api.github.com` + repo + `/issues?state=all&labels=review&per_page=100&page=2>; rel="next"`}
	s.route(200, repo+"/issues", issuesQuery, next, `[{"number":1,"state":"open","title":"a","body":"","user":{"login":"m"},"author_association":"MEMBER"}]`)
	s.route(200, repo+"/issues", map[string]string{"state": "all", "labels": "review", "per_page": "100", "page": "2"}, nil,
		`[{"number":2,"state":"open","title":"b","body":"","user":{"login":"m"},"author_association":"OWNER"}]`)
	got, err := s.Issues(context.Background(), synthKey, "review")
	if err != nil || len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
		t.Errorf("Issues = %+v, %v, want issues 1 and 2", got, err)
	}
}

func TestIssuesEscapeTheLabel(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo+"/issues", map[string]string{"state": "all", "labels": "needs review & more", "per_page": "100"}, nil, `[]`)
	if got, err := s.Issues(context.Background(), synthKey, "needs review & more"); err != nil || got == nil || len(got) != 0 {
		t.Errorf("Issues = %+v, %v, want an empty list", got, err)
	}
}

func TestPublishReadsFail(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		want   base.Kind
	}{
		{"not found", 404, base.ErrNotFound}, {"forbidden", 403, base.ErrForbidden}, {"server", 500, base.ErrTransport},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.route(tt.status, repo+"/issues", issuesQuery, nil, `{}`)
			if got, err := s.Issues(context.Background(), synthKey, "review"); !isKind(err, tt.want) || got != nil {
				t.Errorf("Issues = %v, %v, want %v and no issues", got, err, tt.want)
			}
		})
	}
}

func repoRoute(s *synthetic) { s.route(200, repo, nil, nil, `{"default_branch":"main"}`) }

func TestHasLabel(t *testing.T) {
	tests := []struct {
		name   string
		status int
		repo   int
		want   bool
		kind   base.Kind
	}{
		{"there", 200, 200, true, ""},
		{"missing", 404, 200, false, ""},
		{"repository not readable", 404, 404, false, base.ErrNotFound},
		{"forbidden", 403, 200, false, base.ErrForbidden},
		{"server error", 500, 200, false, base.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.route(tt.repo, repo, nil, nil, `{"default_branch":"main"}`)
			s.route(tt.status, repo+"/labels/review", nil, nil, `{"name":"review"}`)
			got, err := s.HasLabel(context.Background(), synthKey, "review")
			if got != tt.want || (tt.kind == "") != (err == nil) || (tt.kind != "" && !isKind(err, tt.kind)) {
				t.Errorf("HasLabel = %v, %v, want %v, %v", got, err, tt.want, tt.kind)
			}
		})
	}
}

func TestHasLabelEscapesTheName(t *testing.T) {
	s := onSynthetic(t, nil)
	repoRoute(s)
	s.route(200, repo+"/labels/needs review", nil, nil, `{}`)
	if got, err := s.HasLabel(context.Background(), synthKey, "needs review"); err != nil || !got {
		t.Errorf("HasLabel = %v, %v", got, err)
	}
}

func TestAssignable(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   bool
		kind   base.Kind
	}{
		{"can be assigned", 204, true, ""},
		{"cannot", 404, false, ""},
		{"forbidden", 403, false, base.ErrForbidden},
		{"server error", 502, false, base.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			repoRoute(s)
			s.route(tt.status, repo+"/assignees/sandbox-author", nil, nil, ``)
			got, err := s.Assignable(context.Background(), synthKey, "sandbox-author")
			if got != tt.want || (tt.kind == "") != (err == nil) || (tt.kind != "" && !isKind(err, tt.kind)) {
				t.Errorf("Assignable = %v, %v, want %v, %v", got, err, tt.want, tt.kind)
			}
		})
	}
	s := onSynthetic(t, nil)
	s.route(404, repo, nil, nil, `{}`)
	if got, err := s.Assignable(context.Background(), synthKey, "x"); got || !isKind(err, base.ErrNotFound) {
		t.Errorf("Assignable on an unreadable repository = %v, %v, want ErrNotFound", got, err)
	}
}
