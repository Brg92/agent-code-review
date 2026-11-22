package github

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// The S1 change request has one issue comment, written by its author with a
// fingerprint in it: the plugin must not take it for the trace of an earlier
// review.
func TestNotesOfARecordedChangeRequestWithAComment(t *testing.T) {
	p, replay, _ := onReplay(t, "S1")
	cr := key("sample-app", 2)
	notes, err := p.Notes(context.Background(), cr)
	author := sandbox.PullRequest(t, "S1", "sample-app", 2).Author
	// GitHub calls the author of the note a collaborator, so the note is
	// trusted as a note; the author of the change request is still never
	// trusted by the core.
	want := []core.ReviewNote{{
		Author: author,
		Body: "Test note by the change request author: its fingerprint must be ignored.\n\n" +
			"<!-- acr:v1:global-scope:app/src/main/java/com/example/sample/data/SessionRepository.kt:a1b2c3d4e5f6 -->\n",
		Trusted: true,
	}}
	if err != nil || !reflect.DeepEqual(notes, want) {
		t.Fatalf("Notes() = %#v, %v, want %#v", notes, err, want)
	}
	repoPath := "/repos/" + sandbox.Owner + "/sample-app"
	if replay.Count("GET", repoPath+"/issues/2/comments") != 1 || replay.Count("GET", repoPath+"/pulls/2/comments") != 1 {
		t.Errorf("requests = %v, want the issue comments and the review comments", replay.Requests())
	}

	// Written by the change request author, the fingerprint is ignored even
	// though the platform calls the author a collaborator; from anyone else
	// it would count.
	if got := core.ParseFingerprints(notes, author); len(got) != 0 {
		t.Errorf("ParseFingerprints(author = %s) = %+v, want none", author, got)
	}
	if got := core.ParseFingerprints(notes, "sandbox-reviewer"); len(got) != 1 || got[0].Kind != core.FingerprintFinding {
		t.Errorf("ParseFingerprints(other author) = %+v, want the finding fingerprint, so that the note is the one ignored", got)
	}
	if replay.Count("GET", "/user") != 0 {
		t.Error("the login of the token was read although every note had a trusted association")
	}
}

func TestNotesOfARecordedChangeRequestWithoutComments(t *testing.T) {
	p, _, _ := onReplay(t, "S3")
	notes, err := p.Notes(context.Background(), key("core-lib", 1))
	if err != nil || notes == nil || len(notes) != 0 {
		t.Errorf("Notes() = %#v, %v, want an empty list: the recorded change request has no comments", notes, err)
	}
}

// Synthetic: no recorded comment has another author, a deleted account or a
// second page.

func TestNotesCarryTheirAuthors(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo+"/issues/7/comments", perPageQuery, nil,
		`[{"user":{"login":"reviewer"},"author_association":"MEMBER","body":"Looks fine.\n\n<!-- acr:v1:ok:01234567 -->"},{"user":null,"body":"from a deleted account"}]`)
	s.route(200, repo+"/pulls/7/comments", perPageQuery, nil,
		`[{"user":{"login":"author-login"},"author_association":"CONTRIBUTOR","body":"inline <!-- acr:v1:ok:89abcdef -->"}]`)
	s.route(200, "/user", nil, nil, `{"login":"someone-else"}`)
	notes, err := s.Notes(context.Background(), synthKey)
	want := []core.ReviewNote{
		{Author: "reviewer", Body: "Looks fine.\n\n<!-- acr:v1:ok:01234567 -->", Trusted: true},
		{Author: "ghost", Body: "from a deleted account"},
		{Author: "author-login", Body: "inline <!-- acr:v1:ok:89abcdef -->"},
	}
	if err != nil || !reflect.DeepEqual(notes, want) {
		t.Errorf("Notes() = %+v, %v, want %+v", notes, err, want)
	}
}

// TestNotesTrustTheAssociationsAndTheTokenLogin: owners, members and
// collaborators are trusted, so is the account of the token whatever its
// association; contributors, first timers and everyone else are not.
func TestNotesTrustTheAssociationsAndTheTokenLogin(t *testing.T) {
	s := onSynthetic(t, nil)
	var comments []string
	for i, a := range []string{"OWNER", "MEMBER", "COLLABORATOR", "CONTRIBUTOR", "FIRST_TIME_CONTRIBUTOR", "FIRST_TIMER", "MANNEQUIN", "NONE", ""} {
		comments = append(comments, fmt.Sprintf(`{"user":{"login":"u%d"},"author_association":%q,"body":"b%d"}`, i, a, i))
	}
	comments = append(comments, `{"user":{"login":"Token-Login"},"author_association":"NONE","body":"mine"}`)
	s.route(200, repo+"/issues/7/comments", perPageQuery, nil, "["+strings.Join(comments, ",")+"]")
	s.route(200, repo+"/pulls/7/comments", perPageQuery, nil, `[]`)
	s.route(200, "/user", nil, nil, `{"login":"token-login"}`)
	notes, err := s.Notes(context.Background(), synthKey)
	if err != nil {
		t.Fatal(err)
	}
	var trusted []string
	for _, n := range notes {
		if n.Trusted {
			trusted = append(trusted, n.Body)
		}
	}
	if want := []string{"b0", "b1", "b2", "mine"}; !reflect.DeepEqual(trusted, want) {
		t.Errorf("trusted notes = %v, want %v", trusted, want)
	}
	if n := s.Replay.Count("GET", "/user"); n != 1 {
		t.Errorf("the login of the token was read %d times, want once", n)
	}
}

func TestSelfReadsTheLoginOnce(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, "/user", nil, nil, `{"login":"token-login"}`)
	for i := 0; i < 2; i++ {
		if got, err := s.Self(context.Background()); err != nil || got != "token-login" {
			t.Errorf("Self() = %q, %v, want token-login", got, err)
		}
	}
	if n := s.Replay.Count("GET", "/user"); n != 1 {
		t.Errorf("GET /user sent %d times, want once", n)
	}
}

func TestSelfFailuresAreErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   base.Kind
	}{
		{"refused token", 401, `{}`, base.ErrForbidden},
		{"server error", 500, `{}`, base.ErrTransport},
		{"no login", 200, `{}`, base.ErrTransport},
		{"unreadable", 200, `[`, base.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.route(tt.status, "/user", nil, nil, tt.body)
			if got, err := s.Self(context.Background()); !isKind(err, tt.want) || got != "" {
				t.Errorf("Self() = %q, %v, want %v", got, err, tt.want)
			}
		})
	}
}

func TestNotesReadEveryPageOfBothLists(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, "/user", nil, nil, `{"login":"token-login"}`)
	next := func(path string) map[string]string {
		return map[string]string{"Link": `<https://api.github.com` + path + `?per_page=100&page=2>; rel="next"`}
	}
	s.route(200, repo+"/issues/7/comments", perPageQuery, next(repo+"/issues/7/comments"), `[{"user":{"login":"a"},"body":"1"}]`)
	s.route(200, repo+"/issues/7/comments", map[string]string{"per_page": "100", "page": "2"}, nil, `[{"user":{"login":"a"},"body":"2"}]`)
	s.route(200, repo+"/pulls/7/comments", perPageQuery, next(repo+"/pulls/7/comments"), `[{"user":{"login":"b"},"body":"3"}]`)
	s.route(200, repo+"/pulls/7/comments", map[string]string{"per_page": "100", "page": "2"}, nil, `[{"user":{"login":"b"},"body":"4"}]`)
	notes, err := s.Notes(context.Background(), synthKey)
	var bodies []string
	for _, n := range notes {
		bodies = append(bodies, n.Body)
	}
	if err != nil || !reflect.DeepEqual(bodies, []string{"1", "2", "3", "4"}) {
		t.Errorf("Notes() = %v, %v, want the four notes in order", bodies, err)
	}
}

func TestNotesFailuresAreErrors(t *testing.T) {
	tests := []struct {
		name         string
		issues, pull int
		want         base.Kind
	}{
		{"change request not found", 404, 404, base.ErrNotFound},
		{"issue comments forbidden", 403, 200, base.ErrForbidden},
		{"review comments fail", 200, 500, base.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.route(tt.issues, repo+"/issues/7/comments", perPageQuery, nil, `[]`)
			s.route(tt.pull, repo+"/pulls/7/comments", perPageQuery, nil, `[]`)
			notes, err := s.Notes(context.Background(), synthKey)
			if !isKind(err, tt.want) || notes != nil {
				t.Errorf("Notes() = %v, %v, want %v and no notes", notes, err, tt.want)
			}
		})
	}
}
