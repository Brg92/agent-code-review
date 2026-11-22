package core

import (
	"reflect"
	"strings"
	"testing"
)

// The canaries are made of pieces, so that no source line is a token that
// a scanner of the repository would take for a real one.
var (
	ghBody       = strings.Repeat("A1b2", 9)
	canaryGitHub = "gh" + "p_" + ghBody
	canaryAWS    = "AK" + "IA" + "0123456789ABCDEF"
	canaryPEM    = "-----BEGIN RSA PRIVATE " + "KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----"
	canarySlack  = "xo" + "xb-" + "0123456789-abcdefghij"
	canaryAssign = `password = "` + "hunter2hunter2" + `"`
)

func TestScanSecretsFindsEveryPattern(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"github personal token", "see " + canaryGitHub + " here", "github-token"},
		{"github oauth token", "gh" + "o_" + ghBody, "github-token"},
		{"github user token", "gh" + "u_" + ghBody, "github-token"},
		{"github server token", "gh" + "s_" + ghBody, "github-token"},
		{"github refresh token", "gh" + "r_" + ghBody, "github-token"},
		{"github fine-grained token", "github" + "_pat_" + "11ABCDEFG0_" + ghBody, "github-token"},
		{"aws access key", "key " + canaryAWS, "aws-access-key-id"},
		{"aws temporary key", "AS" + "IA" + "ABCDEFGHIJKLMNOP", "aws-access-key-id"},
		{"pem block", "```\n" + canaryPEM + "\n```", "private-key"},
		{"openssh block", "-----BEGIN OPENSSH PRIVATE " + "KEY-----", "private-key"},
		{"pgp block", "-----BEGIN PGP PRIVATE KEY " + "BLOCK-----", "private-key"},
		{"slack token", "token " + canarySlack, "slack-token"},
		{"password assignment", canaryAssign, "credential-assignment"},
		{"passwd with colon", `passwd: 'correct-horse'`, "credential-assignment"},
		{"secret in code", `val clientSecret = "abcdefgh12"` + "\n" + `val secret = "abcdefgh12"`, "credential-assignment"},
		{"json key", `{"api_key": "abcdefgh12345"}`, "credential-assignment"},
		{"apikey in a name", `const apiKey="abcdefgh"`, "credential-assignment"},
		{"token with spaces", `DB_TOKEN   =   "abcdefgh"`, "credential-assignment"},
		{"uppercase", `PASSWORD="abcdefgh"`, "credential-assignment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScanSecrets(tt.text)
			if !reflect.DeepEqual(got, []string{tt.want}) {
				t.Errorf("ScanSecrets = %v, want [%s]", got, tt.want)
			}
		})
	}
}

func TestScanSecretsIgnoresWhatIsNotASecret(t *testing.T) {
	for _, text := range []string{
		"",
		"Plain text about a token and a password in prose.",
		"ghp_ alone, gho_short, github_pat_x",
		"AKIA0123 is too short; ASIAlowercase0123456 too",
		"-----BEGIN CERTIFICATE-----",
		"-----BEGIN PUBLIC KEY-----",
		"xoxb-short",
		`password = ""`,
		`password = "short"`,
		`password = readPassword()`,
		`val passwordLength = 12`,
		`const val TOKEN_URL = "https://example.com/oauth/token"`,
		`secret: ${SECRET_FROM_ENV}`,
		"`password` must be at least 8 characters, 'token' is rotated",
	} {
		if got := ScanSecrets(text); len(got) != 0 {
			t.Errorf("ScanSecrets(%q) = %v, want none", text, got)
		}
	}
}

func TestScanSecretsListsEachPatternOnceInTableOrder(t *testing.T) {
	text := canaryAssign + "\n" + canaryGitHub + "\n" + canaryGitHub + "\n" + canaryAWS + "\n" + canaryPEM
	want := []string{"github-token", "aws-access-key-id", "private-key", "credential-assignment"}
	if got := ScanSecrets(text); !reflect.DeepEqual(got, want) {
		t.Errorf("ScanSecrets = %v, want %v", got, want)
	}
}

// TestScanSecretsNeverReturnsTheValue: the answer is the names of the
// patterns, so that nothing that prints it can print a secret.
func TestScanSecretsNeverReturnsTheValue(t *testing.T) {
	text := canaryGitHub + canaryAWS + canaryPEM + canarySlack + canaryAssign
	for _, name := range ScanSecrets(text) {
		for _, canary := range []string{canaryGitHub, canaryAWS, canarySlack, "hunter2hunter2", "MIIB"} {
			if strings.Contains(name, canary) {
				t.Errorf("pattern name %q holds a secret", name)
			}
		}
	}
}
