package review

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
	"github.com/Brg92/agent-code-review/internal/transport"
)

// canary is the token of the replay tests: nothing the review writes or
// prints may hold it.
const canary = "ghp_CANARYtoken0123456789abcdefABCDEF"

type canaryTokens struct{}

func (canaryTokens) Resolve(ctx context.Context, host string) (auth.Token, error) {
	return auth.Resolver{Getenv: func(string) string { return canary }}.Resolve(ctx, host)
}

// fakeClock advances instead of sleeping.
type fakeClock struct{ at time.Time }

func (c *fakeClock) Now() time.Time { return c.at }
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.at = c.at.Add(d)
	return ctx.Err()
}

// replayDeps returns the dependencies of a review that reads the recordings
// of a scenario through the GitHub provider, with the transport wired to
// the run log.
func replayDeps(t testing.TB, scenarios ...string) (Deps, string, *sandbox.Replay, *verboseLog) {
	t.Helper()
	r := sandbox.NewReplay(t, scenarios[0])
	for _, extra := range scenarios[1:] {
		addRecordings(t, r, extra)
	}
	// The repository endpoint is the one response the recordings lack: the
	// default branch of each sandbox repository.
	for _, repo := range []string{"sample-app", "core-lib", "kmp-shared", "feature-lib"} {
		r.Add(sandbox.Route{Path: "/repos/" + sandbox.Owner + "/" + repo, Status: 200, Body: `{"default_branch":"main"}`})
	}
	verbose := &verboseLog{}
	clock := &fakeClock{at: now}
	log := runlog.New(clock.Now, verbose)
	reader, err := github.NewLoopback("github.com", r.URL(), canaryTokens{}, github.Options{
		Transport: transport.Config{Now: clock.Now, Sleep: clock.Sleep, Observe: log.Observe},
	})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	return Deps{
		Reader:     reader,
		Registries: []refs.Registry{github.Registry{}},
		Packs:      loadPacks(t),
		WorkDir:    work,
		KeepDays:   state.DefaultKeepDays,
		Now:        clock.Now,
		Random:     rand.New(rand.NewSource(1)),
		Log:        log,
	}, work, r, verbose
}

// addRecordings makes the recordings of another scenario answer on r: a
// cascade walks into the pull requests of several scenarios, as S2 reaches
// the one of S4.
func addRecordings(t testing.TB, r *sandbox.Replay, scenario string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "github", scenario, "GET_*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no recordings of %s: %v", scenario, err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var rec struct {
			Path    string            `json:"path"`
			Query   map[string]string `json:"query"`
			Status  int               `json:"status"`
			Headers map[string]string `json:"headers"`
			Body    json.RawMessage   `json:"body"`
		}
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		r.Add(sandbox.Route{Path: rec.Path, Query: rec.Query, Status: rec.Status, Header: rec.Headers, Body: string(rec.Body)})
	}
}

// buildFiles are synthetic build files of the sandbox repositories: their
// trees list them but the recordings hold no contents, and the packs read
// them to be activated and to settle the profile and the dimensions.
var buildFiles = map[string][2]string{
	"sample-app":  {"app/build.gradle.kts", "plugins { id(\"com.android.application\") }\ndependencies { implementation(\"androidx.compose.ui:ui\") }\n"},
	"core-lib":    {"build.gradle.kts", "plugins {\n    id(\"com.android.library\")\n    id(\"maven-publish\")\n}\n"},
	"feature-lib": {"build.gradle.kts", "plugins { id(\"com.android.library\") }\nandroid { buildFeatures { viewBinding = true } }\n"},
	"kmp-shared":  {"shared/build.gradle.kts", "plugins {\n    kotlin(\"multiplatform\")\n    id(\"com.android.library\")\n}\n"},
}

// addBuildFiles answers the reads of the build files of repo at the head of
// pull request number of a scenario: the main one with its synthetic
// contents, any other file the packs read with neutral ones.
func addBuildFiles(t testing.TB, r *sandbox.Replay, scenario, repo string, number int) {
	t.Helper()
	head := sandbox.PullRequest(t, scenario, repo, number).HeadSHA
	for _, p := range sandbox.TreeOf(t, scenario, repo, head).Paths {
		var content string
		switch {
		case p == buildFiles[repo][0]:
			content = buildFiles[repo][1]
		case strings.HasSuffix(p, "build.gradle.kts") || strings.HasSuffix(p, "build.gradle"):
			content = "// synthetic build file\n"
		case strings.HasSuffix(p, "src/main/AndroidManifest.xml") || p == "gradle/libs.versions.toml":
			content = "<manifest/>\n"
		default:
			continue
		}
		body, err := json.Marshal(map[string]any{
			"type": "file", "encoding": "base64", "name": filepath.Base(p), "path": p, "sha": "0000000000000000000000000000000000000000",
			"content": base64.StdEncoding.EncodeToString([]byte(content)),
		})
		if err != nil {
			t.Fatal(err)
		}
		r.Add(sandbox.Route{Path: "/repos/" + sandbox.Owner + "/" + repo + "/contents/" + p, Query: map[string]string{"ref": head}, Status: 200, Body: string(body)})
	}
}

// addNotFound answers 404 for a path of a sandbox repository that the
// recorder did not ask for because the repository does not have it, as
// GitHub does: the references directory of a legacy skill, for example.
func addNotFound(r *sandbox.Replay, repo, path, ref string) {
	r.Add(sandbox.Route{Path: "/repos/" + sandbox.Owner + "/" + repo + "/contents/" + path, Query: map[string]string{"ref": ref}, Status: 404,
		Body: `{"message":"Not Found","status":"404"}`})
}

// verboseLog collects what the log writes as it happens.
type verboseLog struct{ b []byte }

func (v *verboseLog) Write(p []byte) (int, error) { v.b = append(v.b, p...); return len(p), nil }
func (v *verboseLog) String() string              { return string(v.b) }

func sandboxKey(t testing.TB, repo string, n int) core.Key {
	t.Helper()
	return key(t, "github:github.com/"+sandbox.Owner+"/"+repo+"#"+strconv.Itoa(n))
}

// s2Deps are the dependencies of a review of the S2 cascade, which walks
// into S4, and the pull requests whose repositories it reads.
func s2Deps(t testing.TB) (Deps, string, *verboseLog) {
	t.Helper()
	d, work, r, log := replayDeps(t, "S2", "S4")
	addBuildFiles(t, r, "S2", "sample-app", 7)
	addBuildFiles(t, r, "S2", "sample-app", 2)
	addBuildFiles(t, r, "S2", "core-lib", 3)
	addBuildFiles(t, r, "S2", "kmp-shared", 2)
	addBuildFiles(t, r, "S4", "feature-lib", 1)
	addNotFound(r, "core-lib", ".github/skills/core-lib-code-review/references", "main")
	return d, work, log
}

func shortKeys(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = strings.TrimPrefix(k, "github:github.com/"+sandbox.Owner+"/")
	}
	return out
}

func TestCollectS2Cascade(t *testing.T) {
	d, _, log := s2Deps(t)
	run, err := Collect(context.Background(), d, sandboxKey(t, "sample-app", 7))
	if err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	st, err := state.Load(run.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Phase != state.Collected {
		t.Errorf("phase %s", st.Phase)
	}
	var entries []string
	for _, e := range st.ChangeRequests {
		entries = append(entries, strings.TrimPrefix(e.CR.String(), "github:github.com/"+sandbox.Owner+"/")+" d"+strconv.Itoa(e.Depth)+" "+string(e.Status)+" "+string(e.SkipReason))
	}
	want := []string{
		"sample-app#7 d0 collected ", "core-lib#3 d1 collected ", "kmp-shared#2 d2 collected ", "feature-lib#1 d3 collected ",
		"sample-app#7 d2 skipped cycle", "feature-lib#2 d1 skipped already-done", "sample-app#2 d1 collected ",
		"sample-app#1 d1 skipped not-a-change-request", "sample-app#7 d1 skipped self-reference", "core-lib#9999 d1 skipped not-found",
	}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("entries:\n%s\nwant:\n%s", strings.Join(entries, "\n"), strings.Join(want, "\n"))
	}
	var order []string
	for _, p := range run.Packages {
		order = append(order, p.CR.String())
	}
	if want := []string{"feature-lib#1", "kmp-shared#2", "core-lib#3", "sample-app#2", "sample-app#7"}; !reflect.DeepEqual(shortKeys(order), want) {
		t.Errorf("review order %v, want %v", shortKeys(order), want)
	}
	if !run.Cascade.Incomplete() {
		t.Error("a change request that does not exist leaves the cascade incomplete")
	}
	deepest := readWorkPackage(t, run, sandboxKey(t, "feature-lib", 1))
	var chain []string
	for _, a := range deepest.Cascade.Ancestors {
		chain = append(chain, a.CR.String())
	}
	if want := []string{"sample-app#7", "core-lib#3", "kmp-shared#2"}; !reflect.DeepEqual(shortKeys(chain), want) {
		t.Errorf("ancestors of feature-lib#1: %v, want %v", shortKeys(chain), want)
	}
	root := readWorkPackage(t, run, sandboxKey(t, "sample-app", 7))
	if len(root.RelatedChanges) != 4 || len(root.Cascade.Ancestors) != 0 {
		t.Errorf("root: %d related, %d ancestors", len(root.RelatedChanges), len(root.Cascade.Ancestors))
	}
}

// TestNothingTheReviewWritesHoldsTheToken: the token reaches the platform
// and nothing else.
func TestNothingTheReviewWritesHoldsTheToken(t *testing.T) {
	d, work, log := s2Deps(t)
	if _, err := Collect(context.Background(), d, sandboxKey(t, "sample-app", 7)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log.String(), canary) || log.String() == "" {
		t.Errorf("verbose log:\n%s", log)
	}
	err := filepath.WalkDir(work, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), canary) {
			t.Errorf("%s holds the token", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRunLogOfAReview: one line per request, with the status, the
// attempts and the outcome, and no query.
func TestRunLogOfAReview(t *testing.T) {
	d, _, log := s2Deps(t)
	run, err := Collect(context.Background(), d, sandboxKey(t, "sample-app", 7))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(run.Dir, runlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != log.String() {
		t.Error("the verbose output differs from run.log")
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 50 {
		t.Fatalf("%d lines", len(lines))
	}
	for _, l := range lines {
		f := strings.Split(l, "\t")
		if len(f) != 8 || f[1] != "review" || strings.Contains(l, "?") || strings.Contains(l, "ref=") {
			t.Fatalf("line %q", l)
		}
	}
	for _, want := range []string{
		"\tGET /repos/acr-plugin-sandbox/sample-app/pulls/7/files\t200\t0ms\t1\tok",
		"\tGET /repos/acr-plugin-sandbox/sample-app/pulls/1\t404\t0ms\t1\terror",
		"\tnot-verified: search-unavailable",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("no line with %q", want)
		}
	}
}

// scenarioReplays are the sandbox pull requests whose work packages the
// review is checked on: the root of a replay of one scenario.
var scenarioReplays = []struct {
	name, scenario, repo string
	number               int
	check                func(t *testing.T, wp core.WorkPackage)
}{
	{"S1", "S1", "sample-app", 2, func(t *testing.T, wp core.WorkPackage) {
		expect(t, wp.CI.State == core.CIPassing && wp.Criteria.Source == core.SourceOverlay && wp.Criteria.Profile == "app" && len(wp.Signals) > 0,
			"ci %s, source %s, profile %s, %d signals", wp.CI.State, wp.Criteria.Source, wp.Criteria.Profile, len(wp.Signals))
	}},
	{"S5 A", "S5", "sample-app", 3, func(t *testing.T, wp core.WorkPackage) {
		expect(t, wp.CI.State == core.CIFailing && len(wp.CI.Checks) > 0, "ci %+v", wp.CI)
		expect(t, hasNotVerified(wp, ".github/workflows/checks.yml", "unsupported-language"), "not verified %v", wp.NotVerified)
	}},
	{"S5 B", "S5", "sample-app", 4, func(t *testing.T, wp core.WorkPackage) {
		expect(t, wp.CI.State == core.CINone && len(wp.CI.Checks) == 0, "ci %+v", wp.CI)
	}},
	{"S6 A", "S6", "sample-app", 5, func(t *testing.T, wp core.WorkPackage) {
		expect(t, wp.Criteria.Source == core.SourceOverlay && len(wp.Criteria.Ignored) == 0, "criteria %+v", wp.Criteria)
		expect(t, hasNotVerified(wp, core.ConfigFile, "unsupported-language"), "the edited criteria file is a changed file: %v", wp.NotVerified)
	}},
	{"S6 B", "S6", "sample-app", 6, func(t *testing.T, wp core.WorkPackage) {
		expect(t, wp.Criteria.Source == core.SourceDefault && len(wp.Criteria.Ignored) == 1 && wp.Criteria.Ignored[0].Item == "`"+core.ConfigFile+"`", "criteria %+v", wp.Criteria)
		for _, field := range []string{"/profile", "/schema", "/variants"} {
			expect(t, strings.Contains(wp.Criteria.Ignored[0].Reason, field), "the item does not name %s: %s", field, wp.Criteria.Ignored[0].Reason)
		}
	}},
	{"S6 C", "S6", "core-lib", 2, func(t *testing.T, wp core.WorkPackage) {
		expect(t, wp.Criteria.Source == core.SourceLegacy && wp.Criteria.Profile == "library", "criteria %+v", wp.Criteria)
	}},
	{"S6 D", "S6", "kmp-shared", 1, func(t *testing.T, wp core.WorkPackage) {
		expect(t, wp.Criteria.Source == core.SourceDefault && wp.Criteria.Profile == "library" && reflect.DeepEqual(wp.Criteria.Packs, []string{"kotlin-multiplatform", "android"}),
			"criteria %+v", wp.Criteria)
		expect(t, hasNotVerified(wp, "iosApp/iosApp/ContentView.swift", "unsupported-language"), "not verified %v", wp.NotVerified)
	}},
	{"S7 sample-app", "S7", "sample-app", 8, func(t *testing.T, wp core.WorkPackage) {
		expect(t, wp.CI.State == core.CIPassing && len(wp.Signals) > 0, "ci %s, %d signals", wp.CI.State, len(wp.Signals))
	}},
	{"S7 kmp-shared", "S7", "kmp-shared", 3, func(t *testing.T, wp core.WorkPackage) {
		expect(t, hasNotVerified(wp, "shared/src/commonMain/kotlin/com/example/shared/DeviceInfo.kt", "search-unavailable"), "not verified %v", wp.NotVerified)
	}},
}

func expect(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Errorf(format, args...)
	}
}

func hasNotVerified(wp core.WorkPackage, path, reason string) bool {
	return slices.ContainsFunc(wp.NotVerified, func(n core.UnverifiedFile) bool { return n.Path == path && n.Reason == reason })
}

func TestCollectRecordedScenarios(t *testing.T) {
	for _, s := range scenarioReplays {
		t.Run(s.name, func(t *testing.T) {
			d, _, r, log := replayDeps(t, s.scenario)
			addBuildFiles(t, r, s.scenario, s.repo, s.number)
			addNotFound(r, "core-lib", ".github/skills/core-lib-code-review/references", "main")
			run, err := Collect(context.Background(), d, sandboxKey(t, s.repo, s.number))
			if err != nil {
				t.Fatalf("%v\n%s", err, log)
			}
			if run.State.Phase != state.Collected || len(run.Packages) != 1 {
				t.Fatalf("phase %s, %d packages", run.State.Phase, len(run.Packages))
			}
			s.check(t, readWorkPackage(t, run, sandboxKey(t, s.repo, s.number)))
		})
	}
}
