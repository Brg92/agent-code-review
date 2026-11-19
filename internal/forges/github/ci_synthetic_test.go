package github

import (
	"context"
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// Synthetic: the sandbox has no forbidden check runs and no commit
// statuses.

const sha = "0123456789abcdef0123456789abcdef01234567"

var perPageQuery = map[string]string{"per_page": "100"}

func TestCIForbiddenCarriesThePermissions(t *testing.T) {
	tests := []struct {
		name   string
		header map[string]string
		want   []string
	}{
		{"permissions from GitHub", map[string]string{"X-Accepted-GitHub-Permissions": "checks=read; actions=read"}, []string{"checks: read", "actions: read"}},
		{"GitHub says nothing", nil, []string{"actions: read"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.route(403, repo+"/commits/"+sha+"/check-runs", perPageQuery, tt.header, `{"message":"Resource not accessible by personal access token"}`)
			ci, err := s.CI(context.Background(), synthKey, sha)
			if !isKind(err, base.ErrForbidden) || !reflect.DeepEqual(base.PermissionsOf(err), tt.want) || !reflect.DeepEqual(ci, core.CI{}) {
				t.Errorf("CI() = %+v, %v (needs %v), want forbidden needing %v", ci, err, base.PermissionsOf(err), tt.want)
			}
			// The CLI turns the error into an unknown state, never none.
			got := base.ReadCI(context.Background(), s, synthKey, sha)
			if got.State != core.CIUnknown || got.Reason != "forbidden" {
				t.Errorf("ReadCI() = %+v, want unknown with reason forbidden", got)
			}
		})
	}
}

func TestCIAddsTheCombinedStatus(t *testing.T) {
	success, skipped := "success", "skipped"
	run := func(name, status string, conclusion *string) core.Check {
		return core.Check{Name: name, Status: status, Conclusion: conclusion}
	}
	failure := "failure"
	tests := []struct {
		name   string
		runs   string
		status string
		state  core.CIState
		checks []core.Check
	}{
		{"passing runs and a passing status",
			`{"check_runs":[{"name":"build","status":"completed","conclusion":"success"},{"name":"lint","status":"completed","conclusion":"skipped"}]}`,
			`{"statuses":[{"context":"ci/ext","state":"success"}]}`, core.CIPassing,
			[]core.Check{run("build", "completed", &success), run("lint", "completed", &skipped), run("ci/ext", "completed", &success)}},
		{"a failing status fails passing runs",
			`{"check_runs":[{"name":"build","status":"completed","conclusion":"success"}]}`,
			`{"statuses":[{"context":"ci/ext","state":"error"}]}`, core.CIFailing,
			[]core.Check{run("build", "completed", &success), run("ci/ext", "completed", &failure)}},
		{"a pending status",
			`{"check_runs":[{"name":"build","status":"completed","conclusion":"success"}]}`,
			`{"statuses":[{"context":"ci/ext","state":"pending"}]}`, core.CIPending,
			[]core.Check{run("build", "completed", &success), run("ci/ext", "in_progress", nil)}},
		{"statuses alone",
			`{"check_runs":[]}`, `{"statuses":[{"context":"ci/ext","state":"success"}]}`, core.CIPassing,
			[]core.Check{run("ci/ext", "completed", &success)}},
		{"a cancelled run fails",
			`{"check_runs":[{"name":"build","status":"completed","conclusion":"cancelled"}]}`, `{"statuses":[]}`, core.CIFailing,
			[]core.Check{run("build", "completed", func() *string { s := "cancelled"; return &s }())}},
		{"a queued run is pending",
			`{"check_runs":[{"name":"build","status":"queued","conclusion":null}]}`, `{"statuses":[]}`, core.CIPending,
			[]core.Check{run("build", "queued", nil)}},
		{"nothing at all", `{"check_runs":[]}`, `{"state":"pending","statuses":[]}`, core.CINone, []core.Check{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.route(200, repo+"/commits/"+sha+"/check-runs", perPageQuery, nil, tt.runs)
			s.route(200, repo+"/commits/"+sha+"/status", nil, nil, tt.status)
			ci, err := s.CI(context.Background(), synthKey, sha)
			if err != nil || ci.State != tt.state || !reflect.DeepEqual(ci.Checks, tt.checks) {
				t.Errorf("CI() = %+v, %v, want %s with %+v", ci, err, tt.state, tt.checks)
			}
		})
	}
}

func TestCIFailsWhenTheStatusCannotBeRead(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo+"/commits/"+sha+"/check-runs", perPageQuery, nil, `{"check_runs":[{"name":"build","status":"completed","conclusion":"success"}]}`)
	s.route(403, repo+"/commits/"+sha+"/status", nil, map[string]string{"X-Accepted-GitHub-Permissions": "statuses=read"}, `{"message":"no"}`)
	ci, err := s.CI(context.Background(), synthKey, sha)
	if !isKind(err, base.ErrForbidden) || !reflect.DeepEqual(ci, core.CI{}) {
		t.Errorf("CI() = %+v, %v, want forbidden and no partial state", ci, err)
	}
}

func TestCIReadsEveryPageOfCheckRuns(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo+"/commits/"+sha+"/check-runs", perPageQuery,
		map[string]string{"Link": `<https://api.github.com` + repo + `/commits/` + sha + `/check-runs?per_page=100&page=2>; rel="next"`},
		`{"check_runs":[{"name":"a","status":"completed","conclusion":"success"}]}`)
	s.route(200, repo+"/commits/"+sha+"/check-runs", map[string]string{"per_page": "100", "page": "2"}, nil,
		`{"check_runs":[{"name":"b","status":"completed","conclusion":"failure"}]}`)
	s.route(200, repo+"/commits/"+sha+"/status", nil, nil, `{"statuses":[]}`)
	ci, err := s.CI(context.Background(), synthKey, sha)
	if err != nil || ci.State != core.CIFailing || len(ci.Checks) != 2 {
		t.Errorf("CI() = %+v, %v, want both pages and failing", ci, err)
	}
}

func TestCIOfAnUnknownCommitIsNotFound(t *testing.T) {
	s := onSynthetic(t, nil)
	// GitHub answers 422, not 404, for a SHA it has no commit for.
	s.route(422, repo+"/commits/"+sha+"/check-runs", perPageQuery, nil, `{"message":"No commit found for SHA: `+sha+`"}`)
	ci, err := s.CI(context.Background(), synthKey, sha)
	if !isKind(err, base.ErrNotFound) || !reflect.DeepEqual(ci, core.CI{}) {
		t.Errorf("CI() = %+v, %v, want ErrNotFound", ci, err)
	}
}
