// Package credhelper turns a test binary into a git credential helper, so
// tests can drive a real git without touching the user's keychain. The
// helper keeps its entries in a JSON file in the test's temp dir. It is test
// support code only.
//
// A test package uses it from TestMain:
//
//	func TestMain(m *testing.M) {
//		credhelper.Main()
//		os.Exit(m.Run())
//	}
//
// and calls Install in a test to point git at the helper.
package credhelper

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Name is the helper name git is configured with: git runs it as
// git-credential-acrtest, found on PATH.
const Name = "acrtest"

// envStore carries the store path to the helper process.
const envStore = "ACR_TEST_CREDENTIAL_STORE"

// recordedEnv are the variables the helper records, to check how git ran.
var recordedEnv = []string{"GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "GCM_INTERACTIVE"}

// Entry is one stored credential.
type Entry struct {
	Host, Username, Password string
}

type state struct {
	Entries []Entry
	Env     map[string]string
}

// Main serves one helper request and exits when the process was started by
// git as the helper; otherwise it returns at once.
func Main() {
	store := os.Getenv(envStore)
	if store == "" || len(os.Args) != 2 {
		return
	}
	op := os.Args[1]
	if op != "get" && op != "store" && op != "erase" {
		return
	}
	if err := serve(op, os.Stdin, os.Stdout, store, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "credhelper:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Config describes the git configuration Install writes.
type Config struct {
	// Helpers are the credential.helper values, in order; nil means the
	// test helper only.
	Helpers []string
}

// Install puts the test helper on PATH and isolates git from the user's
// configuration: no system config, a global config in a temp dir, and HOME
// in a temp dir. It returns the store path and the home directory.
func Install(t *testing.T, cfg Config) (store, home string) {
	t.Helper()
	dir := t.TempDir()
	home = filepath.Join(dir, "home")
	bin := filepath.Join(dir, "bin")
	for _, d := range []string{home, bin} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "git-credential-" + Name
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	copyFile(t, exe, filepath.Join(bin, name))

	helpers := cfg.Helpers
	if helpers == nil {
		helpers = []string{Name}
	}
	var conf strings.Builder
	conf.WriteString("[credential]\n")
	for _, h := range helpers {
		fmt.Fprintf(&conf, "\thelper = %s\n", h)
	}
	gitconfig := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(gitconfig, []byte(conf.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	store = filepath.Join(dir, "store.json")

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(envStore, store)
	// A race-enabled helper would otherwise wait a second at every exit.
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	checkIsolation(t, dir, helpers)
	return store, home
}

// checkIsolation stops the test if git sees any helper but the configured
// ones, for example one compiled into git or set by the runner: a test must
// never reach a real keychain.
func checkIsolation(t *testing.T, dir string, want []string) {
	t.Helper()
	cmd := exec.Command("git", "config", "--null", "--get-all", "credential.helper")
	cmd.Dir = dir
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		t.Fatalf("git config: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(out) == 0 {
		got = nil
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("git sees credential helpers %q, want only %q: the test isolation leaks", got, want)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o700); err != nil {
		t.Fatal(err)
	}
}

// Read returns the stored entries and the environment the helper last saw.
func Read(t *testing.T, store string) ([]Entry, map[string]string) {
	t.Helper()
	st, err := load(store)
	if err != nil {
		t.Fatal(err)
	}
	return st.Entries, st.Env
}

func load(store string) (state, error) {
	st := state{Env: map[string]string{}}
	data, err := os.ReadFile(store)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(data, &st)
	return st, err
}

// serve answers one git credential helper operation.
func serve(op string, in io.Reader, out io.Writer, store string, getenv func(string) string) error {
	attrs := map[string]string{}
	sc := bufio.NewScanner(in)
	for sc.Scan() && sc.Text() != "" {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			attrs[k] = v
		}
	}
	st, err := load(store)
	if err != nil {
		return err
	}
	for _, k := range recordedEnv {
		st.Env[k] = getenv(k)
	}
	match := func(e Entry) bool { return e.Host == attrs["host"] && e.Username == attrs["username"] }
	kept := st.Entries[:0]
	for _, e := range st.Entries {
		switch {
		case !match(e):
			kept = append(kept, e)
		case op == "get":
			fmt.Fprintf(out, "username=%s\npassword=%s\n", e.Username, e.Password)
			kept = append(kept, e)
		}
	}
	switch op {
	case "get", "erase":
	case "store":
		kept = append(kept, Entry{Host: attrs["host"], Username: attrs["username"], Password: attrs["password"]})
	default:
		return fmt.Errorf("unknown operation %q", op)
	}
	st.Entries = kept
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(store, data, 0o600)
}
