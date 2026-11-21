package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// Document is the findings document of one finalized change request: the
// verdict and everything the report and the publish plan are built from.
// Its JSON form is defined by schemas/findings.schema.json.
type Document struct {
	Schema int `json:"schema"`
	CR     Key `json:"cr"`
	// Title is the title of the change request on the platform.
	Title   string `json:"title"`
	HeadSHA string `json:"headSha"`
	// BaseRef is the target branch, where the criteria were read from.
	BaseRef string  `json:"baseRef"`
	Verdict Verdict `json:"verdict"`
	Summary string  `json:"summary,omitempty"`
	// Files are the changed files, in diff order.
	Files       []string  `json:"files"`
	Findings    []Finding `json:"findings"`
	PreExisting []Finding `json:"preExisting"`
	// NotVerified are the changed files that were not reviewed;
	// AgentNotVerified is what the agent could not check.
	NotVerified      []UnverifiedFile `json:"notVerified"`
	AgentNotVerified []string         `json:"agentNotVerified"`
	SignalsDismissed []Dismissal      `json:"signalsDismissed"`
	Questions        []Question       `json:"questions"`
	Notes            []string         `json:"notes"`
	CI               CI               `json:"ci"`
	Criteria         Criteria         `json:"criteria"`
}

// UnverifiedFile is a changed file that was not reviewed, and why.
type UnverifiedFile struct {
	Path string `json:"path"`
	// Reason is one of the reasons of the findings contract, such as
	// generated or unsupported-language.
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// Dismissal is a signal the judgement dismissed, as the report lists it.
type Dismissal struct {
	Rule string `json:"rule"`
	Path string `json:"path"`
	Line int    `json:"line"`
	// DefaultSeverity is the severity the rule has in its catalog. A rule
	// that needs judgement gives its signals no severity of their own, so
	// the report could not otherwise tell a dismissed BLOCKER from a
	// dismissed MINOR.
	DefaultSeverity Severity `json:"defaultSeverity"`
	Reason          string   `json:"reason"`
}

// CIState is the state of the checks on the head commit.
type CIState string

// The CI states.
const (
	CIPassing CIState = "passing"
	CIFailing CIState = "failing"
	CIPending CIState = "pending"
	CINone    CIState = "none"
	// CIUnknown: the checks could not be read; CI.Reason says why.
	CIUnknown CIState = "unknown"
)

// CI is the state of the checks on the head commit when the change
// request was collected.
type CI struct {
	State CIState `json:"state"`
	// Reason is forbidden or error, set only when State is CIUnknown.
	Reason string  `json:"reason,omitempty"`
	Detail string  `json:"detail,omitempty"`
	Checks []Check `json:"checks"`
}

// CIError reports a CI state that breaks the findings and work package
// contracts.
type CIError struct{ Problem string }

func (e *CIError) Error() string { return "ci: " + e.Problem }

// Validate returns a *CIError when the state is not one the contracts
// allow: a known state, a reason (forbidden or error) if and only if the
// state is unknown, and no checks when it is unknown.
func (c CI) Validate() error {
	switch c.State {
	case CIPassing, CIFailing, CIPending, CINone:
		if c.Reason != "" {
			return &CIError{"a reason is only for the unknown state"}
		}
	case CIUnknown:
		if c.Reason != "forbidden" && c.Reason != "error" {
			return &CIError{"the unknown state needs the reason forbidden or error"}
		}
		if len(c.Checks) > 0 {
			return &CIError{"the unknown state has no checks"}
		}
	default:
		return &CIError{"unknown state " + string(c.State)}
	}
	return nil
}

// Check is one check on the head commit.
type Check struct {
	Name string `json:"name"`
	// Status is queued, in_progress or completed.
	Status string `json:"status"`
	// Conclusion is the outcome of a completed check, such as success or
	// failure; nil while it is not completed.
	Conclusion *string `json:"conclusion"`
}

// Criteria are the criteria the change request was reviewed with.
type Criteria struct {
	// Source is overlay, legacy or default.
	Source        string   `json:"source"`
	Profile       string   `json:"profile"`
	ProfileReason string   `json:"profileReason"`
	Packs         []string `json:"packs"`
	// Dimensions are the repository dimensions of the packs, by id.
	Dimensions map[string]DimensionChoice `json:"dimensions"`
	Notes      []string                   `json:"notes"`
}

// DimensionChoice is the value of a dimension and why it was chosen.
type DimensionChoice struct {
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

// DecodeDocument decodes a findings document, rejecting unknown fields and
// trailing data. The values are checked by the findings contract.
func DecodeDocument(data []byte) (Document, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var d Document
	if err := dec.Decode(&d); err != nil {
		return Document{}, fmt.Errorf("decode findings: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Document{}, errors.New("decode findings: trailing data after the document")
	}
	return d, nil
}

// Header is what a findings document holds besides the result of
// finalizing: the change request as the platform shows it, the files that
// were not reviewed, the checks and the criteria.
type Header struct {
	Title, BaseRef string
	// Files are the changed files, in diff order.
	Files       []string
	NotVerified []UnverifiedFile
	CI          CI
	Criteria    Criteria
}

// Document is the findings document of the result. Empty lists are empty
// arrays, never null, as the findings contract requires.
func (r Result) Document(h Header) Document {
	dismissed := []Dismissal{}
	for _, d := range r.Dismissed {
		dismissed = append(dismissed, Dismissal{Rule: d.Signal.Rule, Path: d.Signal.Path, Line: d.Signal.Line, DefaultSeverity: d.DefaultSeverity, Reason: d.Reason})
	}
	crit := h.Criteria
	crit.Packs, crit.Notes = nonNil(crit.Packs), nonNil(crit.Notes)
	if crit.Dimensions == nil {
		crit.Dimensions = map[string]DimensionChoice{}
	}
	ci := h.CI
	ci.Checks = nonNil(ci.Checks)
	return Document{
		Schema: 1, CR: r.CR, Title: h.Title, HeadSHA: r.HeadSHA, BaseRef: h.BaseRef, Verdict: r.Verdict, Summary: r.Summary,
		Files: nonNil(h.Files), Findings: nonNil(r.Findings), PreExisting: nonNil(r.PreExisting),
		NotVerified: nonNil(h.NotVerified), AgentNotVerified: nonNil(r.AgentNotVerified),
		SignalsDismissed: dismissed, Questions: nonNil(r.Questions), Notes: nonNil(r.Notes),
		CI: ci, Criteria: crit,
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// Header returns what the findings document of the change request holds
// besides the result of finalizing, as the work package recorded it when
// the change request was collected: the checks and the criteria, notes
// included, are copied, not read again.
func (w WorkPackage) Header() Header {
	files := make([]string, 0, len(w.Files))
	for _, f := range w.Files {
		files = append(files, f.Path)
	}
	c := w.Criteria
	return Header{
		Title: w.Title, BaseRef: w.BaseRef, Files: files, NotVerified: slices.Clone(w.NotVerified), CI: w.CI,
		Criteria: Criteria{Source: c.Source, Profile: c.Profile, ProfileReason: c.ProfileReason, Packs: c.Packs, Dimensions: c.Dimensions, Notes: c.Notes},
	}
}

// EncodeDocument returns the JSON of a findings document as it is stored
// in the run's work directory: two-space indent, no HTML escaping, a final
// newline.
func EncodeDocument(d Document) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		return nil, fmt.Errorf("encode findings: %w", err)
	}
	return buf.Bytes(), nil
}
