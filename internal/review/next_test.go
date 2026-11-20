package review

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/render"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
)

// runAt is the id of the run made at time at, with the same random part.
func runAt(at time.Time) string {
	id, err := state.NewRunID(at, strings.NewReader("\xde\xad\xbe\xef"))
	if err != nil {
		panic(err)
	}
	return id
}

// exampleWorkPackage reads a valid work package example of the contracts.
func exampleWorkPackage(t testing.TB, name string) core.WorkPackage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "workpackage", "valid", name))
	if err != nil {
		t.Fatal(err)
	}
	wp, err := core.DecodeWorkPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	return wp
}

// madeRun is a run written by hand: acme/app#7 with acme/lib#3 below it, as
// Collect leaves them, whose work packages are the examples of the
// contracts, the one of the library in two parts.
type madeRun struct {
	ID, Dir  string
	App, Lib core.Key
	// Judgements are the paths where each change request is judged.
	AppJudgement, LibJudgement string
	AppWP, LibWP               core.WorkPackage
}

var lastPhase = map[state.Phase]core.Status{
	state.Collected: core.Collected, state.Judged: core.Judged, state.Finalized: core.Finalized,
	state.Planned: core.Finalized, state.Published: core.Finalized,
}

func makeRun(t testing.TB, work string, at time.Time, phase state.Phase) madeRun {
	t.Helper()
	m := madeRun{ID: runAt(at), App: key(t, appName), Lib: key(t, libName)}
	var err error
	if m.Dir, err = state.CreateRunDir(work, m.ID); err != nil {
		t.Fatal(err)
	}
	entries := []core.Entry{
		{CR: m.App, Status: core.Pending, Title: "Greet by name", HeadSHA: headApp},
		{CR: m.Lib, Depth: 1, Parent: &m.App, Status: core.Pending, Title: "Load greetings", HeadSHA: headLib},
	}
	st, err := state.New(m.ID, m.App, core.DefaultLimits(), entries, at)
	if err != nil {
		t.Fatal(err)
	}
	if phase != state.Resolved {
		for i := range st.ChangeRequests {
			st.ChangeRequests[i].Status = core.Collected
		}
		for _, next := range []state.Phase{state.Collected, state.Judged, state.Finalized, state.Planned, state.Published} {
			if next == state.Judged || next == state.Finalized {
				for i := range st.ChangeRequests {
					st.ChangeRequests[i].Status = lastPhase[next]
				}
			}
			if err := st.Advance(next, at); err != nil {
				t.Fatal(err)
			}
			if next == phase {
				break
			}
		}
	}
	m.AppWP, m.LibWP = exampleWorkPackage(t, "s2-root-with-related-changes.json"), exampleWorkPackage(t, "parts-regions-and-summary.json")
	for _, c := range []struct {
		e  core.Entry
		wp *core.WorkPackage
	}{{entries[0], &m.AppWP}, {entries[1], &m.LibWP}} {
		dir, err := state.CreateChangeRequestDir(m.Dir, c.e.CR, c.e.HeadSHA)
		if err != nil {
			t.Fatal(err)
		}
		c.wp.RunID, c.wp.CR, c.wp.JudgementPath = m.ID, c.e.CR, filepath.Base(dir)+"/"+state.JudgementFile
		data, err := core.EncodeWorkPackage(*c.wp)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, state.WorkPackageFile), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if c.e.CR == m.App {
			m.AppJudgement = filepath.Join(dir, state.JudgementFile)
		} else {
			m.LibJudgement = filepath.Join(dir, state.JudgementFile)
		}
	}
	if err := state.Save(m.Dir, st); err != nil {
		t.Fatal(err)
	}
	return m
}

const validJudgement = `{"schema":1,"cr":"x","headSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","findings":[],"preExisting":[]}`

func writeJudgement(t testing.TB, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func open(t testing.TB, work, runID string) (*Opened, *runlog.Log, *bytes.Buffer) {
	t.Helper()
	var verbose bytes.Buffer
	log := runlog.New(func() time.Time { return now }, &verbose)
	log.SetCommand("next")
	o, err := OpenRun(work, runID, log)
	if err != nil {
		t.Fatalf("OpenRun: %v", err)
	}
	return o, log, &verbose
}

func next(t testing.TB, o *Opened, log *runlog.Log) Served {
	t.Helper()
	s, err := Next(o, log)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return s
}

func TestNextServesPartsInReviewOrder(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	o, log, _ := open(t, work, "")

	// The related change request first, its parts in order and round again.
	for i, want := range []struct {
		cr   core.Key
		part int
	}{{m.Lib, 1}, {m.Lib, 2}, {m.Lib, 1}} {
		s := next(t, o, log)
		brief, err := render.Brief(m.LibWP, want.part)
		if err != nil {
			t.Fatal(err)
		}
		header := "Judge " + want.cr.String() + " part " + string(rune('0'+want.part)) + " of 2 — write the judgement to " + m.LibJudgement + "\n"
		if s.Done || s.CR != want.cr || s.Part != want.part || s.Total != 2 || s.Text != header+brief {
			t.Errorf("call %d: %+v\nwant header %q", i+1, s, header)
		}
	}

	// Judged, it is the root's turn.
	writeJudgement(t, m.LibJudgement, validJudgement)
	s := next(t, o, log)
	brief, err := render.Brief(m.AppWP, 1)
	if err != nil {
		t.Fatal(err)
	}
	header := "Judge " + m.App.String() + " part 1 of 1 — write the judgement to " + m.AppJudgement + "\n"
	if s.Done || s.CR != m.App || s.Text != header+brief {
		t.Errorf("root: %+v", s)
	}
	if got := next(t, o, log).CR; got != m.App {
		t.Errorf("the root is served again until it is judged, not %s", got)
	}

	writeJudgement(t, m.AppJudgement, validJudgement)
	done := next(t, o, log)
	if !done.Done || done.Text != "Done: run agent-code-review finalize\n" {
		t.Errorf("done: %+v", done)
	}
}

func TestNextDoesNotChangeTheState(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	before, err := os.ReadFile(filepath.Join(m.Dir, state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	o, log, _ := open(t, work, "")
	next(t, o, log)
	after, err := os.ReadFile(filepath.Join(m.Dir, state.FileName))
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("state.json changed: %v", err)
	}
}

func TestNextTakesAJudgementThatDoesNotParseForMissing(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	writeJudgement(t, m.LibJudgement, `{"schema":1,`)
	o, log, _ := open(t, work, "")
	s := next(t, o, log)
	if s.Done || s.CR != m.Lib || !strings.HasPrefix(s.Text, "Judge "+m.Lib.String()+" part 1 of 2 — write the judgement to "+m.LibJudgement+" (the file there does not parse and is replaced)\n") {
		t.Errorf("%s", s.Text[:min(len(s.Text), 300)])
	}
	writeJudgement(t, m.LibJudgement, "")
	if s := next(t, o, log); s.CR != m.Lib {
		t.Errorf("an empty judgement counts as judged: %+v", s)
	}
}

func TestNextRefusesARunInAnotherPhase(t *testing.T) {
	for _, phase := range []state.Phase{state.Resolved, state.Judged, state.Finalized, state.Planned, state.Published} {
		t.Run(string(phase), func(t *testing.T) {
			work := t.TempDir()
			m := makeRun(t, work, now, phase)
			o, err := OpenRun(work, m.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Next(o, runlog.New(func() time.Time { return now }, nil))
			f := failure(t, err)
			if f.Kind != KindState || !strings.Contains(f.Why, string(phase)) || f.Hint == "" {
				t.Errorf("failure %+v", f)
			}
		})
	}
}

func TestNextNeedsItsWorkPackages(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	if err := os.Remove(filepath.Join(filepath.Dir(m.LibJudgement), state.WorkPackageFile)); err != nil {
		t.Fatal(err)
	}
	o, log, _ := open(t, work, "")
	_, err := Next(o, log)
	if f := failure(t, err); f.Kind != KindState || !strings.Contains(f.Where, m.Lib.String()) {
		t.Errorf("failure %+v", f)
	}
}

func TestNextRecordsWhatItServedInTheRunLog(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	o, log, verbose := open(t, work, "")
	next(t, o, log)
	writeJudgement(t, m.LibJudgement, validJudgement)
	writeJudgement(t, m.AppJudgement, validJudgement)
	next(t, o, log)
	data, err := os.ReadFile(filepath.Join(m.Dir, runlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	want := "2026-11-20T12:00:00Z\tnext\t" + m.Lib.String() + "\tserve part 1 of 2\t-\t-\t-\tok\n" +
		"2026-11-20T12:00:00Z\tnext\t-\tnothing left to judge\t-\t-\t-\tok\n"
	if string(data) != want || verbose.String() != want {
		t.Errorf("run.log:\n%s\nwant:\n%s", data, want)
	}
}

func TestOpenRunChoosesTheMostRecentRunThatIsNotPublished(t *testing.T) {
	work := t.TempDir()
	old := makeRun(t, work, now.Add(-2*time.Hour), state.Collected)
	published := makeRun(t, work, now.Add(time.Hour), state.Published)
	recent := makeRun(t, work, now, state.Collected)
	// A review that failed before its state was saved leaves a directory
	// with a log only.
	failed, err := state.CreateRunDir(work, runAt(now.Add(30*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(failed, runlog.FileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Something that is not a run is not looked at.
	if err := os.MkdirAll(filepath.Join(work, "runs", "not-a-run-id!"), 0o700); err != nil {
		t.Fatal(err)
	}

	o, err := OpenRun(work, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.ID != recent.ID || o.Dir != recent.Dir || o.State.RunID != recent.ID {
		t.Errorf("chose %s, want %s", o.ID, recent.ID)
	}
	if o, err = OpenRun(work, old.ID, nil); err != nil || o.ID != old.ID {
		t.Errorf("--run %s: %v", old.ID, err)
	}
	// A run given by name is used as it is, published or not; the phase
	// is the command's to judge.
	if o, err = OpenRun(work, published.ID, nil); err != nil || o.State.Phase != state.Published {
		t.Errorf("--run %s: %v", published.ID, err)
	}
}

func TestOpenRunNeverChoosesAPublishedRun(t *testing.T) {
	work := t.TempDir()
	makeRun(t, work, now, state.Published)
	_, err := OpenRun(work, "", nil)
	if f := failure(t, err); f.Kind != KindState || !strings.Contains(f.Hint, "agent-code-review review") {
		t.Errorf("failure %+v", f)
	}
}

func TestOpenRunWithoutAnyRun(t *testing.T) {
	for _, tt := range []struct{ name, work string }{{"no work directory", filepath.Join(t.TempDir(), "absent")}, {"an empty one", t.TempDir()}} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := OpenRun(tt.work, "", nil)
			if f := failure(t, err); f.Kind != KindState || !strings.Contains(f.What, "no run") || !strings.Contains(f.Hint, "agent-code-review review") {
				t.Errorf("failure %+v", f)
			}
		})
	}
}

func TestOpenRunWithARunIDThatIsNotOne(t *testing.T) {
	for _, id := range []string{"short", "../../etc", "20261120T120000Z-deadbeef/../x", strings.Repeat("a", 65)} {
		_, err := OpenRun(t.TempDir(), id, nil)
		if f := failure(t, err); f.Kind != KindUsage || strings.Contains(f.Why, "etc") {
			t.Errorf("%q: failure %+v", id, f)
		}
	}
	_, err := OpenRun(t.TempDir(), "20261120T120000Z-deadbeef", nil)
	if f := failure(t, err); f.Kind != KindState || !strings.Contains(f.What, "no run 20261120T120000Z-deadbeef") {
		t.Errorf("a run that does not exist: %+v", f)
	}
}

func TestOpenRunWithAStateItCannotRead(t *testing.T) {
	for name, content := range map[string]string{
		"an unknown version": `{"schema": 2}`,
		"not json":           `{`,
	} {
		t.Run(name, func(t *testing.T) {
			work := t.TempDir()
			m := makeRun(t, work, now, state.Collected)
			if err := os.WriteFile(filepath.Join(m.Dir, state.FileName), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"", m.ID} {
				_, err := OpenRun(work, id, nil)
				if f := failure(t, err); f.Kind != KindState || !strings.Contains(f.Where, state.FileName) {
					t.Errorf("run %q: failure %+v", id, f)
				}
			}
		})
	}
}

func TestOpenRunAttachesTheRunLog(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	log := runlog.New(func() time.Time { return now }, nil)
	if _, err := OpenRun(work, "", log); err != nil {
		t.Fatal(err)
	}
	if log.Path() != filepath.Join(m.Dir, runlog.FileName) {
		t.Errorf("log path %q", log.Path())
	}
}

func TestNextRefusesAWorkPackageWithoutParts(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	m.LibWP.Parts.Total = 0
	data, err := core.EncodeWorkPackage(m.LibWP)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(m.LibJudgement), state.WorkPackageFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	o, log, _ := open(t, work, "")
	_, err = Next(o, log)
	if f := failure(t, err); f.Kind != KindState || !strings.Contains(f.Where, m.Lib.String()) {
		t.Errorf("failure %+v", f)
	}
}

func TestNextRefusesAJudgementPathOutsideTheRun(t *testing.T) {
	for _, path := range []string{"../escape/judgement.json", "", "/etc/judgement.json"} {
		work := t.TempDir()
		m := makeRun(t, work, now, state.Collected)
		m.LibWP.JudgementPath = path
		data, err := core.EncodeWorkPackage(m.LibWP)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(m.LibJudgement), state.WorkPackageFile), data, 0o600); err != nil {
			t.Fatal(err)
		}
		o, log, _ := open(t, work, "")
		_, err = Next(o, log)
		if f := failure(t, err); f.Kind != KindState || f.Why != "path not allowed" {
			t.Errorf("%q: failure %+v", path, f)
		}
	}
}
