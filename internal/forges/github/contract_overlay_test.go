package github

import (
	"encoding/base64"
	"strconv"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/testutil/forgecontract"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// Synthetic: everything here is made up. The recordings of a scenario have
// no comment by anyone but the author of the change request, no directory
// listing at main in a repository without a criteria overlay, no complete
// search and no failure, and the contract needs them. Nothing here changes a
// recorded response except the comments of the change request, which carry
// the fingerprints the contract looks for. The repository endpoint and the
// listing of .agent-code-review are recorded and used as they are. The
// failures are on repositories of acme, which no recording touches.

const (
	// overlayDir is the criteria overlay, a directory recorded at main in the
	// repositories that have one.
	overlayDir = ".agent-code-review"
	finding    = "acr:v1:logic-defect:app/Greeter.kt:0123456789ab"
	farAway    = 1 << 40 // seconds since the epoch: a reset no one waits for
)

func contractOptions() Options {
	c := &clock{now: epoch}
	opts := testOptions(c)
	opts.MaxPages = 2
	return opts
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func fileBody(content string) string {
	return `{"type":"file","encoding":"base64","size":` + strconv.Itoa(len(content)) + `,"content":"` + b64(content) + `"}`
}

func overlay(t *testing.T, r *sandbox.Replay, sc scenarioCase, fx *forgecontract.Fixture) {
	t.Helper()
	repoPath := "/repos/" + sandbox.Owner + "/" + sc.repo
	route := func(status int, path string, query map[string]string, header map[string]string, body string) {
		r.Add(sandbox.Route{Path: path, Query: query, Status: status, Header: header, Body: body})
	}
	main := map[string]string{"ref": "main"}
	per := map[string]string{"per_page": "100"}
	n := strconv.Itoa(sc.number)

	// An issue and a number that is nothing.
	route(404, repoPath+"/pulls/50", nil, nil, `{"message":"Not Found"}`)
	route(200, repoPath+"/issues/50", nil, nil, `{"number":50}`)
	route(404, repoPath+"/pulls/51", nil, nil, `{"message":"Not Found"}`)
	route(404, repoPath+"/issues/51", nil, nil, `{"message":"Not Found"}`)
	route(404, repoPath+"/pulls/50/files", per, nil, `{"message":"Not Found"}`)
	route(404, repoPath+"/pulls/51/files", per, nil, `{"message":"Not Found"}`)
	// A commit that does not exist.
	route(422, repoPath+"/commits/ffffffffffffffffffffffffffffffffffffffff/check-runs", per, nil, `{"message":"No commit found for SHA"}`)
	fx.Failures = append(fx.Failures, forgecontract.Failure{Op: forgecontract.OpCI, Project: fx.Project, Ref: "ffffffffffffffffffffffffffffffffffffffff", Kind: base.ErrNotFound})

	// Comments with the fingerprints of an earlier run, one by the author.
	// The reviewer is a member; the token's own account has no association;
	// a stranger is a contributor, so what they wrote does not count.
	route(200, "/user", nil, nil, `{"login":"token-login"}`)
	route(200, repoPath+"/issues/"+n+"/comments", per, nil,
		`[{"user":{"login":"sandbox-reviewer"},"author_association":"MEMBER","body":"Looks fine.\n\n<!-- acr:v1:ok:01234567 -->"},`+
			`{"user":{"login":"`+fx.Author+`"},"author_association":"COLLABORATOR","body":"<!-- acr:v1:ok:89abcdef -->"},`+
			`{"user":{"login":"stranger"},"author_association":"CONTRIBUTOR","body":"<!-- acr:v1:ok:fedcba98 -->"},`+
			`{"user":{"login":"token-login"},"author_association":"NONE","body":"<!-- acr:v1:ok:0f0f0f0f -->"}]`)
	route(200, repoPath+"/pulls/"+n+"/comments", per, nil, `[{"user":{"login":"sandbox-reviewer"},"author_association":"OWNER","body":"Inline.\n\n<!-- `+finding+` -->"}]`)
	fx.Fingerprints = []string{"acr:v1:ok:01234567", "acr:v1:ok:0f0f0f0f", finding}
	fx.Untrusted = []string{"acr:v1:ok:89abcdef", "acr:v1:ok:fedcba98"}
	fx.Login = "token-login"

	// A file at main, where none was recorded.
	if fx.Path == "" {
		fx.Path, fx.Content = "shared/build.gradle.kts", "plugins {\n    kotlin(\"multiplatform\")\n}\n"
		route(200, repoPath+"/contents/"+fx.Path, main, nil, fileBody(fx.Content))
	}

	// A directory at main: the recorded listing of the criteria overlay where
	// the repository has one, a made-up one where the sandbox has none.
	if rec := sandbox.Contents(t, sc.scenario, sc.repo, overlayDir, "main"); rec.Recorded && rec.Status == 200 && len(rec.Dir) > 0 {
		fx.Dir = overlayDir
		for _, e := range rec.Dir {
			fx.Entries = append(fx.Entries, core.DirEntry{Name: e.Name, Dir: e.Dir})
		}
	} else {
		fx.Dir, fx.Entries = "app/src", []core.DirEntry{{Name: "main", Dir: true}, {Name: "README.md"}}
		route(200, repoPath+"/contents/app/src", main, nil, `[{"name":"main","type":"dir"},{"name":"README.md","type":"file"}]`)
	}

	// A search that completes, where the recorded one is not the point.
	if !sc.searchFails {
		src := "package acme\n\nfun use() = " + sc.term + "()\n"
		route(200, "/search/code", map[string]string{"q": sc.term + " repo:" + sandbox.Owner + "/" + sc.repo, "per_page": "100"}, nil,
			`{"total_count":1,"incomplete_results":false,"items":[{"path":"app/src/Use.kt"}]}`)
		route(200, repoPath+"/contents/app/src/Use.kt", main, nil, fileBody(src))
		fx.Matches = []forgecontract.At{{Path: "app/src/Use.kt", Line: 3}}
	}
	fx.Failures = append(fx.Failures, forgecontract.Failure{Op: forgecontract.OpSearch, Project: fx.Project, Ref: "release/2.0", Kind: base.ErrSearchUnavailable})

	failures(r, fx)
}

// failures adds repositories of acme that fail in each way the contract
// checks, and the failures to the fixture.
func failures(r *sandbox.Replay, fx *forgecontract.Fixture) {
	acme := func(name string, number int) core.Key {
		return core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/" + name, Number: number}
	}
	route := func(status int, path string, query map[string]string, header map[string]string, body string) {
		r.Add(sandbox.Route{Path: path, Query: query, Status: status, Header: header, Body: body})
	}
	main := map[string]string{"ref": "main"}
	per := map[string]string{"per_page": "100"}
	add := func(op forgecontract.Op, k core.Key, ref string, kind base.Kind) {
		fx.Failures = append(fx.Failures, forgecontract.Failure{Op: op, Project: k, Ref: ref, Kind: kind})
	}
	missing := `{"message":"Not Found"}`
	rate := map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(farAway, 10)}

	// A repository the token cannot see: GitHub says 404 to everything.
	private := acme("private", 1)
	route(404, "/repos/acme/private", nil, nil, missing)
	route(404, "/repos/acme/private/contents/"+fx.Path, main, nil, missing)
	route(404, "/repos/acme/private/git/trees/main", map[string]string{"recursive": "1"}, nil, missing)
	route(404, "/repos/acme/private/pulls/1", nil, nil, missing)
	route(404, "/repos/acme/private/pulls/1/files", per, nil, missing)
	route(404, "/repos/acme/private/issues/1/comments", per, nil, missing)
	add(forgecontract.OpFilesGet, private, "main", base.ErrNotFound)
	add(forgecontract.OpTree, private, "main", base.ErrNotFound)
	add(forgecontract.OpNotes, private, "", base.ErrNotFound)
	add(forgecontract.OpChangedFiles, private, "", base.ErrNotFound)
	add(forgecontract.OpChangeRequest, private, "", base.ErrNotFound)

	// A token without a permission.
	locked := acme("locked", 1)
	perm := map[string]string{"X-Accepted-GitHub-Permissions": "contents=read"}
	route(403, "/repos/acme/locked/contents/"+fx.Dir, main, perm, `{"message":"Resource not accessible by personal access token"}`)
	route(403, "/repos/acme/locked/pulls/1/files", per, map[string]string{"X-Accepted-GitHub-Permissions": "pull_requests=read"}, `{"message":"no"}`)
	add(forgecontract.OpFilesList, locked, "main", base.ErrForbidden)
	add(forgecontract.OpChangedFiles, locked, "", base.ErrForbidden)

	// A tree and a list GitHub cuts short.
	huge := acme("huge", 1)
	route(200, "/repos/acme/huge/git/trees/main", map[string]string{"recursive": "1"}, nil, `{"truncated":true,"tree":[{"path":"a.kt","type":"blob"}]}`)
	for page, link := range map[string]string{"": "2", "2": "3", "3": ""} {
		var h map[string]string
		q := map[string]string{"per_page": "100"}
		if page != "" {
			q["page"] = page
		}
		if link != "" {
			h = map[string]string{"Link": `<https://api.github.com/repos/acme/huge/pulls/1/files?per_page=100&page=` + link + `>; rel="next"`}
		}
		route(200, "/repos/acme/huge/pulls/1/files", q, h, `[{"filename":"f`+page+`.kt","status":"added","changes":1,"patch":"@@ -0,0 +1 @@\n+x"}]`)
	}
	add(forgecontract.OpTree, huge, "main", base.ErrTreeTruncated)
	add(forgecontract.OpChangedFiles, huge, "", base.ErrListTruncated)

	// A primary rate limit with a reset no one waits for.
	busy := acme("busy", 3)
	route(403, "/repos/acme/busy/pulls/3", nil, rate, `{"message":"API rate limit exceeded"}`)
	route(403, "/repos/acme/busy/commits/"+fx.Head+"/check-runs", per, rate, `{"message":"API rate limit exceeded"}`)
	add(forgecontract.OpChange, busy, "", base.ErrRateLimited)
	add(forgecontract.OpChangeRequest, busy, "", base.ErrRateLimited)
	add(forgecontract.OpCI, busy, fx.Head, base.ErrRateLimited)

	// A platform that fails.
	flaky := acme("flaky", 2)
	route(200, "/repos/acme/flaky", nil, nil, `{"default_branch":"main"}`)
	route(500, "/search/code", map[string]string{"q": fx.Term + " repo:acme/flaky", "per_page": "100"}, nil, `{}`)
	route(500, "/repos/acme/flaky/issues/2/comments", per, nil, `{}`)
	add(forgecontract.OpSearch, flaky, "main", base.ErrTransport)
	add(forgecontract.OpNotes, flaky, "", base.ErrTransport)
}
