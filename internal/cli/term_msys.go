package cli

import "regexp"

// msysPtyPattern matches the name of the pipes that Git Bash (mintty), MSYS2
// and Cygwin give a terminal: the program then sees a pipe on stdin, not a
// console, and what is typed is echoed by the terminal itself.
var msysPtyPattern = regexp.MustCompile(`^\\(?:cygwin|msys)-[0-9a-f]+-pty[0-9]+-(?:from|to)-master$`)

// msysPtyName reports whether name, as Windows names a pipe, is one of the
// pipes of an MSYS or Cygwin pseudo-terminal.
func msysPtyName(name string) bool { return msysPtyPattern.MatchString(name) }

// msysTerminalHint is what the user is told to do instead.
const msysTerminalHint = "stdin is a Git Bash (MSYS or Cygwin) terminal, which cannot hide the token while it is typed; " +
	"run the command from PowerShell or cmd, or pipe the token from a password manager"
