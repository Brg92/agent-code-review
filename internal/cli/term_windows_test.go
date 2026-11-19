//go:build windows

package cli

import (
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestSystemTerminalRejectsNonConsoles(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	term := systemTerminal{}
	if term.isTerminal(strings.NewReader("")) || term.isTerminal(r) {
		t.Error("isTerminal = true for a reader or a pipe")
	}
	if _, err := term.disableEcho(r); err == nil {
		t.Error("disableEcho(pipe) = nil error")
	}
}

func TestNotifyInterruptsStops(t *testing.T) {
	c := make(chan os.Signal, 1)
	notifyInterrupts(c)()
}

func TestSystemTerminalPipeIsNotAnMsysPty(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	for name, in := range map[string]io.Reader{"pipe": r, "reader": strings.NewReader(""), "closed": closedFile(t)} {
		if (systemTerminal{}).isMsysPty(in) {
			t.Errorf("isMsysPty(%s) = true", name)
		}
	}
}

func TestPipeName(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	name, err := pipeName(syscall.Handle(r.Fd()))
	if err != nil || name == "" {
		t.Fatalf("pipeName(pipe) = %q, %v", name, err)
	}
	if msysPtyName(name) {
		t.Errorf("an anonymous pipe is named %q, which looks like a pseudo-terminal", name)
	}
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := pipeName(syscall.Handle(f.Fd())); err == nil {
		t.Error("pipeName(regular file) = nil error")
	}
}

func closedFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f
}
