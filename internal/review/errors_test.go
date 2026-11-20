package review

import (
	"errors"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

func TestFailureReportHasAFixedShape(t *testing.T) {
	f := &Failure{Kind: KindPlatform, What: "could not read\nthe change request", Where: "github:github.com/o/r#7, change request", Why: "not-found, status 404",
		Hint: "check the reference", LogPath: "/work/runs/x/run.log"}
	want := "agent-code-review: could not read the change request\n" +
		"  where: github:github.com/o/r#7, change request\n" +
		"  why: not-found, status 404\n" +
		"  what to do: check the reference\n" +
		"  log: /work/runs/x/run.log\n"
	if got := f.Report(); got != want {
		t.Errorf("report:\n%s\nwant:\n%s", got, want)
	}
	// What is not known is a dash; a run that does not exist has no log.
	bare := (&Failure{What: "boom"}).Report()
	if bare != "agent-code-review: boom\n  where: -\n  why: -\n  what to do: -\n" {
		t.Errorf("bare report:\n%s", bare)
	}
	if got := (&Failure{What: "boom"}).Error(); got != "boom" {
		t.Errorf("Error() = %q", got)
	}
	if got := f.Error(); got != "could not read\nthe change request: not-found, status 404" {
		t.Errorf("Error() = %q", got)
	}
}

func TestClassifyReads(t *testing.T) {
	root := &cascade.RootError{Key: key(t, appName), Kind: cascade.KindIssue}
	tests := []struct {
		name string
		err  error
		host string
		want Kind
		why  string
		hint string
	}{
		{"no token", &base.Error{Kind: base.ErrForbidden, Err: auth.ErrNoToken}, "github.com", KindAuth, "no token", "`agent-code-review auth login` or set ACR_GITHUB_TOKEN"},
		{"no token on another host", &base.Error{Kind: base.ErrForbidden, Err: auth.ErrNoToken}, "ghe.example.com", KindAuth, "no token", "auth login --host ghe.example.com"},
		{"permissions", &base.Error{Kind: base.ErrForbidden, Permissions: []string{"checks: read", "contents: read"}}, "github.com", KindAuth,
			"forbidden (needs checks: read, contents: read)", "the token lacks checks: read, contents: read"},
		{"refused", &base.Error{Kind: base.ErrForbidden, Err: errors.New("status 401: Bad credentials")}, "github.com", KindAuth, "forbidden, status 401", "refused or lacks a permission"},
		{"rate limit", &base.Error{Kind: base.ErrRateLimited}, "github.com", KindPlatform, "rate-limited", "run the command again later"},
		{"not found", &base.Error{Kind: base.ErrNotFound, Err: errors.New("status 404: Not Found")}, "github.com", KindPlatform, "not-found, status 404", "check the reference"},
		{"too long a list", &base.Error{Kind: base.ErrListTruncated}, "github.com", KindPlatform, "list-truncated", "split it"},
		{"transport", &base.Error{Kind: base.ErrTransport, Err: errors.New("connection refused")}, "github.com", KindPlatform, "transport", "check the network"},
		{"an issue as the root", root, "github.com", KindUsage, root.Error(), "not of an issue"},
		{"a check of ours", &core.CIError{Problem: "the unknown state needs the reason forbidden or error"}, "github.com", KindPlatform,
			"ci: the unknown state needs the reason forbidden or error", "check the network"},
		{"wrapped", errors.Join(errors.New("context"), &base.Error{Kind: base.ErrNotFound, Op: "tree acme/app@main"}), "github.com", KindPlatform, "not-found", "check the reference"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := classify(tt.err, tt.host, "could not do it", "github:github.com/acme/app#7")
			if f.Kind != tt.want || f.Why != tt.why || !strings.Contains(f.Hint, tt.hint) || !errors.Is(f, tt.err) {
				t.Errorf("failure %+v\nwant kind %d, why %q, hint with %q", f, tt.want, tt.why, tt.hint)
			}
		})
	}
	var be *base.Error
	withOp := classify(&base.Error{Kind: base.ErrNotFound, Op: "tree acme/app@main"}, "github.com", "w", "github:github.com/acme/app#7")
	if !errors.As(withOp, &be) || withOp.Where != "github:github.com/acme/app#7, tree acme/app@main" {
		t.Errorf("where %q", withOp.Where)
	}
}
