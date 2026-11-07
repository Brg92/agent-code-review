# AGENTS.md — agent-code-review

Instructions for any coding agent (Claude Code, Copilot, Codex, Kiro, Cursor) working on this repository.

## 1. Product invariants (inviolable — behaviour of the shipped plugin)

- The plugin **never** approves, merges, pushes, rebases, checks out or builds the reviewed project, and never closes or resolves anything on the platform.
- The plugin writes to the platform (comments, issues, Related ticks) **only** on explicit user request, always as plan → confirmation → execution of exactly the confirmed plan, and only if the CR head has not changed.
- Review criteria are always read from the **target branch**, never from the branch under review.
- Every outgoing write is secret-scanned. Tokens come only from the OS keychain via `git credential` or from an environment variable in CI; never on disk, in logs or in output.
- `ForgeProvider` exposes no approve/merge/push/close/resolve methods (guarded by a test).

These invariants describe the product. They do not restrict the development agent; development rules are in sections 3–6.

## 2. Product and architecture

`agent-code-review` is a Go CLI shipped as an agent plugin (GitHub Copilot in Android Studio, Kiro, Claude Code, Codex, Cursor). It reviews a pull/merge request ("change request", CR) and, in cascade, every CR listed in its `## Related Changes` section, each with the criteria of its own repository. It writes a full report per CR to a temporary work dir and shows only a summary in chat.

- **Platform-agnostic**: the core never knows GitHub or GitLab. Platform code lives in `internal/forges/<forge>/` behind `ForgeProvider` and its capabilities. GitHub is the default; GitLab comes after the first stable release but is prepared from day one. A cascade uses one platform only.
- **Agent-agnostic**: the CLI is the product, used via shell. Skills and agents are thin; host differences live only in manifests (Agent Plugins 1.0 + `.claude-plugin/`). MCP is an optional adapter, never required.
- **Stack-extensible**: the core never knows Android, KMP, iOS or web. Stack knowledge lives in data packs `packs/<stack>/` (manifest, rules catalog, lexer table, checklists, templates) loaded at runtime. Packs for the first stable release: `android`, `kotlin-multiplatform`.
- **Fat CLI, thin agent**: commands `queue`, `review`, `next`, `finalize`, `show`, `publish --plan|--apply`, `auth`, `doctor`, `check-criteria`, `version`. A state machine (`resolved → collected → judged → finalized → planned → published`) drives the flow; the agent only fills a `judgement` file.
- Only binaries, launchers, packs, JSON schemas, skills, agents, hooks and manifests reach users, via the `release` branch (see `docs/release-allowlist.txt`). Tests and fixtures never ship.

Target repository layout (directories appear only when their first real file is added; this section is the single description of the structure):

```text
cmd/agent-code-review/   entry point
internal/                cli, state, core, lexer, rules, packs, render, refs,
                         transport, auth, mcp, version, forges/{base,github,gitlab}
packs/                   stack packs (data)
plugin/                  manifests, skills, agents, hooks, launchers
schemas/                 JSON Schemas (schema: 1)
testdata/                synthetic fixtures only
tools/                   dev-only tools, never shipped
docs/                    project docs for humans
```

## 3. Development rules

1. **Go, standard library only** in production code (`cmd/`, `internal/`). `go.mod` has **no `require` lines** and declares the minimum supported Go version, not the installed one. Dev tools (staticcheck, govulncheck, gitleaks) run via `go run <module>@<version>` or pinned CI actions, never as module dependencies.
2. **TDD, strictly**: failing test first (run it, see it fail), then the minimum code, then refactor. Every production change comes with its test. Prefer table-driven tests.
3. **Cross-platform** (Windows, macOS, Linux): `path/filepath`; `os/exec` with separate arguments, never through a shell (`sh -c`, `bash`, `cmd /c`, `powershell`, `pwsh`); explicit UTF-8; `\n` line endings in written files.
4. **English** for code, identifiers, comments, docs, branch names and commit messages.
5. **SemVer**: untagged builds print `0.0.0-dev`; the real version is injected at build time via `-ldflags`. Never hardcode a release version in code, manifests or docs.
6. **No company code, names, hosts or PR content anywhere**: the repository is public. Fixtures and sandbox code are synthetic and original.
7. **Secrets**: tokens only from the environment (`ACR_GITHUB_TOKEN` for dev tools) or the user's existing `git`/`gh` authentication. Never print, log or write a token to disk.
8. **Before every commit**: `gofmt -l .` must be empty, `go vet ./...` and `go test ./...` must pass.
9. **No placeholders**: never create empty packages, `doc.go` stubs, empty `main` functions, `.gitkeep` files or per-directory `README.md` files to "reserve" structure. A directory is created together with its first real file (with TDD: its first test). The layout is documented once, in section 2. Package documentation is a package comment in the main file of the package; use `doc.go` only for multi-file packages with long documentation. Documentation for humans lives in the root `README.md` and in `docs/`.
10. **No AI attribution** anywhere: no `Co-Authored-By` trailers, no "generated with" lines or similar in commits, PR titles or descriptions, comments, notes or docs. This does not cover the host names and paths the product itself needs.

## 4. Commits — Conventional Commits 1.0.0

- Format: `<type>(<scope>): <description>` — imperative mood, lowercase, no trailing period, subject max 72 chars.
- Types: `feat`, `fix`, `test`, `refactor`, `docs`, `build`, `ci`, `chore`, `perf`, `style`, `revert`.
- Scopes (closest one): `version`, `cli`, `state`, `core`, `lexer`, `rules`, `packs`, `render`, `refs`, `forges`, `github`, `gitlab`, `transport`, `auth`, `mcp`, `guards`, `recorder`, `schemas`, `sandbox`, `plugin`, `deps`, `repo`.
- Body (optional): explain *why*, wrapped at 72 chars. Breaking changes: `!` after the scope **and** a `BREAKING CHANGE:` footer.
- **Small, coherent commits**: one logical change per commit; every commit leaves the repository green. Never mix unrelated changes (feature + docs fix, code + formatting).
- One TDD cycle (one behaviour) = one commit; refactors and docs in their own commits. Prefer commits under ~200 changed lines; generated fixtures and recorded data go in their own `test(...)` commit.
- Review `git diff --staged` before committing; stage selectively (`git add -p` or explicit paths).
- Never `--no-verify`; never amend or rewrite commits already pushed.

## 5. Branches and pull requests

- Branch name: `<type>/<scope>-<short-description>`, kebab-case, lowercase, ASCII, max 50 chars (e.g. `feat/version-command`). The type is the main type of the branch's commits.
- `main` is protected: never commit to it. Each unit of work starts from the latest `main` (`git checkout main && git pull`) on a new branch and reaches `main` through a PR.
- PR titles use the same Conventional Commits format.
- PRs are merged with **squash** only; the PR title (Conventional Commits) becomes the commit message on `main`.
- After the squash merge the branch is deleted: on GitHub automatically (repository setting "Automatically delete head branches"); locally with `git checkout main && git pull && git branch -D <branch>` (`-D` is required because git does not see a squash-merged branch as merged). Sandbox scenario branches are never merged nor deleted.
- Sandbox repositories (organization `acr-plugin-sandbox`): base content on `main`; scenario branches `<type>/<scope>-s<N>-<short-description>`; scenario PRs are opened by the author account and **never merged**.

## 6. Remote actions

The development agent performs remote actions on its own: pushing its branches, opening and updating pull requests, creating and configuring sandbox repositories, triggering workflows, and calling GitHub APIs. It announces each one in its report (command, branch, target).

**Merging needs explicit permission.** Before any merge (`gh pr merge`, the merge API, or the web UI), stop and ask, stating the PR, its title and the merge method; proceed only after an explicit "yes" for that PR.

**Never**, not even with permission: force-push, push to `main`, delete remote branches, delete repositories, merge or delete sandbox scenario branches or pull requests. Fine-grained tokens are edited by the maintainer only. If `gh` is unavailable, give the exact commands instead.

## 7. Working style

- Work in milestones; at the end of each one report: branch, commits (one line each), changed files, test evidence (red → green for TDD), remote actions performed and merges awaiting permission, open questions. Then stop and wait.
- If a request conflicts with these rules, say so before acting.
