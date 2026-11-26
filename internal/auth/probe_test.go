package auth

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestProbe(t *testing.T) {
	tests := []struct {
		name string
		fill answer
		want Probe
		err  bool
	}{
		{"its own token", storedFill(testToken), Probe{Username: Username, HasToken: true}, false},
		{"a token of another account", answer{stdout: "username=someone\npassword=" + testToken + "\n"}, Probe{Username: "someone", HasToken: true}, false},
		{"a user without a token", answer{stdout: "username=someone\n"}, Probe{Username: "someone"}, false},
		{"nothing", answer{stderr: promptsDisabled, err: &ExitError{Code: 128}}, Probe{}, false},
		{"a failing helper", answer{stderr: "error: keychain is locked\n" + promptsDisabled, err: &ExitError{Code: 128}}, Probe{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{Getenv: env(nil), Git: &fakeGit{answers: map[string]answer{"credential fill": tt.fill}}}
			got, err := r.Probe(context.Background(), "github.com")
			var he *HelperError
			if tt.err != errors.As(err, &he) {
				t.Fatalf("Probe error = %v, want a HelperError: %v", err, tt.err)
			}
			if got != tt.want {
				t.Errorf("Probe = %+v, want %+v", got, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), testToken) {
				t.Errorf("error leaks the token: %v", err)
			}
		})
	}
	if _, err := (Resolver{Getenv: env(nil), Git: failRunner(t)}).Probe(context.Background(), "a b"); err == nil {
		t.Error("Probe(bad host) = nil error")
	}
}

func TestProbeAnswersForAnotherAccount(t *testing.T) {
	tests := map[Probe]bool{
		{Username: Username, HasToken: true}:  false,
		{Username: "someone", HasToken: true}: true,
		{Username: "someone"}:                 false,
		{}:                                    false,
	}
	for p, want := range tests {
		if got := p.OtherAccount(); got != want {
			t.Errorf("%+v.OtherAccount() = %v, want %v", p, got, want)
		}
	}
}

func TestProbeKeepsTheTokenOutOfTheProbe(t *testing.T) {
	typ := reflect.TypeOf(Probe{})
	for i := 0; i < typ.NumField(); i++ {
		if f := typ.Field(i); f.Type.Kind() == reflect.String && f.Name != "Username" {
			t.Errorf("Probe field %s is a string: only the username may be", f.Name)
		}
	}
}

func TestSetupCommands(t *testing.T) {
	wantArgs := [][]string{
		{"config", "--global", "--add", "credential.https://acr-api@github.com.helper", ""},
		{"config", "--global", "--add", "credential.https://acr-api@github.com.helper", "libsecret"},
	}
	if got := SetupArgs("github.com", "libsecret"); !reflect.DeepEqual(got, wantArgs) {
		t.Errorf("SetupArgs = %q, want %q", got, wantArgs)
	}
	want := []string{
		`git config --global --add credential.https://acr-api@github.com.helper ""`,
		`git config --global --add credential.https://acr-api@github.com.helper libsecret`,
	}
	if got := SetupCommands("github.com", "libsecret"); !reflect.DeepEqual(got, want) {
		t.Errorf("SetupCommands = %q, want %q", got, want)
	}
}

func TestSuggestedHelper(t *testing.T) {
	tests := map[string]string{
		"linux":   "libsecret",
		"darwin":  "osxkeychain",
		"windows": "manager",
		"freebsd": "libsecret",
	}
	for goos, want := range tests {
		if got := SuggestedHelper(goos); got != want {
			t.Errorf("SuggestedHelper(%q) = %q, want %q", goos, got, want)
		}
		if classifyHelper(want) != HelperKeychain {
			t.Errorf("the helper suggested for %s is not classified as a keychain", goos)
		}
	}
}
