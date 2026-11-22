package github

import "github.com/Brg92/agent-code-review/internal/forges/base"

// Capabilities are what GitHub can read:
//
//   - code search indexes only the default branch of a repository, so a
//     search on any other target branch is search unavailable;
//   - the recursive tree API truncates large trees;
//   - CI comes from check runs and from commit statuses;
//   - a comment, a review comment and an issue body hold at most 65 536
//     characters, and a code block fenced as "suggestion" is a change the
//     reader can apply.
func Capabilities() base.Capabilities {
	return base.Capabilities{
		Search:          base.SearchDefaultBranch,
		TreeMayTruncate: true,
		CI:              base.CISources{CheckRuns: true, CommitStatuses: true},
		Writes:          base.WriteLimits{MaxBodyLength: 65536, SuggestionInfo: "suggestion"},
	}
}
