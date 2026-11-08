package core

import (
	"regexp"
	"strings"
)

// ReviewNote is a note on a change request as the platform returns it: who
// wrote it and its body.
type ReviewNote struct {
	Author string `json:"author"`
	Body   string `json:"body"`
}

// FingerprintKind says what a fingerprint identifies.
type FingerprintKind string

// The fingerprint kinds.
const (
	FingerprintFinding FingerprintKind = "finding"
	FingerprintOK      FingerprintKind = "ok"
	FingerprintIssue   FingerprintKind = "issue"
	FingerprintItem    FingerprintKind = "item"
)

// NoteFingerprint is a fingerprint read back from a note.
type NoteFingerprint struct {
	// Value is the fingerprint as written.
	Value string
	Kind  FingerprintKind
	// Legacy marks the mr-review: forms written by the earlier plugin.
	Legacy bool
	// Finding is the fingerprint of the finding an issue item lists; empty
	// for the other kinds.
	Finding string
}

const (
	rulePart    = `[a-z0-9]+(?:-[a-z0-9]+)*(?:\.[a-z0-9]+(?:-[a-z0-9]+)*)*`
	projectPart = `[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+`
	severityTag = `(?:blocker|major|minor)`
)

var (
	embedded = regexp.MustCompile(`<!--\s*((?:acr|mr-review):[^>]*?)\s*-->`)

	fingerprintForms = []struct {
		re     *regexp.Regexp
		kind   FingerprintKind
		legacy bool
	}{
		{regexp.MustCompile(`^acr:v1:ok:[0-9a-f]{8}$`), FingerprintOK, false},
		{regexp.MustCompile(`^acr:v1:issue:` + projectPart + `#[1-9][0-9]*:` + severityTag + `$`), FingerprintIssue, false},
		{regexp.MustCompile(`^acr:v1:` + rulePart + `:\S(?:.*\S)?:[0-9a-f]{12}$`), FingerprintFinding, false},
		{regexp.MustCompile(`^acr:item:` + rulePart + `:\S(?:.*\S)?:[0-9a-f]{12}$`), FingerprintItem, false},
		{regexp.MustCompile(`^mr-review:v1:ok:[0-9a-f]{8}$`), FingerprintOK, true},
		{regexp.MustCompile(`^mr-review:v1:issue:[^\s!:]+![1-9][0-9]*:` + severityTag + `$`), FingerprintIssue, true},
		{regexp.MustCompile(`^mr-review:v1:` + rulePart + `:\S(?:.*\S)?:[0-9]+$`), FingerprintFinding, true},
		{regexp.MustCompile(`^mr-review:item:` + rulePart + `:\S(?:.*\S)?:[0-9]+$`), FingerprintItem, true},
	}
)

// ParseFingerprints reads the fingerprints embedded in notes, current and
// legacy forms alike, each once, in the order they appear. Notes written by
// the author of the change request are skipped: the author could otherwise
// plant a fingerprint to hide a finding. Authors compare without case, as
// platform logins do; an empty author skips nothing.
func ParseFingerprints(notes []ReviewNote, crAuthor string) []NoteFingerprint {
	var out []NoteFingerprint
	seen := map[string]bool{}
	for _, n := range notes {
		if crAuthor != "" && strings.EqualFold(n.Author, crAuthor) {
			continue
		}
		for _, m := range embedded.FindAllStringSubmatch(n.Body, -1) {
			fp, ok := classify(m[1])
			if !ok || seen[fp.Value] {
				continue
			}
			seen[fp.Value] = true
			out = append(out, fp)
		}
	}
	return out
}

func classify(s string) (NoteFingerprint, bool) {
	for _, f := range fingerprintForms {
		if !f.re.MatchString(s) {
			continue
		}
		fp := NoteFingerprint{Value: s, Kind: f.kind, Legacy: f.legacy}
		switch {
		case f.kind == FingerprintItem && f.legacy:
			fp.Finding = "mr-review:v1:" + strings.TrimPrefix(s, "mr-review:item:")
		case f.kind == FingerprintItem:
			fp.Finding = fingerprintPrefix + strings.TrimPrefix(s, itemPrefix)
		}
		return fp, true
	}
	return NoteFingerprint{}, false
}
