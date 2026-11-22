package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

var _ base.Diagnoser = (*Provider)(nil)

func appKey() core.Key {
	return core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: 1}
}

func TestAccountOfAClassicToken(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, "/user", nil, map[string]string{"X-OAuth-Scopes": "repo, read:org"}, `{"login":"ann"}`)
	got, err := s.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Login != "ann" || !got.Classic || strings.Join(got.Scopes, ",") != "repo,read:org" {
		t.Errorf("Account() = %+v", got)
	}
}

func TestAccountOfATokenWithNoScopes(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, "/user", nil, map[string]string{"X-OAuth-Scopes": ""}, `{"login":"ann"}`)
	got, err := s.Account(context.Background())
	if err != nil || !got.Classic || len(got.Scopes) != 0 {
		t.Errorf("Account() = %+v, %v, want a classic token with no scope", got, err)
	}
}

func TestAccountOfAFineGrainedToken(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, "/user", nil, nil, `{"login":"ann"}`)
	got, err := s.Account(context.Background())
	if err != nil || got.Login != "ann" || got.Classic {
		t.Errorf("Account() = %+v, %v, want a token that is not classic", got, err)
	}
}

func TestAccountErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"refused", 401, `{"message":"Bad credentials"}`, base.ErrForbidden},
		{"server", 500, `{}`, base.ErrTransport},
		{"unreadable", 200, `nope`, base.ErrTransport},
		{"no login", 200, `{}`, base.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.route(tt.status, "/user", nil, nil, tt.body)
			if _, err := s.Account(context.Background()); !errors.Is(err, tt.want) {
				t.Errorf("Account() error = %v, want %v", err, tt.want)
			}
		})
	}
}

// accessRoutes answers every probe of Access with a success, except the
// ones overridden.
func (s *synthetic) accessRoutes(repoBody string) {
	s.route(200, "/repos/acme/app", nil, nil, repoBody)
	s.route(200, "/repos/acme/app/pulls", map[string]string{"per_page": "1"}, nil, `[]`)
	s.route(200, "/repos/acme/app/contents", nil, nil, `[]`)
	s.route(200, "/repos/acme/app/issues", map[string]string{"per_page": "1"}, nil, `[]`)
	s.route(200, "/repos/acme/app/commits/main/check-runs", map[string]string{"per_page": "1"}, nil, `{"total_count":0,"check_runs":[]}`)
}

const repoPublic = `{"default_branch":"main","private":false}`

func readOf(a base.Access, area base.Area) error {
	for _, r := range a.Reads {
		if r.Area == area {
			return r.Err
		}
	}
	return errors.New("no such area " + string(area))
}

func writeOf(a base.Access, area base.Area) base.WriteAccess {
	for _, w := range a.Writes {
		if w.Area == area {
			return w
		}
	}
	return base.WriteAccess{Detail: "no such area"}
}

func TestAccessReadsEveryAreaInOrder(t *testing.T) {
	s := onSynthetic(t, nil)
	s.accessRoutes(repoPublic)
	got, err := s.Access(context.Background(), appKey(), base.Account{Login: "ann", Classic: true, Scopes: []string{"repo"}})
	if err != nil {
		t.Fatal(err)
	}
	var areas []string
	for _, r := range got.Reads {
		areas = append(areas, string(r.Area))
		if r.Err != nil {
			t.Errorf("%s: %v", r.Area, r.Err)
		}
	}
	if want := "repository,pull requests,contents,issues,check runs"; strings.Join(areas, ",") != want {
		t.Errorf("areas = %s, want %s", strings.Join(areas, ","), want)
	}
	for _, req := range s.Replay.Requests() {
		if !strings.HasPrefix(req, "GET ") {
			t.Errorf("request %q is not a read", req)
		}
	}
	for _, area := range []base.Area{base.AreaPullRequests, base.AreaIssues} {
		if w := writeOf(got, area); w.State != base.WriteAllowed {
			t.Errorf("write %s = %+v, want allowed by the repo scope", area, w)
		}
	}
}

func TestAccessNamesTheMissingPermission(t *testing.T) {
	s := onSynthetic(t, nil)
	s.accessRoutes(repoPublic)
	s.route(403, "/repos/acme/app/issues", map[string]string{"per_page": "1"}, map[string]string{"X-Accepted-GitHub-Permissions": "issues=read"}, `{"message":"Resource not accessible by personal access token"}`)
	s.route(403, "/repos/acme/app/commits/main/check-runs", map[string]string{"per_page": "1"}, map[string]string{"X-Accepted-GitHub-Permissions": "checks=read; actions=read"}, `{}`)
	got, err := s.Access(context.Background(), appKey(), base.Account{Login: "ann"})
	if err != nil {
		t.Fatal(err)
	}
	if err := readOf(got, base.AreaPullRequests); err != nil {
		t.Errorf("pull requests: %v", err)
	}
	issues := readOf(got, base.AreaIssues)
	if !errors.Is(issues, base.ErrForbidden) || strings.Join(base.PermissionsOf(issues), ",") != "issues: read" {
		t.Errorf("issues: %v, permissions %v", issues, base.PermissionsOf(issues))
	}
	checks := readOf(got, base.AreaChecks)
	if !errors.Is(checks, base.ErrForbidden) || strings.Join(base.PermissionsOf(checks), ",") != "checks: read,actions: read" {
		t.Errorf("check runs: %v, permissions %v", checks, base.PermissionsOf(checks))
	}
}

func TestAccessToARepositoryThatIsNotThere(t *testing.T) {
	s := onSynthetic(t, nil)
	for _, p := range []string{"/repos/acme/app", "/repos/acme/app/contents"} {
		s.route(404, p, nil, nil, `{"message":"Not Found"}`)
	}
	for _, p := range []string{"/repos/acme/app/pulls", "/repos/acme/app/issues"} {
		s.route(404, p, map[string]string{"per_page": "1"}, nil, `{"message":"Not Found"}`)
	}
	got, err := s.Access(context.Background(), appKey(), base.Account{Login: "ann"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got.Reads {
		if !errors.Is(r.Err, base.ErrNotFound) {
			t.Errorf("%s: %v, want not found", r.Area, r.Err)
		}
	}
	// Without the repository there is no default branch to read the checks on:
	// no request was sent for them.
	if n := s.Replay.Count("GET", "/repos/acme/app/commits/main/check-runs"); n != 0 {
		t.Errorf("%d check run requests, want 0", n)
	}
	if w := writeOf(got, base.AreaIssues); w.State != base.WriteUnknown {
		t.Errorf("write issues = %+v, want unknown: nothing was read", w)
	}
}

func TestAccessWriteCheckFollowsTheTokenScopes(t *testing.T) {
	tests := []struct {
		name    string
		account base.Account
		repo    string
		want    base.WriteState
	}{
		{"repo scope", base.Account{Classic: true, Scopes: []string{"repo"}}, `{"default_branch":"main","private":true}`, base.WriteAllowed},
		{"public_repo on a public repository", base.Account{Classic: true, Scopes: []string{"public_repo"}}, repoPublic, base.WriteAllowed},
		{"public_repo on a private repository", base.Account{Classic: true, Scopes: []string{"public_repo"}}, `{"default_branch":"main","private":true}`, base.WriteDenied},
		{"no useful scope", base.Account{Classic: true, Scopes: []string{"read:org"}}, repoPublic, base.WriteDenied},
		{"fine-grained token", base.Account{}, repoPublic, base.WriteUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.accessRoutes(tt.repo)
			got, err := s.Access(context.Background(), appKey(), tt.account)
			if err != nil {
				t.Fatal(err)
			}
			for _, area := range []base.Area{base.AreaPullRequests, base.AreaIssues} {
				if w := writeOf(got, area); w.State != tt.want || w.Detail == "" {
					t.Errorf("write %s = %+v, want state %v with a detail", area, w, tt.want)
				}
			}
		})
	}
}

func TestAccessRefusesAProjectOfAnotherHost(t *testing.T) {
	s := onSynthetic(t, nil)
	k := appKey()
	k.Host = "gitlab.com"
	if _, err := s.Access(context.Background(), k, base.Account{}); err == nil {
		t.Error("Access() on another host succeeded")
	}
	if len(s.Replay.Requests()) != 0 {
		t.Errorf("requests = %v", s.Replay.Requests())
	}
}

func TestAccessKeepsTheTokenOutOfErrors(t *testing.T) {
	s := onSynthetic(t, nil)
	s.accessRoutes(repoPublic)
	s.route(500, "/repos/acme/app/contents", nil, nil, `{"message":"oops"}`)
	got, err := s.Access(context.Background(), appKey(), base.Account{})
	if err != nil {
		t.Fatal(err)
	}
	e := readOf(got, base.AreaContents)
	if !errors.Is(e, base.ErrTransport) || strings.Contains(e.Error(), canary) {
		t.Errorf("contents: %v", e)
	}
}

func TestAccessChecksNeedActionsReadWhenTheAnswerDoesNotSay(t *testing.T) {
	s := onSynthetic(t, nil)
	s.accessRoutes(repoPublic)
	s.route(403, "/repos/acme/app/commits/main/check-runs", map[string]string{"per_page": "1"}, nil, `{"message":"Resource not accessible"}`)
	got, err := s.Access(context.Background(), appKey(), base.Account{})
	if err != nil {
		t.Fatal(err)
	}
	checks := readOf(got, base.AreaChecks)
	if !errors.Is(checks, base.ErrForbidden) || strings.Join(base.PermissionsOf(checks), ",") != "actions: read" {
		t.Errorf("check runs: %v, permissions %v, want actions: read, as a fine-grained token needs", checks, base.PermissionsOf(checks))
	}
}

func TestAccessWithoutADefaultBranchHasNoChecks(t *testing.T) {
	s := onSynthetic(t, nil)
	s.accessRoutes(`{"default_branch":"","private":false}`)
	got, err := s.Access(context.Background(), appKey(), base.Account{})
	if err != nil {
		t.Fatal(err)
	}
	if checks := readOf(got, base.AreaChecks); !errors.Is(checks, base.ErrNotFound) {
		t.Errorf("check runs: %v, want not found", checks)
	}
}
