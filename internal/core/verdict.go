package core

// Verdict is the outcome of a review. The plugin never approves: OK only
// means that nothing blocks.
type Verdict string

// The verdicts.
const (
	Blocked Verdict = "BLOCKED"
	OK      Verdict = "OK"
	// Incomplete is a cascade verdict only: part of the cascade was not
	// reviewed.
	Incomplete Verdict = "INCOMPLETE"
)

// Valid reports whether v is a known verdict.
func (v Verdict) Valid() bool { return v == Blocked || v == OK || v == Incomplete }

// ChangeRequestVerdict is BLOCKED when the change request introduces a
// BLOCKER or a pre-existing defect is a crash, and OK otherwise: MAJOR,
// MINOR and INFO findings never block.
func ChangeRequestVerdict(findings, preExisting []Finding) Verdict {
	for _, f := range findings {
		if f.Severity == Blocker {
			return Blocked
		}
	}
	for _, f := range preExisting {
		if f.Crash {
			return Blocked
		}
	}
	return OK
}
