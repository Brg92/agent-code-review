// Command recorder records read-only GitHub REST API responses of the
// sandbox scenarios into testdata/github/<scenario>/ for offline replay.
//
// Usage:
//
//	ACR_RECORDER_TOKEN=... ACR_SANDBOX_LOGINS=<reviewer>,<author> \
//	    go run ./tools/recorder [-out dir] [-api url] [-extend] <scenario>... | all
//
// Each scenario records the pull requests it lists with their repository
// trees at head, and the code searches it lists with the files they
// return. With -extend, only the trees and searches are recorded, into the
// existing recordings, which are left as they are.
//
// The token is read only from the environment and is never printed or
// written. Every recording is sanitized: the two sandbox logins become
// sandbox-reviewer and sandbox-author.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Brg92/agent-code-review/tools/ghrecord"
)

const org = "acr-plugin-sandbox"

type scenario struct {
	targets  []ghrecord.Target
	requests []ghrecord.Request
	// searches are the code searches a review of the scenario makes. They
	// are listed by hand, not computed: each one names the rule whose
	// detector needs it.
	searches []search
}

// search is a code search of one term in a sandbox repository, at its
// default branch, the only one GitHub code search indexes.
type search struct {
	Repo, Term string
	// Rule is the rule that needs the search.
	Rule string
}

// defaultBranch is the default branch of every sandbox repository, the
// ref at which the files a search returns are read.
const defaultBranch = "main"

// searchInterval spaces code searches. GitHub limits the code search
// endpoint to 10 requests per minute for authenticated requests (the
// code_search resource of GET /rate_limit; documented in the "Rate limit"
// and "Search code" sections of https://docs.github.com/en/rest/search/search),
// which is one request every 6 seconds; one more second is a margin.
const searchInterval = 7 * time.Second

func pr(repo string, number int) ghrecord.Target {
	return ghrecord.Target{Owner: org, Repo: repo, Number: number}
}

// scenarios lists what a review of each scenario reads. The numbers are the
// sandbox pull requests described in testdata/scenarios.
var scenarios = map[string]scenario{
	"S1": {targets: []ghrecord.Target{pr("sample-app", 2)}},
	// The cascade within a limit of 2: the root, its entries at depth 1
	// (including the tracking issue and a missing number) and depth 2.
	"S2": {searches: []search{
		// core-lib S2 removes legacyToken(), which sample-app still calls on main.
		{Repo: "sample-app", Term: "legacyToken", Rule: "uses-removed-api"},
	}, targets: []ghrecord.Target{
		pr("sample-app", 7),
		pr("core-lib", 3), pr("sample-app", 2), pr("sample-app", 1), pr("core-lib", 9999),
		pr("kmp-shared", 2),
	}},
	"S3": {targets: []ghrecord.Target{pr("core-lib", 1)}},
	"S4": {targets: []ghrecord.Target{pr("feature-lib", 1)}},
	"S5": {targets: []ghrecord.Target{pr("sample-app", 3), pr("sample-app", 4)}},
	"S6": {targets: []ghrecord.Target{pr("sample-app", 5), pr("sample-app", 6), pr("core-lib", 2), pr("kmp-shared", 1)}},
	// CR-level detectors: missing tests and variant asymmetry in sample-app,
	// whose base branch holds a test written first; source-set asymmetry and
	// an expect without an iOS actual in kmp-shared.
	"S7": {targets: []ghrecord.Target{pr("sample-app", 8), pr("kmp-shared", 3)}, searches: []search{
		// kmp-shared S7 adds expect fun deviceModel() with an androidMain actual only.
		{Repo: "kmp-shared", Term: "deviceModel", Rule: "kmp-expect-without-actual"},
	}},
	"queue": {requests: []ghrecord.Request{{
		Path:  "/search/issues",
		Query: map[string]string{"q": "is:pr is:open review-requested:@me org:" + org, "per_page": "100"},
	}}},
}

var synthetic = []struct {
	login string
	id    int64
}{{"sandbox-reviewer", 1001}, {"sandbox-author", 1002}}

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr)) }

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("recorder", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", filepath.Join("testdata", "github"), "output directory")
	api := fs.String("api", "https://api.github.com", "REST API base URL")
	extend := fs.Bool("extend", false, "record only the trees and searches, keeping the existing recordings")
	interval := fs.Duration("search-interval", searchInterval, "minimum time between code searches")
	usage := func(msg string) int {
		if msg != "" {
			fmt.Fprintln(stderr, "recorder:", msg)
		}
		fmt.Fprintln(stderr, "usage: recorder [-out dir] [-api url] [-extend] <scenario>... | all")
		return 2
	}
	if err := fs.Parse(args); err != nil {
		return usage(err.Error())
	}
	names, err := scenarioNames(fs.Args())
	if err != nil {
		return usage(err.Error())
	}
	token := getenv("ACR_RECORDER_TOKEN")
	if token == "" {
		return usage("ACR_RECORDER_TOKEN is not set")
	}
	sanitizer, err := newSanitizer(getenv("ACR_SANDBOX_LOGINS"))
	if err != nil {
		return usage(err.Error())
	}

	httpClient := &http.Client{Timeout: 60 * time.Second}
	// One throttle for the whole run, so that searches of different
	// scenarios are spaced too.
	throttle := &ghrecord.Throttle{Interval: *interval}
	for _, name := range names {
		dir := filepath.Join(*out, name)
		if !*extend {
			if err := resetDir(dir); err != nil {
				fmt.Fprintln(stderr, "recorder:", err)
				return 1
			}
		}
		c := &ghrecord.Client{BaseURL: *api, Token: token, HTTP: httpClient, Sanitizer: sanitizer, Dir: dir, SearchThrottle: throttle}
		var err error
		if *extend {
			err = recordExtension(c, name, scenarios[name], stderr)
		} else {
			err = record(c, name, scenarios[name], stderr)
		}
		if err != nil {
			fmt.Fprintf(stderr, "recorder: %s: %v\n", name, err)
			return 1
		}
		fmt.Fprintf(stdout, "recorded %s into %s\n", name, dir)
	}
	return 0
}

func scenarioNames(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errors.New("no scenario given")
	}
	if len(args) == 1 && args[0] == "all" {
		var names []string
		for name := range scenarios {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}
	for _, name := range args {
		if _, ok := scenarios[name]; !ok {
			return nil, fmt.Errorf("unknown scenario %q", name)
		}
	}
	return args, nil
}

// newSanitizer maps the reviewer and author logins, in this order, to the
// synthetic identities.
func newSanitizer(logins string) (*ghrecord.Sanitizer, error) {
	parts := strings.Split(logins, ",")
	if len(parts) != len(synthetic) {
		return nil, errors.New("ACR_SANDBOX_LOGINS must be <reviewer>,<author>")
	}
	ids := make([]ghrecord.Identity, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, errors.New("ACR_SANDBOX_LOGINS has an empty login")
		}
		ids[i] = ghrecord.Identity{Login: p, Synthetic: synthetic[i].login, ID: synthetic[i].id}
	}
	s, err := ghrecord.NewSanitizer(ids)
	if err != nil {
		return nil, fmt.Errorf("ACR_SANDBOX_LOGINS: %w", err)
	}
	return s, nil
}

// resetDir creates dir and removes the recordings of a previous run.
func resetDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	old, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}

func record(c *ghrecord.Client, name string, s scenario, stderr io.Writer) error {
	for _, t := range s.targets {
		if err := c.Record(t); err != nil {
			return err
		}
	}
	for _, req := range s.requests {
		status, _, err := c.Get(req)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("%s: status %d", req.Path, status)
		}
	}
	return recordSearches(c, name, s, stderr)
}

// recordExtension records what an earlier run of the scenario did not:
// the trees of its recorded pull requests and its code searches.
func recordExtension(c *ghrecord.Client, name string, s scenario, stderr io.Writer) error {
	for _, t := range s.targets {
		if err := c.RecordTreeOfRecorded(t); err != nil {
			return err
		}
	}
	return recordSearches(c, name, s, stderr)
}

// recordSearches records the code searches of a scenario one after the
// other, never in parallel, and warns about a search GitHub could not
// complete: its empty result is recorded as returned.
func recordSearches(c *ghrecord.Client, name string, s scenario, stderr io.Writer) error {
	for _, q := range s.searches {
		res, err := c.RecordSearch(org, q.Repo, q.Term, defaultBranch)
		if err != nil {
			return err
		}
		if res.Incomplete {
			fmt.Fprintf(stderr, "recorder: warning: %s: search %q in %s returned incomplete results (%d found)\n", name, q.Term, q.Repo, len(res.Paths))
		}
	}
	return nil
}
