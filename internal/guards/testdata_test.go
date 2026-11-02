package guards

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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
