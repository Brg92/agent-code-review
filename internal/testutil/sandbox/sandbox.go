// Package sandbox reads the recorded GitHub responses of the sandbox
// scenarios in testdata/github: the changed files of a pull request and
// the contents of a file at a commit. It is test support code only.
package sandbox

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"testing"
)

// Owner is the organization of the sandbox repositories.
const Owner = "acr-plugin-sandbox"

// File is one entry of a recorded list of the changed files of a pull
// request.
type File struct {
	Filename         string  `json:"filename"`
	PreviousFilename string  `json:"previous_filename"`
	Status           string  `json:"status"`
	Additions        int     `json:"additions"`
	Deletions        int     `json:"deletions"`
	Patch            *string `json:"patch"`
}

// FileList is the recorded list of the changed files of one pull request.
type FileList struct {
	Scenario string
	Repo     string
	Number   int
	Files    []File
}

type recording struct {
	Path  string            `json:"path"`
	Query map[string]string `json:"query"`
	Body  json.RawMessage   `json:"body"`
}

// root is the repository root, found from this file so that tests of any
// package can use the helpers.
func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func recordings(t testing.TB, pattern string) map[string]recording {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root(), "testdata", "github", pattern))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]recording{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var r recording
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[f] = r
	}
	return out
}

var filesPath = regexp.MustCompile(`^/repos/` + Owner + `/([^/]+)/pulls/([0-9]+)/files$`)

// PullFiles returns the recorded changed files of pull request number of
// repo in a scenario, such as "S3".
func PullFiles(t testing.TB, scenario, repo string, number int) []File {
	t.Helper()
	for _, l := range AllPullFiles(t) {
		if l.Scenario == scenario && l.Repo == repo && l.Number == number {
			return l.Files
		}
	}
	t.Fatalf("no recorded files of %s#%d in %s", repo, number, scenario)
	return nil
}

// AllPullFiles returns every recorded list of changed files, ordered by
// scenario, repository and number.
func AllPullFiles(t testing.TB) []FileList {
	t.Helper()
	var lists []FileList
	for f, r := range recordings(t, filepath.Join("*", "GET_repos_*_pulls_*_files_*.json")) {
		m := filesPath.FindStringSubmatch(r.Path)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		l := FileList{Scenario: filepath.Base(filepath.Dir(f)), Repo: m[1], Number: n}
		if err := json.Unmarshal(r.Body, &l.Files); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		lists = append(lists, l)
	}
	sort.Slice(lists, func(i, j int) bool {
		a, b := lists[i], lists[j]
		if a.Scenario != b.Scenario {
			return a.Scenario < b.Scenario
		}
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		return a.Number < b.Number
	})
	return lists
}

// Pull is what the tests read of a recorded pull request.
type Pull struct {
	HeadSHA, BaseSHA string
	// Author is the login of the author.
	Author string
}

// PullRequest returns the recorded pull request number of repo in a
// scenario.
func PullRequest(t testing.TB, scenario, repo string, number int) Pull {
	t.Helper()
	want := "/repos/" + Owner + "/" + repo + "/pulls/" + strconv.Itoa(number)
	for f, r := range recordings(t, filepath.Join(scenario, "GET_repos_*_pulls_*.json")) {
		if r.Path != want {
			continue
		}
		var body struct {
			Head struct{ SHA string }   `json:"head"`
			Base struct{ SHA string }   `json:"base"`
			User struct{ Login string } `json:"user"`
		}
		if err := json.Unmarshal(r.Body, &body); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		return Pull{HeadSHA: body.Head.SHA, BaseSHA: body.Base.SHA, Author: body.User.Login}
	}
	t.Fatalf("no recorded pull request %s#%d in %s", repo, number, scenario)
	return Pull{}
}

// Content returns the recorded contents of path in repo at ref, and
// whether they were recorded in the scenario.
func Content(t testing.TB, scenario, repo, path, ref string) (string, bool) {
	t.Helper()
	want := "/repos/" + Owner + "/" + repo + "/contents/" + path
	for f, r := range recordings(t, filepath.Join(scenario, "GET_repos_*_contents_*.json")) {
		if r.Path != want || r.Query["ref"] != ref {
			continue
		}
		var body struct {
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(r.Body, &body); err != nil || body.Encoding != "base64" {
			t.Fatalf("%s: not a base64 file: %v", f, err)
		}
		data, err := base64.StdEncoding.DecodeString(stripNewlines(body.Content))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		return string(data), true
	}
	return "", false
}

func stripNewlines(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
