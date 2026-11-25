// Command review422 captures the body that GitHub answers when a review has
// a comment on a line that is not in the diff. It is a development tool: it
// sends one review with the event COMMENT and one comment to a pull request
// of the sandbox, and keeps the answer, sanitised, as a synthetic fixture. It
// never ships, and the product does not use it.
//
// The token is the one the product uses: the environment variable
// ACR_GITHUB_TOKEN or the git credential helper (`agent-code-review auth
// login`). It is only sent to the API and never printed or saved.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Brg92/agent-code-review/internal/auth"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, auth.GitRunner{}, os.Stdout, os.Stderr))
}

const usage = "usage: review422 -repo owner/repo -pr N -path <file of the diff> -line N -out <file> [-api url]\n" +
	"the line must not be in the diff of the file; the answer is saved only when GitHub refuses the review with a 422"

func run(args []string, getenv func(string) string, git auth.Runner, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review422", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", "https://api.github.com", "REST API base URL")
	repo := fs.String("repo", "", "owner/repo of the pull request")
	pr := fs.Int("pr", 0, "number of the pull request")
	path := fs.String("path", "", "a file of the pull request")
	line := fs.Int("line", 0, "a line of that file that is not in the diff")
	out := fs.String("out", "", "file that receives the sanitised answer")
	fail := func(msg string) int {
		fmt.Fprintln(stderr, "review422:", msg)
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if err := fs.Parse(args); err != nil {
		return fail(err.Error())
	}
	owner, name, ok := strings.Cut(*repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return fail("-repo must be owner/repo")
	}
	if *pr < 1 || *path == "" || *line < 1 || *out == "" {
		return fail("-pr, -path, -line and -out are required")
	}
	if git == nil {
		git = auth.GitRunner{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	token, err := auth.Resolver{Getenv: getenv, Git: git}.Resolve(ctx, "github.com")
	if err != nil {
		if errors.Is(err, auth.ErrNoToken) {
			fmt.Fprintln(stderr, "review422: no token; run `agent-code-review auth login` or set ACR_GITHUB_TOKEN")
		} else {
			fmt.Fprintln(stderr, "review422: could not read the token")
		}
		return 1
	}
	c := client{base: strings.TrimRight(*api, "/"), token: token.Value(), http: &http.Client{Timeout: 30 * time.Second}}
	prPath := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, name, *pr)

	var pull struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	status, body, err := c.do(ctx, http.MethodGet, prPath, nil)
	if err != nil || status != http.StatusOK || json.Unmarshal(body, &pull) != nil || pull.Head.SHA == "" {
		fmt.Fprintf(stderr, "review422: could not read the pull request (status %d)\n", status)
		return 1
	}

	review, err := json.Marshal(map[string]any{
		"commit_id": pull.Head.SHA,
		"event":     "COMMENT",
		"comments": []map[string]any{{
			"path": *path, "line": *line, "side": "RIGHT",
			"body": "Development capture of the answer to a comment outside the diff.",
		}},
	})
	if err != nil {
		fmt.Fprintln(stderr, "review422:", err)
		return 1
	}
	status, body, err = c.do(ctx, http.MethodPost, prPath+"/reviews", review)
	if err != nil {
		fmt.Fprintln(stderr, "review422: the request failed")
		return 1
	}
	if status != http.StatusUnprocessableEntity {
		fmt.Fprintf(stderr, "review422: GitHub answered %d, not 422: nothing saved (if the review was accepted, it is on the pull request now)\n", status)
		return 1
	}
	clean := sanitize(body, owner, name, pull.Head.SHA)
	if err := os.WriteFile(*out, clean, 0o644); err != nil {
		fmt.Fprintln(stderr, "review422: could not write the file:", err)
		return 1
	}
	fmt.Fprintf(stdout, "GitHub answered 422; the sanitised body is in %s\n", *out)
	return 0
}

type client struct {
	base, token string
	http        *http.Client
}

func (c client) do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "review422")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
}

// sanitize makes the answer synthetic: the repository is o/r, the owner is o
// and the head commit is a run of zeros. The file ends with a newline.
func sanitize(body []byte, owner, repo, head string) []byte {
	s := string(bytes.TrimSpace(body))
	s = strings.ReplaceAll(s, head, strings.Repeat("0", len(head)))
	s = strings.ReplaceAll(s, owner+"/"+repo, "o/r")
	s = strings.ReplaceAll(s, owner, "o")
	s = strings.ReplaceAll(s, repo, "r")
	return []byte(s + "\n")
}
