package core

import (
	"regexp"
	"strings"
)

// ReviewNote is a note on a change request as the platform returns it: who
// wrote it and its body, and whether the platform vouches for the author.
type ReviewNote struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	// Trusted says the provider trusts the author: the account of the
	// token, or one the platform shows as an owner, member or collaborator
	// of the project. Whether an account is trusted is the provider's to
	// say; the core only reads this flag. A fingerprint in a note that is
	// not trusted is read but never counts.
	Trusted bool `json:"trusted"`
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
	// Trusted is true when a note that holds the fingerprint was written
	// by a trusted account other than the change request author.
	Trusted bool
}

const (
	rulePart    = `[a-z0-9]+(?:-[a-z0-9]+)*(?:\.[a-z0-9]+(?:-[a-z0-9]+)*)*`
	projectPart = projectSegments
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

// ReadFingerprints reads the fingerprints embedded in notes, current and
// legacy forms alike, each once, in the order they first appear, with the
// characters Embed encoded restored. A fingerprint is Trusted when a note
// that holds it is trusted and was not written by the change request
// author: the author could otherwise plant a fingerprint to hide a
// finding, and so could any account the platform does not vouch for.
// Authors compare without case, as platform logins do; an empty crAuthor
// excludes nobody.
func ReadFingerprints(notes []ReviewNote, crAuthor string) []NoteFingerprint {
	var out []NoteFingerprint
	at := map[string]int{}
	for _, n := range notes {
		trusted := n.Trusted && (crAuthor == "" || !strings.EqualFold(n.Author, crAuthor))
		for _, m := range embedded.FindAllStringSubmatch(n.Body, -1) {
			fp, ok := classify(m[1])
			if !ok {
				continue
			}
			fp.Value = unescapeComment(fp.Value)
			fp.Finding = unescapeComment(fp.Finding)
			if i, seen := at[fp.Value]; seen {
				out[i].Trusted = out[i].Trusted || trusted
				continue
			}
			fp.Trusted = trusted
			at[fp.Value] = len(out)
			out = append(out, fp)
		}
	}
	return out
}

// ParseFingerprints is ReadFingerprints without the fingerprints that are
// not trusted: the ones a review may rely on to skip what is already
// posted.
func ParseFingerprints(notes []ReviewNote, crAuthor string) []NoteFingerprint {
	var out []NoteFingerprint
	for _, fp := range ReadFingerprints(notes, crAuthor) {
		if fp.Trusted {
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
