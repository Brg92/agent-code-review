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
	t.Cleanup(func() { _ = log.Close() })
	log.SetCommand("next")
	o, err := OpenRun(work, runID, log)
	if err != nil {
		t.Fatalf("OpenRun: %v", err)
	}
	return o, log, &verbose
}

func next(t testing.TB, o *Opened, log *runlog.Log) Served {
	t.Helper()
	return nextPart(t, o, log, 0)
}

// nextPart is next with --part: 0 serves forward.
func nextPart(t testing.TB, o *Opened, log *runlog.Log, part int) Served {
	t.Helper()
	s, err := Next(o, log, part)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return s
}

func waitLine(path string) string {
	return "Write the judgement to " + path + ", then run agent-code-review next\n"
}

func TestNextMovesForwardOnly(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	o, log, _ := open(t, work, "")

	// The related change request first, its parts in order.
	for i, part := range []int{1, 2} {
		s := next(t, o, log)
		brief, err := render.Brief(m.LibWP, part)
		if err != nil {
			t.Fatal(err)
		}
		header := "Judge " + m.Lib.String() + " part " + string(rune('0'+part)) + " of 2 — write the judgement to " + m.LibJudgement + "\n"
		if s.Done || s.Waiting || s.CR != m.Lib || s.Part != part || s.Total != 2 || s.Text != header+brief {
			t.Errorf("call %d: %+v\nwant header %q", i+1, s, header)
		}
	}
	// Every part served, it says one thing until the judgement is there.
	for range 3 {
		s := next(t, o, log)
		if !s.Waiting || s.Done || s.CR != m.Lib || s.Text != waitLine(m.LibJudgement) {
			t.Fatalf("waiting: %+v", s)
		}
	}

	// Judged, it is the root's turn, which has one part.
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
	if again := next(t, o, log); !again.Waiting || again.CR != m.App || again.Text != waitLine(m.AppJudgement) {
		t.Errorf("the root is not served twice: %+v", again)
	}

	writeJudgement(t, m.AppJudgement, validJudgement)
	done := next(t, o, log)
	if !done.Done || done.Text != "Done: run agent-code-review finalize\n" {
		t.Errorf("done: %+v", done)
	}
}

func TestNextJudgedBeforeEveryPartWasServedMovesOn(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	o, log, _ := open(t, work, "")
	next(t, o, log) // part 1 of the library
	writeJudgement(t, m.LibJudgement, validJudgement)
	if s := next(t, o, log); s.CR != m.App || s.Waiting {
		t.Errorf("served %+v", s)
	}
}

// --part prints a part again without moving what is served.
func TestNextPartServesAgainWithoutMoving(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	o, log, _ := open(t, work, "")
	brief := func(part int) string {
		b, err := render.Brief(m.LibWP, part)
		if err != nil {
			t.Fatal(err)
		}
		return "Judge " + m.Lib.String() + " part " + string(rune('0'+part)) + " of 2 — write the judgement to " + m.LibJudgement + "\n" + b
	}
	// Before anything is served the part can be read, and forward still starts at 1.
	if s := nextPart(t, o, log, 2); s.Text != brief(2) || s.Part != 2 || s.CR != m.Lib {
		t.Errorf("part 2 first: %+v", s)
	}
	if s := next(t, o, log); s.Part != 1 {
		t.Errorf("forward after --part: %+v", s)
	}
	if s := next(t, o, log); s.Part != 2 {
		t.Errorf("forward: %+v", s)
	}
	if s := nextPart(t, o, log, 1); s.Text != brief(1) {
		t.Errorf("part 1 again: %+v", s)
	}
	// Reading parts again does not make forward serve anything.
	if s := next(t, o, log); !s.Waiting {
		t.Errorf("forward after the last part: %+v", s)
	}
	// Once judged, --part is about the change request that is left.
	writeJudgement(t, m.LibJudgement, validJudgement)
	if s := nextPart(t, o, log, 1); s.CR != m.App || s.Part != 1 {
		t.Errorf("--part after the judgement: %+v", s)
	}
	writeJudgement(t, m.AppJudgement, validJudgement)
	if s := nextPart(t, o, log, 1); !s.Done {
		t.Errorf("--part with nothing left: %+v", s)
	}
}

func TestNextPartOutOfRangeIsAUsageError(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	o, log, _ := open(t, work, "")
	for _, part := range []int{3, 99, -1} {
		_, err := Next(o, log, part)
		if f := failure(t, err); f.Kind != KindUsage || !strings.Contains(f.Why, "2 parts") || !strings.Contains(f.Where, m.Lib.String()) {
			t.Errorf("part %d: failure %+v", part, f)
		}
	}
	// It did not count as a part served.
	if s := next(t, o, log); s.Part != 1 {
		t.Errorf("served %+v", s)
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
	// The same on the line that waits for it.
	next(t, o, log)
	waiting := next(t, o, log)
	if want := "Write the judgement to " + m.LibJudgement + " (the file there does not parse and is replaced), then run agent-code-review next\n"; !waiting.Waiting || waiting.Text != want {
		t.Errorf("waiting: %q", waiting.Text)
	}
	writeJudgement(t, m.LibJudgement, "")
	if s := next(t, o, log); !s.Waiting || s.CR != m.Lib {
		t.Errorf("an empty file is not a judgement: %+v", s)
	}
	writeJudgement(t, m.LibJudgement, "{}")
	if s := next(t, o, log); s.CR != m.App {
		t.Errorf("a document that parses counts as judged: %+v", s)
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
			_, err = Next(o, runlog.New(func() time.Time { return now }, nil), 0)
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
	_, err := Next(o, log, 0)
	if f := failure(t, err); f.Kind != KindState || !strings.Contains(f.Where, m.Lib.String()) {
		t.Errorf("failure %+v", f)
	}
}

func TestNextRecordsEveryPartServedInTheRunLog(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	o, log, verbose := open(t, work, "")
	next(t, o, log)
	next(t, o, log)
	next(t, o, log)
	nextPart(t, o, log, 1)
	lib := "2026-11-20T12:00:00Z\tnext\t" + m.Lib.String() + "\t"
	want := lib + "serve part 1 of 2\t-\t-\t-\tok\n" + lib + "serve part 2 of 2\t-\t-\t-\tok\n" +
		lib + "wait for the judgement\t-\t-\t-\tok\n" + lib + "serve part 1 of 2 again\t-\t-\t-\tok\n"
	data, err := os.ReadFile(filepath.Join(m.Dir, runlog.FileName))
	if err != nil || string(data) != want || verbose.String() != want {
		t.Errorf("run.log:\n%s\nwant:\n%s", data, want)
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
	t.Cleanup(func() { _ = log.Close() })
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
	_, err = Next(o, log, 0)
	if f := failure(t, err); f.Kind != KindState || !strings.Contains(f.Where, m.Lib.String()) {
		t.Errorf("failure %+v", f)
	}
}

func TestNextRefusesAJudgementPathOutsideTheRun(t *testing.T) {
	for _, path := range []string{"../escape/judgement.json", "", "/etc/judgement.json", "a/../../x/judgement.json", "./x/judgement.json", "x//judgement.json",
		`C:\x\judgement.json`, `x\judgement.json`, "C:/x/judgement.json", "x/judgement.json/"} {
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
		_, err = Next(o, log, 0)
		if f := failure(t, err); f.Kind != KindState || f.Why != "path not allowed" {
			t.Errorf("%q: failure %+v", path, f)
		}
	}
}

func TestServedWithSchema(t *testing.T) {
	const schema = "/opt/acr/schemas/judgement.schema.json"
	tests := []struct {
		name   string
		served Served
		schema string
		want   string
	}{
		{"a part", Served{Text: "Judge x part 1 of 1 — write the judgement to /j\nbrief\nmore\n"}, schema,
			"Judge x part 1 of 1 — write the judgement to /j\nJudgement schema: " + schema + "\nbrief\nmore\n"},
		{"waiting", Served{Text: "Write the judgement to /j, then run agent-code-review next\n", Waiting: true}, schema,
			"Write the judgement to /j, then run agent-code-review next\nJudgement schema: " + schema + "\n"},
		{"done", Served{Text: DoneText, Done: true}, schema, DoneText},
		{"no schema", Served{Text: "Judge x part 1 of 1 — write the judgement to /j\nbrief\n"}, "", "Judge x part 1 of 1 — write the judgement to /j\nbrief\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.served.WithSchema(tt.schema).Text; got != tt.want {
				t.Errorf("Text = %q, want %q", got, tt.want)
			}
		})
	}
}
