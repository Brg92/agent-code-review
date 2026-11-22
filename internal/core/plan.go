package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// PlanSchemaVersion is the version of the plan and published formats.
const PlanSchemaVersion = 1

// Choice is what the user asked publish to write.
type Choice string

// The choices.
const (
	// Comments: the inline comments, the note that nothing blocks and the
	// Related Changes tick.
	Comments Choice = "comments"
	// Issues: the issues of MAJOR, MINOR and existing-code findings.
	Issues Choice = "issues"
	// Both: everything.
	Both Choice = "both"
)

// Valid reports whether c is a known choice.
func (c Choice) Valid() bool { return c == Comments || c == Issues || c == Both }

// WantsComments reports whether the choice writes comments.
func (c Choice) WantsComments() bool { return c == Comments || c == Both }

// WantsIssues reports whether the choice writes issues.
func (c Choice) WantsIssues() bool { return c == Issues || c == Both }

// ParseChoice reads the argument of publish --plan.
func ParseChoice(s string) (Choice, error) {
	if c := Choice(s); c.Valid() {
		return c, nil
	}
	return "", fmt.Errorf("%q is not comments, issues or both", s)
}

// ActionKind is the kind of write an action is.
type ActionKind string

// The kinds of action.
const (
	ActionReviewComment  ActionKind = "review-comment"
	ActionSummaryComment ActionKind = "summary-comment"
	ActionIssueCreate    ActionKind = "issue-create"
	ActionIssueUpdate    ActionKind = "issue-update"
	ActionLabelCreate    ActionKind = "label-create"
	ActionRelatedTick    ActionKind = "related-tick"
)

// Valid reports whether k is a known kind.
func (k ActionKind) Valid() bool {
	switch k {
	case ActionReviewComment, ActionSummaryComment, ActionIssueCreate, ActionIssueUpdate, ActionLabelCreate, ActionRelatedTick:
		return true
	}
	return false
}

// PlanStatus says why something is not written, or what happened to an
// action. It is the status of a skipped entry of a plan and of an outcome
// in published.json.
type PlanStatus string

// The statuses. The first group is what a plan may skip; the second adds
// what an outcome says besides.
const (
	// StatusAlreadyPosted: a trusted fingerprint is already on the change
	// request or in the issue.
	StatusAlreadyPosted PlanStatus = "already-posted"
	// StatusIgnoredUntrusted: the fingerprint was written by an account
	// that is not trusted, so the write goes ahead.
	StatusIgnoredUntrusted PlanStatus = "ignored-untrusted"
	// StatusHeadChanged: the change request has new commits.
	StatusHeadChanged PlanStatus = "head-changed"
	// StatusNotOpen: the change request is closed or merged.
	StatusNotOpen PlanStatus = "not-open"
	// StatusOwnCR: the account of the token opened the change request.
	StatusOwnCR PlanStatus = "own-cr"
	// StatusRejected: the platform refused the position of a comment.
	StatusRejected PlanStatus = "rejected"
	// StatusSecretBlocked: a text matches a secret pattern.
	StatusSecretBlocked PlanStatus = "secret-blocked"
	// StatusTooLong: a body is longer than the platform accepts.
	StatusTooLong PlanStatus = "too-long"
	// StatusNotEligible: the Related Changes entry is not ticked.
	StatusNotEligible PlanStatus = "not-eligible"
	// StatusNoAnchor: a BLOCKER of the change request that no line of the
	// diff can carry.
	StatusNoAnchor PlanStatus = "no-anchor"

	// StatusPosted: the action was written.
	StatusPosted PlanStatus = "posted"
	// StatusPostedUnassigned: an issue was written, without the assignee,
	// who cannot be assigned in the project.
	StatusPostedUnassigned PlanStatus = "posted-unassigned"
	// StatusFailed: the platform or the network failed; the action has no
	// final outcome and a later apply tries it again.
	StatusFailed PlanStatus = "failed"
)

var planStatuses = []PlanStatus{StatusAlreadyPosted, StatusIgnoredUntrusted, StatusHeadChanged, StatusNotOpen, StatusOwnCR, StatusRejected,
	StatusSecretBlocked, StatusTooLong, StatusNotEligible, StatusNoAnchor}

// ValidInPlan reports whether s may be the status of a skipped entry.
func (s PlanStatus) ValidInPlan() bool {
	for _, v := range planStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// Valid reports whether s is a known status of an outcome.
func (s PlanStatus) Valid() bool {
	return s.ValidInPlan() || s == StatusPosted || s == StatusPostedUnassigned || s == StatusFailed
}

// Final reports whether an outcome with status s is the last word on its
// action: everything but a failure, which a later apply tries again.
func (s PlanStatus) Final() bool { return s.Valid() && s != StatusFailed }

// MarshalText implements encoding.TextMarshaler.
func (s PlanStatus) MarshalText() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("unknown status %q", string(s))
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (s *PlanStatus) UnmarshalText(b []byte) error {
	v := PlanStatus(b)
	if !v.Valid() {
		return fmt.Errorf("unknown status %q", string(b))
	}
	*s = v
	return nil
}

// ReviewLabel is the label every issue publish writes carries.
var ReviewLabel = Label{Name: "review", Color: "5319e7", Description: "Findings of a code review"}

// Label is the label the issues carry.
type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// Action is one write of a plan. Which fields are set depends on the kind;
// Validate says which.
type Action struct {
	ID   int        `json:"id"`
	Kind ActionKind `json:"kind"`
	// Severity and Rules say what an inline comment or an issue is about.
	Severity Severity `json:"severity,omitempty"`
	Rules    []string `json:"rules,omitempty"`
	// Body is the text to post, empty for a label and a tick.
	Body string `json:"body"`
	// FindingID is the fingerprint of the finding an inline comment is
	// about; Fingerprint the one that finds a note or an issue again.
	FindingID   string `json:"findingId,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	// Path, Side, StartLine and Line are where an inline comment goes.
	Path      string `json:"path,omitempty"`
	Side      Side   `json:"side,omitempty"`
	StartLine int    `json:"startLine,omitempty"`
	Line      int    `json:"line,omitempty"`
	// Project is the project key of an issue or a label; Number the issue
	// an update rewrites; Title and Assignee those of the issue.
	Project  string `json:"project,omitempty"`
	Number   int    `json:"number,omitempty"`
	Title    string `json:"title,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	// Label is what a label-create makes.
	Label *Label `json:"label,omitempty"`
	// Parent is the change request whose Related Changes a tick edits and
	// Entry the entry it ticks: the change request of the plan.
	Parent *Key `json:"parent,omitempty"`
	Entry  *Key `json:"entry,omitempty"`
}

// PlanSkip is something a plan does not write, or notes it found.
type PlanSkip struct {
	Status    PlanStatus `json:"status"`
	Kind      ActionKind `json:"kind,omitempty"`
	FindingID string     `json:"findingId,omitempty"`
	Target    string     `json:"target,omitempty"`
	Rule      string     `json:"rule,omitempty"`
	Path      string     `json:"path,omitempty"`
	Line      int        `json:"line,omitempty"`
	// Pattern is the name of the secret pattern that matched, never the
	// text.
	Pattern string `json:"pattern,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// Plan is what publish --plan proposes to write for one change request.
// Its JSON form is defined by schemas/plan.schema.json.
type Plan struct {
	Schema    int       `json:"schema"`
	RunID     string    `json:"runId"`
	CR        Key       `json:"cr"`
	HeadSHA   string    `json:"headSha"`
	CreatedAt time.Time `json:"-"`
	// Publish is what the user asked for.
	Publish Choice     `json:"publish"`
	Actions []Action   `json:"actions"`
	Skipped []PlanSkip `json:"skipped"`
}

// planFile is the JSON form of a Plan: the same fields with the time as
// text, in the order of the file.
type planFile struct {
	Schema    int        `json:"schema"`
	RunID     string     `json:"runId"`
	CR        Key        `json:"cr"`
	HeadSHA   string     `json:"headSha"`
	CreatedAt string     `json:"createdAt"`
	Publish   Choice     `json:"publish"`
	Actions   []Action   `json:"actions"`
	Skipped   []PlanSkip `json:"skipped"`
}

var (
	runIDPattern       = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
	findingIDPattern   = regexp.MustCompile(`^acr:v1:` + rulePart + `:` + `(?:[^/\\.][^/\\]*|\.[^/\\.][^/\\]*)(?:/(?:[^/\\.][^/\\]*|\.[^/\\.][^/\\]*))*` + `:[0-9a-f]{12}$`)
	fingerprintPattern = regexp.MustCompile(`^acr:v1:\S+$`)
	colorPattern       = regexp.MustCompile(`^[0-9a-f]{6}$`)
	sha256Pattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	timePattern        = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?Z$`)
)

// ParseProjectKey parses a project key, <forge>:<host>/<project>, as a Key
// without a number.
func ParseProjectKey(s string) (Key, error) {
	k, err := ParseKey(s + "#1")
	if err != nil {
		return Key{}, fmt.Errorf("invalid project key %q", s)
	}
	k.Number = 0
	return k, nil
}

// FormatTime is the form timestamps have in the plan and published files.
func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	if !timePattern.MatchString(s) {
		return time.Time{}, fmt.Errorf("%q is not a UTC RFC 3339 time", s)
	}
	return time.Parse(time.RFC3339Nano, s)
}

// PlanError reports a plan that breaks its contract.
type PlanError struct{ Problem string }

func (e *PlanError) Error() string { return "invalid plan: " + e.Problem }

// Validate returns a *PlanError for a plan that breaks its contract.
func (p Plan) Validate() error {
	fail := func(format string, args ...any) error { return &PlanError{fmt.Sprintf(format, args...)} }
	switch {
	case p.Schema != PlanSchemaVersion:
		return fail("schema is %d, want %d", p.Schema, PlanSchemaVersion)
	case !runIDPattern.MatchString(p.RunID):
		return fail("invalid run id %q", p.RunID)
	case p.CR.IsZero():
		return fail("missing cr")
	case !shaPattern.MatchString(p.HeadSHA):
		return fail("headSha %q is not 40 lowercase hexadecimal characters", p.HeadSHA)
	case p.CreatedAt.IsZero():
		return fail("missing createdAt")
	case !p.Publish.Valid():
		return fail("publish %q is not comments, issues or both", string(p.Publish))
	case p.Actions == nil || p.Skipped == nil:
		return fail("actions and skipped are arrays, never null")
	}
	for i, a := range p.Actions {
		if err := a.validate(); err != nil {
			return fail("action %d: %v", i+1, err)
		}
		if a.ID != i+1 {
			return fail("action %d has id %d: ids run from 1 in order", i+1, a.ID)
		}
	}
	for i, s := range p.Skipped {
		if err := s.validate(); err != nil {
			return fail("skipped %d: %v", i+1, err)
		}
	}
	return nil
}

func (a Action) validate() error {
	if !a.Kind.Valid() {
		return fmt.Errorf("unknown kind %q", string(a.Kind))
	}
	need := func(ok bool, field string) error {
		if !ok {
			return fmt.Errorf("%s needs %s", a.Kind, field)
		}
		return nil
	}
	checks := []error{}
	switch a.Kind {
	case ActionReviewComment:
		checks = append(checks, need(findingIDPattern.MatchString(a.FindingID), "a findingId"), need(a.Severity.Valid() && a.Severity != Info, "a severity"),
			need(len(a.Rules) > 0, "rules"), need(a.Path != "" && relativePath.MatchString(a.Path), "a path"), need(a.Side.Valid(), "a side"), need(a.Line >= 1, "a line"))
		if a.StartLine < 0 || a.StartLine > a.Line {
			checks = append(checks, fmt.Errorf("startLine %d is not between 1 and line %d", a.StartLine, a.Line))
		}
	case ActionSummaryComment:
		checks = append(checks, need(fingerprintPattern.MatchString(a.Fingerprint), "a fingerprint"))
	case ActionIssueCreate, ActionIssueUpdate:
		checks = append(checks, need(fingerprintPattern.MatchString(a.Fingerprint), "a fingerprint"), need(a.Severity.Valid() && a.Severity != Info, "a severity"),
			need(len(a.Rules) > 0, "rules"), need(isProjectKey(a.Project), "a project"), need(strings.TrimSpace(a.Title) != "", "a title"))
		if a.Kind == ActionIssueUpdate {
			checks = append(checks, need(a.Number >= 1, "a number"))
		}
	case ActionLabelCreate:
		checks = append(checks, need(isProjectKey(a.Project), "a project"), need(a.Label != nil, "a label"))
		if a.Label != nil {
			checks = append(checks, need(strings.TrimSpace(a.Label.Name) != "" && colorPattern.MatchString(a.Label.Color), "a label with a name and a six-digit colour"))
		}
	case ActionRelatedTick:
		checks = append(checks, need(a.Parent != nil && !a.Parent.IsZero(), "a parent"), need(a.Entry != nil && !a.Entry.IsZero(), "an entry"))
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	return nil
}

func isProjectKey(s string) bool { _, err := ParseProjectKey(s); return err == nil }

func (s PlanSkip) validate() error {
	switch {
	case !s.Status.ValidInPlan():
		return fmt.Errorf("status %q cannot be skipped", string(s.Status))
	case s.Kind != "" && !s.Kind.Valid():
		return fmt.Errorf("unknown kind %q", string(s.Kind))
	case s.FindingID != "" && !findingIDPattern.MatchString(s.FindingID):
		return fmt.Errorf("findingId %q is not a fingerprint", s.FindingID)
	case s.Line < 0:
		return errors.New("line is negative")
	case s.Path != "" && !relativePath.MatchString(s.Path):
		return fmt.Errorf("path %q is not a relative path", s.Path)
	}
	return nil
}

// EncodePlan returns the JSON of a plan as it is stored in the run's work
// directory: two-space indent, no HTML escaping, a final newline. The
// SHA-256 of these bytes is what state.json records.
func EncodePlan(p Plan) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	f := planFile{Schema: p.Schema, RunID: p.RunID, CR: p.CR, HeadSHA: p.HeadSHA, CreatedAt: FormatTime(p.CreatedAt), Publish: p.Publish, Actions: p.Actions, Skipped: p.Skipped}
	return encodeJSON(f)
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return buf.Bytes(), nil
}

// DecodePlan decodes a plan, rejecting unknown fields, trailing data and a
// plan that breaks its contract.
func DecodePlan(data []byte) (Plan, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f planFile
	if err := dec.Decode(&f); err != nil {
		return Plan{}, fmt.Errorf("decode plan: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Plan{}, errors.New("decode plan: trailing data after the document")
	}
	at, err := parseTime(f.CreatedAt)
	if err != nil {
		return Plan{}, fmt.Errorf("decode plan: createdAt: %w", err)
	}
	p := Plan{Schema: f.Schema, RunID: f.RunID, CR: f.CR, HeadSHA: f.HeadSHA, CreatedAt: at, Publish: f.Publish, Actions: f.Actions, Skipped: f.Skipped}
	if err := p.Validate(); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// Hash is the SHA-256 of data as 64 lowercase hexadecimal characters.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Outcome is what happened to one action of a plan.
type Outcome struct {
	// ID is the id of the action.
	ID     int        `json:"id"`
	Status PlanStatus `json:"status"`
	// Number is the issue an action created or updated.
	Number int `json:"number,omitempty"`
	// Detail says what else there is to know, in one line, with no secret.
	Detail string `json:"detail,omitempty"`
}

// Published is the outcome of the actions of one plan, written as soon as
// each is known. Its JSON form is defined by schemas/published.schema.json.
type Published struct {
	Schema int    `json:"schema"`
	RunID  string `json:"runId"`
	CR     Key    `json:"cr"`
	// PlanSHA256 is the hash of the plan the outcomes are for.
	PlanSHA256 string `json:"planSha256"`
	// Blocked is why nothing was written for the change request, if so.
	Blocked   PlanStatus `json:"blocked,omitempty"`
	UpdatedAt time.Time  `json:"-"`
	Outcomes  []Outcome  `json:"outcomes"`
}

type publishedFile struct {
	Schema     int        `json:"schema"`
	RunID      string     `json:"runId"`
	CR         Key        `json:"cr"`
	PlanSHA256 string     `json:"planSha256"`
	Blocked    PlanStatus `json:"blocked,omitempty"`
	UpdatedAt  string     `json:"updatedAt"`
	Outcomes   []Outcome  `json:"outcomes"`
}

// Validate returns a *PlanError for outcomes that break their contract.
func (p Published) Validate() error {
	fail := func(format string, args ...any) error {
		return &PlanError{"published: " + fmt.Sprintf(format, args...)}
	}
	switch {
	case p.Schema != PlanSchemaVersion:
		return fail("schema is %d, want %d", p.Schema, PlanSchemaVersion)
	case !runIDPattern.MatchString(p.RunID):
		return fail("invalid run id %q", p.RunID)
	case p.CR.IsZero():
		return fail("missing cr")
	case !sha256Pattern.MatchString(p.PlanSHA256):
		return fail("planSha256 %q is not a SHA-256", p.PlanSHA256)
	case p.UpdatedAt.IsZero():
		return fail("missing updatedAt")
	case p.Outcomes == nil:
		return fail("outcomes is an array, never null")
	case p.Blocked != "" && p.Blocked != StatusHeadChanged && p.Blocked != StatusNotOpen && p.Blocked != StatusOwnCR:
		return fail("blocked %q is not head-changed, not-open or own-cr", string(p.Blocked))
	}
	seen := map[int]bool{}
	for _, o := range p.Outcomes {
		switch {
		case o.ID < 1:
			return fail("outcome with id %d", o.ID)
		case seen[o.ID]:
			return fail("two outcomes for action %d", o.ID)
		case !o.Status.Valid() || o.Status == StatusIgnoredUntrusted || o.Status == StatusNoAnchor:
			return fail("action %d has status %q", o.ID, string(o.Status))
		case o.Number < 0:
			return fail("action %d has a negative number", o.ID)
		}
		seen[o.ID] = true
	}
	return nil
}

// EncodePublished returns the JSON of the outcomes as it is stored in the
// directory of the change request.
func EncodePublished(p Published) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return encodeJSON(publishedFile{Schema: p.Schema, RunID: p.RunID, CR: p.CR, PlanSHA256: p.PlanSHA256, Blocked: p.Blocked, UpdatedAt: FormatTime(p.UpdatedAt), Outcomes: p.Outcomes})
}

// DecodePublished decodes the outcomes, rejecting unknown fields, trailing
// data and outcomes that break their contract.
func DecodePublished(data []byte) (Published, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f publishedFile
	if err := dec.Decode(&f); err != nil {
		return Published{}, fmt.Errorf("decode published: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Published{}, errors.New("decode published: trailing data after the document")
	}
	at, err := parseTime(f.UpdatedAt)
	if err != nil {
		return Published{}, fmt.Errorf("decode published: updatedAt: %w", err)
	}
	p := Published{Schema: f.Schema, RunID: f.RunID, CR: f.CR, PlanSHA256: f.PlanSHA256, Blocked: f.Blocked, UpdatedAt: at, Outcomes: f.Outcomes}
	if err := p.Validate(); err != nil {
		return Published{}, err
	}
	return p, nil
}
