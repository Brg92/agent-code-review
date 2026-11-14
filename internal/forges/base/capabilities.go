package base

import "fmt"

// Capabilities say what a platform can read, so that the core decides its
// behaviour without knowing the platform. Each provider fills them; they
// are plain data.
type Capabilities struct {
	// Search is on which refs the platform's code search works.
	Search SearchScope
	// TreeMayTruncate is true when the platform may return only part of a
	// large tree; such a read is ErrTreeTruncated.
	TreeMayTruncate bool
	// CI is where the platform reports the CI state of a commit.
	CI CISources
}

// SearchScope is the set of refs a platform's code search covers.
type SearchScope string

// The search scopes.
const (
	// SearchNone: the platform has no code search.
	SearchNone SearchScope = "none"
	// SearchDefaultBranch: the platform only indexes the default branch.
	SearchDefaultBranch SearchScope = "default-branch"
	// SearchAnyRef: the platform searches any branch.
	SearchAnyRef SearchScope = "any-ref"
)

// CISources are the sources of the CI state of a commit a platform has,
// read as neutral values (core.CI).
type CISources struct {
	// CheckRuns: checks reported by CI apps, with a status and a
	// conclusion.
	CheckRuns bool
	// CommitStatuses: states set on a commit by external services.
	CommitStatuses bool
}

// Readable reports whether the platform has any CI source.
func (s CISources) Readable() bool { return s.CheckRuns || s.CommitStatuses }

// SearchOn reports whether a code search at ref can run, given the default
// branch of the project. When it cannot, a search there is
// ErrSearchUnavailable.
func (c Capabilities) SearchOn(ref, defaultBranch string) bool {
	switch c.Search {
	case SearchAnyRef:
		return true
	case SearchDefaultBranch:
		return ref != "" && ref == defaultBranch
	}
	return false
}

// Validate reports capabilities a provider filled wrongly.
func (c Capabilities) Validate() error {
	switch c.Search {
	case SearchNone, SearchDefaultBranch, SearchAnyRef:
		return nil
	}
	return fmt.Errorf("capabilities: unknown search scope %q", string(c.Search))
}
