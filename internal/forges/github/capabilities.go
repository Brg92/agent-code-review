package github

import "github.com/Brg92/agent-code-review/internal/forges/base"

// Capabilities are what GitHub can read:
//
//   - code search indexes only the default branch of a repository, so a
//     search on any other target branch is search unavailable;
//   - the recursive tree API truncates large trees;
//   - CI comes from check runs and from commit statuses.
func Capabilities() base.Capabilities {
	return base.Capabilities{
		Search:          base.SearchDefaultBranch,
		TreeMayTruncate: true,
		CI:              base.CISources{CheckRuns: true, CommitStatuses: true},
	}
}
