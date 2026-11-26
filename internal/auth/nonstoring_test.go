package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/testutil/credhelper"
)

// ghLike configures git as `gh auth setup-git` does: for https://github.com
// the helpers are reset, then one helper answers with another account and
// ignores store.
const ghLike = "[credential \"https://github.com\"]\n\thelper =\n\thelper = " + credhelper.NameOther + "\n"

func TestRealGitHelperThatKeepsNothing(t *testing.T) {
	requireGit(t)
	credhelper.Install(t, credhelper.Config{Helpers: []string{credhelper.NameDrop}})
	r := realResolver(t)
	_, err := r.Save(context.Background(), "github.com", testToken)
	var ns *NotStoredError
	if !errors.As(err, &ns) || ns.Reason != NotStoredNothing {
		t.Fatalf("Save = %v, want a NotStoredError for a helper that keeps nothing", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error leaks the token: %v", err)
	}
	if _, err := r.Resolve(context.Background(), "github.com"); !errors.Is(err, ErrNoToken) {
		t.Errorf("Resolve after the failed login = %v, want ErrNoToken", err)
	}
}

// This is the case of D248 and D250: the helper of the GitHub CLI answers
// for github.com whatever the username, with the token of its own account.
func TestRealGitFillReturnsTheTokenOfTheOtherHelper(t *testing.T) {
	requireGit(t)
	credhelper.Install(t, credhelper.Config{Helpers: []string{credhelper.NameOther}})
	ctx := context.Background()
	out, _, err := GitRunner{Dir: t.TempDir()}.Run(ctx, credentialInput("github.com", ""), "credential", "fill")
	if err != nil {
		t.Fatalf("git credential fill: %v", err)
	}
	got := parseCredential(out)
	if got["username"] != credhelper.OtherUser {
		t.Errorf("fill for %s answered the user %q, want %q", Username, got["username"], credhelper.OtherUser)
	}
	if got["password"] != credhelper.OtherToken {
		t.Error("fill for acr-api did not return the token of the helper that answers for another account")
	}
	if got["password"] == testToken {
		t.Error("fill returned the user's own token")
	}

	r := realResolver(t)
	if _, err := r.Resolve(ctx, "github.com"); !errors.Is(err, ErrNoToken) {
		t.Errorf("Resolve = %v, want ErrNoToken: that token is not acr-api's", err)
	}
	probe, err := r.Probe(ctx, "github.com")
	if err != nil || probe != (Probe{Username: credhelper.OtherUser, HasToken: true}) || !probe.OtherAccount() {
		t.Errorf("Probe = %+v, %v", probe, err)
	}
}

func TestRealGitSaveWithAHelperOfAnotherAccount(t *testing.T) {
	requireGit(t)
	// The helper that stores is configured too, behind the other one.
	store, _ := credhelper.Install(t, credhelper.Config{Helpers: []string{credhelper.NameOther, credhelper.Name}})
	_, err := realResolver(t).Save(context.Background(), "github.com", testToken)
	var ns *NotStoredError
	if !errors.As(err, &ns) || ns.Reason != NotStoredOtherAccount {
		t.Fatalf("Save = %v, want a NotStoredError for another account", err)
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), credhelper.OtherToken) || strings.Contains(err.Error(), credhelper.OtherUser) {
		t.Errorf("error leaks a token or a user: %v", err)
	}
	// The helper that stores had kept the approved token: it was rejected.
	if entries, _ := credhelper.Read(t, store); len(entries) != 0 {
		t.Errorf("a half-stored credential stays: %d entries", len(entries))
	}
}

func TestRealGitSetupCommandsGiveAcrApiItsOwnHelper(t *testing.T) {
	requireGit(t)
	store, _ := credhelper.Install(t, credhelper.Config{Extra: ghLike})
	ctx := context.Background()
	r := realResolver(t)
	if _, err := r.Save(ctx, "github.com", testToken); !errors.Is(err, ErrNotStored) {
		t.Fatalf("Save before the setup = %v, want ErrNotStored", err)
	}

	for _, args := range SetupArgs("github.com", credhelper.Name) {
		if _, _, err := (GitRunner{Dir: t.TempDir()}).Run(ctx, "", args...); err != nil {
			t.Fatalf("git %q: %v", args, err)
		}
	}
	check, err := r.CheckHelpers(ctx, "github.com")
	if err != nil || len(check.Helpers) != 1 || check.Helpers[0].Value != credhelper.Name {
		t.Fatalf("helpers after the setup = %+v, %v; want only %s", check.Helpers, err, credhelper.Name)
	}
	if _, err := r.Save(ctx, "github.com", testToken); err != nil {
		t.Fatalf("Save after the setup: %v", err)
	}
	tok, err := r.Resolve(ctx, "github.com")
	if err != nil || tok.Value() != testToken {
		t.Fatalf("Resolve after the setup = %v, %v", tok, err)
	}
	if entries, _ := credhelper.Read(t, store); len(entries) != 1 {
		t.Errorf("entries = %d, want 1", len(entries))
	}

	// The user's own git operations on github.com keep their helper.
	own, _, err := GitRunner{Dir: t.TempDir()}.Run(ctx, "protocol=https\nhost=github.com\n\n", "credential", "fill")
	if err != nil || parseCredential(own)["username"] != credhelper.OtherUser {
		t.Errorf("a request without acr-api no longer reaches the user's helper: %v", err)
	}
}
