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
- Only binaries, launchers, packs, skills, agents, hooks and manifests reach users, via the `release` branch (see `docs/release-allowlist.txt`). Tests and fixtures never ship.

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
