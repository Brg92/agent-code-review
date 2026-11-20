package review

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
)

func TestCollectCascadeUpToCollected(t *testing.T) {
	d, work, _ := deps(t, fakeCascade(t))
	run := collect(t, d, appName)

	st, err := state.Load(run.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Phase != state.Collected || st.RunID != run.ID || st.Root != key(t, appName) {
		t.Errorf("state: phase %s, run %s, root %s", st.Phase, st.RunID, st.Root)
	}
	if run.Dir != filepath.Join(work, "runs", run.ID) {
		t.Errorf("run dir %s", run.Dir)
	}
	var got []string
	for _, e := range st.ChangeRequests {
		got = append(got, e.CR.String()+" "+string(e.Status)+" "+e.HeadSHA[:8])
	}
	want := []string{appName + " collected aaaaaaaa", libName + " collected bbbbbbbb"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries %v, want %v", got, want)
	}
	// Related first, root last.
	if len(run.Packages) != 2 || run.Packages[0].CR != key(t, libName) || run.Packages[1].CR != key(t, appName) {
		t.Errorf("packages %+v", run.Packages)
	}
	for _, p := range run.Packages {
		if _, err := os.Stat(p.File); err != nil {
			t.Error(err)
		}
		if filepath.Dir(p.File) != p.Dir || filepath.Base(p.File) != state.WorkPackageFile || p.Parts != 1 {
			t.Errorf("package %+v", p)
		}
	}

	app := readWorkPackage(t, run, key(t, appName))
	lib := readWorkPackage(t, run, key(t, libName))
	if app.RunID != run.ID || app.HeadSHA != headApp || app.BaseRef != "main" || app.URL != "https://github.com/acme/app/pull/7" {
		t.Errorf("app work package header: %+v", app)
	}
	if app.CI.State != core.CIPassing || lib.CI.State != core.CINone {
		t.Errorf("ci: %+v, %+v", app.CI, lib.CI)
	}
	if len(app.Cascade.Ancestors) != 0 || len(lib.Cascade.Ancestors) != 1 || lib.Cascade.Ancestors[0].CR != key(t, appName) {
		t.Errorf("ancestors: %+v, %+v", app.Cascade, lib.Cascade)
	}
	if len(app.RelatedChanges) != 1 || app.RelatedChanges[0].CR != key(t, libName) ||
		app.RelatedChanges[0].WorkPackage != "acme_lib-3-bbbbbbbb/workpackage.json" {
		t.Errorf("related of app: %+v", app.RelatedChanges)
	}
	if app.JudgementPath != "acme_app-7-aaaaaaaa/judgement.json" {
		t.Errorf("judgement path %s", app.JudgementPath)
	}
	if lib.Criteria.Profile != "library" || lib.Criteria.ProfileReason == "" || app.Criteria.Profile != "app" {
		t.Errorf("profiles: app %q, lib %q (%s)", app.Criteria.Profile, lib.Criteria.Profile, lib.Criteria.ProfileReason)
	}
	if !reflect.DeepEqual(app.Criteria.Packs, []string{"android"}) || app.Criteria.Source != core.SourceDefault {
		t.Errorf("criteria: %+v", app.Criteria)
	}
}

func TestCollectCleansOldRunsFirst(t *testing.T) {
	d, work, _ := deps(t, fakeCascade(t))
	old := filepath.Join(work, "runs", "20260101T000000Z-deadbeef")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(old, now.Add(-30*24*time.Hour), now.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	run := collect(t, d, appName)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("the old run still exists: %v", err)
	}
	if _, err := os.Stat(run.Dir); err != nil {
		t.Error(err)
	}
}

// TestCollectWritesWorkPackagesThatMatchTheContract: every work package
// on disk validates against the schema and decodes back.
func TestCollectWorkPackagesMatchTheContract(t *testing.T) {
	schemaData, err := os.ReadFile(filepath.Join("..", "..", "schemas", "workpackage.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := jsonschema.Compile(schemaData)
	if err != nil {
		t.Fatal(err)
	}
	d, _, _ := deps(t, fakeCascade(t))
	run := collect(t, d, appName)
	for _, p := range run.Packages {
		data, err := os.ReadFile(p.File)
		if err != nil {
			t.Fatal(err)
		}
		issues, err := schema.Validate(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range issues {
			t.Errorf("%s: %s", p.CR, i)
		}
	}
}

func TestCollectKeepsTheWorkPackagesPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix permission bits")
	}
	d, _, _ := deps(t, fakeCascade(t))
	run := collect(t, d, appName)
	for _, p := range run.Packages {
		for _, path := range []string{p.File, p.Dir, run.Dir} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&0o077 != 0 {
				t.Errorf("%s is %v", path, info.Mode().Perm())
			}
		}
	}
}
