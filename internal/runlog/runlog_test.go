package runlog

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/transport"
)

var noon = time.Date(2026, 11, 20, 12, 0, 5, 0, time.UTC)

func fixedClock() time.Time { return noon }

func key(t *testing.T) core.Key {
	t.Helper()
	k, err := core.ParseKey("github:github.com/acme/app#7")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// attach attaches l to dir and closes it when the test ends: Windows cannot
// remove a directory that holds an open file.
func attach(t *testing.T, l *Log, dir string) {
	t.Helper()
	if err := l.Attach(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
}

func readLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestObserveWritesOneLinePerEvent(t *testing.T) {
	dir := t.TempDir()
	l := New(fixedClock, nil)
	attach(t, l, dir)
	l.SetCommand("review")
	l.SetCR(key(t))
	l.Observe(transport.Event{Method: "GET", Path: "/repos/acme/app/pulls/7/files", Status: 200, Duration: 120 * time.Millisecond, Attempts: 1, Outcome: transport.OutcomeOK})
	l.Observe(transport.Event{Method: "GET", Path: "/repos/acme/app/pulls/7", Status: 503, Attempts: 1, Outcome: transport.OutcomeRetry})
	l.Observe(transport.Event{Method: "GET", Path: "/repos/acme/app", Duration: 3 * time.Second, Attempts: 2, Outcome: transport.OutcomeError})
	want := "2026-11-20T12:00:05Z\treview\tgithub:github.com/acme/app#7\tGET /repos/acme/app/pulls/7/files\t200\t120ms\t1\tok\n" +
		"2026-11-20T12:00:05Z\treview\tgithub:github.com/acme/app#7\tGET /repos/acme/app/pulls/7\t503\t0ms\t1\tretry\n" +
		"2026-11-20T12:00:05Z\treview\tgithub:github.com/acme/app#7\tGET /repos/acme/app\t-\t3000ms\t2\terror\n"
	if got := readLog(t, dir); got != want {
		t.Errorf("run.log:\n%s\nwant:\n%s", got, want)
	}
}

func TestEventsWithoutAChangeRequestOrAnAttempt(t *testing.T) {
	dir := t.TempDir()
	l := New(fixedClock, nil)
	attach(t, l, dir)
	l.SetCommand("next")
	l.Event("serve part 2 of 3", "ok")
	l.SetCR(key(t))
	l.NotVerified("GET /repos/acme/app/git/trees/abc", "tree-truncated")
	l.SetCR(core.Key{})
	l.Event("end", "ok")
	want := "2026-11-20T12:00:05Z\tnext\t-\tserve part 2 of 3\t-\t-\t-\tok\n" +
		"2026-11-20T12:00:05Z\tnext\tgithub:github.com/acme/app#7\tGET /repos/acme/app/git/trees/abc\t-\t-\t-\tnot-verified: tree-truncated\n" +
		"2026-11-20T12:00:05Z\tnext\t-\tend\t-\t-\t-\tok\n"
	if got := readLog(t, dir); got != want {
		t.Errorf("run.log:\n%s\nwant:\n%s", got, want)
	}
}

func TestFieldsNeverBreakTheLine(t *testing.T) {
	dir := t.TempDir()
	l := New(fixedClock, nil)
	attach(t, l, dir)
	l.Event("a\nb\tc\r\x1b[31m", "x\ny")
	got := readLog(t, dir)
	if strings.Count(got, "\n") != 1 || strings.Count(got, "\t") != 7 || strings.ContainsAny(got, "\r\x1b") {
		t.Errorf("run.log = %q", got)
	}
}

func TestAttachAppends(t *testing.T) {
	dir := t.TempDir()
	for i := range 2 {
		l := New(fixedClock, nil)
		attach(t, l, dir)
		l.SetCommand([]string{"review", "next"}[i])
		l.Event("op", "ok")
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
	}
	got := readLog(t, dir)
	if strings.Count(got, "\n") != 2 || !strings.Contains(got, "\treview\t") || !strings.Contains(got, "\tnext\t") {
		t.Errorf("run.log = %q", got)
	}
}

func TestLogIsPrivate(t *testing.T) {
	dir := t.TempDir()
	l := New(fixedClock, nil)
	attach(t, l, dir)
	info, err := os.Stat(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	if l.Path() != filepath.Join(dir, FileName) {
		t.Errorf("Path = %q", l.Path())
	}
	// Windows has no Unix permission bits.
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm&0o077 != 0 {
		t.Errorf("mode = %v, want the owner only", perm)
	}
}

func TestVerboseWritesTheSameLines(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	l := New(fixedClock, &stderr)
	attach(t, l, dir)
	l.SetCommand("review")
	l.Event("op", "ok")
	if got := readLog(t, dir); got != stderr.String() || got == "" {
		t.Errorf("stderr = %q, run.log = %q", stderr.String(), got)
	}
}

func TestWorksBeforeAttach(t *testing.T) {
	var stderr bytes.Buffer
	l := New(fixedClock, &stderr)
	if l.Path() != "" {
		t.Errorf("Path = %q before Attach", l.Path())
	}
	l.Event("op", "ok")
	if stderr.Len() == 0 {
		t.Error("verbose output is lost before Attach")
	}
	quiet := New(fixedClock, nil)
	quiet.Event("op", "ok")
	if err := quiet.Close(); err != nil {
		t.Error(err)
	}
}

func TestAttachFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "such", "dir")
	l := New(fixedClock, nil)
	if err := l.Attach(missing); err == nil {
		t.Fatal("Attach to a missing directory succeeded")
	}
	if l.Path() != "" {
		t.Errorf("Path = %q after a failed Attach", l.Path())
	}
	l.Event("op", "ok") // never panics
}

func TestWriteErrorsDoNotStopTheRun(t *testing.T) {
	dir := t.TempDir()
	l := New(fixedClock, nil)
	attach(t, l, dir)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l.Event("op", "ok") // the file is closed
	if l.Err() == nil {
		t.Error("Err does not report the failed write")
	}
}

// TestAttachAgainClosesTheFileBefore: a command that opens the run again,
// as publish does when it is applied twice in a process, must not leave the
// first run.log open: Windows cannot remove a directory that holds one.
func TestAttachAgainClosesTheFileBefore(t *testing.T) {
	l := New(fixedClock, nil)
	dir := t.TempDir()
	if err := l.Attach(dir); err != nil {
		t.Fatal(err)
	}
	first := l.file
	if err := l.Attach(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if _, err := first.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("writing to the first file = %v, want it closed", err)
	}
	l.Event("still logs", "ok")
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil || !strings.Contains(string(data), "still logs") {
		t.Errorf("run.log = %q, %v", data, err)
	}
}
