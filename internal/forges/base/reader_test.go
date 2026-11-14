package base

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/rules"
)

var project = core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: 1}

// files is a core.Files of one repository at one ref; fail fails every
// read.
type files struct {
	ref  string
	data map[string]string
	fail error
}

func (f files) Get(_ context.Context, p core.Key, ref, path string) ([]byte, bool, error) {
	if f.fail != nil {
		return nil, false, f.fail
	}
	if p != project || ref != f.ref {
		return nil, false, ErrNotFound
	}
	s, ok := f.data[path]
	if !ok {
		return nil, false, nil
	}
	return []byte(s), true, nil
}

func (f files) List(context.Context, core.Key, string, string) ([]core.DirEntry, error) {
	return nil, errors.New("not used")
}

func TestRepo(t *testing.T) {
	ctx := context.Background()
	repo := Repo(ctx, files{ref: "main", data: map[string]string{"build.gradle.kts": "plugins {}"}}, project, "main")

	got, err := repo.ReadFile("build.gradle.kts")
	if err != nil || string(got) != "plugins {}" {
		t.Errorf("ReadFile = %q, %v, want the content", got, err)
	}

	got, err = repo.ReadFile("settings.gradle.kts")
	if !errors.Is(err, ErrNotFound) || got != nil {
		t.Errorf("ReadFile of a missing file = %q, %v, want nil, ErrNotFound: packs read only files they were told exist", got, err)
	}
	if err != nil && !strings.Contains(err.Error(), "settings.gradle.kts") {
		t.Errorf("error %q does not name the file", err)
	}

	failing := Repo(ctx, files{fail: &Error{Kind: ErrRateLimited, Op: "contents"}}, project, "main")
	if got, err := failing.ReadFile("build.gradle.kts"); !errors.Is(err, ErrRateLimited) || got != nil {
		t.Errorf("ReadFile on a failing platform = %q, %v, want nil, ErrRateLimited", got, err)
	}
}

// reader is a Reader whose CI is fixed.
type reader struct {
	caps   Capabilities
	ci     core.CI
	err    error
	called bool
}

func (r *reader) Capabilities() Capabilities                                 { return r.caps }
func (r *reader) Changes() cascade.Source                                    { return nil }
func (r *reader) Files() core.Files                                          { return nil }
func (r *reader) Search() rules.Search                                       { return nil }
func (r *reader) Tree(context.Context, core.Key, string) rules.Tree          { return nil }
func (r *reader) Notes(context.Context, core.Key) ([]core.ReviewNote, error) { return nil, nil }
func (r *reader) CI(context.Context, core.Key, string) (core.CI, error) {
	r.called = true
	return r.ci, r.err
}

func TestReadCI(t *testing.T) {
	success := "success"
	passing := core.CI{State: core.CIPassing, Checks: []core.Check{{Name: "build", Status: "completed", Conclusion: &success}}}
	both := Capabilities{Search: SearchNone, CI: CISources{CheckRuns: true, CommitStatuses: true}}
	tests := []struct {
		name     string
		r        *reader
		want     core.CI
		wantCall bool
	}{
		{"read", &reader{caps: both, ci: passing}, passing, true},
		{"forbidden", &reader{caps: both, err: &Error{Kind: ErrForbidden, Op: "checks acme/app@abc", Permissions: []string{"checks: read"}}},
			core.CI{State: core.CIUnknown, Reason: "forbidden", Detail: "checks acme/app@abc: forbidden (needs checks: read)", Checks: []core.Check{}}, true},
		{"rate limited", &reader{caps: both, err: &Error{Kind: ErrRateLimited, Op: "checks acme/app@abc"}},
			core.CI{State: core.CIUnknown, Reason: "error", Detail: "checks acme/app@abc: rate limited", Checks: []core.Check{}}, true},
		{"no CI source", &reader{caps: Capabilities{Search: SearchNone}, ci: passing},
			core.CI{State: core.CIUnknown, Reason: "error", Detail: "the platform reports no CI", Checks: []core.Check{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadCI(context.Background(), tt.r, project, "abc")
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReadCI = %+v, want %+v", got, tt.want)
			}
			if tt.r.called != tt.wantCall {
				t.Errorf("provider CI called = %v, want %v", tt.r.called, tt.wantCall)
			}
		})
	}
}
