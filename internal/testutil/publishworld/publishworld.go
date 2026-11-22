// Package publishworld sets up what the tests of publish need: a review of
// a recorded scenario, collected and finalized by the product's own flow
// with the hand-written scenario judgements, and a synthetic GitHub
// (package ghserver) that has the pull requests of the review, as the
// recordings say, to plan and apply against. No write ever goes further
// than that server. It is test support code only.
package publishworld

import (
	"context"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/review"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/ghserver"
	"github.com/Brg92/agent-code-review/internal/testutil/reviewreplay"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
	"github.com/Brg92/agent-code-review/internal/transport"
)

// Login is the account the token of the world belongs to.
const Login = "token-login"

func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// Clock advances instead of sleeping.
type Clock struct{ At time.Time }

// Now is the time.
func (c *Clock) Now() time.Time { return c.At }

// Sleep advances the clock.
func (c *Clock) Sleep(ctx context.Context, d time.Duration) error {
	c.At = c.At.Add(d)
	return ctx.Err()
}

// World is a finalized review of a recorded scenario and the synthetic
// GitHub it is published to.
type World struct {
	T        testing.TB
	Scenario string
	// Work is the base of the work directories.
	Work string
	// Run is the review.
	Run *review.Run
	// Server is the synthetic GitHub, Provider the provider that talks to it.
	Server   *ghserver.Server
	Provider base.ForgeProvider
	Packs    *packs.Set
	Clock    *Clock
	Log      *runlog.Log
}

// Key is the key of a pull request of the sandbox.
func Key(t testing.TB, repo string, number int) core.Key {
	t.Helper()
	k, err := core.ParseKey("github:github.com/" + sandbox.Owner + "/" + repo + "#" + strconv.Itoa(number))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// New reviews pull request number of repo in a scenario, as the review
// command does on the recordings, judges each change request with its
// scenario judgement, finalizes the run, and starts a synthetic GitHub with
// every change request of the run as an open pull request by its recorded
// author, at its reviewed head, with its description. Each repository
// accepts its author as an assignee.
func New(t testing.TB, scenario, repo string, number int) *World {
	t.Helper()
	var r *sandbox.Replay
	if scenario == "S2" {
		r = reviewreplay.S2(t)
	} else {
		r = reviewreplay.New(t, scenario)
		reviewreplay.AddBuildFiles(t, r, scenario, repo, number)
		reviewreplay.AddNotFound(r, "core-lib", ".github/skills/core-lib-code-review/references", "main")
	}
	set, err := packs.Load(filepath.Join(root(), "packs"))
	if err != nil {
		t.Fatal(err)
	}
	// The work directory comes first: cleanups run last in, first out, and
	// Windows cannot remove a directory that holds an open log.
	work := t.TempDir()
	clock := &Clock{At: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	log := runlog.New(clock.Now, io.Discard)
	t.Cleanup(func() { _ = log.Close() })
	reader, err := github.NewLoopback("github.com", r.URL(), reviewreplay.Tokens{}, github.Options{
		Transport: transport.Config{Now: clock.Now, Sleep: clock.Sleep, Observe: log.Observe},
	})
	if err != nil {
		t.Fatal(err)
	}
	registries := []refs.Registry{github.Registry{}, gitlab.Registry{}}
	d := review.Deps{Reader: reader, Registries: registries, Packs: set, WorkDir: work, KeepDays: state.DefaultKeepDays,
		Now: clock.Now, Random: rand.New(rand.NewSource(1)), Log: log}
	run, err := review.Collect(context.Background(), d, Key(t, repo, number))
	if err != nil {
		t.Fatalf("%s %s#%d: %v", scenario, repo, number, err)
	}
	for _, p := range run.Packages {
		name := strings.TrimPrefix(p.CR.Project, sandbox.Owner+"/") + "-" + strconv.Itoa(p.CR.Number) + ".json"
		data, err := os.ReadFile(filepath.Join(root(), "testdata", "scenarios", scenario, "judgements", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.Judgement, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	o, err := review.OpenRun(work, "", log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := review.Finalize(o, review.FinalizeDeps{Packs: set, Registries: registries, Version: "0.0.0-dev", Now: clock.Now}, log); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	w := &World{T: t, Scenario: scenario, Work: work, Run: run, Packs: set, Clock: clock, Log: log}
	w.startServer(o.State.ChangeRequests)
	return w
}

// startServer mirrors the change requests of the run on a synthetic GitHub.
func (w *World) startServer(entries []core.Entry) {
	w.T.Helper()
	srv := ghserver.New(w.T)
	srv.Token = reviewreplay.Canary
	srv.Login = Login
	repos := map[string]*ghserver.Repo{}
	for _, e := range entries {
		if e.Status == core.Skipped {
			continue
		}
		short := strings.TrimPrefix(e.CR.Project, sandbox.Owner+"/")
		recorded := sandbox.PullRequest(w.T, w.recordedIn(short, e.CR.Number), short, e.CR.Number)
		repo := repos[e.CR.Project]
		if repo == nil {
			repo = srv.AddRepo(e.CR.Project, recorded.Author)
			repos[e.CR.Project] = repo
		}
		pr := repo.AddPR(e.CR.Number, recorded.Author, e.HeadSHA)
		pr.Title, pr.Body = recorded.Title, recorded.Body
	}
	w.Server = srv
	provider, err := github.NewLoopback("github.com", srv.URL(), reviewreplay.Tokens{}, github.Options{
		Transport: transport.Config{Now: w.Clock.Now, Sleep: w.Clock.Sleep, Observe: w.Log.Observe},
	})
	if err != nil {
		w.T.Fatal(err)
	}
	w.Provider = provider
}

// recordedIn finds the scenario that recorded a pull request: the one of the
// world, or S4 for the pull request S2 reaches there.
func (w *World) recordedIn(repo string, number int) string {
	for _, s := range []string{w.Scenario, "S4", "S1"} {
		for _, l := range sandbox.AllPullFiles(w.T) {
			if l.Scenario == s && l.Repo == repo && l.Number == number {
				return s
			}
		}
	}
	w.T.Fatalf("no recording of %s#%d", repo, number)
	return ""
}

// Deps are the dependencies of publish on the synthetic GitHub.
func (w *World) Deps() review.PublishDeps {
	return review.PublishDeps{Provider: w.Provider, Registries: []refs.Registry{github.Registry{}, gitlab.Registry{}}, Now: w.Clock.Now}
}

// Open opens the run.
func (w *World) Open() *review.Opened {
	w.T.Helper()
	o, err := review.OpenRun(w.Work, w.Run.ID, w.Log)
	if err != nil {
		w.T.Fatal(err)
	}
	return o
}

// Plan plans the run.
func (w *World) Plan(choice core.Choice) (*review.Planned, error) {
	w.T.Helper()
	return review.PlanPublish(context.Background(), w.Open(), w.Deps(), choice, w.Log)
}

// MustPlan plans the run and fails the test if that fails.
func (w *World) MustPlan(choice core.Choice) *review.Planned {
	w.T.Helper()
	p, err := w.Plan(choice)
	if err != nil {
		w.T.Fatalf("PlanPublish(%s): %v", choice, err)
	}
	return p
}

// Apply applies the plans of the run.
func (w *World) Apply() (*review.Applied, error) {
	w.T.Helper()
	return review.ApplyPublish(context.Background(), w.Open(), w.Deps(), w.Log)
}

// MustApply applies the plans and fails the test if that fails.
func (w *World) MustApply() *review.Applied {
	w.T.Helper()
	a, err := w.Apply()
	if err != nil {
		w.T.Fatalf("ApplyPublish: %v", err)
	}
	return a
}

// Package returns the package of a change request of the run.
func (w *World) Package(repo string, number int) review.Package {
	w.T.Helper()
	k := Key(w.T, repo, number)
	for _, p := range w.Run.Packages {
		if p.CR.Same(k) {
			return p
		}
	}
	w.T.Fatalf("the run has no %s", k)
	return review.Package{}
}

// PR returns the synthetic pull request of a change request.
func (w *World) PR(repo string, number int) *ghserver.PR {
	w.T.Helper()
	return w.Server.Repo(sandbox.Owner + "/" + repo).PRs[number]
}

// Document reads the findings of a change request.
func (w *World) Document(repo string, number int) core.Document {
	w.T.Helper()
	data, err := os.ReadFile(filepath.Join(w.Package(repo, number).Dir, state.FindingsFile))
	if err != nil {
		w.T.Fatal(err)
	}
	doc, err := core.DecodeDocument(data)
	if err != nil {
		w.T.Fatal(err)
	}
	return doc
}

// SetDocument rewrites the findings of a change request, as a test that
// needs another finding does.
func (w *World) SetDocument(repo string, number int, doc core.Document) {
	w.T.Helper()
	data, err := core.EncodeDocument(doc)
	if err != nil {
		w.T.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Package(repo, number).Dir, state.FindingsFile), data, 0o600); err != nil {
		w.T.Fatal(err)
	}
}

// PlanFile is the path of the plan of a change request.
func (w *World) PlanFile(repo string, number int) string {
	return filepath.Join(w.Package(repo, number).Dir, state.PlanFile)
}

// Plan reads the plan of a change request.
func (w *World) ReadPlan(repo string, number int) core.Plan {
	w.T.Helper()
	data, err := os.ReadFile(w.PlanFile(repo, number))
	if err != nil {
		w.T.Fatal(err)
	}
	p, err := core.DecodePlan(data)
	if err != nil {
		w.T.Fatal(err)
	}
	return p
}

// Published reads the outcomes of a change request; ok is false when there
// are none.
func (w *World) Published(repo string, number int) (core.Published, bool) {
	w.T.Helper()
	data, err := os.ReadFile(filepath.Join(w.Package(repo, number).Dir, state.PublishedFile))
	if os.IsNotExist(err) {
		return core.Published{}, false
	}
	if err != nil {
		w.T.Fatal(err)
	}
	p, err := core.DecodePublished(data)
	if err != nil {
		w.T.Fatal(err)
	}
	return p, true
}

// State loads the state of the run.
func (w *World) State() *state.State {
	w.T.Helper()
	st, err := state.Load(filepath.Join(w.Work, "runs", w.Run.ID))
	if err != nil {
		w.T.Fatal(err)
	}
	return st
}

// SetState saves a state, as a test that tampers with it does.
func (w *World) SetState(st *state.State) {
	w.T.Helper()
	if err := state.Save(filepath.Join(w.Work, "runs", w.Run.ID), st); err != nil {
		w.T.Fatal(err)
	}
}

// Action returns the actions of a plan of a kind.
func Actions(p core.Plan, kind core.ActionKind) []core.Action {
	var out []core.Action
	for _, a := range p.Actions {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

// SkippedWith returns the skipped entries of a plan with a status.
func SkippedWith(p core.Plan, status core.PlanStatus) []core.PlanSkip {
	var out []core.PlanSkip
	for _, s := range p.Skipped {
		if s.Status == status {
			out = append(out, s)
		}
	}
	return out
}
