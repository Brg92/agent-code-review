//go:build windows

package cli

import (
	"os"
	"strings"
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
