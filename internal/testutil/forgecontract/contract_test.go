package forgecontract

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// recorder keeps what the suite reports instead of failing the test.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

var capabilities = map[string]base.Capabilities{
	"default-branch search": {Search: base.SearchDefaultBranch, TreeMayTruncate: true, CI: base.CISources{CheckRuns: true, CommitStatuses: true}},
	"any-ref search":        {Search: base.SearchAnyRef, CI: base.CISources{CommitStatuses: true}},
	"no search, no CI":      {Search: base.SearchNone, TreeMayTruncate: true},
}

func TestFakePasses(t *testing.T) {
	for name, caps := range capabilities {
		t.Run(name, func(t *testing.T) {
			fake, fx := Example(caps)
			rec := &recorder{TB: t}
			Run(rec, fake, fx)
			for _, e := range rec.errs {
				t.Error(e)
			}
		})
	}
}

// searchOverride answers a search on a ref other than the default with
// no matches instead of search unavailable.
type searchOverride struct{ *Fake }

func (s searchOverride) Search() rules.Search { return emptyOtherRef{s.Fake} }

type emptyOtherRef struct{ f *Fake }

func (e emptyOtherRef) Search(project, ref, term string) ([]rules.Match, error) {
	if ref != "main" {
		return nil, nil
	}
	return e.f.Search().Search(project, ref, term)
}

func reported(errs []string, want string) bool {
	for _, e := range errs {
		if strings.Contains(e, want) {
			return true
		}
	}
	return false
}

func TestRunCatchesBrokenProviders(t *testing.T) {
	github := capabilities["default-branch search"]
	tests := []struct {
		name   string
		mutate func(*Fake, *Fixture) base.Reader
		want   string
	}{
		{"truncated tree returned as a partial list", func(f *Fake, _ *Fixture) base.Reader {
			delete(f.Fail, Call{OpTree, "github:github.com/acme/huge", "main"})
			f.Repos["github:github.com/acme/huge@main"] = map[string]string{"app/src/main/A.kt": ""}
			return f
		}, "no error, want tree truncated"},
		{"no matches instead of search unavailable", func(f *Fake, _ *Fixture) base.Reader {
			return searchOverride{f}
		}, "want ErrSearchUnavailable"},
		{"forbidden without permissions", func(f *Fake, _ *Fixture) base.Reader {
			f.Fail[Call{OpFilesList, "github:github.com/acme/locked", "main"}] = base.ErrForbidden
			return f
		}, "forbidden without the permissions"},
		{"untyped error", func(f *Fake, _ *Fixture) base.Reader {
			f.Fail[Call{OpSearch, "github:github.com/acme/flaky", "main"}] = errors.New("boom")
			return f
		}, `reason ""`},
		{"missing file as an error", func(f *Fake, fx *Fixture) base.Reader {
			fx.MissingPath = "missing"
			f.Fail[Call{OpFilesGet, "github:github.com/acme/app", "main"}] = base.ErrNotFound
			return f
		}, "want not found and no error"},
		{"capabilities that differ", func(f *Fake, _ *Fixture) base.Reader {
			f.Caps.Search = base.SearchAnyRef
			return f
		}, "capabilities ="},
		{"invalid capabilities", func(f *Fake, fx *Fixture) base.Reader {
			f.Caps.Search, fx.Capabilities.Search = "", ""
			return f
		}, "unknown search scope"},
		{"truncation the capabilities do not declare", func(f *Fake, fx *Fixture) base.Reader {
			f.Caps.TreeMayTruncate, fx.Capabilities.TreeMayTruncate = false, false
			fx.Failures = append(fx.Failures, Failure{OpTree, core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/huge", Number: 1}, "main", base.ErrTreeTruncated})
			return f
		}, "needs the tree operation and TreeMayTruncate"},
		{"no truncated tree in the fixture", func(f *Fake, fx *Fixture) base.Reader {
			fx.Failures = fx.Failures[:0]
			return f
		}, "a truncated tree must be among the failures"},
		{"unknown CI as an answer", func(f *Fake, fx *Fixture) base.Reader {
			fx.CI = core.CI{State: core.CIUnknown, Reason: "error"}
			f.CIs["github:github.com/acme/app@"+fx.Head] = fx.CI
			return f
		}, "returned an unknown state"},
		{"notes without review comments", func(f *Fake, fx *Fixture) base.Reader {
			f.NoteSets[fx.ChangeRequest] = f.NoteSets[fx.ChangeRequest][:2]
			return f
		}, "fingerprints in Notes"},
		{"empty fixture", func(f *Fake, fx *Fixture) base.Reader {
			fx.Matches = nil
			return f
		}, "must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, fx := Example(github)
			r := tt.mutate(fake, &fx)
			rec := &recorder{TB: t}
			Run(rec, r, fx)
			if !reported(rec.errs, tt.want) {
				t.Errorf("suite reported %q, want an error with %q", rec.errs, tt.want)
			}
		})
	}
}
