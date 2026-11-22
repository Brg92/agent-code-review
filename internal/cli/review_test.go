package cli

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/reviewreplay"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

var clockNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// instantClock never waits: the retries of the transport cost no time.
type instantClock struct{ at time.Time }

func (c *instantClock) Now() time.Time { return c.at }
func (c *instantClock) Sleep(ctx context.Context, d time.Duration) error {
	c.at = c.at.Add(d)
	return ctx.Err()
}

func packsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "packs"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// reviewEnv is the environment of a review whose GitHub is the replay
// server r, with a work directory of its own. The token is the canary,
// given the way CI gives it.
func reviewEnv(t *testing.T, r *sandbox.Replay) (env, string) {
	t.Helper()
	work := t.TempDir()
	vars := map[string]string{
		"ACR_WORKDIR":      work,
		"ACR_PACKS_DIR":    packsDir(t),
		"ACR_GITHUB_TOKEN": reviewreplay.Canary,
	}
	clock := &instantClock{at: clockNow}
	return env{
		getenv: func(k string) string { return vars[k] },
		git: auth.RunnerFunc(func(context.Context, string, ...string) (string, string, error) {
			return "", "", errors.New("git is not used")
		}),
		now:    clock.Now,
		random: rand.New(rand.NewSource(1)),
		newReader: func(host string, tokens github.Resolver, opts github.Options) (base.ForgeProvider, error) {
			opts.Transport.Now, opts.Transport.Sleep = clock.Now, clock.Sleep
			return github.NewLoopback(host, r.URL(), tokens, opts)
		},
		userConfigDir: func() (string, error) { return filepath.Join(work, "config"), nil },
		userCacheDir:  func() (string, error) { return filepath.Join(work, "cache"), nil },
	}, work
}

func goldenText(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "review", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// portable is the first line of what next prints with the work directory
// and its separators the same on every system.
func portable(out, work string) string {
	head, rest, found := strings.Cut(out, "\n")
	if !found {
		return out
	}
	head = strings.ReplaceAll(head, work, "<workdir>")
	if i := strings.Index(head, "<workdir>"); i >= 0 {
		head = head[:i] + strings.ReplaceAll(head[i:], string(filepath.Separator), "/")
	}
	return head + "\n" + rest
}

func noCanary(t *testing.T, label string, text string) {
	t.Helper()
	if strings.Contains(text, reviewreplay.Canary) {
		t.Errorf("%s holds the token:\n%s", label, text)
	}
}

const s2Root = "https://github.com/acr-plugin-sandbox/sample-app/pull/7"

func TestReviewThenNextOnS2(t *testing.T) {
	for _, ref := range []string{s2Root, "acr-plugin-sandbox/sample-app#7", s2Root + "/files?diff=split#top"} {
		t.Run(ref, func(t *testing.T) {
			e, work := reviewEnv(t, reviewreplay.S2(t))
			r := runWith(t, e, "review", ref)
			if r.code != exitOK || r.stderr != "" || r.stdout != goldenText(t, "s2-review.txt") {
				t.Fatalf("review: code %d\nstderr: %s\nstdout:\n%s", r.code, r.stderr, r.stdout)
			}
			noCanary(t, "stdout", r.stdout)

			n := runWith(t, e, "next")
			if n.code != exitOK || n.stderr != "" || portable(n.stdout, work) != goldenText(t, "s2-next-1.txt") {
				t.Fatalf("next: code %d\nstderr: %s\nstdout:\n%s", n.code, n.stderr, n.stdout)
			}
			// The run is named by its id as well.
			id := "20261120T120000Z-52fdfc07"
			// The first part was served; the run named by its id says what is awaited.
			if m := runWith(t, e, "next", "--run", id); m.code != exitOK || !strings.HasPrefix(m.stdout, "Write the judgement to ") {
				t.Errorf("next --run: code %d\n%s", m.code, m.stdout)
			}
		})
	}
}

func TestNextMovesForwardAndPartReadsAgain(t *testing.T) {
	e, work := reviewEnv(t, reviewreplay.S2(t))
	if r := runWith(t, e, "review", s2Root); r.code != exitOK {
		t.Fatalf("review: %d\n%s", r.code, r.stderr)
	}
	first := runWith(t, e, "next")
	if first.code != exitOK || portable(first.stdout, work) != goldenText(t, "s2-next-1.txt") {
		t.Fatalf("next: code %d\n%s", first.code, first.stdout)
	}
	// The one part was served: the command says what to do, as often as asked.
	judgement := strings.TrimSuffix(strings.TrimPrefix(strings.Split(first.stdout, "\n")[0], "Judge github:github.com/acr-plugin-sandbox/feature-lib#1 part 1 of 1 — write the judgement to "), "\n")
	for range 2 {
		wait := runWith(t, e, "next")
		if wait.code != exitOK || wait.stdout != "Write the judgement to "+judgement+", then run agent-code-review next\n" || wait.stderr != "" {
			t.Errorf("waiting: code %d, stdout %q, stderr %q", wait.code, wait.stdout, wait.stderr)
		}
	}
	// --part reads it again without moving.
	again := runWith(t, e, "next", "--part", "1")
	if again.code != exitOK || again.stdout != first.stdout {
		t.Errorf("--part 1: code %d\n%s", again.code, again.stdout)
	}
	if again := runWith(t, e, "next", "--part=1", "--run", "20261120T120000Z-52fdfc07"); again.stdout != first.stdout {
		t.Errorf("--part with --run: %s", again.stdout)
	}
	if wait := runWith(t, e, "next"); !strings.HasPrefix(wait.stdout, "Write the judgement to ") {
		t.Errorf("after --part: %s", wait.stdout)
	}
	// A part the change request does not have is a usage error.
	out := runWith(t, e, "next", "--part", "2")
	if out.code != exitUsage || out.stdout != "" || !strings.Contains(out.stderr, "no part 2") || !strings.Contains(out.stderr, "1 part") {
		t.Errorf("--part 2: code %d, stderr:\n%s", out.code, out.stderr)
	}
	failShape(t, out.stderr, true)
	// Judged, the next change request is served.
	if err := os.WriteFile(judgement, []byte(`{"schema":1,"cr":"x","headSha":"e7665e368da1fcad940a56e09eeb9f10ad4b4a45","findings":[],"preExisting":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := runWith(t, e, "next"); n.code != exitOK || !strings.HasPrefix(n.stdout, "Judge github:github.com/acr-plugin-sandbox/kmp-shared#2 part 1 of 1") {
		t.Errorf("after the judgement: %s", n.stdout)
	}
}

func TestVerboseWritesTheRunLogToStderr(t *testing.T) {
	e, work := reviewEnv(t, reviewreplay.S2(t))
	r := runWith(t, e, "review", "--verbose", s2Root)
	if r.code != exitOK || r.stdout != goldenText(t, "s2-review.txt") {
		t.Fatalf("review: code %d\n%s", r.code, r.stderr)
	}
	log, err := os.ReadFile(filepath.Join(work, "runs", "20261120T120000Z-52fdfc07", runlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(log) != r.stderr || len(log) == 0 {
		t.Errorf("--verbose differs from run.log:\n%s", r.stderr)
	}
	noCanary(t, "run.log", string(log))
	if !strings.Contains(r.stderr, "\treview\t") || strings.Contains(r.stderr, "?") {
		t.Errorf("stderr:\n%s", r.stderr)
	}

	// next appends to the same log, and --verbose is its flag too.
	n := runWith(t, e, "next", "--verbose")
	if n.code != exitOK || !strings.Contains(n.stderr, "\tnext\t") || strings.Contains(n.stderr, "\treview\t") {
		t.Errorf("next --verbose: code %d, stderr:\n%s", n.code, n.stderr)
	}
	after, _ := os.ReadFile(filepath.Join(work, "runs", "20261120T120000Z-52fdfc07", runlog.FileName))
	if !strings.HasPrefix(string(after), string(log)) || !strings.HasSuffix(string(after), n.stderr) {
		t.Error("next did not append its lines to run.log")
	}
	noCanary(t, "run.log", string(after))

	quiet := runWith(t, e, "next")
	if quiet.stderr != "" {
		t.Errorf("without --verbose, stderr = %q", quiet.stderr)
	}
}

// fail checks the fixed shape of the error message: what failed, where,
// why and what to do, then the log when a run exists.
func failShape(t *testing.T, stderr string, withLog bool) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	want := []string{"agent-code-review: ", "  where: ", "  why: ", "  what to do: "}
	if withLog {
		want = append(want, "  log: ")
	}
	if len(lines) != len(want) {
		t.Fatalf("the message has %d lines, want %d:\n%s", len(lines), len(want), stderr)
	}
	for i, prefix := range want {
		if !strings.HasPrefix(lines[i], prefix) || strings.TrimSpace(strings.TrimPrefix(lines[i], prefix)) == "" {
			t.Errorf("line %d = %q, want it to start with %q and say something", i+1, lines[i], prefix)
		}
	}
	return lines
}

func TestReviewFailures(t *testing.T) {
	synthetic := func(routes ...sandbox.Route) func(t *testing.T) *sandbox.Replay {
		return func(t *testing.T) *sandbox.Replay {
			r := sandbox.NewSynthetic(t)
			for _, rt := range routes {
				r.Add(rt)
			}
			return r
		}
	}
	tests := []struct {
		name    string
		replay  func(t *testing.T) *sandbox.Replay
		args    []string
		code    int
		withLog bool
		stderr  []string
	}{
		{"root not found", func(t *testing.T) *sandbox.Replay { return reviewreplay.S2(t) },
			[]string{"review", "acr-plugin-sandbox/core-lib#9999"}, exitPlatform, true,
			[]string{"could not read the root change request", "core-lib#9999", "not-found, status 404", "check the reference"}},
		{"rate limit beyond the longest wait", synthetic(sandbox.Route{Path: "/repos/o/r/pulls/7", Status: 403,
			Header: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "4102444800"}, Body: `{"message":"API rate limit exceeded"}`}),
			[]string{"review", "o/r#7"}, exitPlatform, true,
			[]string{"could not read the root change request", "rate-limited", "run the command again later"}},
		{"server error", synthetic(sandbox.Route{Path: "/repos/o/r/pulls/7", Status: 502, Body: `{"message":"bad gateway"}`}),
			[]string{"review", "o/r#7"}, exitPlatform, true, []string{"transport, status 502", "check the network"}},
		{"a permission is missing", synthetic(sandbox.Route{Path: "/repos/o/r/pulls/7", Status: 403,
			Header: map[string]string{"X-Accepted-GitHub-Permissions": "pull_requests=read"}, Body: `{"message":"Resource not accessible by personal access token"}`}),
			[]string{"review", "o/r#7"}, exitAuth, true,
			[]string{"forbidden (needs pull_requests: read), status 403", "the token lacks pull_requests: read; add it to the fine-grained token"}},
		{"the token is refused", synthetic(sandbox.Route{Path: "/repos/o/r/pulls/7", Status: 401, Body: `{"message":"Bad credentials"}`}),
			[]string{"review", "o/r#7"}, exitAuth, true, []string{"forbidden, status 401", "agent-code-review auth login"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, work := reviewEnv(t, tt.replay(t))
			r := runWith(t, e, tt.args...)
			if r.code != tt.code || r.stdout != "" {
				t.Fatalf("code %d, stdout %q, stderr:\n%s", r.code, r.stdout, r.stderr)
			}
			lines := failShape(t, r.stderr, tt.withLog)
			for _, want := range tt.stderr {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
				}
			}
			if log := strings.TrimPrefix(lines[len(lines)-1], "  log: "); !strings.HasPrefix(log, work) || filepath.Base(log) != runlog.FileName {
				t.Errorf("log line %q", lines[len(lines)-1])
			}
			noCanary(t, "stderr", r.stderr)
		})
	}
}

func TestReviewWithoutAToken(t *testing.T) {
	e, _ := reviewEnv(t, sandbox.NewSynthetic(t))
	git := &scriptedGit{answers: map[string]gitAnswer{helperQuery: {stdout: "credential.helper\x00cache\x00"}, fill: noEntry}}
	e.git = git
	vars := map[string]string{"ACR_WORKDIR": filepath.Join(t.TempDir(), "w"), "ACR_PACKS_DIR": packsDir(t)}
	e.getenv = func(k string) string { return vars[k] }
	e.newReader = func(host string, tokens github.Resolver, opts github.Options) (base.ForgeProvider, error) {
		return github.New(host, tokens, opts)
	}
	r := runWith(t, e, "review", "o/r#7")
	if r.code != exitAuth || !strings.Contains(r.stderr, "no token") || !strings.Contains(r.stderr, "`agent-code-review auth login` or set ACR_GITHUB_TOKEN") {
		t.Errorf("code %d, stderr:\n%s", r.code, r.stderr)
	}
	failShape(t, r.stderr, true)
}

func TestReviewUsageErrors(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		stderr string
	}{
		{"no reference", []string{"review"}, "missing the pull request"},
		{"two references", []string{"review", "o/r#1", "o/r#2"}, "one pull request"},
		{"not a reference", []string{"review", "banana"}, `"banana" is not a pull request`},
		{"a reference that is not a pull request", []string{"review", "https://example.com/"}, "is not a pull request"},
		{"unknown flag", []string{"review", "--nope", "o/r#1"}, "unknown flag"},
		{"a flag that needs a value", []string{"next", "--run"}, "flag needs an argument"},
		{"next with an argument", []string{"next", "extra"}, "takes no argument"},
		{"part zero", []string{"next", "--part", "0"}, "from 1"},
		{"part negative", []string{"next", "--part=-2"}, "from 1"},
		{"part not a number", []string{"next", "--part", "two"}, "not a number"},
		{"part without a value", []string{"next", "--part"}, "flag needs an argument"},
		{"part on review", []string{"review", "--part", "1", "o/r#1"}, "unknown flag"},
		{"a run id that is not one", []string{"next", "--run", "../x"}, "run id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _ := reviewEnv(t, sandbox.NewSynthetic(t))
			e.newReader = func(string, github.Resolver, github.Options) (base.ForgeProvider, error) {
				t.Error("a reader was made")
				return nil, errors.New("no reader")
			}
			r := runWith(t, e, tt.args...)
			if r.code != exitUsage || r.stdout != "" || !strings.Contains(r.stderr, tt.stderr) {
				t.Errorf("code %d, stdout %q, stderr:\n%s", r.code, r.stdout, r.stderr)
			}
		})
	}
}

func TestReviewOfAnUnsupportedHost(t *testing.T) {
	for _, ref := range []string{
		"https://ghe.example.com/o/r/pull/1",
		"https://gitlab.com/group/project/-/merge_requests/3",
	} {
		t.Run(ref, func(t *testing.T) {
			e, _ := reviewEnv(t, sandbox.NewSynthetic(t))
			asked := false
			e.getenv = func(k string) string {
				if k == "ACR_GITHUB_TOKEN" {
					asked = true
				}
				return map[string]string{"ACR_WORKDIR": t.TempDir(), "ACR_PACKS_DIR": packsDir(t)}[k]
			}
			e.newReader = func(host string, tokens github.Resolver, opts github.Options) (base.ForgeProvider, error) {
				return github.New(host, tokens, opts)
			}
			r := runWith(t, e, "review", ref)
			if r.code != exitUsage || r.stdout != "" || asked {
				t.Errorf("code %d, token asked %v, stderr:\n%s", r.code, asked, r.stderr)
			}
			failShape(t, r.stderr, false)
			if !strings.Contains(r.stderr, "not supported") {
				t.Errorf("stderr:\n%s", r.stderr)
			}
		})
	}
}

func TestNextFailures(t *testing.T) {
	work := func(t *testing.T) (env, string) {
		e, w := reviewEnv(t, sandbox.NewSynthetic(t))
		e.newReader = func(string, github.Resolver, github.Options) (base.ForgeProvider, error) {
			t.Error("next reads no platform")
			return nil, errors.New("no reader")
		}
		return e, w
	}
	t.Run("no run", func(t *testing.T) {
		e, _ := work(t)
		r := runWith(t, e, "next")
		if r.code != exitState || r.stdout != "" || !strings.Contains(r.stderr, "agent-code-review review") {
			t.Errorf("code %d, stderr:\n%s", r.code, r.stderr)
		}
		failShape(t, r.stderr, false)
	})
	t.Run("a run that does not exist", func(t *testing.T) {
		e, _ := work(t)
		if r := runWith(t, e, "next", "--run", "20261120T120000Z-deadbeef"); r.code != exitState {
			t.Errorf("code %d, stderr:\n%s", r.code, r.stderr)
		}
	})
	t.Run("a run in another phase", func(t *testing.T) {
		e, w := work(t)
		root, err := core.ParseKey("github:github.com/o/r#7")
		if err != nil {
			t.Fatal(err)
		}
		st, err := state.New("20261120T120000Z-deadbeef", root, core.DefaultLimits(), []core.Entry{{CR: root, Status: core.Pending}}, clockNow)
		if err != nil {
			t.Fatal(err)
		}
		dir, err := state.CreateRunDir(w, st.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Save(dir, st); err != nil {
			t.Fatal(err)
		}
		r := runWith(t, e, "next")
		if r.code != exitState || !strings.Contains(r.stderr, "its phase is resolved") {
			t.Errorf("code %d, stderr:\n%s", r.code, r.stderr)
		}
		failShape(t, r.stderr, true)
	})
	t.Run("a state of an unknown version", func(t *testing.T) {
		e, w := work(t)
		dir, err := state.CreateRunDir(w, "20261120T120000Z-deadbeef")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, state.FileName), []byte(`{"schema": 2}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if r := runWith(t, e, "next"); r.code != exitState || !strings.Contains(r.stderr, "unsupported schema version 2") {
			t.Errorf("code %d, stderr:\n%s", r.code, r.stderr)
		}
	})
}

func TestEnvironmentFailures(t *testing.T) {
	t.Run("no packs", func(t *testing.T) {
		e, _ := reviewEnv(t, sandbox.NewSynthetic(t))
		empty := t.TempDir()
		e.getenv = func(k string) string {
			return map[string]string{"ACR_WORKDIR": t.TempDir(), "ACR_PACKS_DIR": empty}[k]
		}
		r := runWith(t, e, "review", "o/r#7")
		if r.code != exitUnexpected || !strings.Contains(r.stderr, "no packs found") {
			t.Errorf("code %d, stderr:\n%s", r.code, r.stderr)
		}
		failShape(t, r.stderr, false)
	})
	t.Run("an invalid user configuration", func(t *testing.T) {
		e, work := reviewEnv(t, sandbox.NewSynthetic(t))
		cfg := filepath.Join(work, "config", "agent-code-review")
		if err := os.MkdirAll(cfg, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"schema": 1, "keepDays": "many"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		r := runWith(t, e, "review", "o/r#7")
		if r.code != exitValidation || !strings.Contains(r.stderr, "invalid user configuration") || !strings.Contains(r.stderr, "/keepDays") {
			t.Errorf("code %d, stderr:\n%s", r.code, r.stderr)
		}
		failShape(t, r.stderr, false)
	})
	t.Run("no work directory", func(t *testing.T) {
		e, _ := reviewEnv(t, sandbox.NewSynthetic(t))
		e.getenv = func(k string) string { return map[string]string{"ACR_WORKDIR": "relative/dir"}[k] }
		if r := runWith(t, e, "next"); r.code != exitUnexpected || !strings.Contains(r.stderr, "ACR_WORKDIR") {
			t.Errorf("code %d, stderr:\n%s", r.code, r.stderr)
		}
	})
	t.Run("no cache directory", func(t *testing.T) {
		e, _ := reviewEnv(t, sandbox.NewSynthetic(t))
		e.getenv = func(string) string { return "" }
		e.userCacheDir = func() (string, error) { return "", errors.New("no home") }
		if r := runWith(t, e, "next"); r.code != exitUnexpected || !strings.Contains(r.stderr, "work directory") {
			t.Errorf("code %d, stderr:\n%s", r.code, r.stderr)
		}
	})
}
