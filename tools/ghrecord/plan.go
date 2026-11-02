package ghrecord

import "fmt"

// Target is a change request to record.
type Target struct {
	Owner, Repo string
	Number      int
}

func (t Target) String() string { return fmt.Sprintf("%s/%s#%d", t.Owner, t.Repo, t.Number) }

func (t Target) repoPath() string { return "/repos/" + t.Owner + "/" + t.Repo }

// Request is one read-only request to the REST API. Path is not escaped.
type Request struct {
	Path  string
	Query map[string]string
}

// Ref is the head or base of a pull request.
type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// PullRequest holds the pull request fields the recorder needs.
type PullRequest struct {
	Head Ref `json:"head"`
	Base Ref `json:"base"`
}

// PullRequestFile holds the changed-file fields the recorder needs. Patch is
// nil when GitHub omits it (binary, too large, or a pure rename).
type PullRequestFile struct {
	Filename         string  `json:"filename"`
	PreviousFilename string  `json:"previous_filename"`
	Status           string  `json:"status"`
	Patch            *string `json:"patch"`
}

// criteriaPaths are the review criteria files read from the target branch.
// FollowUps adds the legacy skill file, which is named after the repository.
var criteriaPaths = []string{
	".agent-code-review/config.json",
	".agent-code-review/criteria.md",
	".github/mr-review.json",
	".github/skills",
}

func paged(path string) Request {
	return Request{Path: path, Query: map[string]string{"per_page": "100"}}
}

func contents(t Target, path, ref string) Request {
	return Request{Path: t.repoPath() + "/contents/" + path, Query: map[string]string{"ref": ref}}
}

// PullRequestRequest returns the request for the pull request itself.
func PullRequestRequest(t Target) Request {
	return Request{Path: fmt.Sprintf("%s/pulls/%d", t.repoPath(), t.Number)}
}

// IssueRequest returns the request that tells an issue from a missing number.
func IssueRequest(t Target) Request {
	return Request{Path: fmt.Sprintf("%s/issues/%d", t.repoPath(), t.Number)}
}

// FilesRequest returns the first page of the changed files.
func FilesRequest(t Target) Request {
	return paged(fmt.Sprintf("%s/pulls/%d/files", t.repoPath(), t.Number))
}

// FollowUps returns the requests a review of the pull request makes after
// reading the pull request and its changed files. Files without a patch are
// not fetched: they are binary, too large to review, or pure renames.
func FollowUps(t Target, pr PullRequest, files []PullRequestFile) []Request {
	pull := fmt.Sprintf("%s/pulls/%d", t.repoPath(), t.Number)
	reqs := []Request{
		paged(pull + "/commits"),
		paged(pull + "/reviews"),
		paged(pull + "/comments"),
		paged(fmt.Sprintf("%s/issues/%d/comments", t.repoPath(), t.Number)),
		paged(t.repoPath() + "/commits/" + pr.Head.SHA + "/check-runs"),
		{Path: t.repoPath() + "/commits/" + pr.Head.SHA + "/status"},
	}
	for _, p := range criteriaPaths {
		reqs = append(reqs, contents(t, p, pr.Base.Ref))
	}
	reqs = append(reqs, contents(t, ".github/skills/"+t.Repo+"-code-review/SKILL.md", pr.Base.Ref))
	for _, f := range files {
		if f.Patch == nil {
			continue
		}
		if f.Status != "removed" {
			reqs = append(reqs, contents(t, f.Filename, pr.Head.SHA))
		}
		if f.Status != "added" {
			old := f.Filename
			if f.PreviousFilename != "" {
				old = f.PreviousFilename
			}
			reqs = append(reqs, contents(t, old, pr.Base.SHA))
		}
	}
	return reqs
}
