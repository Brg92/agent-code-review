// Package productflow runs the product's own flow on the recordings of the
// sandbox scenarios, for the tests that need reviews as the product builds
// them: review collects a pull request and its cascade through the GitHub
// provider, the hand-written scenario judgement of each change request in
// testdata/scenarios decides its signals, and finalize judges the result.
// It is test support code only.
package productflow

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
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/review"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/reviewreplay"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
	"github.com/Brg92/agent-code-review/internal/transport"
)

func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// Reviewed is a change request as the product reviewed it.
type Reviewed struct {
	CR core.Key
	// WorkPackage is the work package review wrote.
	WorkPackage core.WorkPackage
	// Review is what finalize judges, rebuilt from the work package alone.
	Review core.Review
	// Judgement is the hand-written judgement of the scenario.
	Judgement core.Judgement
}

// Flow is the change requests a review of one pull request reached, in
// review order: related changes first, the root last.
type Flow struct {
	Scenario string
	CRs      []Reviewed
}

type clock struct{ at time.Time }

func (c *clock) Now() time.Time { return c.at }
func (c *clock) Sleep(ctx context.Context, d time.Duration) error {
	c.at = c.at.Add(d)
	return ctx.Err()
}

// Collect reviews pull request number of repo, as the review command does
// on the recordings of a scenario: S2 walks into the pull request of S4
// that it lists. Each change request gets the scenario judgement of its
// repository and number.
func Collect(t testing.TB, scenario, repo string, number int) Flow {
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
	c := &clock{at: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	log := runlog.New(c.Now, io.Discard)
	t.Cleanup(func() { _ = log.Close() })
	reader, err := github.NewLoopback("github.com", r.URL(), reviewreplay.Tokens{}, github.Options{
		Transport: transport.Config{Now: c.Now, Sleep: c.Sleep, Observe: log.Observe},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := review.Collect(context.Background(), review.Deps{
		Reader:     reader,
		Registries: []refs.Registry{github.Registry{}, gitlab.Registry{}},
		Packs:      set,
		WorkDir:    work,
		KeepDays:   state.DefaultKeepDays,
		Now:        c.Now,
		Random:     rand.New(rand.NewSource(1)),
		Log:        log,
	}, key(t, repo, number))
	if err != nil {
		t.Fatalf("%s %s#%d: %v", scenario, repo, number, err)
	}
	flow := Flow{Scenario: scenario}
	for _, p := range run.Packages {
		data, err := os.ReadFile(p.File)
		if err != nil {
			t.Fatal(err)
		}
		wp, err := core.DecodeWorkPackage(data)
		if err != nil {
			t.Fatal(err)
		}
		rv, err := core.ReviewOf(wp, set)
		if err != nil {
			t.Fatalf("%s: %v", p.CR, err)
		}
		flow.CRs = append(flow.CRs, Reviewed{CR: p.CR, WorkPackage: wp, Review: rv, Judgement: judgement(t, scenario, p.CR)})
	}
	return flow
}

func key(t testing.TB, repo string, number int) core.Key {
	t.Helper()
	k, err := core.ParseKey("github:github.com/" + sandbox.Owner + "/" + repo + "#" + strconv.Itoa(number))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// judgement reads testdata/scenarios/<scenario>/judgements/<repo>-<number>.json.
func judgement(t testing.TB, scenario string, k core.Key) core.Judgement {
	t.Helper()
	name := strings.TrimPrefix(k.Project, sandbox.Owner+"/") + "-" + strconv.Itoa(k.Number) + ".json"
	data, err := os.ReadFile(filepath.Join(root(), "testdata", "scenarios", scenario, "judgements", name))
	if err != nil {
		t.Fatal(err)
	}
	j, err := core.DecodeJudgement(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return j
}

// Of returns the change request number of repo in the flow.
func (f Flow) Of(t testing.TB, repo string, number int) Reviewed {
	t.Helper()
	k := key(t, repo, number)
	for _, r := range f.CRs {
		if r.CR.Same(k) {
			return r
		}
	}
	t.Fatalf("the review of %s reached no %s#%d", f.Scenario, repo, number)
	return Reviewed{}
}

// Finalize finalizes the change request with its scenario judgement, after
// change has edited a copy of it; change may be nil.
func (r Reviewed) Finalize(t testing.TB, change func(j *core.Judgement)) core.Result {
	t.Helper()
	j := r.Judgement
	j.Findings, j.PreExisting, j.Signals = append([]core.JudgedFinding{}, j.Findings...), append([]core.JudgedFinding{}, j.PreExisting...), append([]core.SignalDecision{}, j.Signals...)
	if change != nil {
		change(&j)
	}
	res, err := core.Finalize(r.Review, j)
	if err != nil {
		t.Fatalf("%s: %v", r.CR, err)
	}
	return res
}

// Document is the findings document of Finalize with the header of the work
// package; header may edit it, as a case that varies the checks does.
func (r Reviewed) Document(t testing.TB, change func(j *core.Judgement), header func(h *core.Header)) core.Document {
	t.Helper()
	h := r.WorkPackage.Header()
	if header != nil {
		header(&h)
	}
	return r.Finalize(t, change).Document(h)
}
