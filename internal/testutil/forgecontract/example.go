package forgecontract

import (
	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// Example returns a fake with the given capabilities and the fixture it
// must pass. The data is synthetic.
func Example(caps base.Capabilities) (*Fake, Fixture) {
	key := func(project string, n int) core.Key {
		return core.Key{Forge: core.GitHub, Host: "github.com", Project: project, Number: n}
	}
	app := key("acme/app", 7)
	pk := app.ProjectKey()
	const head = "0123456789abcdef0123456789abcdef01234567"
	files := map[string]string{
		"build.gradle.kts":                        "plugins {\n    id(\"com.android.application\")\n}\n",
		"app/src/main/kotlin/acme/Greeter.kt":     "package acme\n\nfun greet(name: String) = \"Hello, $name\"\n",
		"app/src/main/kotlin/acme/Main.kt":        "package acme\n\nfun main() = println(greet(\"world\"))\n",
		"app/src/test/kotlin/acme/GreeterTest.kt": "package acme\n\nclass GreeterTest {\n    fun greets() = check(greet(\"a\") == \"Hello, a\")\n}\n",
	}
	success := "success"
	ci := core.CI{State: core.CIPassing, Checks: []core.Check{{Name: "build", Status: "completed", Conclusion: &success}}}
	item := cascade.Item{Kind: cascade.KindChangeRequest, Title: "Greet by name", Description: "## Related Changes\n\nNone.\n", Head: head}
	finding := "acr:v1:logic-defect:app/src/main/kotlin/acme/Greeter.kt:0123456789ab"

	locked, huge, busy, flaky, private := key("acme/locked", 1), key("acme/huge", 1), key("acme/busy", 3), key("acme/flaky", 2), key("acme/private", 1)
	meta := core.ChangeRequest{
		Key: app, Title: item.Title, Description: item.Description, HeadSHA: head,
		BaseSHA: "89abcdef0123456789abcdef0123456789abcdef", TargetBranch: "main", Author: "author-login", State: core.StateOpen,
	}
	changed := []core.FileChange{
		{Path: "app/src/main/kotlin/acme/Greeter.kt", Status: core.FileModified, Patch: "@@ -1,3 +1,3 @@\n package acme\n \n-fun greet() = \"Hello\"\n+fun greet(name: String) = \"Hello, $name\""},
		{Path: "app/src/main/kotlin/acme/Names.kt", OldPath: "app/src/main/kotlin/acme/Name.kt", Status: core.FileRenamed},
		{Path: "app/src/main/res/logo.png", Status: core.FileAdded, Missing: core.PatchBinary, Binary: true},
	}
	fake := &Fake{
		Caps:           caps,
		ChangeRequests: map[core.Key]core.ChangeRequest{app: meta},
		FileSets:       map[core.Key][]core.FileChange{app: changed},
		Items:          map[core.Key]cascade.Item{app: item, key("acme/app", 8): {Kind: cascade.KindIssue}},
		DefaultBranch:  map[string]string{pk: "main", flaky.ProjectKey(): "main"},
		Repos:          map[string]map[string]string{pk + "@main": files, pk + "@release/2.0": files},
		CIs:            map[string]core.CI{pk + "@" + head: ci},
		NoteSets: map[core.Key][]core.ReviewNote{app: {
			{Author: "reviewer", Trusted: true, Body: "Looks fine.\n\n<!-- acr:v1:ok:01234567 -->"},
			{Author: "author-login", Trusted: true, Body: "<!-- acr:v1:ok:89abcdef -->"},
			{Author: "stranger", Body: "<!-- acr:v1:ok:fedcba98 -->"},
			{Author: "token-login", Trusted: true, Body: "<!-- acr:v1:ok:0f0f0f0f -->"},
			{Author: "reviewer", Trusted: true, Body: "Inline finding.\n\n<!-- " + finding + " -->"},
		}},
		Login: "token-login",
		IssueSets: map[string][]core.Issue{pk: {
			{Number: 3, Open: true, Author: "token-login", Trusted: true, Title: "Review of #7: MAJOR findings", Body: "major <!-- acr:v1:issue:acme/app#7:major -->"},
			{Number: 4, Open: false, Author: "stranger", Title: "Planted", Body: "<!-- acr:v1:issue:acme/app#7:minor -->"},
		}},
		Labels:    map[string][]string{pk: {"review", "bug"}},
		Assignees: map[string][]string{pk: {"author-login"}},
		ThreadSets: map[core.Key][]core.ReviewThread{app: {
			{Resolved: false, Author: "token-login", Trusted: true, Body: "Blocker <!-- " + finding + " -->"},
			{Resolved: true, Author: "stranger", Body: "Planted"},
		}},
		Fail: map[Call]error{
			{OpChangedFiles, locked.ProjectKey(), ""}:  &base.Error{Kind: base.ErrForbidden, Op: "changed files acme/locked#1", Permissions: []string{"pull requests: read"}},
			{OpChangeRequest, busy.ProjectKey(), ""}:   &base.Error{Kind: base.ErrRateLimited, Op: "change request acme/busy#3"},
			{OpFilesList, locked.ProjectKey(), "main"}: &base.Error{Kind: base.ErrForbidden, Op: "contents acme/locked@main", Permissions: []string{"contents: read"}},
			{OpTree, huge.ProjectKey(), "main"}:        &base.Error{Kind: base.ErrTreeTruncated, Op: "tree acme/huge@main"},
			{OpCI, busy.ProjectKey(), head}:            &base.Error{Kind: base.ErrRateLimited, Op: "ci acme/busy"},
			{OpChange, busy.ProjectKey(), ""}:          &base.Error{Kind: base.ErrRateLimited, Op: "change acme/busy#3"},
			{OpSearch, flaky.ProjectKey(), "main"}:     &base.Error{Kind: base.ErrTransport, Op: "search acme/flaky@main"},
			{OpNotes, flaky.ProjectKey(), ""}:          &base.Error{Kind: base.ErrTransport, Op: "notes acme/flaky#2"},
			{OpIssues, locked.ProjectKey(), ""}:        &base.Error{Kind: base.ErrForbidden, Op: "issues acme/locked", Permissions: []string{"issues: read"}},
			{OpAssignable, busy.ProjectKey(), ""}:      &base.Error{Kind: base.ErrRateLimited, Op: "assignee acme/busy"},
			{OpThreads, flaky.ProjectKey(), ""}:        &base.Error{Kind: base.ErrTransport, Op: "review threads acme/flaky#2"},
		},
	}

	fx := Fixture{
		Capabilities:  caps,
		ChangeRequest: app,
		Item:          item,
		Metadata:      meta,
		ChangedFiles:  changed,
		Issue:         key("acme/app", 8),
		Missing:       key("acme/app", 9),
		Project:       app,
		DefaultBranch: "main",
		OtherRef:      "release/2.0",
		Path:          "build.gradle.kts",
		Content:       files["build.gradle.kts"],
		MissingPath:   "settings.gradle.kts",
		Dir:           "app/src",
		Entries:       []core.DirEntry{{Name: "main", Dir: true}, {Name: "test", Dir: true}},
		MissingDir:    "docs",
		TreePrefix:    "app/src/main/",
		TreePaths:     []string{"app/src/main/kotlin/acme/Greeter.kt", "app/src/main/kotlin/acme/Main.kt"},
		Term:          "greet(",
		Matches: []At{
			{"app/src/main/kotlin/acme/Greeter.kt", 3},
			{"app/src/main/kotlin/acme/Main.kt", 3},
			{"app/src/test/kotlin/acme/GreeterTest.kt", 4},
		},
		Head:         head,
		CI:           ci,
		Author:       "author-login",
		Fingerprints: []string{"acr:v1:ok:01234567", "acr:v1:ok:0f0f0f0f", finding},
		Untrusted:    []string{"acr:v1:ok:89abcdef", "acr:v1:ok:fedcba98"},
		Login:        "token-login",
		Label:        "review",
		Issues: []core.Issue{
			{Number: 3, Open: true, Author: "token-login", Trusted: true, Title: "Review of #7: MAJOR findings", Body: "major <!-- acr:v1:issue:acme/app#7:major -->"},
			{Number: 4, Open: false, Author: "stranger", Title: "Planted", Body: "<!-- acr:v1:issue:acme/app#7:minor -->"},
		},
		ExistingLabel: "review",
		MissingLabel:  "needs-triage",
		Assignable:    "author-login",
		NotAssignable: "nobody-here",
		Threads: []core.ReviewThread{
			{Resolved: false, Author: "token-login", Trusted: true, Body: "Blocker <!-- " + finding + " -->"},
			{Resolved: true, Author: "stranger", Body: "Planted"},
		},
		Failures: []Failure{
			{OpFilesGet, private, "main", base.ErrNotFound},
			{OpTree, private, "main", base.ErrNotFound},
			{OpNotes, private, "", base.ErrNotFound},
			{OpCI, app, "ffffffffffffffffffffffffffffffffffffffff", base.ErrNotFound},
			{OpFilesList, locked, "main", base.ErrForbidden},
			{OpCI, busy, head, base.ErrRateLimited},
			{OpChange, busy, "", base.ErrRateLimited},
			{OpChangeRequest, busy, "", base.ErrRateLimited},
			{OpChangedFiles, locked, "", base.ErrForbidden},
			{OpChangedFiles, private, "", base.ErrNotFound},
			{OpNotes, flaky, "", base.ErrTransport},
			{OpIssues, private, "", base.ErrNotFound},
			{OpIssues, locked, "", base.ErrForbidden},
			{OpHasLabel, private, "", base.ErrNotFound},
			{OpAssignable, private, "", base.ErrNotFound},
			{OpAssignable, busy, "", base.ErrRateLimited},
			{OpThreads, private, "", base.ErrNotFound},
			{OpThreads, flaky, "", base.ErrTransport},
		},
	}
	if caps.TreeMayTruncate {
		fx.Failures = append(fx.Failures, Failure{OpTree, huge, "main", base.ErrTreeTruncated})
	}
	if caps.SearchOn("main", "main") {
		fx.Failures = append(fx.Failures, Failure{OpSearch, flaky, "main", base.ErrTransport})
	}
	if !caps.SearchOn("release/2.0", "main") {
		fx.Failures = append(fx.Failures, Failure{OpSearch, app, "release/2.0", base.ErrSearchUnavailable})
	}
	return fake, fx
}
