package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// docCommands are the commands the documentation shows, as the first words
// of a command line: the command and, for auth, its subcommand.
var docCommands = []string{
	"review", "next", "finalize", "show", "publish", "queue", "doctor", "check-criteria",
	"auth login", "auth status", "auth logout",
}

// helpUsage returns the usage lines that a command prints, without the
// "usage: " prefix and with the runs of blanks collapsed. An unknown flag
// makes every command print them.
func helpUsage(t *testing.T, command string) []string {
	t.Helper()
	var stderr bytes.Buffer
	args := append(strings.Fields(command), "--no-such-flag")
	run(args, env{stdin: strings.NewReader(""), stdout: &bytes.Buffer{}, stderr: &stderr, getenv: func(string) string { return "" }})
	var lines []string
	for _, l := range strings.Split(stderr.String(), "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "usage:"))
		if strings.HasPrefix(l, "agent-code-review "+strings.Fields(command)[0]+" ") {
			lines = append(lines, strings.Join(strings.Fields(l), " "))
		}
	}
	if len(lines) == 0 {
		t.Fatalf("%s: no usage line in %q", command, stderr.String())
	}
	if command == "auth login" || command == "auth status" || command == "auth logout" {
		for i, l := range lines {
			lines[i] = strings.Replace(l, "auth <command>", command, 1)
		}
	}
	return lines
}

var (
	flagPattern = regexp.MustCompile(`--[a-z][a-z-]*`)
	spanPattern = regexp.MustCompile("`([^`]+)`")
)

// commandOf returns the entry of docCommands that a line of the form
// "agent-code-review <command> ..." starts with.
func commandOf(line string) string {
	for _, c := range docCommands {
		if line == "agent-code-review "+c || strings.HasPrefix(line, "agent-code-review "+c+" ") {
			return c
		}
	}
	return ""
}

type docLine struct {
	file, text string
	fenced     bool
}

// docLines returns the command lines of README.md and docs/*.md: the lines
// of fenced blocks and the code spans of the text, and the commands of the
// first column of the table of README.md, which leave out the program name.
func docLines(t *testing.T) []docLine {
	t.Helper()
	root := filepath.Join("..", "..")
	files, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join(root, "README.md"))
	var out []docLine
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.ToSlash(strings.TrimPrefix(f, root+string(filepath.Separator)))
		fenced := false
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				fenced = !fenced
				continue
			}
			if fenced {
				if commandOf(trimmed) != "" {
					out = append(out, docLine{name, strings.Join(strings.Fields(trimmed), " "), true})
				}
				continue
			}
			if name == "README.md" && strings.HasPrefix(trimmed, "| `") {
				first, _, _ := strings.Cut(strings.TrimPrefix(trimmed, "|"), "|")
				for _, m := range spanPattern.FindAllStringSubmatch(first, -1) {
					if c := m[1]; c != "version" {
						out = append(out, docLine{name, "agent-code-review " + strings.Join(strings.Fields(c), " "), false})
					}
				}
				continue
			}
			for _, m := range spanPattern.FindAllStringSubmatch(trimmed, -1) {
				if commandOf(m[1]) != "" {
					out = append(out, docLine{name, strings.Join(strings.Fields(m[1]), " "), false})
				}
			}
		}
	}
	return out
}

// TestDocumentedCommandsMatchTheHelp keeps every command of README.md and
// docs/ as the binary prints it: its flags are in the usage of the command,
// and a command line with an argument or an option in brackets is one of
// the usage lines, word for word.
func TestDocumentedCommandsMatchTheHelp(t *testing.T) {
	usage := map[string][]string{}
	for _, c := range docCommands {
		usage[c] = helpUsage(t, c)
	}
	lines := docLines(t)
	if len(lines) == 0 {
		t.Fatal("no command in README.md and docs/")
	}
	for _, l := range lines {
		c := commandOf(l.text)
		allowed := strings.Join(usage[c], " ")
		for _, flag := range flagPattern.FindAllString(l.text, -1) {
			if !strings.Contains(allowed, flag) {
				t.Errorf("%s: %q uses %s, which the usage of %s does not have: %q", l.file, l.text, flag, c, usage[c])
			}
		}
		if l.fenced && strings.ContainsAny(l.text, "[<") && !slices.Contains(usage[c], l.text) {
			t.Errorf("%s: %q is not a usage line of the binary: %q", l.file, l.text, usage[c])
		}
	}
}

// TestEveryCommandIsDocumented keeps docs/ from missing a command: each
// one is in README.md and has a usage block in one document.
func TestEveryCommandIsDocumented(t *testing.T) {
	lines := docLines(t)
	for _, c := range docCommands {
		var inReadme, inDocs bool
		for _, l := range lines {
			if commandOf(l.text) != c {
				continue
			}
			inReadme = inReadme || l.file == "README.md"
			inDocs = inDocs || (l.file != "README.md" && l.fenced)
		}
		if !inReadme {
			t.Errorf("%s is not in the table of README.md", c)
		}
		if !inDocs && !strings.HasPrefix(c, "auth") {
			t.Errorf("%s has no command block in docs/", c)
		}
	}
}
