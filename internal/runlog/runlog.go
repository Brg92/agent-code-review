// Package runlog writes run.log, the diagnostics of a review run: one line
// per event, in plain text, appended by every command that works on the
// run. A line holds the UTC time, the command, the change request, the
// operation, the status, the duration in milliseconds, the number of
// attempts and the outcome, separated by tabs, with - for a field that does
// not apply:
//
//	2026-11-20T12:00:05Z	review	github:github.com/acme/app#7	GET /repos/acme/app/pulls/7/files	200	120ms	1	ok
//
// The outcome is ok, retry, rate-limited, error or "not-verified: <reason>".
// The log never holds a token, a header, a query string or a response
// body: its inputs, a transport.Event and short names chosen by the caller,
// cannot carry them. With a verbose writer the same lines also go there as
// they happen.
package runlog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/transport"
)

// FileName is the name of the log in a run's work directory.
const FileName = "run.log"

// Log is the diagnostics of one command on a run. It is safe for use from
// several goroutines.
type Log struct {
	now     func() time.Time
	verbose io.Writer

	mu   sync.Mutex
	file *os.File
	path string
	cmd  string
	cr   string
	err  error
}

// New returns a Log that stamps lines with now and, when verbose is not
// nil, also writes them there. It writes no file until Attach.
func New(now func() time.Time, verbose io.Writer) *Log {
	return &Log{now: now, verbose: verbose}
}

// Attach opens run.log in the work directory of the run, creating it if
// needed and appending to what earlier commands wrote. The file is for the
// user only.
func (l *Log) Attach(runDir string) error {
	path := filepath.Join(runDir, FileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("run log: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.file, l.path = f, path
	return nil
}

// Path returns the path of run.log, or "" before a successful Attach.
func (l *Log) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// SetCommand sets the command the following lines belong to.
func (l *Log) SetCommand(cmd string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cmd = cmd
}

// SetCR sets the change request the following lines belong to; the zero
// key means none.
func (l *Log) SetCR(k core.Key) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cr = ""
	if !k.IsZero() {
		l.cr = k.String()
	}
}

// Observe logs a request of the transport. It is the Observe function of
// the transport's configuration.
func (l *Log) Observe(e transport.Event) {
	status := "-"
	if e.Status != 0 {
		status = fmt.Sprint(e.Status)
	}
	l.write(e.Method+" "+e.Path, status, fmt.Sprintf("%dms", e.Duration.Milliseconds()), fmt.Sprint(e.Attempts), string(e.Outcome))
}

// Event logs something that is not a request, such as a part served, with
// its outcome.
func (l *Log) Event(op, outcome string) { l.write(op, "-", "-", "-", outcome) }

// NotVerified logs a read the core turned into a Not verified entry; reason
// is its stable name, such as tree-truncated.
func (l *Log) NotVerified(op, reason string) { l.write(op, "-", "-", "-", "not-verified: "+reason) }

// Err returns the first error met writing the log, if any. A failed write
// never stops a run.
func (l *Log) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// Close closes run.log. Lines written later only go to the verbose writer.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	if err != nil {
		return fmt.Errorf("run log: %w", err)
	}
	return nil
}

func (l *Log) write(op, status, duration, attempts, outcome string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cr := l.cr
	if cr == "" {
		cr = "-"
	}
	line := strings.Join([]string{
		l.now().UTC().Format("2006-01-02T15:04:05Z"), oneLine(l.cmd), cr, oneLine(op), status, duration, attempts, oneLine(outcome),
	}, "\t") + "\n"
	if l.file != nil {
		if _, err := l.file.WriteString(line); err != nil && l.err == nil {
			l.err = fmt.Errorf("run log: %w", err)
		}
	} else if l.path != "" && l.err == nil {
		l.err = fmt.Errorf("run log: %s is closed", l.path)
	}
	if l.verbose != nil {
		_, _ = io.WriteString(l.verbose, line)
	}
}

// oneLine replaces every control character, tabs and newlines included,
// with a space, so that a value never breaks the line or the columns.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
