package cli

import (
	"strings"
	"testing"
)

func TestAuthLogout(t *testing.T) {
	const rejectInput = "protocol=https\nhost=ghe.example.invalid\nusername=acr-api\n\n"
	git := &scriptedGit{answers: map[string]gitAnswer{"credential reject": {}}}
	r := runWith(t, env{git: git}, "auth", "logout", "--host", "ghe.example.invalid")
	if r.code != exitOK || r.stdout != "ghe.example.invalid: token removed from the git credential helper (username acr-api)\n" || r.stderr != "" {
		t.Errorf("got %+v", r)
	}
	if git.stdin["credential reject"] != rejectInput {
		t.Errorf("reject stdin = %q", git.stdin["credential reject"])
	}
}

func TestAuthLogoutWithEnvironmentToken(t *testing.T) {
	git := &scriptedGit{answers: map[string]gitAnswer{"credential reject": {}}}
	r := runWith(t, env{git: git, getenv: func(k string) string {
		if k == "ACR_GITHUB_TOKEN" {
			return testToken
		}
		return ""
	}}, "auth", "logout")
	if r.code != exitOK || !strings.Contains(r.stderr, "warning: ACR_GITHUB_TOKEN is still set") {
		t.Errorf("got %+v", r)
	}
}

func TestAuthLogoutFails(t *testing.T) {
	git := &scriptedGit{answers: map[string]gitAnswer{"credential reject": {err: errExit1}}}
	r := runWith(t, env{git: git}, "auth", "logout")
	if r.code != exitAuth || r.stdout != "" || !strings.Contains(r.stderr, "reject") {
		t.Errorf("got %+v", r)
	}
}
