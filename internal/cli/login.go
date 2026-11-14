package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/Brg92/agent-code-review/internal/auth"
)

// maxTokenBytes bounds the line read as a token.
const maxTokenBytes = 4096

// terminal hides typed input.
type terminal interface {
	// isTerminal reports whether r is an interactive terminal.
	isTerminal(r io.Reader) bool
	// disableEcho turns echo off on r and returns the function restoring it.
	disableEcho(r io.Reader) (restore func() error, err error)
}

var errTokenTooLong = fmt.Errorf("the token is too long (more than %d bytes)", maxTokenBytes)

// readLine reads one line, without its line ending.
func readLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(r, maxTokenBytes+2)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) > maxTokenBytes {
		return "", errTokenTooLong
	}
	return line, nil
}

type lineResult struct {
	line string
	err  error
}

// readToken reads the token from stdin, with echo off at a terminal. Echo is
// restored on every path: success, read error, interrupt and panic.
func readToken(e env, host string) (string, int) {
	if !e.term.isTerminal(e.stdin) {
		line, err := readLine(e.stdin)
		return line, readFailure(e, err)
	}
	fmt.Fprintf(e.stderr, "Token for %s (input hidden): ", host)
	restore, err := e.term.disableEcho(e.stdin)
	if err != nil {
		fmt.Fprintln(e.stderr)
		return "", fail(e, exitUnexpected, "cannot hide the input on this terminal: %v; pipe the token instead", err)
	}
	var once sync.Once
	restoreEcho := func() { once.Do(func() { _ = restore() }) }
	defer restoreEcho()

	sigs := make(chan os.Signal, 1)
	stop := e.notify(sigs)
	defer stop()
	lines := make(chan lineResult, 1)
	go func() {
		line, err := readLine(e.stdin)
		lines <- lineResult{line, err}
	}()
	select {
	case l := <-lines:
		restoreEcho()
		fmt.Fprintln(e.stderr)
		return l.line, readFailure(e, l.err)
	case <-sigs:
		restoreEcho()
		fmt.Fprintln(e.stderr)
		return "", fail(e, exitInterrupted, "interrupted")
	}
}

func readFailure(e env, err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errTokenTooLong):
		return fail(e, exitAuth, "%v", err)
	}
	return fail(e, exitUnexpected, "reading the token from stdin: %v", err)
}

func authLogin(e env, host string) int {
	ctx := context.Background()
	r := e.resolver()
	// Check the helpers before asking for the token, so nobody types it for nothing.
	check, err := r.CheckHelpers(ctx, host)
	if err != nil {
		return fail(e, exitAuth, "%s: %v", host, err)
	}
	if err := check.Err(); err != nil {
		return fail(e, exitUnsafeStorage, "%s: token not stored: %v", host, err)
	}
	token, code := readToken(e, host)
	if code != exitOK {
		return code
	}
	check, err = r.Save(ctx, host, token)
	switch {
	case errors.Is(err, auth.ErrNoToken):
		return fail(e, exitAuth, "%s: no token on stdin", host)
	case errors.Is(err, auth.ErrNoHelper), errors.Is(err, auth.ErrPlainTextHelper):
		return fail(e, exitUnsafeStorage, "%s: token not stored: %v", host, err)
	case err != nil:
		return fail(e, exitAuth, "%s: token not stored: %v", host, err)
	}
	for _, w := range check.Warnings() {
		fmt.Fprintf(e.stderr, "agent-code-review: warning: %s\n", w)
	}
	if v := auth.EnvVar(host); v != "" && strings.TrimSpace(e.getenv(v)) != "" {
		fmt.Fprintf(e.stderr, "agent-code-review: warning: %s is set and takes precedence over the stored token\n", v)
	}
	fmt.Fprintf(e.stdout, "%s: token stored in the git credential helper (username %s)\n", host, auth.Username)
	return exitOK
}
