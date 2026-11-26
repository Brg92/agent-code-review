package cli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/testutil/credhelper"
)

// ghLike configures git as `gh auth setup-git` does: for https://github.com
// the helpers are reset, then a helper answers with another account and
// ignores store.
const ghLike = "[credential \"https://github.com\"]\n\thelper =\n\thelper = " + credhelper.NameOther + "\n"

const helperLine = "credential helper for github.com:"

// realGitWorld is a doctor world that runs the real git, isolated from the
// developer's configuration by credhelper.Install, with no token in the
// environment.
func realGitWorld(t *testing.T, cfg credhelper.Config) *world {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	credhelper.Install(t, cfg)
	w := newWorld(t)
	noToken(w)
	w.e.git = auth.GitRunner{Dir: t.TempDir()}
	w.e.goos = "linux"
	return w
}

func TestDoctorWithRealHelpers(t *testing.T) {
	tests := []struct {
		name    string
		cfg     credhelper.Config
		storage string // the start of the credential storage line
		warns   bool   // the line of another account's helper
	}{
		{"a helper that stores and returns", credhelper.Config{}, "warn  credential storage: unknown git credential helper \"" + credhelper.Name + "\"", false},
		{"a helper that accepts and never stores", credhelper.Config{Helpers: []string{credhelper.NameDrop}}, "warn  credential storage: unknown git credential helper", false},
		{"a helper that answers for another account", credhelper.Config{Helpers: []string{credhelper.Name}, Extra: ghLike}, "warn  credential storage: unknown git credential helper", true},
		{"no helper", credhelper.Config{Helpers: []string{""}}, "fail  credential storage: no git credential helper", false},
		{"store only", credhelper.Config{Helpers: []string{"store"}}, "fail  credential storage: the git credential helper \"store\"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := realGitWorld(t, tt.cfg)
			got := w.run(t)
			if !strings.Contains(got.stdout, tt.storage) {
				t.Errorf("no line starts with %q:\n%s", tt.storage, got.stdout)
			}
			if strings.Contains(got.stdout, credhelper.OtherToken) {
				t.Errorf("stdout holds the token of the other helper:\n%s", got.stdout)
			}
			has := strings.Contains(got.stdout, helperLine)
			if has != tt.warns {
				t.Errorf("line %q present = %v, want %v:\n%s", helperLine, has, tt.warns, got.stdout)
			}
			if got.code != exitAuth {
				t.Errorf("code = %d, want %d: the token is missing in every case, and a warning changes nothing", got.code, exitAuth)
			}
		})
	}
}

// The gh-like helper answers in place of the helper that stores, until
// acr-api has its own: then login works and doctor stops warning.
func TestDoctorAfterGivingAcrApiItsOwnHelper(t *testing.T) {
	w := realGitWorld(t, credhelper.Config{Helpers: []string{credhelper.Name}, Extra: ghLike})
	if !strings.Contains(w.run(t).stdout, helperLine) {
		t.Fatal("no warning before the setup")
	}
	for _, args := range auth.SetupArgs("github.com", credhelper.Name) {
		if _, _, err := w.e.git.Run(t.Context(), "", args...); err != nil {
			t.Fatalf("git %q: %v", args, err)
		}
	}
	got := w.run(t)
	if strings.Contains(got.stdout, helperLine) {
		t.Errorf("still a warning after the setup:\n%s", got.stdout)
	}
	if l := line(t, got.stdout, "credential storage:"); !strings.Contains(l, credhelper.Name) || strings.Contains(l, credhelper.NameOther) {
		t.Errorf("storage line = %q, want only the helper of acr-api", l)
	}
}

func TestDoctorFindsTheTokenAHelperKept(t *testing.T) {
	w := realGitWorld(t, credhelper.Config{})
	r := auth.Resolver{Getenv: w.e.getenv, Git: w.e.git}
	if _, err := r.Save(t.Context(), "github.com", testToken); err != nil {
		t.Fatal(err)
	}
	got := w.run(t)
	if l := line(t, got.stdout, "token for github.com:"); !strings.HasPrefix(l, "ok    ") || !strings.Contains(l, "from the git credential helper") {
		t.Errorf("token line = %q", l)
	}
	if strings.Contains(got.stdout, helperLine) || strings.Contains(got.stdout, testToken) || got.code != exitOK {
		t.Errorf("code %d:\n%s", got.code, got.stdout)
	}
}
