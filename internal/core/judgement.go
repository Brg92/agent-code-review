package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MarshalJSON implements json.Marshaler: an empty severity, the severity
// of a signal whose rule needs judgement, is null.
func (s Severity) MarshalJSON() ([]byte, error) {
	if s == "" {
		return []byte("null"), nil
	}
	return json.Marshal(string(s))
}

// UnmarshalJSON implements json.Unmarshaler; null is the empty severity.
func (s *Severity) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*s = ""
		return nil
	}
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*s = Severity(v)
	return nil
}

// Origin says whether the change request adds the line of a signal.
type Origin string

// The origins of a signal.
const (
	// OriginMR is a line the change request adds.
	OriginMR Origin = "mr"
	// OriginExisting is a line already on the target branch; its findings
	// are pre-existing.
	OriginExisting Origin = "existing"
)

// Signal is a place a mechanical rule points at, as listed in the work
// package.
type Signal struct {
	// ID is the short id the judgement decides the signal by: s1, s2, ...
	ID   string `json:"id"`
	Rule string `json:"rule"`
	// Severity is the rule's; empty, null in JSON, when the rule needs
	// judgement.
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	// Line is on the RIGHT side.
	Line           int    `json:"line"`
	Column         int    `json:"column"`
	Origin         Origin `json:"origin"`
	NeedsJudgement bool   `json:"needsJudgement"`
}

// RuleInfo is what finalizing needs to know of a rule: its default
// severity, whether its signals need judgement, and the text of the
// findings made from its signals.
type RuleInfo struct {
	Severity        Severity
	NeedsJudgement  bool
	Title, Why, Fix string
}

// ChangedFile is a changed file of a change request with the lines of its
// two versions, without line endings. Base or Head is nil when that version
// does not exist or was not read.
type ChangedFile struct {
	Diff       FileDiff
	Base, Head []string
}

// lines returns the lines of the side, and whether they are known.
func (f ChangedFile) lines(side Side) ([]string, bool) {
	if side == Left {
		return f.Base, f.Base != nil
	}
	return f.Head, f.Head != nil
}

// SplitLines splits the contents of a file into lines without their line
// endings; a final line ending does not start another line.
func SplitLines(src string) []string {
	lines := strings.Split(src, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// Review is what one change request is finalized with: its key and head,
// its changed files, its rules and the signals of its work package.
type Review struct {
	CR      Key
	HeadSHA string
	Files   []ChangedFile
	// Rules are the rules that apply to the change request together with
	// the core rules, by id. A finding may only apply one of them.
	Rules   map[string]RuleInfo
	Signals []Signal
}

func (r Review) file(path string) (ChangedFile, bool) {
	for _, f := range r.Files {
		if f.Diff.Path == path {
			return f, true
		}
	}
	return ChangedFile{}, false
}

// Reference is another place in the code a finding relies on, possibly in
// another project.
type Reference struct {
	Project string `json:"project"`
	Ref     string `json:"ref"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
}

// JudgedFinding is a finding as the agent writes it in the judgement. Its
// JSON form is defined by schemas/judgement.schema.json.
type JudgedFinding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Side     Side     `json:"side"`
	Line     int      `json:"line"`
	// StartLine is the first line of a finding over several lines; 0 for a
	// single line.
	StartLine int    `json:"startLine,omitempty"`
	Title     string `json:"title"`
	Why       string `json:"why"`
	Fix       string `json:"fix"`
	// Suggestion replaces the lines from StartLine (or Line) to Line; nil
	// when there is none. An empty suggestion removes the lines.
	Suggestion *string     `json:"suggestion,omitempty"`
	References []Reference `json:"references"`
	Crash      bool        `json:"crash,omitempty"`
}

// Decision is what the judgement does with a signal.
type Decision string

// The decisions.
const (
	Confirm Decision = "confirm"
	Dismiss Decision = "dismiss"
)

// SignalDecision confirms or dismisses one signal of the work package.
type SignalDecision struct {
	ID       string   `json:"id"`
	Decision Decision `json:"decision"`
	// Severity of a confirmed signal: required when its rule needs
	// judgement, an override otherwise.
	Severity Severity `json:"severity,omitempty"`
	Crash    bool     `json:"crash,omitempty"`
	// Reason is required to dismiss a signal.
	Reason string `json:"reason,omitempty"`
}

// Question is a question for the author, shown in the report and never
// published.
type Question struct {
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
	Text string `json:"text"`
}

// Judgement is what the agent concludes about one change request. Its JSON
// form is defined by schemas/judgement.schema.json.
type Judgement struct {
	Schema      int              `json:"schema"`
	CR          string           `json:"cr"`
	HeadSHA     string           `json:"headSha"`
	Summary     string           `json:"summary,omitempty"`
	Findings    []JudgedFinding  `json:"findings"`
	PreExisting []JudgedFinding  `json:"preExisting"`
	Signals     []SignalDecision `json:"signals,omitempty"`
	Questions   []Question       `json:"questions,omitempty"`
	Notes       []string         `json:"notes,omitempty"`
	NotVerified []string         `json:"notVerified,omitempty"`
}

// DecodeJudgement decodes a judgement, rejecting unknown fields. The values
// are checked by ValidateJudgement.
func DecodeJudgement(data []byte) (Judgement, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var j Judgement
	if err := dec.Decode(&j); err != nil {
		return Judgement{}, fmt.Errorf("decode judgement: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Judgement{}, errors.New("decode judgement: trailing data after the document")
	}
	return j, nil
}

// Limits of the report fields of a judgement.
const (
	MaxNotes      = 3
	MaxNoteLength = 200
)

// FieldError is one problem of a judgement: the JSON pointer of the field
// and what is wrong with it.
type FieldError struct {
	Pointer string
	Message string
}

// ValidationError lists every problem of a judgement, in document order.
type ValidationError []FieldError

func (e ValidationError) Error() string {
	var b strings.Builder
	b.WriteString("invalid judgement:")
	for _, f := range e {
		b.WriteString("\n" + f.Pointer + ": " + f.Message)
	}
	return b.String()
}
