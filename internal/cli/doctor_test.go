package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/testutil/reviewreplay"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
	"github.com/Brg92/agent-code-review/internal/version"
)

// scriptGit answers the git commands doctor runs.
type scriptGit struct {
	version   func() (string, error)
	helpers   string
	helpersEr error
	fill      func() (string, string, error)
	calls     [][]string
}

func (g *scriptGit) Run(_ context.Context, _ string, args ...string) (string, string, error) {
	g.calls = append(g.calls, args)
	switch args[0] {
	case "--version":
		out, err := g.version()
		return out, "", err
	case "config":
		return g.helpers, "", g.helpersEr
	case "credential":
		if args[1] == "fill" {
			return g.fill()
		}
	}
	return "", "", errors.New("scriptGit: unexpected git " + strings.Join(args, " "))
}

// world is what doctor runs against: a GitHub that answers for acme/app,
// a git that works, a token in the environment and a work directory.
type world struct {
	e    env
	vars map[string]string
	git  *scriptGit
	srv  *sandbox.Replay
	work string
}

func serve(r *sandbox.Replay, status int, path string, query, header map[string]string, body string) {
	r.Add(sandbox.Route{Path: path, Query: query, Status: status, Header: header, Body: body})
}

func newWorld(t *testing.T) *world {
	t.Helper()
	srv := sandbox.NewSynthetic(t)
	serve(srv, 200, "/user", nil, map[string]string{"X-OAuth-Scopes": "repo"}, `{"login":"ann"}`)
	serve(srv, 200, "/repos/acme/app", nil, nil, `{"default_branch":"main","private":true}`)
	serve(srv, 200, "/repos/acme/app/pulls", map[string]string{"per_page": "1"}, nil, `[]`)
	serve(srv, 200, "/repos/acme/app/contents", nil, nil, `[]`)
	serve(srv, 200, "/repos/acme/app/issues", map[string]string{"per_page": "1"}, nil, `[]`)
	serve(srv, 200, "/repos/acme/app/commits/main/check-runs", map[string]string{"per_page": "1"}, nil, `{"total_count":0,"check_runs":[]}`)
	e, work := reviewEnv(t, srv)
	vars := map[string]string{"ACR_WORKDIR": work, "ACR_PACKS_DIR": packsDir(t), "ACR_GITHUB_TOKEN": reviewreplay.Canary}
	e.getenv = func(k string) string { return vars[k] }
	git := &scriptGit{
		version: func() (string, error) { return "git version 2.43.0\n", nil },
		helpers: "credential.helper\nlibsecret\x00",
		fill: func() (string, string, error) {
			return "", "fatal: could not read Username for 'https://github.com': terminal prompts disabled\n", &auth.ExitError{Code: 128}
		},
	}
	e.git = git
	return &world{e: e, vars: vars, git: git, srv: srv, work: work}
}

// run runs doctor and returns its result, with no token in its output.
func (w *world) run(t *testing.T, args ...string) result {
	t.Helper()
	r := runWith(t, w.e, append([]string{"doctor"}, args...)...)
	noCanary(t, "stdout", r.stdout)
	noCanary(t, "stderr", r.stderr)
	return r
}

// statuses reduces the output to the status of each line.
func statuses(out string) []string {
	var got []string
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		got = append(got, strings.Fields(l)[0])
	}
	return got
}

func line(t *testing.T, out, prefix string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.Join(strings.Fields(l)[min(1, len(strings.Fields(l))):], " "), prefix) {
			return l
		}
	}
	t.Fatalf("no line starts with %q:\n%s", prefix, out)
	return ""
}

func TestDoctorAllWell(t *testing.T) {
	w := newWorld(t)
	got := w.run(t)
	if got.code != exitOK || got.stderr != "" {
		t.Fatalf("doctor: code %d\nstderr: %s\nstdout: %s", got.code, got.stderr, got.stdout)
	}
	if s := statuses(got.stdout); strings.Join(s, " ") != "ok ok ok ok ok ok ok ok" {
		t.Errorf("statuses = %v:\n%s", s, got.stdout)
	}
	wants := []string{
		"ok    version: " + version.Version,
		"ok    git: git version 2.43.0",
		"ok    credential storage: libsecret (OS keychain)",
		"ok    token for github.com: from the environment variable ACR_GITHUB_TOKEN",
		"ok    GET /user: logged in as ann; classic token (scopes: repo)",
		"ok    packs: android, kotlin-multiplatform (from " + packsDir(t) + ")",
		"ok    user config: " + filepath.Join(w.work, "config", "agent-code-review", "config.json") + " is not there, the defaults apply",
		"ok    work directory: " + w.work + " is writable",
	}
	lines := strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")
	for i, want := range wants {
		if lines[i] != want {
			t.Errorf("line %d = %q, want %q", i+1, lines[i], want)
		}
	}
}

func TestDoctorWithARepository(t *testing.T) {
	w := newWorld(t)
	got := w.run(t, "--repo", "acme/app")
	if got.code != exitOK {
		t.Fatalf("doctor: code %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{
		"ok    repository acme/app: readable",
		"ok    pull requests of acme/app: readable",
		"ok    contents of acme/app: readable",
		"ok    issues of acme/app: readable",
		"ok    check runs of acme/app: readable",
		"ok    write to pull requests of acme/app: the token has the repo scope",
		"ok    write to issues of acme/app: the token has the repo scope",
	} {
		if !strings.Contains(got.stdout, want+"\n") {
			t.Errorf("no line %q:\n%s", want, got.stdout)
		}
	}
	// The repository checks come after the account and before the packs.
	order := []string{"GET /user", "repository acme/app", "check runs of acme/app", "write to issues of", "packs:"}
	last := -1
	for _, o := range order {
		i := strings.Index(got.stdout, o)
		if i <= last {
			t.Errorf("%q is out of order:\n%s", o, got.stdout)
		}
		last = i
	}
	for _, req := range w.srv.Requests() {
		if !strings.HasPrefix(req, "GET ") {
			t.Errorf("request %q is not a read", req)
		}
	}
}

func TestDoctorWarnsWhenWritingIsDeniedOrUnknown(t *testing.T) {
	w := newWorld(t)
	serve(w.srv, 200, "/user", nil, map[string]string{"X-OAuth-Scopes": "read:org"}, `{"login":"ann"}`)
	got := w.run(t, "--repo", "acme/app")
	if got.code != exitOK {
		t.Fatalf("code %d: a token that cannot write only warns\n%s", got.code, got.stdout)
	}
	l := line(t, got.stdout, "write to pull requests of acme/app")
	if !strings.HasPrefix(l, "warn  ") || !strings.Contains(l, "needed only by publish") {
		t.Errorf("line = %q", l)
	}

	w = newWorld(t)
	serve(w.srv, 200, "/user", nil, nil, `{"login":"ann"}`)
	got = w.run(t, "--repo", "acme/app")
	if l := line(t, got.stdout, "GET /user"); !strings.Contains(l, "token is not classic") {
		t.Errorf("GET /user line = %q", l)
	}
	if l := line(t, got.stdout, "write to issues of acme/app"); !strings.HasPrefix(l, "ok    ") || !strings.Contains(l, "cannot be checked") {
		t.Errorf("a token whose write access cannot be read is not a warning: %q", l)
	}
}

func TestDoctorWarnsOfVariablesItNeverReads(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"GITHUB_TOKEN", map[string]string{"GITHUB_TOKEN": "x"}, "GITHUB_TOKEN: set but never read as a token"},
		{"GH_TOKEN", map[string]string{"GH_TOKEN": "x"}, "GH_TOKEN: set but never read as a token"},
		{"both", map[string]string{"GITHUB_TOKEN": "x", "GH_TOKEN": "y"}, "GITHUB_TOKEN and GH_TOKEN: set but never read as a token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			w.vars["ACR_GITHUB_TOKEN"] = ""
			w.git.fill = func() (string, string, error) {
				return "username=acr-api\npassword=" + reviewreplay.Canary + "\n", "", nil
			}
			for k, v := range tt.vars {
				w.vars[k] = v
			}
			got := w.run(t)
			if got.code != exitOK {
				t.Fatalf("code %d: a warning does not fail doctor\n%s", got.code, got.stdout)
			}
			l := line(t, got.stdout, strings.Split(tt.want, ":")[0])
			if !strings.HasPrefix(l, "warn  ") || !strings.Contains(l, tt.want) || !strings.Contains(l, "ACR_GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}") {
				t.Errorf("line = %q", l)
			}
			if strings.Contains(got.stdout, "x") && strings.Contains(l, "=x") {
				t.Errorf("the line shows the value: %q", l)
			}
		})
	}
	w := newWorld(t)
	w.vars["GITHUB_TOKEN"] = "x"
	if got := w.run(t); strings.Contains(got.stdout, "never read") {
		t.Errorf("warned although ACR_GITHUB_TOKEN is set:\n%s", got.stdout)
	}
}

func TestDoctorExitCodes(t *testing.T) {
	notClean := func(w *world) { w.vars["ACR_WORKDIR"] = filepath.Join(w.work, "file") }
	tests := []struct {
		name  string
		setup func(w *world)
		args  []string
		code  int
		// line is the line that fails, by what it starts with after the status.
		line string
	}{
		{"git missing", func(w *world) {
			w.git.version = func() (string, error) { return "", &exec.Error{Name: "git", Err: exec.ErrNotFound} }
		}, nil, exitAuth, "git:"},
		{"no token", func(w *world) {
			w.vars["ACR_GITHUB_TOKEN"] = ""
		}, nil, exitAuth, "token for github.com:"},
		{"the helper fails", func(w *world) {
			w.vars["ACR_GITHUB_TOKEN"] = ""
			w.git.fill = func() (string, string, error) { return "", "boom", &auth.ExitError{Code: 1} }
		}, nil, exitAuth, "token for github.com:"},
		{"the helpers cannot be read", func(w *world) {
			w.git.helpersEr = &auth.ExitError{Code: 129}
		}, nil, exitAuth, "credential storage:"},
		{"no credential helper", func(w *world) {
			w.vars["ACR_GITHUB_TOKEN"] = ""
			w.git.helpers = ""
			w.git.fill = func() (string, string, error) {
				return "username=acr-api\npassword=" + reviewreplay.Canary + "\n", "", nil
			}
		}, nil, exitUnsafeStorage, "credential storage:"},
		{"plain text helper", func(w *world) {
			w.vars["ACR_GITHUB_TOKEN"] = ""
			w.git.helpers = "credential.helper\nstore\x00"
			w.git.fill = func() (string, string, error) {
				return "username=acr-api\npassword=" + reviewreplay.Canary + "\n", "", nil
			}
		}, nil, exitUnsafeStorage, "credential storage:"},
		{"token refused", func(w *world) {
			serve(w.srv, 401, "/user", nil, nil, `{"message":"Bad credentials"}`)
		}, nil, exitAuth, "GET /user:"},
		{"platform down", func(w *world) {
			serve(w.srv, 500, "/user", nil, nil, `{}`)
		}, nil, exitPlatform, "GET /user:"},
		{"missing permission", func(w *world) {
			serve(w.srv, 403, "/repos/acme/app/issues", map[string]string{"per_page": "1"}, map[string]string{"X-Accepted-GitHub-Permissions": "issues=read"}, `{}`)
		}, []string{"--repo", "acme/app"}, exitAuth, "issues of acme/app:"},
		{"no such repository", func(w *world) {
			serve(w.srv, 404, "/repos/acme/app", nil, nil, `{"message":"Not Found"}`)
		}, []string{"--repo", "acme/app"}, exitPlatform, "repository acme/app:"},
		{"invalid user config", func(w *world) {
			path := filepath.Join(w.work, "config", "agent-code-review", "config.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"schema":1,"keepDays":-1}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}, nil, exitValidation, "user config:"},
		{"invalid pack", func(w *world) {
			dir := filepath.Join(t.TempDir(), "packs")
			if err := os.MkdirAll(filepath.Join(dir, "broken"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "broken", "manifest.json"), []byte(`{`), 0o600); err != nil {
				t.Fatal(err)
			}
			w.vars["ACR_PACKS_DIR"] = dir
		}, nil, exitValidation, "packs:"},
		{"no packs", func(w *world) {
			w.vars["ACR_PACKS_DIR"] = t.TempDir()
		}, nil, exitUnexpected, "packs:"},
		{"work directory not writable", func(w *world) {
			if err := os.WriteFile(filepath.Join(w.work, "file"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			notClean(w)
		}, nil, exitUnexpected, "work directory:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			tt.setup(w)
			got := w.run(t, tt.args...)
			if got.code != tt.code {
				t.Errorf("code %d, want %d\n%s%s", got.code, tt.code, got.stdout, got.stderr)
			}
			if l := line(t, got.stdout, tt.line); !strings.HasPrefix(l, "fail  ") || !strings.Contains(l, "to do: ") {
				t.Errorf("line = %q, want a fail that says what to do", l)
			}
			if got.stderr != "" {
				t.Errorf("stderr = %q: the result is the lines on stdout", got.stderr)
			}
		})
	}
}

func TestDoctorRunsEveryCheckWhateverFails(t *testing.T) {
	w := newWorld(t)
	w.git.version = func() (string, error) { return "", &exec.Error{Name: "git", Err: exec.ErrNotFound} }
	w.vars["ACR_GITHUB_TOKEN"] = ""
	got := w.run(t, "--repo", "acme/app")
	// 8 checks, and the repository ones, which are not checked without a token.
	if n := len(strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")); n < 9 {
		t.Errorf("%d lines:\n%s", n, got.stdout)
	}
	if l := line(t, got.stdout, "GET /user"); !strings.HasPrefix(l, "fail  ") || !strings.Contains(l, "not checked") {
		t.Errorf("GET /user line = %q", l)
	}
	if n := len(w.srv.Requests()); n != 0 {
		t.Errorf("requests = %v, want none without a token", w.srv.Requests())
	}
	for _, c := range []string{"packs:", "user config:", "work directory:"} {
		if l := line(t, got.stdout, c); !strings.HasPrefix(l, "ok    ") {
			t.Errorf("%s line = %q: the checks that do not depend on git still run", c, l)
		}
	}
}

func TestDoctorExitCodePrecedence(t *testing.T) {
	invalidConfig := func(w *world) {
		path := filepath.Join(w.work, "config", "agent-code-review", "config.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"schema":2}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	noWorkDir := func(w *world) {
		if err := os.WriteFile(filepath.Join(w.work, "file"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		w.vars["ACR_WORKDIR"] = filepath.Join(w.work, "file")
	}
	insecure := func(w *world) {
		w.vars["ACR_GITHUB_TOKEN"] = ""
		w.git.helpers = ""
		w.git.fill = func() (string, string, error) {
			return "username=acr-api\npassword=" + reviewreplay.Canary + "\n", "", nil
		}
	}
	noToken := func(w *world) {
		w.vars["ACR_GITHUB_TOKEN"] = ""
		w.git.helpers = "credential.helper\nlibsecret\x00"
	}
	down := func(w *world) { serve(w.srv, 500, "/user", nil, nil, `{}`) }
	tests := []struct {
		name  string
		steps []func(*world)
		code  int
	}{
		{"auth before unsafe storage", []func(*world){noToken, func(w *world) { w.git.helpers = "" }}, exitAuth},
		{"unsafe storage before platform", []func(*world){insecure, down}, exitUnsafeStorage},
		{"platform before validation", []func(*world){down, invalidConfig}, exitPlatform},
		{"validation before the rest", []func(*world){invalidConfig, noWorkDir}, exitValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			for _, step := range tt.steps {
				step(w)
			}
			if got := w.run(t); got.code != tt.code {
				t.Errorf("code %d, want %d\n%s", got.code, tt.code, got.stdout)
			}
		})
	}
}

func TestDoctorStorageNeedsNothingInCIWhenTheTokenIsInTheEnvironment(t *testing.T) {
	w := newWorld(t)
	w.git.helpers = ""
	got := w.run(t)
	if got.code != exitOK {
		t.Fatalf("code %d: nothing is stored when the token is in the environment\n%s", got.code, got.stdout)
	}
	if l := line(t, got.stdout, "credential storage:"); !strings.HasPrefix(l, "warn  ") || !strings.Contains(l, "no git credential helper") {
		t.Errorf("line = %q", l)
	}
}

func TestDoctorWarnsOfAnUnknownHelper(t *testing.T) {
	w := newWorld(t)
	w.git.helpers = "credential.helper\nmy-vault\x00"
	got := w.run(t)
	if l := line(t, got.stdout, "credential storage:"); got.code != exitOK || !strings.HasPrefix(l, "warn  ") || !strings.Contains(l, "my-vault") {
		t.Errorf("code %d, line %q", got.code, l)
	}
}

func TestDoctorUsageErrors(t *testing.T) {
	tests := [][]string{
		{"--repo"},
		{"--repo", "acme"},
		{"--repo", "acme/app/extra"},
		{"--repo", "https://github.com/acme/app"},
		{"--repo", "ac me/app"},
		{"--repo", "../app"},
		{"--host", "github.example.com"},
		{"acme/app"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			w := newWorld(t)
			got := w.run(t, args...)
			if got.code != exitUsage || got.stdout != "" || !strings.Contains(got.stderr, "usage: agent-code-review doctor") {
				t.Errorf("code %d\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
			}
			if len(w.git.calls) != 0 || len(w.srv.Requests()) != 0 {
				t.Errorf("doctor ran: git %v, requests %v", w.git.calls, w.srv.Requests())
			}
		})
	}
}

func TestDoctorIsListedInTheUsage(t *testing.T) {
	if !strings.Contains(usage, "doctor [--repo owner/repo]") {
		t.Error("the usage does not list doctor")
	}
}
