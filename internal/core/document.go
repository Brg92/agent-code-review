package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	Rule   string `json:"rule"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Reason string `json:"reason"`
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
