package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/guards"
	"github.com/Brg92/agent-code-review/internal/testutil/ghserver"
	"github.com/Brg92/agent-code-review/internal/testutil/reviewreplay"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// errorPath is a command run into one of its errors, and the exit code the
// contract says it ends with.
type errorPath struct {
	command string
	name    string
	want    int
	run     func(t *testing.T) result
}

// noRun is an environment whose work directory has no run.
func noRun(t *testing.T) env {
	t.Helper()
	e, _ := reviewEnv(t, sandbox.NewSynthetic(t))
	return e
}

// reviewOf runs review o/r#7 against a GitHub that answers the pull request
// with status.
func reviewOf(status int, header map[string]string) func(t *testing.T) result {
	return func(t *testing.T) result {
		r := sandbox.NewSynthetic(t)
		r.Add(sandbox.Route{Path: "/repos/o/r/pulls/7", Status: status, Header: header, Body: `{"message":"no"}`})
		e, _ := reviewEnv(t, r)
		return runWith(t, e, "review", "o/r#7")
	}
}

// queueAnswering runs queue against a GitHub that answers the search with
// status.
func queueAnswering(status int) func(t *testing.T) result {
	return func(t *testing.T) result {
		r := sandbox.NewSynthetic(t)
		route := queueRoute(t, "30")
		route.Status, route.Body = status, `{"message":"no"}`
		r.Add(route)
		e, _ := reviewEnv(t, r)
		return runWith(t, e, "queue")
	}
}

// doctorWith runs doctor in a world changed by setup.
func doctorWith(setup func(w *world), args ...string) func(t *testing.T) result {
	return func(t *testing.T) result {
		w := newWorld(t)
		setup(w)
		return w.run(t, args...)
	}
}

func noToken(w *world) { w.vars["ACR_GITHUB_TOKEN"] = "" }

func withUserConfig(t *testing.T, e *env, content string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-code-review", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	e.userConfigDir = func() (string, error) { return dir, nil }
}

func errorPaths() []errorPath {
	return []errorPath{
		{"version", "success", exitOK, func(t *testing.T) result { return runWith(t, env{}, "version") }},
		{"version", "an argument", exitUsage, func(t *testing.T) result { return runWith(t, env{}, "version", "now") }},

		{"auth", "no subcommand", exitUsage, func(t *testing.T) result { return runWith(t, env{}, "auth") }},
		{"auth", "no token", exitAuth, func(t *testing.T) result {
			w := newWorld(t)
			noToken(w)
			return runWith(t, w.e, "auth", "status")
		}},
		{"auth", "no credential helper", exitUnsafeStorage, func(t *testing.T) result {
			return runWith(t, env{git: loginGit(""), term: &fakeTerm{}, stdin: strings.NewReader(testToken + "\n")}, "auth", "login")
		}},
		{"auth", "plain text helper", exitUnsafeStorage, func(t *testing.T) result {
			return runWith(t, env{git: loginGit("store"), term: &fakeTerm{}, stdin: strings.NewReader(testToken + "\n")}, "auth", "login")
		}},
		{"auth", "helper that does not store", exitUnsafeStorage, func(t *testing.T) result {
			git := loginGit("acme-vault")
			git.answers[fill] = noEntry
			git.answers["credential reject"] = gitAnswer{}
			return runWith(t, env{git: git, term: &fakeTerm{}, stdin: strings.NewReader(testToken + "\n")}, "auth", "login")
		}},
		{"auth", "echo cannot be hidden", exitUnexpected, func(t *testing.T) result {
			term := &fakeTerm{terminal: true, disableErr: errors.New("not a terminal")}
			return runWith(t, env{git: loginGit("osxkeychain"), term: term, stdin: strings.NewReader("")}, "auth", "login")
		}},
		{"auth", "interrupted", exitInterrupted, func(t *testing.T) result {
			stdin := blockingReader{done: make(chan struct{})}
			t.Cleanup(func() { close(stdin.done) })
			notify := func(c chan<- os.Signal) func() { c <- os.Interrupt; return func() {} }
			return runWith(t, env{git: loginGit("osxkeychain"), term: &fakeTerm{terminal: true}, stdin: stdin, notify: notify}, "auth", "login")
		}},

		{"review", "no pull request", exitUsage, func(t *testing.T) result { return runWith(t, noRun(t), "review") }},
		{"review", "a platform it does not read", exitUsage, func(t *testing.T) result {
			return runWith(t, noRun(t), "review", "https://gitlab.com/acme/app/-/merge_requests/1")
		}},
		{"review", "invalid user config", exitValidation, func(t *testing.T) result {
			e := noRun(t)
			withUserConfig(t, &e, `{"schema":1,"keepDays":-1}`)
			return runWith(t, e, "review", "o/r#7")
		}},
		{"review", "no packs", exitUnexpected, func(t *testing.T) result {
			e := noRun(t)
			dir := t.TempDir()
			vars := map[string]string{"ACR_PACKS_DIR": dir}
			e.getenv = func(k string) string { return vars[k] }
			return runWith(t, e, "review", "o/r#7")
		}},
		{"review", "token refused", exitAuth, reviewOf(401, nil)},
		{"review", "server error", exitPlatform, reviewOf(502, nil)},

		{"next", "a part that is not a number", exitUsage, func(t *testing.T) result { return runWith(t, noRun(t), "next", "--part", "x") }},
		{"next", "no run", exitState, func(t *testing.T) result { return runWith(t, noRun(t), "next") }},
		{"next", "work directory that cannot be read", exitUnexpected, func(t *testing.T) result {
			e := noRun(t)
			work := t.TempDir()
			if err := os.WriteFile(filepath.Join(work, "runs"), []byte("a file"), 0o600); err != nil {
				t.Fatal(err)
			}
			vars := map[string]string{"ACR_WORKDIR": work}
			e.getenv = func(k string) string { return vars[k] }
			return runWith(t, e, "next")
		}},

		{"finalize", "an argument", exitUsage, func(t *testing.T) result { return runWith(t, noRun(t), "finalize", "now") }},
		{"finalize", "no run", exitState, func(t *testing.T) result { return runWith(t, noRun(t), "finalize") }},
		{"finalize", "no judgement", exitValidation, func(t *testing.T) result {
			e, _, _, _ := reviewedS1(t)
			return runWith(t, e, "finalize")
		}},
		{"finalize", "invalid user config", exitValidation, func(t *testing.T) result {
			e, _, _, _ := reviewedS1(t)
			withUserConfig(t, &e, `{"schema":2}`)
			return runWith(t, e, "finalize")
		}},

		{"show", "two reports", exitUsage, func(t *testing.T) result { return runWith(t, noRun(t), "show", "a#1", "b#2") }},
		{"show", "no run", exitState, func(t *testing.T) result { return runWith(t, noRun(t), "show") }},
		{"show", "a folder with ..", exitPath, func(t *testing.T) result {
			e, _, _, judgement := reviewedS1(t)
			copyFile(t, []string{"testdata", "scenarios", "S1", "judgements", "sample-app-2.json"}, judgement)
			if r := runWith(t, e, "finalize"); r.code != exitOK {
				t.Fatalf("finalize: %d\n%s", r.code, r.stderr)
			}
			return runWith(t, e, "show", "--save", t.TempDir()+string(filepath.Separator)+".."+string(filepath.Separator)+"x")
		}},

		{"publish", "nothing to do", exitUsage, func(t *testing.T) result { return runWith(t, noRun(t), "publish") }},
		{"publish", "no run", exitState, func(t *testing.T) result { return runWith(t, noRun(t), "publish", "--apply") }},
		{"publish", "head changed", exitHeadChanged, func(t *testing.T) result {
			e, srv, _ := finalizedS1(t)
			if r := runWith(t, e, "publish", "--plan", "both"); r.code != exitOK {
				t.Fatalf("plan: %s", r.stderr)
			}
			srv.Repo("acr-plugin-sandbox/sample-app").PRs[2].Head = strings.Repeat("e", 40)
			return runWith(t, e, "publish", "--apply")
		}},
		{"publish", "server error", exitPlatform, func(t *testing.T) result {
			e, srv, _ := finalizedS1(t)
			srv.Fail("GET", `/pulls/2$`, ghserver.Fault{Status: 502, Body: `{}`})
			return runWith(t, e, "publish", "--plan", "comments")
		}},
		{"publish", "no permission", exitAuth, func(t *testing.T) result {
			e, srv, _ := finalizedS1(t)
			srv.Fail("GET", `/pulls/2$`, ghserver.Fault{Status: 403, Body: `{"message":"Resource not accessible"}`})
			return runWith(t, e, "publish", "--plan", "comments")
		}},

		{"queue", "a limit of zero", exitUsage, func(t *testing.T) result { return runWith(t, noRun(t), "queue", "--limit", "0") }},
		{"queue", "token refused", exitAuth, queueAnswering(401)},
		{"queue", "server error", exitPlatform, queueAnswering(500)},
		{"queue", "work directory that cannot be read", exitUnexpected, func(t *testing.T) result {
			e, work, _ := queueEnv(t)
			if err := os.WriteFile(filepath.Join(work, "runs"), []byte("a file"), 0o600); err != nil {
				t.Fatal(err)
			}
			return runWith(t, e, "queue")
		}},

		{"doctor", "success", exitOK, doctorWith(func(*world) {})},
		{"doctor", "a repository that is not owner/repo", exitUsage, doctorWith(func(*world) {}, "--repo", "acme")},
		{"doctor", "no token", exitAuth, doctorWith(noToken)},
		{"doctor", "no credential helper", exitUnsafeStorage, doctorWith(func(w *world) {
			noToken(w)
			w.git.helpers = ""
			w.git.fill = func() (string, string, error) {
				return "username=acr-api\npassword=" + reviewreplay.Canary + "\n", "", nil
			}
		})},
		{"doctor", "platform down", exitPlatform, doctorWith(func(w *world) { serve(w.srv, 500, "/user", nil, nil, `{}`) })},
		{"doctor", "invalid pack", exitValidation, doctorWith(func(w *world) {
			dir := filepath.Join(t0(w), "packs")
			if err := os.MkdirAll(filepath.Join(dir, "broken"), 0o700); err != nil {
				panic(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "broken", "manifest.json"), []byte(`{`), 0o600); err != nil {
				panic(err)
			}
			w.vars["ACR_PACKS_DIR"] = dir
		})},
		{"doctor", "no packs", exitUnexpected, doctorWith(func(w *world) { w.vars["ACR_PACKS_DIR"] = t0(w) })},

		{"check-criteria", "valid", exitOK, func(t *testing.T) result { return runWith(t, offline(t), "check-criteria", fixture(t, "valid")) }},
		{"check-criteria", "two directories", exitUsage, func(t *testing.T) result { return runWith(t, offline(t), "check-criteria", "a", "b") }},
		{"check-criteria", "not a directory", exitUsage, func(t *testing.T) result {
			return runWith(t, offline(t), "check-criteria", filepath.Join(fixture(t, "valid"), "nothing"))
		}},
		{"check-criteria", "something would be ignored", exitValidation, func(t *testing.T) result {
			return runWith(t, offline(t), "check-criteria", fixture(t, "ignored"))
		}},
		{"check-criteria", "an invalid config.json", exitValidation, func(t *testing.T) result {
			return runWith(t, offline(t), "check-criteria", fixture(t, "invalid-config"))
		}},
		{"check-criteria", "no packs", exitUnexpected, func(t *testing.T) result {
			e := offline(t)
			vars := map[string]string{"ACR_PACKS_DIR": t.TempDir()}
			e.getenv = func(k string) string { return vars[k] }
			return runWith(t, e, "check-criteria", fixture(t, "valid"))
		}},
	}
}

// t0 is a directory of the world's work directory, for what a case needs
// a place for.
func t0(w *world) string { return w.work }

// TestEveryCommandEndsWithTheCodeTheContractLists runs each command into
// its errors and checks the code against the code and against the table of
// docs/exit-codes.md.
func TestEveryCommandEndsWithTheCodeTheContractLists(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "exit-codes.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := guards.ParseExitCodes(string(data))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[int]guards.ExitCode{}
	for _, r := range rows {
		listed[r.Code] = r
	}
	seen := map[int]bool{}
	commands := map[string]bool{}
	for _, p := range errorPaths() {
		t.Run(p.command+"/"+p.name, func(t *testing.T) {
			got := p.run(t)
			if got.code != p.want {
				t.Fatalf("code %d, want %d\nstdout: %s\nstderr: %s", got.code, p.want, got.stdout, got.stderr)
			}
			row, ok := listed[got.code]
			switch {
			case !ok:
				t.Errorf("%d is not in docs/exit-codes.md", got.code)
			case row.Status != "Binding":
				t.Errorf("%d is returned, but the document says %s", got.code, row.Status)
			case !row.All && !slices.Contains(row.Commands, p.command):
				t.Errorf("%s returns %d, but the document names only %v", p.command, got.code, row.Commands)
			}
		})
		seen[p.want] = true
		commands[p.command] = true
	}
	for _, r := range rows {
		if r.Status == "Binding" && !seen[r.Code] {
			t.Errorf("no error path ends with %d, which the document lists as binding", r.Code)
		}
	}
	for _, c := range []string{"version", "auth", "review", "next", "finalize", "show", "publish", "queue", "doctor", "check-criteria"} {
		if !commands[c] {
			t.Errorf("no error path of %s", c)
		}
	}
	// Anything that is not a command is a usage error.
	for _, args := range [][]string{nil, {"frobnicate"}, {"--frobnicate"}} {
		var out, errOut strings.Builder
		if code := run(args, env{stdout: &out, stderr: &errOut}); code != exitUsage {
			t.Errorf("run(%v) = %d, want %d", args, code, exitUsage)
		}
	}
}
