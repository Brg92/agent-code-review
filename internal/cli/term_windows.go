//go:build windows

package cli

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

// enableEchoInput is the console mode flag that echoes typed characters.
const enableEchoInput = 0x0004

var (
	kernel32                         = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleMode               = kernel32.NewProc("SetConsoleMode")
	procGetFileInformationByHandleEx = kernel32.NewProc("GetFileInformationByHandleEx")
)

// fileNameInfoClass is FileNameInfo, the class of GetFileInformationByHandleEx
// that returns the name of the file behind a handle.
const fileNameInfoClass = 2

// fileNameInfo is FILE_NAME_INFO with room for a name of 512 characters.
type fileNameInfo struct {
	Length uint32 // bytes of Name that are used
	Name   [512]uint16
}

// pipeName returns the name Windows gives the pipe behind h, such as
// \msys-<hash>-pty0-from-master for the stdin of a Git Bash terminal.
func pipeName(h syscall.Handle) (string, error) {
	if t, err := syscall.GetFileType(h); err != nil || t != syscall.FILE_TYPE_PIPE {
		return "", syscall.EINVAL
	}
	var info fileNameInfo
	if ok, _, err := procGetFileInformationByHandleEx.Call(uintptr(h), fileNameInfoClass, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)); ok == 0 {
		return "", err
	}
	n := int(info.Length / 2)
	if n > len(info.Name) {
		return "", syscall.EINVAL
	}
	return syscall.UTF16ToString(info.Name[:n]), nil
}

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

// isMsysPty reports whether r is the stdin of a Git Bash, MSYS2 or Cygwin
// terminal, which Windows shows as a pipe.
func (systemTerminal) isMsysPty(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	name, err := pipeName(syscall.Handle(f.Fd()))
	return err == nil && msysPtyName(name)
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
