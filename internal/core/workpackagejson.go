package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// EncodeWorkPackage returns the JSON of a work package as it is stored in
// the run's work directory: two-space indent, no HTML escaping, a final
// newline.
func EncodeWorkPackage(wp WorkPackage) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(wp); err != nil {
		return nil, fmt.Errorf("encode work package: %w", err)
	}
	return buf.Bytes(), nil
}

// DecodeWorkPackage decodes a work package, rejecting unknown fields,
// trailing data, a schema other than 1 and a signal without lineText,
// which only a work package written by an earlier version lacks. The values are checked by the
// work package contract.
func DecodeWorkPackage(data []byte) (WorkPackage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var wp WorkPackage
	if err := dec.Decode(&wp); err != nil {
		return WorkPackage{}, fmt.Errorf("decode work package: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return WorkPackage{}, errors.New("decode work package: trailing data after the document")
	}
	if wp.Schema != 1 {
		return WorkPackage{}, fmt.Errorf("decode work package: unsupported schema %d", wp.Schema)
	}
	var raw struct {
		Signals []map[string]json.RawMessage `json:"signals"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return WorkPackage{}, fmt.Errorf("decode work package: %w", err)
	}
	for i, s := range raw.Signals {
		if _, ok := s["lineText"]; !ok {
			return WorkPackage{}, fmt.Errorf("decode work package: signal %s has no lineText: the work package was written by an earlier version; run the review again", wp.Signals[i].ID)
		}
	}
	return wp, nil
}
