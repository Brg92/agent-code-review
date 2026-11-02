package guards

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var tokenLike = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})`)

// leaks returns a description of every token-like string in data and of
// every real login that is not part of a longer login.
func leaks(data string, logins []string) []string {
	var found []string
	for range tokenLike.FindAllStringIndex(data, -1) {
		found = append(found, "token-like string")
	}
	lower := strings.ToLower(data)
	for _, login := range logins {
		l := strings.ToLower(login)
		for i := 0; ; {
			j := strings.Index(lower[i:], l)
			if j < 0 {
				break
			}
			start, end := i+j, i+j+len(l)
			if (start == 0 || !isLoginChar(lower[start-1])) && (end == len(lower) || !isLoginChar(lower[end])) {
				found = append(found, fmt.Sprintf("login %q", login))
				break
			}
			i = start + 1
		}
	}
	return found
}

func isLoginChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
}

func TestLeaks(t *testing.T) {
	logins := []string{"RealReviewer", "real-author"}
	tests := []struct {
		name string
		data string
		want []string
	}{
		{"clean", `{"login":"sandbox-author"}`, nil},
		{"classic token", "x ghp_" + strings.Repeat("Z9", 18), []string{"token-like string"}},
		{"oauth token", "gho_" + strings.Repeat("a", 36), []string{"token-like string"}},
		{"fine-grained token", "github_pat_" + strings.Repeat("A_1", 9), []string{"token-like string"}},
		{"short prefix only", "ghp_short", nil},
		{"login", `{"login":"realreviewer"}`, []string{`login "RealReviewer"`}},
		{"login in url", "https://github.com/real-author/x", []string{`login "real-author"`}},
		{"longer login", "RealReviewer2 real-author-bot xreal-author", nil},
		{"both", "RealReviewer then real-author", []string{`login "RealReviewer"`, `login "real-author"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := leaks(tt.data, logins); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("leaks(%q) = %q, want %q", tt.data, got, tt.want)
			}
		})
	}
}

// sandboxLogins returns the real sandbox logins from ACR_SANDBOX_LOGINS.
func sandboxLogins() []string {
	var logins []string
	for _, l := range strings.Split(os.Getenv("ACR_SANDBOX_LOGINS"), ",") {
		if l = strings.TrimSpace(l); l != "" {
			logins = append(logins, l)
		}
	}
	return logins
}

func checkTestdata(t *testing.T, logins []string) {
	t.Helper()
	root := filepath.Join(repoRoot, "testdata")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, l := range leaks(string(data), logins) {
			t.Errorf("%s: %s", path, l)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTestdataHasNoTokens(t *testing.T) {
	checkTestdata(t, nil)
}

func TestTestdataHasNoSandboxLogins(t *testing.T) {
	logins := sandboxLogins()
	if len(logins) == 0 {
		t.Skip("ACR_SANDBOX_LOGINS is not set")
	}
	checkTestdata(t, logins)
}

var allowedLogin = regexp.MustCompile(`^(?:sandbox-(?:reviewer|author|user-[0-9]+)|acr-plugin-sandbox|github|[a-z0-9-]+\[bot\])$`)

// unexpectedLogins returns, sorted, every login in a JSON document that is
// neither synthetic, the sandbox organization, GitHub itself nor a bot.
func unexpectedLogins(data []byte) ([]string, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	var bad []string
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case []any:
			for _, e := range v {
				walk(e)
			}
		case map[string]any:
			if login, ok := v["login"].(string); ok && !allowedLogin.MatchString(login) {
				bad = append(bad, login)
			}
			for _, e := range v {
				walk(e)
			}
		}
	}
	walk(v)
	sort.Strings(bad)
	return bad, nil
}

func TestUnexpectedLogins(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []string
	}{
		{"allowed", `{"user":{"login":"sandbox-author"},"r":[{"login":"sandbox-reviewer"},{"login":"sandbox-user-12"}],` +
			`"owner":{"login":"acr-plugin-sandbox"},"app":{"owner":{"login":"github"}},"bot":{"login":"github-actions[bot]"}}`, nil},
		{"stranger", `{"commit":{"author":{"login":"Someone"}},"items":[{"login":"sandbox-author"},{"login":"other"}]}`, []string{"Someone", "other"}},
		{"look-alike", `[{"login":"sandbox-user-"},{"login":"xsandbox-author"},{"login":"github2"}]`, []string{"github2", "sandbox-user-", "xsandbox-author"}},
		{"not a string", `{"login":7}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := unexpectedLogins([]byte(tt.data))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("unexpectedLogins = %q, want %q", got, tt.want)
			}
		})
	}
	if _, err := unexpectedLogins([]byte(`{`)); err == nil {
		t.Error("unexpectedLogins accepted invalid JSON")
	}
}

func TestRecordingsHaveOnlySyntheticLogins(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot, "testdata", "github", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		logins, err := unexpectedLogins(data)
		if err != nil {
			t.Errorf("%s: %v", f, err)
		}
		for _, l := range logins {
			t.Errorf("%s: unexpected login %q", f, l)
		}
	}
}
