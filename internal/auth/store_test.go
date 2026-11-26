package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func keychainGit(fill answer) *fakeGit {
	return &fakeGit{answers: map[string]answer{
		helperQuery:          {stdout: nulEntries("credential.helper", "osxkeychain")},
		"credential approve": {},
		"credential fill":    fill,
	}}
}

func storedFill(token string) answer {
	return answer{stdout: "username=acr-api\npassword=" + token + "\n"}
}

func TestSave(t *testing.T) {
	git := keychainGit(storedFill(testToken))
	r := Resolver{Getenv: env(nil), Git: git}
	check, err := r.Save(context.Background(), "github.com", " "+testToken+"\r\n")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(check.Helpers) != 1 {
		t.Errorf("Check = %v", check)
	}
	var approve *call
	for i := range git.calls {
		if strings.Join(git.calls[i].args, " ") == "credential approve" {
			approve = &git.calls[i]
		}
	}
	want := "protocol=https\nhost=github.com\nusername=acr-api\npassword=" + testToken + "\n\n"
	if approve == nil || approve.stdin != want {
		t.Errorf("approve = %+v, want stdin %q", approve, want)
	}
}

func TestSaveRefusals(t *testing.T) {
	tests := []struct {
		name  string
		host  string
		token string
		git   *fakeGit
		want  error
	}{
		{"empty token", "github.com", " \n", keychainGit(answer{}), ErrNoToken},
		{"token with space", "github.com", "abc def", keychainGit(answer{}), ErrInvalidToken},
		{"token with newline", "github.com", "abc\nhost=evil", keychainGit(answer{}), ErrInvalidToken},
		{"bad host", "evil\nhost", testToken, keychainGit(answer{}), nil},
		{"no helper", "github.com", testToken, &fakeGit{answers: map[string]answer{helperQuery: {err: &ExitError{Code: 1}}}}, ErrNoHelper},
		{"store helper", "github.com", testToken, &fakeGit{answers: map[string]answer{helperQuery: {stdout: nulEntries("credential.helper", "store")}}}, ErrPlainTextHelper},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolver{Getenv: env(nil), Git: tt.git}.Save(context.Background(), tt.host, tt.token)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("Save = %v, want %v", err, tt.want)
			}
			for _, c := range tt.git.calls {
				if c.args[0] == "credential" {
					t.Errorf("git %v ran after a refusal", c.args)
				}
			}
			if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "abc") {
				t.Errorf("error leaks the token: %v", err)
			}
		})
	}
}

func TestSaveFailures(t *testing.T) {
	tests := map[string]*fakeGit{
		"approve fails": {answers: map[string]answer{
			helperQuery:          {stdout: nulEntries("credential.helper", "osxkeychain")},
			"credential approve": {stderr: "helper said " + testToken, err: &ExitError{Code: 1}},
		}},
		"helper check fails": {answers: map[string]answer{helperQuery: {err: &ExitError{Code: 5}}}},
		"read back fails":    keychainGit(answer{stderr: "error: keychain is locked\n" + promptsDisabled, err: &ExitError{Code: 128}}),
	}
	for name, git := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Resolver{Getenv: env(nil), Git: git}.Save(context.Background(), "github.com", testToken)
			if err == nil {
				t.Fatal("Save = nil error")
			}
			if errors.Is(err, ErrNotStored) {
				t.Errorf("Save = %v, a failing helper is not a helper that does not store", err)
			}
			if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "other-token-value") {
				t.Errorf("error leaks a token: %v", err)
			}
		})
	}
}

func TestSaveNotStored(t *testing.T) {
	tests := []struct {
		name   string
		fill   answer
		reason NotStoredReason
	}{
		{"nothing comes back", answer{stderr: promptsDisabled, err: &ExitError{Code: 128}}, NotStoredNothing},
		{"no password comes back", answer{stdout: "username=acr-api\n"}, NotStoredNothing},
		{"another token for acr-api", storedFill("other-token-value-4567"), NotStoredOtherToken},
		{"a token of another account", answer{stdout: "username=someone\npassword=other-token-value-4567\n"}, NotStoredOtherAccount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			git := keychainGit(tt.fill)
			git.answers["credential reject"] = answer{}
			_, err := Resolver{Getenv: env(nil), Git: git}.Save(context.Background(), "github.com", testToken)
			var ns *NotStoredError
			if !errors.Is(err, ErrNotStored) || !errors.As(err, &ns) || ns.Reason != tt.reason {
				t.Fatalf("Save = %v, want ErrNotStored with reason %v", err, tt.reason)
			}
			if msg := err.Error(); strings.Contains(msg, testToken) || strings.Contains(msg, "other-token-value") || strings.Contains(msg, "someone") {
				t.Errorf("error leaks a token or a user: %v", err)
			}
			var rejects []string
			for _, c := range git.calls {
				if strings.Join(c.args, " ") == "credential reject" {
					rejects = append(rejects, c.stdin)
				}
			}
			if len(rejects) != 1 || rejects[0] != fillInput {
				t.Errorf("rejects = %q, want one with %q: what was approved must not stay", rejects, fillInput)
			}
		})
	}
}

func TestSaveNotStoredEvenIfTheRejectFails(t *testing.T) {
	git := keychainGit(answer{stderr: promptsDisabled, err: &ExitError{Code: 128}})
	git.answers["credential reject"] = answer{err: &ExitError{Code: 1}}
	_, err := Resolver{Getenv: env(nil), Git: git}.Save(context.Background(), "github.com", testToken)
	if !errors.Is(err, ErrNotStored) {
		t.Errorf("Save = %v, want ErrNotStored", err)
	}
}

func TestSaveDoesNotRejectAStoredToken(t *testing.T) {
	git := keychainGit(storedFill(testToken))
	if _, err := (Resolver{Getenv: env(nil), Git: git}).Save(context.Background(), "github.com", testToken); err != nil {
		t.Fatal(err)
	}
	for _, c := range git.calls {
		if c.args[0] == "credential" && c.args[1] == "reject" {
			t.Error("Save rejected a token that was stored")
		}
	}
}

func TestNotStoredErrorsSayWhatHappened(t *testing.T) {
	for reason, want := range map[NotStoredReason]string{
		NotStoredNothing:      "returned nothing",
		NotStoredOtherToken:   "different token",
		NotStoredOtherAccount: "another account",
	} {
		if got := (&NotStoredError{Reason: reason}).Error(); !strings.Contains(got, want) {
			t.Errorf("Error() for %v = %q, want it to contain %q", reason, got, want)
		}
	}
}

func TestErase(t *testing.T) {
	git := &fakeGit{answers: map[string]answer{"credential reject": {}}}
	if err := (Resolver{Getenv: env(nil), Git: git}).Erase(context.Background(), "github.com"); err != nil {
		t.Fatalf("Erase: %v", err)
	}
	if len(git.calls) != 1 || git.calls[0].stdin != fillInput {
		t.Errorf("calls = %q, want one reject with %q", git.calls, fillInput)
	}

	git = &fakeGit{answers: map[string]answer{"credential reject": {err: &ExitError{Code: 1}}}}
	if err := (Resolver{Getenv: env(nil), Git: git}).Erase(context.Background(), "github.com"); err == nil {
		t.Error("Erase with git failing = nil error")
	}
	if err := (Resolver{Getenv: env(nil), Git: failRunner(t)}).Erase(context.Background(), "a b"); err == nil {
		t.Error("Erase(bad host) = nil error")
	}
}
