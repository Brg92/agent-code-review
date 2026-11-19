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

// Embed writes a fingerprint as the HTML comment it is published in. A
// path in the fingerprint can hold characters that would end or corrupt the
// comment; they are percent-encoded (see escapeComment), and ParseFingerprints
// decodes them. The fingerprint of an ordinary path is embedded as is.
func Embed(fingerprint string) string { return "<!-- " + escapeComment(fingerprint) + " -->" }

// escapedByte reports whether b is written as %XX inside an embedded
// fingerprint: the characters that end or open an HTML comment ('<' and '>';
// "-->" cannot form without a '>') and the control characters, which can
// break lines and be read by markdown. '%' itself is escaped only in front
// of a sequence the decoder would take for an escape.
func escapedByte(b byte) bool { return b == '<' || b == '>' || b < 0x20 || b == 0x7f }

const upperHex = "0123456789ABCDEF"

// hexValue returns the value of an uppercase hexadecimal digit.
func hexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// escapeSequence reads the %XX at the start of s. It reports the byte the
// sequence stands for, and whether s starts with an escape the decoder
// resolves: %XX with uppercase hexadecimal digits, for '%' or an escapedByte.
func escapeSequence(s string) (byte, bool) {
	if len(s) < 3 || s[0] != '%' {
		return 0, false
	}
	hi, ok1 := hexValue(s[1])
	lo, ok2 := hexValue(s[2])
	if !ok1 || !ok2 {
		return 0, false
	}
	b := hi<<4 | lo
	return b, b == '%' || escapedByte(b)
}

// escapeComment percent-encodes what Embed must not write as is: escapedByte
// characters, and a '%' that starts something unescapeComment would decode,
// so that decoding always restores the original. Everything else, including
// '%' in a path such as "my%20file.txt", stays as it is.
func escapeComment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		_, decodable := escapeSequence(s[i:])
		if escapedByte(c) || (c == '%' && decodable) {
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0x0f])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// unescapeComment is the inverse of escapeComment. A %XX that escapeComment
// would not have written, such as "%20" or "%3c", is left as it is.
func unescapeComment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if v, ok := escapeSequence(s[i:]); ok {
			b.WriteByte(v)
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
