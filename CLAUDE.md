# agent-code-review

CLI written in Go and shipped as an agent plugin. It reviews a change request (CR, a pull or merge request) and, in cascade, every CR listed in its `## Related Changes` section, each one with the review criteria of its own repository read from the target branch. It writes a full report per CR to a temporary work dir, shows only a summary in chat, and writes to the platform only on explicit request, always as plan → confirmation → execution.

## Architecture

- **Platform-agnostic**: the core never knows GitHub or GitLab. Everything platform-specific lives in `internal/forges/<forge>/` behind a `ForgeProvider` interface with capabilities. GitHub is the default; GitLab comes after 1.0.0 but is prepared from day one.
- **Agent-agnostic**: the CLI is the product, used via shell. Skills and agents are thin; host differences live only in manifests (Agent Plugins 1.0 + `.claude-plugin/`). MCP is an optional adapter, never required.
- **Stack-extensible**: the core never knows Android, KMP, iOS or web. Stack-specific knowledge lives in data packs `packs/<stack>/` (manifest, rules catalog, lexer table, checklists, templates) loaded at runtime. Packs for 1.0.0: `android`, `kotlin-multiplatform`.
- **Fat CLI, thin agent**: commands `queue`, `review`, `next`, `finalize`, `show`, `publish --plan|--apply`, `auth`, `doctor`, `check-criteria`, `version`. A state machine (`resolved → collected → judged → finalized → planned → published`) drives the flow. The agent only fills a `judgement` file.

## Rules

1. **Go, standard library only** in production code (`cmd/`, `internal/`). `go.mod` has no `require` lines. Dev tools (staticcheck, govulncheck, gitleaks) run in CI via `go run <module>@<version>` or pinned actions, never as module dependencies.
2. **TDD, strictly**: write the failing test first, run it and see it fail, write the minimum code, run tests green, then refactor. Every production change comes with its test. Prefer table-driven tests.
3. **Cross-platform** (Windows, macOS, Linux): `path/filepath`, `os/exec` with separate arguments, never through a shell (`sh -c`, `bash`, `cmd /c`, `powershell`, `pwsh`). Explicit UTF-8, `\n` line endings in files we write.
4. **English** for code, identifiers, comments, docs in the repo and commit messages.
5. **Conventional Commits**, small commits. For TDD steps, the test may be committed together with the code that makes it pass, but always run red first.
6. **No AI attribution** anywhere: no `Co-Authored-By` trailers, no "generated with" lines or similar in commits, comments, notes or docs. This does not cover the host names and paths the product itself needs.
7. **SemVer**: untagged builds print `0.0.0-dev`; the real version is injected at build time via `-ldflags`. Never hardcode a release version such as `1.0.0`; roadmap mentions in this file are the only exception.
8. **No company code, names, hosts or PR content anywhere**: the repo is public. All fixtures and sandbox code are synthetic and original.
9. **Never** run `git push`, create remote repos, open PRs, call GitHub write APIs, or change remote settings. The user does all remote actions. The network may be used only to download Go tools and actions metadata and to read public docs.
10. **Secrets**: tokens only from the environment variable `ACR_GITHUB_TOKEN` (dev tools only). Never print, log, or write a token to disk.

## Before finishing a change

Run `gofmt -l .`, `go vet ./...` and `go test ./...`.
