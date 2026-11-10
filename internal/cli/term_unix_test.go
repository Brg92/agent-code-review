//go:build !windows

package cli

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSystemTerminalRejectsNonTerminals(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	closed, _ := os.Open(os.DevNull)
	closed.Close()

	term := systemTerminal{}
	if term.isTerminal(strings.NewReader("")) || term.isTerminal(r) || term.isTerminal(devNull) || term.isTerminal(closed) {
		t.Error("isTerminal = true for a reader, a pipe, the null device or a closed file")
	}
	for name, f := range map[string]*os.File{"pipe": r, "null device": devNull} {
		if _, err := term.disableEcho(f); err == nil {
			t.Errorf("disableEcho(%s) = nil error", name)
		}
	}
}

func TestSystemTerminalRoundTrip(t *testing.T) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		t.Skip("no controlling terminal")
	}
	defer tty.Close()
	term := systemTerminal{}
	if !term.isTerminal(tty) {
		t.Skip("/dev/tty has no settings")
	}
	before, _ := stty(tty, "-g")
	restore, err := term.disableEcho(tty)
	if err != nil {
		t.Fatalf("disableEcho: %v", err)
	}
	if err := restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if after, _ := stty(tty, "-g"); after != before {
		t.Errorf("terminal settings changed: %q -> %q", before, after)
	}
}

func TestNotifyInterrupts(t *testing.T) {
	c := make(chan os.Signal, 1)
	stop := notifyInterrupts(c)
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case sig := <-c:
		if sig != syscall.SIGTERM {
			t.Errorf("signal = %v", sig)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no signal relayed")
	}
}
