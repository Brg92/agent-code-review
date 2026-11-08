package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Severity is how serious a finding is.
type Severity string

// The severities, from the most to the least serious.
const (
	Blocker Severity = "BLOCKER"
	Major   Severity = "MAJOR"
	Minor   Severity = "MINOR"
	Info    Severity = "INFO"
)

// Valid reports whether s is a known severity.
func (s Severity) Valid() bool {
	switch s {
	case Blocker, Major, Minor, Info:
		return true
	}
	return false
}

// The prefixes of the fingerprints embedded in published notes.
const (
	fingerprintPrefix = "acr:v1:"
	itemPrefix        = "acr:item:"
)

// NormalizeLine trims a line and collapses the whitespace inside it, so
// that re-indenting a line keeps the fingerprints of its findings.
func NormalizeLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// FindingFingerprint identifies a finding across pushes:
// acr:v1:<rule>:<path>:<h12>. h12 is the first 12 hexadecimal characters
// of the SHA-256 of the normalised text of the line the finding is about,
// a newline and the occurrence index of that text among the findings with
// the same rule and path. It does not depend on the line number, so it
// survives lines moving.
func FindingFingerprint(rule, path, lineText string, occurrence int) string {
	sum := sha256.Sum256([]byte(NormalizeLine(lineText) + "\n" + strconv.Itoa(occurrence)))
	return fingerprintPrefix + rule + ":" + path + ":" + hex.EncodeToString(sum[:])[:12]
}

// OKFingerprint identifies the note that says nothing blocks the head
// commit: acr:v1:ok:<the first 8 characters of the head SHA>.
func OKFingerprint(headSHA string) string {
	return fingerprintPrefix + "ok:" + headSHA[:min(8, len(headSHA))]
}

// IssueFingerprint identifies the issue that collects the findings of one
// severity of a change request: acr:v1:issue:<project>#<number>:<severity>.
// Only BLOCKER, MAJOR and MINOR findings are collected in issues.
func IssueFingerprint(cr Key, s Severity) (string, error) {
	switch s {
	case Blocker, Major, Minor:
		return fmt.Sprintf("%sissue:%s#%d:%s", fingerprintPrefix, cr.Project, cr.Number, strings.ToLower(string(s))), nil
	}
	return "", fmt.Errorf("no issue collects %q findings", string(s))
}

// ItemFingerprint identifies the item of an issue that lists a finding:
// acr:item: followed by the finding fingerprint without its acr:v1: prefix.
func ItemFingerprint(finding string) string {
	return itemPrefix + strings.TrimPrefix(finding, fingerprintPrefix)
}

// Embed writes a fingerprint as the HTML comment it is published in.
func Embed(fingerprint string) string { return "<!-- " + fingerprint + " -->" }
