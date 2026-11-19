//go:build windows

package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
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
	// An anonymous pipe has no name.
	name, err := pipeName(syscall.Handle(r.Fd()))
	if err != nil || msysPtyName(name) {
		t.Fatalf("pipeName(anonymous pipe) = %q, %v", name, err)
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

// TestMsysPtyPipe creates a named pipe as mintty names the stdin of a Git
// Bash terminal and checks that it is recognised, as the stdin of a program,
// while a pipe of another name is not.
func TestMsysPtyPipe(t *testing.T) {
	create := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateNamedPipeW")
	named := func(name string) *os.File {
		t.Helper()
		p, err := syscall.UTF16PtrFromString(`\\.\pipe\` + name)
		if err != nil {
			t.Fatal(err)
		}
		const pipeAccessDuplex = 3
		h, _, err := create.Call(uintptr(unsafe.Pointer(p)), pipeAccessDuplex, 0, 1, 4096, 4096, 0, 0)
		if syscall.Handle(h) == syscall.InvalidHandle {
			t.Fatalf("CreateNamedPipe(%s): %v", name, err)
		}
		return os.NewFile(h, name)
	}
	suffix := fmt.Sprintf("%016x", time.Now().UnixNano())
	pty := named("msys-" + suffix + "-pty0-from-master")
	defer pty.Close()
	other := named("acr-test-" + suffix)
	defer other.Close()

	term := systemTerminal{}
	if got, err := pipeName(syscall.Handle(pty.Fd())); err != nil || got != `\msys-`+suffix+`-pty0-from-master` {
		t.Errorf("pipeName(pty pipe) = %q, %v", got, err)
	}
	if !term.isMsysPty(pty) {
		t.Error("isMsysPty = false for the pipe of an MSYS pseudo-terminal")
	}
	if term.isMsysPty(other) {
		t.Error("isMsysPty = true for a pipe with another name")
	}
}
