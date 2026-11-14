package auth

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const helperQuery = `config --null --get-regexp ^credential\.(.*\.)?helper$`

// nulEntries renders git config --null --get-regexp output.
func nulEntries(kv ...string) string {
	var b strings.Builder
	for i := 0; i < len(kv); i += 2 {
		b.WriteString(kv[i] + "\n" + kv[i+1] + "\x00")
	}
	return b.String()
}

func TestClassifyHelper(t *testing.T) {
	tests := map[string]HelperKind{
		"osxkeychain":  HelperKeychain,
		"manager":      HelperKeychain,
		"manager-core": HelperKeychain,
		"wincred":      HelperKeychain,
		"libsecret":    HelperKeychain,
		"/usr/share/doc/git/contrib/credential/libsecret/git-credential-libsecret": HelperKeychain,
		`"C:\Program Files\Git\mingw64\bin\git-credential-manager.exe"`:            HelperKeychain,
		"/usr/local/bin/git-credential-manager":                                    HelperKeychain,
		"store":                                                                    HelperPlainText,
		"store --file ~/.my-creds":                                                 HelperPlainText,
		"  store  ":                                                                HelperPlainText,
		"git-credential-store":                                                     HelperPlainText,
		"cache --timeout 300":                                                      HelperUnknown,
		"!f() { echo password=x; }; f":                                             HelperUnknown,
		`'/opt/my tools/git-credential-osxkeychain' --flag`:                        HelperKeychain,
		`"/unterminated/store`:                                                     HelperUnknown,
		"acme-vault":                                                               HelperUnknown,
	}
	for value, want := range tests {
		if got := classifyHelper(value); got != want {
			t.Errorf("classifyHelper(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestCheckHelpers(t *testing.T) {
	tests := []struct {
		name     string
		out      answer
		want     []Helper
		wantErr  error
		warnings int
	}{
		{name: "none", out: answer{err: &ExitError{Code: 1}}, wantErr: ErrNoHelper},
		{name: "store only", out: answer{stdout: nulEntries("credential.helper", "store")},
			want: []Helper{{"store", HelperPlainText}}, wantErr: ErrPlainTextHelper},
		{name: "store beside keychain", out: answer{stdout: nulEntries("credential.helper", "osxkeychain", "credential.helper", "store")},
			want: []Helper{{"osxkeychain", HelperKeychain}, {"store", HelperPlainText}}, wantErr: ErrPlainTextHelper},
		{name: "keychain", out: answer{stdout: nulEntries("credential.helper", "osxkeychain")},
			want: []Helper{{"osxkeychain", HelperKeychain}}},
		{name: "unknown", out: answer{stdout: nulEntries("credential.helper", "acme-vault")},
			want: []Helper{{"acme-vault", HelperUnknown}}, warnings: 1},
		{name: "reset clears earlier helpers", out: answer{stdout: nulEntries("credential.helper", "store", "credential.helper", "", "credential.helper", "manager")},
			want: []Helper{{"manager", HelperKeychain}}},
		{name: "reset to nothing", out: answer{stdout: nulEntries("credential.helper", "osxkeychain", "credential.helper", "")}, wantErr: ErrNoHelper},
		{name: "host specific", out: answer{stdout: nulEntries("credential.helper", "osxkeychain", "credential.https://github.com.helper", "acme-vault")},
			want: []Helper{{"osxkeychain", HelperKeychain}, {"acme-vault", HelperUnknown}}, warnings: 1},
		{name: "host specific case and slash", out: answer{stdout: nulEntries("credential.https://GitHub.com/.helper", "manager")},
			want: []Helper{{"manager", HelperKeychain}}},
		{name: "host without scheme", out: answer{stdout: nulEntries("credential.github.com.helper", "manager")},
			want: []Helper{{"manager", HelperKeychain}}},
		{name: "wildcard host", out: answer{stdout: nulEntries("credential.https://*.com.helper", "manager")},
			want: []Helper{{"manager", HelperKeychain}}},
		{name: "other hosts ignored", out: answer{stdout: nulEntries(
			"credential.https://gitlab.com.helper", "store",
			"credential.http://github.com.helper", "store",
			"credential.https://github.com:8443.helper", "store",
			"credential.https://github.com/org.helper", "store",
			"credential.https://*.github.com.helper", "store",
			"credential.helper", "osxkeychain")},
			want: []Helper{{"osxkeychain", HelperKeychain}}},
		{name: "valueless entry ignored", out: answer{stdout: "credential.helper\x00" + nulEntries("credential.helper", "manager")},
			want: []Helper{{"manager", HelperKeychain}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{Getenv: env(nil), Git: &fakeGit{answers: map[string]answer{helperQuery: tt.out}}}
			check, err := r.CheckHelpers(context.Background(), "github.com")
			if err != nil {
				t.Fatalf("CheckHelpers: %v", err)
			}
			if !reflect.DeepEqual(check.Helpers, tt.want) {
				t.Errorf("Helpers = %v, want %v", check.Helpers, tt.want)
			}
			if err := check.Err(); !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Errorf("Err() = %v, want %v", err, tt.wantErr)
			}
			if len(check.Warnings()) != tt.warnings {
				t.Errorf("Warnings() = %q, want %d", check.Warnings(), tt.warnings)
			}
		})
	}
}

func TestCheckHelpersMessagesExplainTheFix(t *testing.T) {
	for name, err := range map[string]error{"none": Check{}.Err(), "store": Check{Helpers: []Helper{{"store", HelperPlainText}}}.Err()} {
		if err == nil || !strings.Contains(err.Error(), "git config --global credential.helper") {
			t.Errorf("%s: Err() = %v, want a hint naming the setting", name, err)
		}
	}
	w := Check{Helpers: []Helper{{"acme-vault", HelperUnknown}}}.Warnings()
	if len(w) != 1 || !strings.Contains(w[0], "acme-vault") {
		t.Errorf("Warnings() = %q", w)
	}
}

func TestCheckHelpersGitErrors(t *testing.T) {
	r := Resolver{Getenv: env(nil), Git: &fakeGit{answers: map[string]answer{helperQuery: {err: &ExitError{Code: 3}}}}}
	if _, err := r.CheckHelpers(context.Background(), "github.com"); err == nil {
		t.Error("CheckHelpers with git config failing = nil error")
	}
	if _, err := r.CheckHelpers(context.Background(), "bad host"); err == nil {
		t.Error("CheckHelpers(bad host) = nil error")
	}
}

func TestHelperKindString(t *testing.T) {
	for k, want := range map[HelperKind]string{HelperKeychain: "OS keychain", HelperPlainText: "plain text", HelperUnknown: "unknown"} {
		if k.String() != want {
			t.Errorf("%d.String() = %q", k, k.String())
		}
	}
}
