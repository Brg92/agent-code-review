package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Side is a side of a diff.
type Side string

// The sides of a diff.
const (
	// Left is the target branch version of a file.
	Left Side = "LEFT"
	// Right is the change request version of a file.
	Right Side = "RIGHT"
)

// Valid reports whether s is a known side.
func (s Side) Valid() bool { return s == Left || s == Right }

// FileStatus is the kind of change a change request makes to a file.
type FileStatus string

// The file statuses.
const (
	FileAdded    FileStatus = "added"
	FileRemoved  FileStatus = "removed"
	FileModified FileStatus = "modified"
	FileRenamed  FileStatus = "renamed"
	FileCopied   FileStatus = "copied"
)

// Valid reports whether s is a known file status.
func (s FileStatus) Valid() bool {
	switch s {
	case FileAdded, FileRemoved, FileModified, FileRenamed, FileCopied:
		return true
	}
	return false
}

// HasSide reports whether a file with this status exists on the side: an
// added file has no LEFT version and a removed file no RIGHT one.
func (s FileStatus) HasSide(side Side) bool {
	switch {
	case s == FileAdded && side == Left, s == FileRemoved && side == Right:
		return false
	}
	return side.Valid()
}

// LineKind is the kind of a line of a hunk.
type LineKind string

// The kinds of line.
const (
	LineAdded   LineKind = "added"
	LineRemoved LineKind = "removed"
	LineContext LineKind = "context"
)

// DiffLine is one line of a hunk.
type DiffLine struct {
	Kind LineKind
	// Old is the line number on the LEFT side; 0 for an added line.
	Old int
	// New is the line number on the RIGHT side; 0 for a removed line.
	New int
	// Text is the line without its diff marker and line ending.
	Text string
	// NoNewline is set on the last line of a side that has no newline at
	// the end of the file.
	NoNewline bool
}

// Hunk is one hunk of a unified diff.
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	// Section is the text after the closing "@@" of the header, often the
	// enclosing declaration.
	Section string
	Lines   []DiffLine
}

// FileDiff is the change a change request makes to one file. A file whose
// patch the platform omitted, such as a binary or oversized file, has no
// hunks.
type FileDiff struct {
	// Path is the path in the change request; for a removed file, its old
	// path.
	Path string
	// OldPath is the previous path of a renamed or copied file.
	OldPath string
	Status  FileStatus
	Hunks   []Hunk
}

// NewFileDiff parses the unified diff of one file. An empty patch gives a
// file without hunks.
func NewFileDiff(path, oldPath string, status FileStatus, patch string) (FileDiff, error) {
	if !status.Valid() {
		return FileDiff{}, fmt.Errorf("%s: unknown file status %q", path, string(status))
	}
	hunks, err := ParsePatch(patch)
	if err != nil {
		return FileDiff{}, fmt.Errorf("%s: %w", path, err)
	}
	return FileDiff{Path: path, OldPath: oldPath, Status: status, Hunks: hunks}, nil
}

// Visible reports whether the diff shows at least one line of the file.
func (d FileDiff) Visible() bool {
	for _, h := range d.Hunks {
		if len(h.Lines) > 0 {
			return true
		}
	}
	return false
}

// lineAt finds the line numbered n on the side: an added or context line
// on the RIGHT side, a removed or context line on the LEFT side. It
// returns the index of its hunk.
func (d FileDiff) lineAt(side Side, n int) (int, DiffLine, bool) {
	for i, h := range d.Hunks {
		for _, l := range h.Lines {
			if side == Right && l.New == n || side == Left && l.Old == n {
				return i, l, true
			}
		}
	}
	return 0, DiffLine{}, false
}

var hunkHeader = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@(.*)$`)

// fileHeaders start the lines of a git diff that come before the first
// hunk of a file.
var fileHeaders = []string{
	"diff ", "index ", "--- ", "+++ ", "old mode ", "new mode ", "new file mode ", "deleted file mode ",
	"similarity index ", "dissimilarity index ", "rename from ", "rename to ", "copy from ", "copy to ", "Binary files ",
}

// ParsePatch parses a unified diff of one file: hunks, optionally preceded
// by git file headers. Each hunk must hold exactly the lines its header
// counts.
func ParsePatch(patch string) ([]Hunk, error) {
	if patch == "" {
		return nil, nil
	}
	lines := strings.Split(patch, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var (
		hunks            []Hunk
		cur              *Hunk
		headerAt         int
		oldNext, newNext int
		oldLeft, newLeft int
		last             *DiffLine
	)
	end := func() error {
		if cur != nil && (oldLeft > 0 || newLeft > 0) {
			return fmt.Errorf("line %d: hunk ends early: %d old and %d new lines missing", headerAt, oldLeft, newLeft)
		}
		return nil
	}
	for i, raw := range lines {
		n := i + 1
		line := strings.TrimSuffix(raw, "\r")
		if strings.HasPrefix(line, "@@") {
			if err := end(); err != nil {
				return nil, err
			}
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				return nil, fmt.Errorf("line %d: malformed hunk header %q", n, line)
			}
			h := Hunk{OldStart: atoi(m[1]), OldLines: count(m[2]), NewStart: atoi(m[3]), NewLines: count(m[4]), Section: m[5]}
			hunks = append(hunks, h)
			cur, headerAt, last = &hunks[len(hunks)-1], n, nil
			oldNext, newNext, oldLeft, newLeft = h.OldStart, h.NewStart, h.OldLines, h.NewLines
			continue
		}
		if strings.HasPrefix(line, `\`) {
			if last == nil {
				return nil, fmt.Errorf("line %d: no line before the no-newline marker", n)
			}
			last.NoNewline = true
			continue
		}
		if cur == nil || oldLeft == 0 && newLeft == 0 {
			if cur == nil && isFileHeader(line) {
				continue
			}
			return nil, fmt.Errorf("line %d: outside a hunk: %q", n, line)
		}
		var l DiffLine
		switch {
		case strings.HasPrefix(line, "+"):
			if newLeft == 0 {
				return nil, fmt.Errorf("line %d: more added lines than the hunk header says", n)
			}
			l = DiffLine{Kind: LineAdded, New: newNext, Text: line[1:]}
			newNext, newLeft = newNext+1, newLeft-1
		case strings.HasPrefix(line, "-"):
			if oldLeft == 0 {
				return nil, fmt.Errorf("line %d: more removed lines than the hunk header says", n)
			}
			l = DiffLine{Kind: LineRemoved, Old: oldNext, Text: line[1:]}
			oldNext, oldLeft = oldNext+1, oldLeft-1
		case line == "" || strings.HasPrefix(line, " "):
			// A blank context line may have lost its leading space.
			if oldLeft == 0 || newLeft == 0 {
				return nil, fmt.Errorf("line %d: more context lines than the hunk header says", n)
			}
			l = DiffLine{Kind: LineContext, Old: oldNext, New: newNext, Text: strings.TrimPrefix(line, " ")}
			oldNext, newNext, oldLeft, newLeft = oldNext+1, newNext+1, oldLeft-1, newLeft-1
		default:
			return nil, fmt.Errorf("line %d: unknown line %q", n, line)
		}
		cur.Lines = append(cur.Lines, l)
		last = &cur.Lines[len(cur.Lines)-1]
	}
	if err := end(); err != nil {
		return nil, err
	}
	return hunks, nil
}

func isFileHeader(line string) bool {
	for _, p := range fileHeaders {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

// atoi converts digits the hunk header pattern already matched.
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// count is a line count of a hunk header; an omitted count is one.
func count(s string) int {
	if s == "" {
		return 1
	}
	return atoi(s)
}
