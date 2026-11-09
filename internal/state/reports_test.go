package state

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
)

func TestDefaultReportsDir(t *testing.T) {
	home := t.TempDir()
	got, err := DefaultReportsDir(func() (string, error) { return home, nil })
	if want := filepath.Join(home, "agent-code-review", "reports"); err != nil || got != want {
		t.Errorf("reports dir = %q, %v, want %q", got, err, want)
	}
	if _, err := DefaultReportsDir(func() (string, error) { return "", errors.New("no home") }); err == nil {
		t.Error("no error without a home directory")
	}
}

func TestReportName(t *testing.T) {
	at := time.Date(2026, 10, 21, 17, 17, 59, 0, time.UTC)
	k, _ := core.ParseKey("github:github.com/acr-plugin-sandbox/sample-app#53")
	got, err := ReportName(k, "9b96ceb6a1f0e2d3c4b5a6978877665544332211", at)
	if want := "sample-app-53-9b96ceb6_2026-10-21_1717.txt"; err != nil || got != want {
		t.Errorf("name = %q, %v, want %q", got, err, want)
	}
	nested, _ := core.ParseKey("gitlab:gitlab.example.com/group/sub/lib#7")
	if got, _ := ReportName(nested, "0123456789abcdef", at); got != "lib-7-01234567_2026-10-21_1717.txt" {
		t.Errorf("nested project name = %q", got)
	}
	if _, err := ReportName(k, "abc", at); err == nil {
		t.Error("a short head was accepted")
	}
}

func TestRootReport(t *testing.T) {
	if got, want := RootReport("Report\nend\n\n", "Cascade\n"), "Report\nend\n\nCascade\n"; got != want {
		t.Errorf("root report = %q, want %q", got, want)
	}
}

func TestSaveReport(t *testing.T) {
	tmp := t.TempDir()
	if err := checkOutsideGit(tmp); err != nil {
		t.Skipf("the temporary directory is inside a git work tree: %v", err)
	}
	dir := filepath.Join(tmp, "reports", "nested")
	name := "sample-app-53-9b96ceb6_2026-10-21_1717.txt"
	var saved []string
	for i := 0; i < 3; i++ {
		p, err := SaveReport(dir, name, "Report\r\nline")
		if err != nil {
			t.Fatal(err)
		}
		saved = append(saved, p)
	}
	want := []string{
		filepath.Join(dir, name),
		filepath.Join(dir, "sample-app-53-9b96ceb6_2026-10-21_1717_2.txt"),
		filepath.Join(dir, "sample-app-53-9b96ceb6_2026-10-21_1717_3.txt"),
	}
	for i := range want {
		if saved[i] != want[i] {
			t.Errorf("save %d went to %s, want %s", i+1, saved[i], want[i])
		}
	}
	data, err := os.ReadFile(saved[0])
	if err != nil || string(data) != "Report\nline\n" {
		t.Errorf("content = %q, %v, want LF line endings and a final newline", data, err)
	}
	if info, err := os.Stat(saved[0]); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v, want 0600", info.Mode().Perm(), err)
	}
	for _, bad := range []string{"", "../x.txt", "a/b.txt", "report.md"} {
		if _, err := SaveReport(dir, bad, "x"); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	if _, err := SaveReport("relative/dir", name, "x"); err == nil {
		t.Error("a relative folder was accepted")
	}
}

func TestSaveReportRefusesAGitWorkTree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "worktree")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: ../repo/.git/worktrees/w\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"the root of a repository": repo,
		"a folder to create below": filepath.Join(repo, "docs", "reviews"),
		"a linked work tree":       filepath.Join(worktree, "reviews"),
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(repo, link); err == nil {
		cases["a link into a repository"] = filepath.Join(link, "reviews")
	}
	for name, dir := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := SaveReport(dir, "r-1-01234567_2026-10-21_1717.txt", "x")
			var ge *InsideGitError
			if !errors.As(err, &ge) {
				t.Fatalf("error %v, want an *InsideGitError", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "r-1-01234567_2026-10-21_1717.txt")); err == nil {
				t.Error("the report was written")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(repo, "docs")); err == nil {
		t.Error("a folder was created inside the repository")
	}
}
