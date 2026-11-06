# Continuous integration

The workflow `.github/workflows/ci.yml` runs on every push to `main` and on every pull request targeting `main`. It has read-only access to the repository (`permissions: contents: read`), and every third-party action is pinned to a full commit SHA, with its version in a comment.

## Jobs

| Job | What it checks |
| --- | --- |
| `lint` | `gofmt -l .` is empty, `go vet ./...` passes, `staticcheck ./...` passes |
| `security` | `govulncheck ./...` finds no vulnerabilities; `gitleaks` finds no secrets in the whole git history |
| `test (<os>, go <version>)` | `go test -race -coverprofile=coverage.out ./...` passes; prints the total coverage (no threshold yet) |
| `build (<goos>, <goarch>)` | `CGO_ENABLED=0 go build -trimpath` succeeds; uploads the binary as an artifact kept for 3 days |

The test matrix covers `ubuntu-latest`, `macos-latest` and `windows-latest` with Go `stable` and `oldstable`. The build matrix covers `linux`, `darwin` and `windows` on `amd64` and `arm64`. CI binaries report the version `0.0.0-ci+<short sha>`.

## Required checks

The `protect-main` ruleset requires these checks on `main`, each from the GitHub Actions app:

- `lint`
- `security`
- `test (ubuntu-latest, go stable)`
- `test (ubuntu-latest, go oldstable)`
- `test (macos-latest, go stable)`
- `test (macos-latest, go oldstable)`
- `test (windows-latest, go stable)`
- `test (windows-latest, go oldstable)`
- `build (linux, amd64)`
- `build (linux, arm64)`
- `build (darwin, amd64)`
- `build (darwin, arm64)`
- `build (windows, amd64)`
- `build (windows, arm64)`

Renaming a job or changing a matrix value changes these names, so that pull request cannot be merged until the ruleset lists the new names.

## Extra secret patterns

The repository secret `ACR_SECRET_PATTERNS` adds regular expressions to the default gitleaks rules, one per line. Blank lines are ignored, and a pattern must not contain `'''`. When the secret is empty or unset, only the default rules apply.

## Pinned tools

| Tool | Version | How it runs |
| --- | --- | --- |
| `actions/checkout` | v7.0.1 | action |
| `actions/setup-go` | v7.0.0 | action |
| `actions/upload-artifact` | v7.0.2 | action |
| staticcheck | `honnef.co/go/tools` v0.8.2 | `go run` |
| govulncheck | `golang.org/x/vuln` v1.9.0 | `go run` |
| gitleaks | `github.com/zricethezav/gitleaks/v8` v8.30.1 | `go run` |

To update an action, resolve the new tag to its commit with `git ls-remote https://github.com/<owner>/<repo>.git refs/tags/<tag>` and replace both the SHA and the version comment.
