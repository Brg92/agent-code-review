//go:build !windows

package cli

import (
	"io"
	"os"
	"os/exec"
	"strings"
)

// systemTerminal hides input with stty, run without a shell on the
// terminal itself.
type systemTerminal struct{}

func (systemTerminal) isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	// /dev/null is a character device too; only a terminal has settings.
	_, err = stty(f, "-g")
	return err == nil
}

func (systemTerminal) disableEcho(r io.Reader) (func() error, error) {
	f, _ := r.(*os.File)
	saved, err := stty(f, "-g")
	if err != nil {
		return nil, err
	}
	if _, err := stty(f, "-echo"); err != nil {
		return nil, err
	}
	return func() error {
		_, err := stty(f, strings.TrimSpace(saved))
		return err
	}, nil
}

// stty runs stty with the terminal f as its stdin.
func stty(f *os.File, args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = f
	out, err := cmd.Output()
	return string(out), err
}
