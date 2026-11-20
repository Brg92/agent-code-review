package review

import (
	"bytes"
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/forgecontract"
)

// now is the time of every test; run ids sort by it.
var now = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

const (
	headApp  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	headLib  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	baseApp  = "1111111111111111111111111111111111111111"
	baseLib  = "2222222222222222222222222222222222222222"
	greeter  = "app/src/main/kotlin/acme/Greeter.kt"
	greeterT = "app/src/test/kotlin/acme/GreeterTest.kt"
	loader   = "lib/src/main/kotlin/acme/Loader.kt"
)

func key(t testing.TB, s string) core.Key {
	t.Helper()
	k, err := core.ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

var (
	appName = "github:github.com/acme/app#7"
	libName = "github:github.com/acme/lib#3"
)

// loadPacks loads the built-in packs.
func loadPacks(t testing.TB) *packs.Set {
	t.Helper()
	set, err := packs.Load(filepath.Join("..", "..", "packs"))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func kotlinFile(name string, n int) string {
	var b strings.Builder
	b.WriteString("package acme\n\n")
	for i := 3; i <= n; i++ {
		b.WriteString("fun " + name + strings.Repeat("x", i) + "() = " + string(rune('0'+i%10)) + "\n")
	}
	return b.String()
}

// fakeCascade is a root change request, acme/app#7, that lists acme/lib#3
// as a related change, each with a repository that has one changed source
// and its test.
func fakeCascade(t testing.TB) *forgecontract.Fake {
	t.Helper()
	app, lib := key(t, appName), key(t, libName)
	success := "success"
	f := &forgecontract.Fake{
		Caps: base.Capabilities{Search: base.SearchDefaultBranch, TreeMayTruncate: true, CI: base.CISources{CheckRuns: true}},
		Items: map[core.Key]cascade.Item{
			app: {Kind: cascade.KindChangeRequest, Title: "Greet by name", Head: headApp, Description: "Greets.\n\n## Related Changes\n\n- [ ] acme/lib#3\n"},
			lib: {Kind: cascade.KindChangeRequest, Title: "Load greetings", Head: headLib, Description: "Loads."},
		},
		ChangeRequests: map[core.Key]core.ChangeRequest{
			app: {Key: app, Title: "Greet by name", Description: "Greets.\n\n## Related Changes\n\n- [ ] acme/lib#3\n", HeadSHA: headApp, BaseSHA: baseApp, TargetBranch: "main", Author: "dev", State: core.StateOpen},
			lib: {Key: lib, Title: "Load greetings", Description: "Loads.", HeadSHA: headLib, BaseSHA: baseLib, TargetBranch: "main", Author: "dev", State: core.StateOpen},
		},
		FileSets: map[core.Key][]core.FileChange{
			app: {{Path: greeter, Status: core.FileModified, Patch: "@@ -1,3 +1,4 @@\n package acme\n \n fun greet() = 1\n+fun greetName() = 2"}},
			lib: {{Path: loader, Status: core.FileModified, Patch: "@@ -1,3 +1,4 @@\n package acme\n \n fun load() = 1\n+fun loadAll() = 2"}},
		},
		DefaultBranch: map[string]string{app.ProjectKey(): "main", lib.ProjectKey(): "main"},
		Repos: map[string]map[string]string{
			app.ProjectKey() + "@main": {},
			lib.ProjectKey() + "@main": {},
			app.ProjectKey() + "@" + headApp: {
				"app/build.gradle.kts": "plugins {\n    id(\"com.android.application\")\n}\n",
				greeter:                "package acme\n\nfun greet() = 1\nfun greetName() = 2\n",
				greeterT:               "package acme\n\nclass GreeterTest\n",
			},
			app.ProjectKey() + "@" + baseApp: {greeter: "package acme\n\nfun greet() = 1\n"},
			lib.ProjectKey() + "@" + headLib: {
				"lib/build.gradle.kts":                   "plugins {\n    id(\"com.android.library\")\n}\n",
				loader:                                   "package acme\n\nfun load() = 1\nfun loadAll() = 2\n",
				"lib/src/test/kotlin/acme/LoaderTest.kt": "package acme\n\nclass LoaderTest\n",
			},
			lib.ProjectKey() + "@" + baseLib: {loader: "package acme\n\nfun load() = 1\n"},
		},
		CIs: map[string]core.CI{
			app.ProjectKey() + "@" + headApp: {State: core.CIPassing, Checks: []core.Check{{Name: "build", Status: "completed", Conclusion: &success}}},
			lib.ProjectKey() + "@" + headLib: {State: core.CINone, Checks: []core.Check{}},
		},
		Fail: map[forgecontract.Call]error{},
	}
	return f
}

// deps are the dependencies of a review on reader, in a fresh work directory.
func deps(t testing.TB, reader base.Reader) (Deps, string, *bytes.Buffer) {
	t.Helper()
	work := t.TempDir()
	var verbose bytes.Buffer
	return Deps{
		Reader:     reader,
		Registries: []refs.Registry{github.Registry{}},
		Packs:      loadPacks(t),
		WorkDir:    work,
		KeepDays:   state.DefaultKeepDays,
		Now:        func() time.Time { return now },
		Random:     rand.New(rand.NewSource(1)),
		Log:        runlog.New(func() time.Time { return now }, &verbose),
	}, work, &verbose
}

func collect(t testing.TB, d Deps, root string) *Run {
	t.Helper()
	run, err := Collect(context.Background(), d, key(t, root))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return run
}

func readWorkPackage(t testing.TB, run *Run, k core.Key) core.WorkPackage {
	t.Helper()
	for _, p := range run.Packages {
		if p.CR.Same(k) {
			data, err := os.ReadFile(p.File)
			if err != nil {
				t.Fatal(err)
			}
			wp, err := core.DecodeWorkPackage(data)
			if err != nil {
				t.Fatal(err)
			}
			return wp
		}
	}
	t.Fatalf("no work package for %s", k)
	return core.WorkPackage{}
}
