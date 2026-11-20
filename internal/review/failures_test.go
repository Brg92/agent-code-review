package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/forgecontract"
)

func failure(t *testing.T, err error) *Failure {
	t.Helper()
	var f *Failure
	if !errors.As(err, &f) {
		t.Fatalf("error = %v, want a *Failure", err)
	}
	return f
}

func TestCollectRootNotFound(t *testing.T) {
	fake := fakeCascade(t)
	delete(fake.ChangeRequests, key(t, appName))
	d, work, _ := deps(t, fake)
	_, err := Collect(context.Background(), d, key(t, appName))
	f := failure(t, err)
	if f.Kind != KindPlatform || !strings.Contains(f.What, "root change request "+appName) || !strings.Contains(f.Where, appName) ||
		!strings.Contains(f.Why, "not-found") || f.Hint == "" {
		t.Errorf("failure %+v", f)
	}
	// The run log is where the user looks.
	if f.LogPath == "" || filepath.Base(f.LogPath) != runlog.FileName || !strings.HasPrefix(f.LogPath, work) {
		t.Errorf("log path %q", f.LogPath)
	}
	if _, err := os.Stat(f.LogPath); err != nil {
		t.Error(err)
	}
}

func TestCollectRootIsNotAChangeRequest(t *testing.T) {
	tests := []struct {
		name string
		kind cascade.Kind
		want Kind
	}{
		{"an issue", cascade.KindIssue, KindUsage},
		{"nothing", cascade.KindNotFound, KindPlatform},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := fakeCascade(t)
			fake.Items[key(t, appName)] = cascade.Item{Kind: tt.kind}
			d, _, _ := deps(t, fake)
			_, err := Collect(context.Background(), d, key(t, appName))
			if f := failure(t, err); f.Kind != tt.want || !strings.Contains(f.What, "cascade") {
				t.Errorf("failure %+v", f)
			}
		})
	}
}

func TestCollectRelatedNotFoundIsIncompleteNotAnError(t *testing.T) {
	fake := fakeCascade(t)
	fake.Items[key(t, libName)] = cascade.Item{Kind: cascade.KindNotFound}
	d, _, _ := deps(t, fake)
	run := collect(t, d, appName)
	if !run.Cascade.Incomplete() {
		t.Error("the cascade is not incomplete")
	}
	if len(run.Packages) != 1 || run.Packages[0].CR != key(t, appName) {
		t.Errorf("packages %+v", run.Packages)
	}
	st, err := state.Load(run.Dir)
	if err != nil {
		t.Fatal(err)
	}
	last := st.ChangeRequests[1]
	if st.Phase != state.Collected || last.Status != core.Skipped || last.SkipReason != core.NotFound {
		t.Errorf("state %s, last entry %+v", st.Phase, last)
	}
}

func TestCollectSkipsWhatIsNotReviewed(t *testing.T) {
	fake := fakeCascade(t)
	app := key(t, appName)
	it := fake.Items[app]
	it.Description = "## Related Changes\n\n- [ ] acme/lib#3\n- [x] acme/done#1\n- [ ] https://gitlab.com/group/project/-/merge_requests/4\n- [ ] #7\n- [ ] acme/lib#3\n"
	fake.Items[app] = it
	meta := fake.ChangeRequests[app]
	meta.Description = it.Description
	fake.ChangeRequests[app] = meta
	d, _, _ := deps(t, fake)
	d.Registries = append(d.Registries, gitlab.Registry{})
	run := collect(t, d, appName)
	got := map[core.SkipReason]int{}
	for _, e := range run.State.ChangeRequests {
		if e.Status == core.Skipped {
			got[e.SkipReason]++
		}
	}
	want := map[core.SkipReason]int{core.AlreadyDone: 1, core.OtherPlatform: 1, core.SelfReference: 1, core.Cycle: 1}
	for reason, n := range want {
		if got[reason] != n {
			t.Errorf("skipped %v, want %v", got, want)
			break
		}
	}
}

func TestCollectStopsOnAnUnsupportedPlatform(t *testing.T) {
	d, _, _ := deps(t, fakeCascade(t))
	gl := key(t, "gitlab:gitlab.com/group/project#4")
	_, err := Collect(context.Background(), d, gl)
	if f := failure(t, err); f.Kind != KindUsage || !strings.Contains(f.Why, "gitlab") {
		t.Errorf("failure %+v", f)
	}
}

func TestCollectFailureKinds(t *testing.T) {
	app := key(t, appName)
	pk := app.ProjectKey()
	tests := []struct {
		name     string
		fail     map[forgecontract.Call]error
		want     Kind
		why, fix string
	}{
		{"no token", map[forgecontract.Call]error{{Op: forgecontract.OpChangeRequest, Project: pk}: &base.Error{Kind: base.ErrForbidden, Op: "change request", Err: auth.ErrNoToken}},
			KindAuth, "no token", "agent-code-review auth login"},
		{"a permission is missing", map[forgecontract.Call]error{{Op: forgecontract.OpChangedFiles, Project: pk}: &base.Error{Kind: base.ErrForbidden, Op: "changed files", Permissions: []string{"pull_requests: read"}}},
			KindAuth, "forbidden (needs pull_requests: read)", "the token lacks pull_requests: read; add it to the fine-grained token"},
		{"the token is refused", map[forgecontract.Call]error{{Op: forgecontract.OpChangeRequest, Project: pk}: &base.Error{Kind: base.ErrForbidden, Op: "change request", Err: errors.New("status 401: Bad credentials")}},
			KindAuth, "forbidden, status 401", "agent-code-review auth login"},
		{"rate limit", map[forgecontract.Call]error{{Op: forgecontract.OpChangeRequest, Project: pk}: &base.Error{Kind: base.ErrRateLimited, Op: "change request", Err: errors.New("status 403; retry after 2026-11-20T13:00:00Z")}},
			KindPlatform, "rate-limited, status 403", "run the command again later"},
		{"server error", map[forgecontract.Call]error{{Op: forgecontract.OpChangeRequest, Project: pk}: &base.Error{Kind: base.ErrTransport, Op: "change request", Err: errors.New("status 502: bad gateway")}},
			KindPlatform, "transport, status 502", "check the network"},
		{"too many files", map[forgecontract.Call]error{{Op: forgecontract.OpChangedFiles, Project: pk}: &base.Error{Kind: base.ErrListTruncated, Op: "changed files"}},
			KindPlatform, "list-truncated", "split it"},
		{"a file cannot be read", map[forgecontract.Call]error{{Op: forgecontract.OpFilesGet, Project: pk, Ref: headApp}: &base.Error{Kind: base.ErrTransport, Op: "file", Err: errors.New("status 500")}},
			KindPlatform, "transport, status 500", "check the network"},
		{"a related change request cannot be read", map[forgecontract.Call]error{{Op: forgecontract.OpChangeRequest, Project: key(t, libName).ProjectKey()}: &base.Error{Kind: base.ErrNotFound, Op: "change request"}},
			KindPlatform, "not-found", "check the reference"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := fakeCascade(t)
			fake.Fail = tt.fail
			d, _, _ := deps(t, fake)
			_, err := Collect(context.Background(), d, app)
			f := failure(t, err)
			if f.Kind != tt.want || f.Why != tt.why || !strings.Contains(f.Hint, tt.fix) || f.LogPath == "" {
				t.Errorf("failure %+v\nwant kind %d, why %q, hint with %q", f, tt.want, tt.why, tt.fix)
			}
			if !errors.Is(f, f.Err) || f.Error() == "" {
				t.Error("the failure does not wrap its cause")
			}
		})
	}
}

// TestReadsTheCoreTurnsIntoNotVerifiedDoNotStopTheReview: a tree that is
// cut short and checks that cannot be read are listed, and the review goes
// on.
func TestReadsTheCoreTurnsIntoNotVerifiedDoNotStopTheReview(t *testing.T) {
	fake := fakeCascade(t)
	lib := key(t, libName)
	fake.Fail[forgecontract.Call{Op: forgecontract.OpTree, Project: lib.ProjectKey(), Ref: headLib}] = &base.Error{Kind: base.ErrTreeTruncated, Op: "tree"}
	fake.Repos[lib.ProjectKey()+"@main"][core.ConfigFile] = `{"schema": 1, "packs": ["android"]}`
	delete(fake.CIs, lib.ProjectKey()+"@"+headLib)
	d, _, verbose := deps(t, fake)
	run := collect(t, d, appName)
	wp := readWorkPackage(t, run, lib)
	if wp.CI.State != core.CIUnknown || wp.CI.Reason != "error" {
		t.Errorf("ci %+v", wp.CI)
	}
	var reasons []string
	for _, n := range wp.NotVerified {
		reasons = append(reasons, n.Reason)
	}
	if !contains(reasons, "tree-unavailable") {
		t.Errorf("not verified %v, want tree-unavailable", wp.NotVerified)
	}
	log := verbose.String()
	for _, want := range []string{"\tnot-verified: tree-truncated\n", "\tnot-verified: error\n", "\tnot-verified: tree-unavailable\n"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log has no %q:\n%s", want, log)
		}
	}
}

// TestPacksDetectedFromChangedFilesOnlyWhenTheTreeFails: without the tree
// no pack is detected, and the files say why they are not reviewed.
func TestAnUnreadableTreeIsNamedWhenNoPackIsDetected(t *testing.T) {
	fake := fakeCascade(t)
	lib := key(t, libName)
	fake.Fail[forgecontract.Call{Op: forgecontract.OpTree, Project: lib.ProjectKey(), Ref: headLib}] = &base.Error{Kind: base.ErrTreeTruncated, Op: "tree"}
	d, _, _ := deps(t, fake)
	run := collect(t, d, appName)
	wp := readWorkPackage(t, run, lib)
	if len(wp.NotVerified) != 1 || wp.NotVerified[0].Reason != "unsupported-language" ||
		!strings.Contains(wp.NotVerified[0].Detail, "tree could not be read (tree-truncated)") {
		t.Errorf("not verified %+v", wp.NotVerified)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestInvalidRepositoryConfigurationIsIgnoredNotFatal(t *testing.T) {
	fake := fakeCascade(t)
	app := key(t, appName)
	fake.Repos[app.ProjectKey()+"@main"][core.ConfigFile] = `{"schema": 2, "profile": 7}`
	d, _, verbose := deps(t, fake)
	run := collect(t, d, appName)
	wp := readWorkPackage(t, run, app)
	if wp.Criteria.Source != core.SourceDefault || len(wp.Criteria.Ignored) != 1 ||
		wp.Criteria.Ignored[0].Item != "`"+core.ConfigFile+"`" || !strings.Contains(wp.Criteria.Ignored[0].Reason, "/profile") {
		t.Errorf("criteria %+v", wp.Criteria)
	}
	if !strings.Contains(verbose.String(), "not-verified: criteria-invalid") {
		t.Errorf("log:\n%s", verbose.String())
	}
}

func TestCriteriaPackNotLoadedIsIgnored(t *testing.T) {
	fake := fakeCascade(t)
	app := key(t, appName)
	fake.Repos[app.ProjectKey()+"@main"][core.ConfigFile] = `{"schema": 1, "packs": ["android", "no-such-pack"], "profile": "library"}`
	d, _, _ := deps(t, fake)
	run := collect(t, d, appName)
	wp := readWorkPackage(t, run, app)
	if len(wp.Criteria.Packs) != 1 || wp.Criteria.Packs[0] != "android" || wp.Criteria.Profile != "library" ||
		len(wp.Criteria.Ignored) != 1 || !strings.Contains(wp.Criteria.Ignored[0].Item, "no-such-pack") {
		t.Errorf("criteria %+v", wp.Criteria)
	}
}

func TestCriteriaSetTheCascadeLimits(t *testing.T) {
	fake := fakeCascade(t)
	app := key(t, appName)
	fake.Repos[app.ProjectKey()+"@main"][core.ConfigFile] = `{"schema": 1, "cascade": {"maxChangeRequests": 1}}`
	d, _, _ := deps(t, fake)
	run := collect(t, d, appName)
	if run.State.Cascade.MaxChangeRequests != 1 || len(run.Packages) != 1 {
		t.Errorf("limits %+v, %d packages", run.State.Cascade, len(run.Packages))
	}
	if e := run.State.ChangeRequests[1]; e.SkipReason != core.OverCountLimit {
		t.Errorf("second entry %+v", e)
	}
}

func TestUnchangedFilesAreNotCollected(t *testing.T) {
	fake := fakeCascade(t)
	app := key(t, appName)
	fake.FileSets[app] = append(fake.FileSets[app], core.FileChange{Path: "app/docs.md", Status: core.FileUnchanged})
	d, _, _ := deps(t, fake)
	run := collect(t, d, appName)
	wp := readWorkPackage(t, run, app)
	for _, f := range wp.Files {
		if f.Path == "app/docs.md" {
			t.Error("an unchanged file is in the work package")
		}
	}
	for _, n := range wp.NotVerified {
		if n.Path == "app/docs.md" {
			t.Error("an unchanged file is listed as not verified")
		}
	}
}

func TestFilesThatCannotBeReviewed(t *testing.T) {
	fake := fakeCascade(t)
	app := key(t, appName)
	fake.FileSets[app] = append(fake.FileSets[app],
		core.FileChange{Path: "app/src/main/res/logo.png", Status: core.FileAdded, Missing: core.PatchBinary, Binary: true},
		core.FileChange{Path: "app/src/main/kotlin/acme/Big.kt", Status: core.FileModified, Missing: core.PatchTooLarge},
		core.FileChange{Path: "app/src/main/kotlin/acme/Names.kt", OldPath: "app/src/main/kotlin/acme/Name.kt", Status: core.FileRenamed},
		core.FileChange{Path: "Package.swift", Status: core.FileAdded, Patch: "@@ -0,0 +1 @@\n+// swift"},
		core.FileChange{Path: "app/src/main/kotlin/acme/Broken.kt", Status: core.FileModified, Patch: "this is not a patch"},
	)
	d, _, _ := deps(t, fake)
	run := collect(t, d, appName)
	wp := readWorkPackage(t, run, app)
	got := map[string]string{}
	for _, n := range wp.NotVerified {
		got[n.Path] = n.Reason
	}
	want := map[string]string{
		"app/src/main/res/logo.png":          "binary",
		"app/src/main/kotlin/acme/Big.kt":    "patch-unavailable",
		"Package.swift":                      "unsupported-language",
		"app/src/main/kotlin/acme/Broken.kt": "patch-unavailable",
	}
	for p, reason := range want {
		if got[p] != reason {
			t.Errorf("%s: %q, want %q (all: %v)", p, got[p], reason, got)
		}
	}
	if _, ok := got["app/src/main/kotlin/acme/Names.kt"]; ok {
		t.Error("a rename without changes is not unverified: it is in the summary")
	}
}

func TestAPathWithAControlCharacterIsNotReviewed(t *testing.T) {
	fake := fakeCascade(t)
	app := key(t, appName)
	fake.FileSets[app] = append(fake.FileSets[app], core.FileChange{Path: "app/evil\nname.kt", Status: core.FileAdded, Patch: "@@ -0,0 +1 @@\n+x"})
	d, _, _ := deps(t, fake)
	_, err := Collect(context.Background(), d, app)
	if f := failure(t, err); f.Kind != KindPlatform || !strings.Contains(f.Hint, "control character") || !strings.Contains(f.Where, appName) {
		t.Errorf("failure %+v", f)
	}
}
