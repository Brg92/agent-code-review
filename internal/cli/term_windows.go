//go:build windows

package cli

import (
	"io"
	"os"
	"syscall"
)

// enableEchoInput is the console mode flag that echoes typed characters.
const enableEchoInput = 0x0004

var procSetConsoleMode = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")

// systemTerminal hides input by clearing the console echo mode.
type systemTerminal struct{}

func consoleHandle(r io.Reader) (syscall.Handle, uint32, bool) {
	f, ok := r.(*os.File)
	if !ok {
		return 0, 0, false
	}
	h := syscall.Handle(f.Fd())
	var mode uint32
	if syscall.GetConsoleMode(h, &mode) != nil {
		return 0, 0, false
	}
	return h, mode, true
}

func setConsoleMode(h syscall.Handle, mode uint32) error {
	if ok, _, err := procSetConsoleMode.Call(uintptr(h), uintptr(mode)); ok == 0 {
		return err
	}
	return nil
}

func (systemTerminal) isTerminal(r io.Reader) bool {
	_, _, ok := consoleHandle(r)
	return ok
}

func (systemTerminal) disableEcho(r io.Reader) (func() error, error) {
	h, mode, ok := consoleHandle(r)
	if !ok {
		return nil, syscall.EINVAL
	}
	if err := setConsoleMode(h, mode&^enableEchoInput); err != nil {
		return nil, err
	}
	return func() error { return setConsoleMode(h, mode) }, nil
}
