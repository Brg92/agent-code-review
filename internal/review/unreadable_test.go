package review

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/forgecontract"
)

// A related change request that cannot be read during collection, though the
// cascade read it, is skipped as not-found: the review goes on and the
// cascade is incomplete. Only the root stops the review.
func TestRelatedChangeRequestThatCannotBeReadIsSkipped(t *testing.T) {
	lib := key(t, libName)
	pk := lib.ProjectKey()
	tests := []struct {
		name string
		fail map[forgecontract.Call]error
	}{
		{"it disappeared", map[forgecontract.Call]error{{Op: forgecontract.OpChangeRequest, Project: pk}: &base.Error{Kind: base.ErrNotFound, Op: "change request"}}},
		{"it became unreadable", map[forgecontract.Call]error{{Op: forgecontract.OpChangeRequest, Project: pk}: &base.Error{Kind: base.ErrForbidden, Op: "change request"}}},
		{"its files are gone", map[forgecontract.Call]error{{Op: forgecontract.OpChangedFiles, Project: pk}: &base.Error{Kind: base.ErrNotFound, Op: "changed files"}}},
		{"its criteria are forbidden", map[forgecontract.Call]error{{Op: forgecontract.OpFilesGet, Project: pk, Ref: "main"}: &base.Error{Kind: base.ErrForbidden, Op: "file"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := fakeCascade(t)
			fake.Fail = tt.fail
			d, work, verbose := deps(t, fake)
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
			last := st.ChangeRequests[len(st.ChangeRequests)-1]
			if st.Phase != state.Collected || len(st.ChangeRequests) != 2 || last.CR != lib || last.Status != core.Skipped || last.SkipReason != core.NotFound {
				t.Errorf("state %s, entries %+v", st.Phase, st.ChangeRequests)
			}
			if !strings.Contains(verbose.String(), "\t"+lib.String()+"\tread the change request\t-\t-\t-\tnot-verified: ") {
				t.Errorf("run.log has no line for it:\n%s", verbose.String())
			}
			// Nothing of it is left in the run, and the root does not list it.
			dirs, _ := filepath.Glob(filepath.Join(work, "runs", run.ID, "acme_lib-*"))
			if len(dirs) != 0 {
				t.Errorf("work packages of the skipped change request: %v", dirs)
			}
			if wp := readWorkPackage(t, run, key(t, appName)); len(wp.RelatedChanges) != 0 {
				t.Errorf("related changes %+v", wp.RelatedChanges)
			}
		})
	}
}

// What a skipped change request lists is not walked: its own related
// changes are not in the run.
func TestSkippedChangeRequestDropsWhatItLists(t *testing.T) {
	fake := fakeCascade(t)
	app, lib := key(t, appName), key(t, libName)
	deep := key(t, "github:github.com/acme/deep#1")
	it := fake.Items[lib]
	it.Description = "## Related Changes\n\n- [ ] acme/deep#1\n"
	fake.Items[lib] = it
	fake.Items[deep] = cascade.Item{Kind: cascade.KindChangeRequest, Title: "Deep", Head: headLib[:39] + "c", Description: ""}
	fake.ChangeRequests[deep] = core.ChangeRequest{Key: deep, Title: "Deep", HeadSHA: headLib[:39] + "c", BaseSHA: baseLib, TargetBranch: "main", State: core.StateOpen}
	fake.FileSets[deep] = nil
	fake.Repos[deep.ProjectKey()+"@main"] = map[string]string{}
	fake.Repos[deep.ProjectKey()+"@"+headLib[:39]+"c"] = map[string]string{}
	fake.CIs[deep.ProjectKey()+"@"+headLib[:39]+"c"] = core.CI{State: core.CINone, Checks: []core.Check{}}
	fake.Fail[forgecontract.Call{Op: forgecontract.OpChangedFiles, Project: lib.ProjectKey()}] = &base.Error{Kind: base.ErrNotFound}
	d, work, _ := deps(t, fake)
	run := collect(t, d, appName)
	var got []string
	for _, e := range run.State.ChangeRequests {
		got = append(got, e.CR.String()+" "+string(e.Status))
	}
	if want := []string{app.String() + " collected", lib.String() + " skipped"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("entries %v, want %v", got, want)
	}
	if dirs, _ := filepath.Glob(filepath.Join(work, "runs", run.ID, "acme_*")); len(dirs) != 1 {
		t.Errorf("work package directories: %v", dirs)
	}
	if _, err := os.Stat(run.Packages[0].File); err != nil {
		t.Error(err)
	}
}

// The count limit is recomputed when a change request drops out: one that
// was over the limit takes its place.
func TestSkippedChangeRequestFreesItsPlaceInTheCount(t *testing.T) {
	fake := fakeCascade(t)
	app, lib := key(t, appName), key(t, libName)
	extra := key(t, "github:github.com/acme/extra#5")
	it := fake.Items[app]
	it.Description = "## Related Changes\n\n- [ ] acme/lib#3\n- [ ] acme/extra#5\n"
	fake.Items[app] = it
	meta := fake.ChangeRequests[app]
	meta.Description = it.Description
	fake.ChangeRequests[app] = meta
	const head = "cccccccccccccccccccccccccccccccccccccccc"
	fake.Items[extra] = cascade.Item{Kind: cascade.KindChangeRequest, Title: "Extra", Head: head}
	fake.ChangeRequests[extra] = core.ChangeRequest{Key: extra, Title: "Extra", HeadSHA: head, BaseSHA: baseLib, TargetBranch: "main", State: core.StateOpen}
	fake.FileSets[extra] = nil
	fake.Repos[extra.ProjectKey()+"@main"] = map[string]string{core.ConfigFile: `{"schema": 1}`}
	fake.Repos[extra.ProjectKey()+"@"+head] = map[string]string{}
	fake.CIs[extra.ProjectKey()+"@"+head] = core.CI{State: core.CINone, Checks: []core.Check{}}
	fake.Repos[app.ProjectKey()+"@main"][core.ConfigFile] = `{"schema": 1, "cascade": {"maxChangeRequests": 2}}`
	fake.Fail[forgecontract.Call{Op: forgecontract.OpChangedFiles, Project: lib.ProjectKey()}] = &base.Error{Kind: base.ErrNotFound}
	d, _, _ := deps(t, fake)
	run := collect(t, d, appName)
	var got []string
	for _, e := range run.State.ChangeRequests {
		got = append(got, e.CR.String()+" "+string(e.Status)+" "+string(e.SkipReason))
	}
	want := []string{app.String() + " collected ", lib.String() + " skipped not-found", extra.String() + " collected "}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("entries %v, want %v", got, want)
	}
}

// A changed file whose content cannot be read is not verified and left out
// of the rules; the review goes on.
func TestContentThatCannotBeReadIsNotVerified(t *testing.T) {
	app := key(t, appName)
	pk := app.ProjectKey()
	// The criteria name the packs, the profile and the dimension, so that
	// nothing reads the repository at head before the changed files.
	config := `{"schema": 1, "packs": ["android"], "profile": "app", "dimensions": {"ui-toolkit": "compose"}}`
	for _, ref := range []struct{ name, ref string }{{"at head", headApp}, {"at base", baseApp}} {
		t.Run(ref.name, func(t *testing.T) {
			fake := fakeCascade(t)
			fake.Repos[pk+"@main"][core.ConfigFile] = config
			fake.Fail[forgecontract.Call{Op: forgecontract.OpFilesGet, Project: pk, Ref: ref.ref}] = &base.Error{Kind: base.ErrTransport, Op: "file", Err: errors.New("status 500")}
			d, _, verbose := deps(t, fake)
			run := collect(t, d, appName)
			wp := readWorkPackage(t, run, app)
			if len(wp.NotVerified) != 1 || wp.NotVerified[0].Path != greeter || wp.NotVerified[0].Reason != "content-unavailable" ||
				wp.NotVerified[0].Detail != "transport, status 500" {
				t.Errorf("not verified %+v", wp.NotVerified)
			}
			if len(wp.Files) != 1 || wp.Files[0].Code != nil || len(wp.Signals) != 0 {
				t.Errorf("the file is in the rules: %+v, %d signals", wp.Files, len(wp.Signals))
			}
			if !strings.Contains(verbose.String(), "\tnot-verified: content-unavailable\n") {
				t.Errorf("run.log:\n%s", verbose.String())
			}
			if len(run.Packages) != 2 {
				t.Errorf("%d packages", len(run.Packages))
			}
		})
	}
}
