package github

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// Synthetic: GitHub answers 404 both for a missing file and for a repository
// the token cannot read, and the recordings only have the first kind.

var notFound = `{"message":"Not Found"}`

var refMain = map[string]string{"ref": "main"}

func TestMissingFileOfAReadableRepositoryIsAbsence(t *testing.T) {
	s := onSynthetic(t, nil)
	s.repoRoute()
	s.route(404, repo+"/contents/a.md", refMain, nil, notFound)
	s.route(404, repo+"/contents/docs", refMain, nil, notFound)
	ctx := context.Background()

	got, found, err := s.Files().Get(ctx, synthKey, "main", "a.md")
	if err != nil || found || got != nil {
		t.Errorf("Get() = %q, %v, %v, want not found and no error", got, found, err)
	}
	entries, err := s.Files().List(ctx, synthKey, "main", "docs")
	if err != nil || len(entries) != 0 {
		t.Errorf("List() = %v, %v, want no entries and no error", entries, err)
	}
	if n := s.Replay.Count("GET", repo); n != 1 {
		t.Errorf("the repository was read %d times for two missing paths, want once (cached)", n)
	}
}

func TestMissingFileOfAnUnreadableRepositoryIsAnError(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header map[string]string
		want   base.Kind
		perms  []string
	}{
		{"404: no such repository or no access", 404, nil, base.ErrNotFound, nil},
		{"403: a permission is missing", 403, map[string]string{"X-Accepted-GitHub-Permissions": "metadata=read"}, base.ErrForbidden, []string{"metadata: read"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.route(tt.status, repo, nil, tt.header, `{"message":"x"}`)
			s.route(404, repo+"/contents/a.md", refMain, nil, notFound)
			s.route(404, repo+"/contents/docs", refMain, nil, notFound)
			s.route(404, repo+"/git/trees/main", map[string]string{"recursive": "1"}, nil, notFound)
			ctx := context.Background()

			got, found, err := s.Files().Get(ctx, synthKey, "main", "a.md")
			if !isKind(err, tt.want) || found || got != nil || !reflect.DeepEqual(base.PermissionsOf(err), tt.perms) {
				t.Errorf("Get() = %q, %v, %v, want %v needing %v", got, found, err, tt.want, tt.perms)
			}
			entries, err := s.Files().List(ctx, synthKey, "main", "docs")
			if !isKind(err, tt.want) || entries != nil {
				t.Errorf("List() = %v, %v, want %v", entries, err, tt.want)
			}
			paths, err := s.Tree(ctx, synthKey, "main").List("")
			if !isKind(err, tt.want) || paths != nil {
				t.Errorf("Tree().List() = %v, %v, want %v", paths, err, tt.want)
			}
			// The same through the reader the packs use: never "no such file".
			if _, err := base.Repo(ctx, s.Files(), synthKey, "main").ReadFile("a.md"); !isKind(err, tt.want) {
				t.Errorf("ReadFile() error = %v, want %v", err, tt.want)
			}
			if n := s.Replay.Count("GET", repo); n != 1 {
				t.Errorf("the repository was read %d times, want once, whatever the answer", n)
			}
		})
	}
}

func TestTreeOfAMissingRefOfAReadableRepositoryIsNotFound(t *testing.T) {
	s := onSynthetic(t, nil)
	s.repoRoute()
	s.route(404, repo+"/git/trees/gone", map[string]string{"recursive": "1"}, nil, notFound)
	paths, err := s.Tree(context.Background(), synthKey, "gone").List("")
	if !isKind(err, base.ErrNotFound) || paths != nil {
		t.Errorf("List() = %v, %v, want ErrNotFound: a tree has no empty valid answer", paths, err)
	}
}

func TestAFailedRepositoryReadIsNotCached(t *testing.T) {
	s := onSynthetic(t, nil)
	s.Replay.Add(sandbox.Route{Path: repo, Status: 500, Then: []sandbox.Route{{Status: 500}, {Status: 500}, {Status: 500}, {Status: 200, Body: `{"default_branch":"main"}`}}})
	s.route(404, repo+"/contents/a.md", refMain, nil, notFound)
	ctx := context.Background()

	_, found, err := s.Files().Get(ctx, synthKey, "main", "a.md")
	if !isKind(err, base.ErrTransport) || found {
		t.Fatalf("first Get() = %v, %v, want a transport error: the repository could not be read", found, err)
	}
	// The failed read used the four answers of the first request; the next gets the repository.
	if _, found, err = s.Files().Get(ctx, synthKey, "main", "a.md"); err != nil || found {
		t.Errorf("second Get() = %v, %v, want not found and no error", found, err)
	}
}

func TestDefaultBranchComesFromTheRepositoryOnce(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo, nil, nil, `{"default_branch":"trunk"}`)
	s.route(200, "/search/code", map[string]string{"q": "x repo:acme/app", "per_page": "100"}, nil, `{"total_count":0,"incomplete_results":false,"items":[]}`)
	for _, ref := range []string{"trunk", "main", "trunk"} {
		_, err := s.Search().Search(synthKey.ProjectKey(), ref, "x")
		wantUnavailable := ref != "trunk"
		if got := errors.Is(err, base.ErrSearchUnavailable); got != wantUnavailable || (!wantUnavailable && err != nil) {
			t.Errorf("Search(%s) error = %v, want unavailable = %v", ref, err, wantUnavailable)
		}
	}
	if n := s.Replay.Count("GET", repo); n != 1 {
		t.Errorf("the repository was read %d times for three searches, want once", n)
	}
}

func TestSearchOfAnUnreadableRepositoryIsAnErrorNotAnAbsence(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(404, repo, nil, nil, notFound)
	got, err := s.Search().Search(synthKey.ProjectKey(), "main", "x")
	if !isKind(err, base.ErrNotFound) || got != nil {
		t.Errorf("Search() = %v, %v, want ErrNotFound", got, err)
	}
}
