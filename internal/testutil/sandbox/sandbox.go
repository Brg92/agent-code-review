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
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
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
	// BaseRef is the target branch.
	BaseRef string
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
			Head struct{ SHA string }      `json:"head"`
			Base struct{ SHA, Ref string } `json:"base"`
			User struct{ Login string }    `json:"user"`
		}
		if err := json.Unmarshal(r.Body, &body); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		return Pull{HeadSHA: body.Head.SHA, BaseSHA: body.Base.SHA, BaseRef: body.Base.Ref, Author: body.User.Login}
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

// Entry is an entry of a recorded directory listing.
type Entry struct {
	Name string
	Dir  bool
}

// Recorded is the recorded contents response for a path at a ref: a file,
// a directory listing, or a status without either, such as 404.
type Recorded struct {
	// Recorded is false when no response was recorded for the request.
	Recorded bool
	Status   int
	File     []byte
	Dir      []Entry
}

// Contents returns the recorded contents response for path of repo at
// ref in a scenario, whatever its status.
func Contents(t testing.TB, scenario, repo, path, ref string) Recorded {
	t.Helper()
	want := "/repos/" + Owner + "/" + repo + "/contents/" + path
	for f, r := range recordings(t, filepath.Join(scenario, "GET_repos_*_contents_*.json")) {
		if r.Path != want || r.Query["ref"] != ref {
			continue
		}
		out := Recorded{Recorded: true, Status: r.Status}
		if r.Status != 200 {
			return out
		}
		var list []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if json.Unmarshal(r.Body, &list) == nil {
			out.Dir = []Entry{}
			for _, e := range list {
				out.Dir = append(out.Dir, Entry{Name: e.Name, Dir: e.Type == "dir"})
			}
			return out
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
		out.File = data
		return out
	}
	return Recorded{}
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

// Tree is the recorded recursive tree of a repository at a commit.
type Tree struct {
	// Recorded is false when no tree was recorded for the commit.
	Recorded bool
	// Truncated is the flag as GitHub returned it: the tree is partial.
	Truncated bool
	// Paths are the files (not the directories) of the tree.
	Paths []string
}

// TreeOf returns the recorded tree of repo at the commit sha in a scenario.
func TreeOf(t testing.TB, scenario, repo, sha string) Tree {
	t.Helper()
	want := "/repos/" + Owner + "/" + repo + "/git/trees/" + sha
	for f, r := range recordings(t, filepath.Join(scenario, "GET_repos_*_git_trees_*.json")) {
		if r.Path != want {
			continue
		}
		var body struct {
			Truncated bool `json:"truncated"`
			Tree      []struct {
				Path string `json:"path"`
				Type string `json:"type"`
			} `json:"tree"`
		}
		if err := json.Unmarshal(r.Body, &body); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out := Tree{Recorded: true, Truncated: body.Truncated}
		for _, e := range body.Tree {
			if e.Type == "blob" {
				out.Paths = append(out.Paths, e.Path)
			}
		}
		return out
	}
	return Tree{}
}

// Search is a recorded code search of a term in a repository.
type Search struct {
	// Recorded is false when the search was not recorded.
	Recorded bool
	Total    int
	// Incomplete is the incomplete_results flag as GitHub returned it: the
	// search did not finish, and an empty result proves nothing.
	Incomplete bool
	// Paths are the files returned, with no line numbers.
	Paths []string
}

func parseSearch(body []byte) (Search, error) {
	var v struct {
		Total      int  `json:"total_count"`
		Incomplete bool `json:"incomplete_results"`
		Items      []struct {
			Path string `json:"path"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return Search{}, err
	}
	out := Search{Recorded: true, Total: v.Total, Incomplete: v.Incomplete}
	for _, it := range v.Items {
		out.Paths = append(out.Paths, it.Path)
	}
	return out, nil
}

// SearchOf returns the recorded code search of term in repo in a
// scenario.
func SearchOf(t testing.TB, scenario, repo, term string) Search {
	t.Helper()
	want := term + " repo:" + Owner + "/" + repo
	for f, r := range recordings(t, filepath.Join(scenario, "GET_search_code_*.json")) {
		if r.Path != "/search/code" || r.Query["q"] != want {
			continue
		}
		s, err := parseSearch(r.Body)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		return s
	}
	return Search{}
}
