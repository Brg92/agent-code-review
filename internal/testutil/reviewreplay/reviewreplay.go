// Package reviewreplay serves the recordings of the sandbox scenarios the
// way a review reads them: several scenarios on one server, the answers the
// recordings lack, and a token to read them with. It is test support code
// only.
//
// The recordings lack two kinds of answer, which this package adds as
// synthetic routes: the contents of the build files that the trees list,
// and the 404 of directories the sandbox repositories do not have.
package reviewreplay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// Canary is the token the replay tests read with: nothing a review writes or
// prints may hold it.
const Canary = "ghp_CANARYtoken0123456789abcdefABCDEF"

// Tokens resolves Canary for any host.
type Tokens struct{}

// Resolve implements the token source of the GitHub provider.
func (Tokens) Resolve(ctx context.Context, host string) (auth.Token, error) {
	return auth.Resolver{Getenv: func(string) string { return Canary }}.Resolve(ctx, host)
}

// New starts a replay of the recordings of the scenarios, the first one
// served as it is and the others added to it: a cascade walks into pull
// requests of several scenarios, as S2 reaches the one of S4.
func New(t testing.TB, scenarios ...string) *sandbox.Replay {
	t.Helper()
	r := sandbox.NewReplay(t, scenarios[0])
	for _, extra := range scenarios[1:] {
		addRecordings(t, r, extra)
	}
	return r
}

// S2 starts a replay of the S2 cascade with the pull request of S4 it
// reaches, ready to be reviewed from sample-app#7.
func S2(t testing.TB) *sandbox.Replay {
	t.Helper()
	r := New(t, "S2", "S4")
	AddBuildFiles(t, r, "S2", "sample-app", 7)
	AddBuildFiles(t, r, "S2", "sample-app", 2)
	AddBuildFiles(t, r, "S2", "core-lib", 3)
	AddBuildFiles(t, r, "S2", "kmp-shared", 2)
	AddBuildFiles(t, r, "S4", "feature-lib", 1)
	AddNotFound(r, "core-lib", ".github/skills/core-lib-code-review/references", "main")
	return r
}

// root is the repository root, found from this file.
func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func addRecordings(t testing.TB, r *sandbox.Replay, scenario string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root(), "testdata", "github", scenario, "GET_*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no recordings of %s: %v", scenario, err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var rec struct {
			Path    string            `json:"path"`
			Query   map[string]string `json:"query"`
			Status  int               `json:"status"`
			Headers map[string]string `json:"headers"`
			Body    json.RawMessage   `json:"body"`
		}
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		r.Add(sandbox.Route{Path: rec.Path, Query: rec.Query, Status: rec.Status, Header: rec.Headers, Body: string(rec.Body)})
	}
}

// buildFiles are synthetic build files of the sandbox repositories.
var buildFiles = map[string][2]string{
	"sample-app":  {"app/build.gradle.kts", "plugins { id(\"com.android.application\") }\ndependencies { implementation(\"androidx.compose.ui:ui\") }\n"},
	"core-lib":    {"build.gradle.kts", "plugins {\n    id(\"com.android.library\")\n    id(\"maven-publish\")\n}\n"},
	"feature-lib": {"build.gradle.kts", "plugins { id(\"com.android.library\") }\nandroid { buildFeatures { viewBinding = true } }\n"},
	"kmp-shared":  {"shared/build.gradle.kts", "plugins {\n    kotlin(\"multiplatform\")\n    id(\"com.android.library\")\n}\n"},
}

// AddBuildFiles answers the reads of the build files of repo at the head of
// pull request number of a scenario: the main one with its synthetic
// contents, any other file the packs read with neutral ones.
func AddBuildFiles(t testing.TB, r *sandbox.Replay, scenario, repo string, number int) {
	t.Helper()
	head := sandbox.PullRequest(t, scenario, repo, number).HeadSHA
	for _, p := range sandbox.TreeOf(t, scenario, repo, head).Paths {
		var content string
		switch {
		case p == buildFiles[repo][0]:
			content = buildFiles[repo][1]
		case strings.HasSuffix(p, "build.gradle.kts") || strings.HasSuffix(p, "build.gradle"):
			content = "// synthetic build file\n"
		case strings.HasSuffix(p, "src/main/AndroidManifest.xml") || p == "gradle/libs.versions.toml":
			content = "<manifest/>\n"
		default:
			continue
		}
		body, err := json.Marshal(map[string]any{
			"type": "file", "encoding": "base64", "name": filepath.Base(p), "path": p, "sha": "0000000000000000000000000000000000000000",
			"content": base64.StdEncoding.EncodeToString([]byte(content)),
		})
		if err != nil {
			t.Fatal(err)
		}
		r.Add(sandbox.Route{Path: "/repos/" + sandbox.Owner + "/" + repo + "/contents/" + p, Query: map[string]string{"ref": head}, Status: 200, Body: string(body)})
	}
}

// AddNotFound answers 404 for a path of a sandbox repository that the
// recorder did not ask for because the repository does not have it, as
// GitHub does: the references directory of a legacy skill, for example.
func AddNotFound(r *sandbox.Replay, repo, path, ref string) {
	r.Add(sandbox.Route{Path: "/repos/" + sandbox.Owner + "/" + repo + "/contents/" + path, Query: map[string]string{"ref": ref}, Status: 404,
		Body: `{"message":"Not Found","status":"404"}`})
}
