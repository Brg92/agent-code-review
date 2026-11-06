# Phase F0 summary

Phase F0 lays the foundations of `agent-code-review`: a minimal CLI, the guards that keep the repository's rules enforceable, CI, a sandbox of real pull requests, offline recordings of them, and the JSON contracts between the CLI and the agent. No review logic exists yet; that starts in F1.

## What was delivered

| Milestone | Pull request | Result |
| --- | --- | --- |
| M1 | [#1](https://github.com/Brg92/agent-code-review/pull/1) | Agent conventions (`AGENTS.md`) and the repository layout |
| M2 | [#2](https://github.com/Brg92/agent-code-review/pull/2) | `version` and `--version`; the version is injected at build time and defaults to `0.0.0-dev` |
| M3 | [#4](https://github.com/Brg92/agent-code-review/pull/4) | Structural guards |
| M4 | [#5](https://github.com/Brg92/agent-code-review/pull/5) | GitHub Actions: lint, security, a test matrix and cross-platform builds ([ci.md](ci.md)) |
| M5 | [#6](https://github.com/Brg92/agent-code-review/pull/6) | Sandbox organization, four repositories and scenarios S1 to S6 ([sandbox.md](sandbox.md)) |
| M6 | [#7](https://github.com/Brg92/agent-code-review/pull/7) | Read-only API recorder, sanitized recordings of every scenario, hand-written judgements |
| M7 | [#8](https://github.com/Brg92/agent-code-review/pull/8) | Nine JSON Schemas with valid and invalid examples, a test-only validator, proposed exit codes ([exit-codes.md](exit-codes.md)) |

[#3](https://github.com/Brg92/agent-code-review/pull/3) added the squash-merge and branch-cleanup rules to `AGENTS.md`.

## What the tests enforce

- **Standard library only** in `cmd/` and `internal/`, and no `require` lines in `go.mod`.
- **No shell:** no `os/exec` call runs `sh`, `bash`, `cmd`, `powershell` or similar.
- **No platform writes in the forge interface:** no method in `internal/forges/base` starts with Approve, Merge, Push, Close or Resolve. The package does not exist yet; the guard is ready for it.
- **Release allowlist:** the patterns in `docs/release-allowlist.txt` are well-formed.
- **Clean testdata:** no token-like strings anywhere in `testdata/`. With `ACR_SANDBOX_LOGINS` set, no real sandbox login either. Recordings may only contain synthetic logins.
- **Contracts:** every schema follows the conventions and keeps shared definitions identical, every valid example matches and every invalid example fails. The invalid overlay of S6 B is reported field by field. Scenario judgements only use rules from the example catalog.
- **CLI:** `version` and `--version` print the injected version; any other use prints the usage on stderr and exits with 2.

## How to verify

```bash
gofmt -l .
```

```bash
go vet ./...
```

```bash
go test ./...
```

```bash
ACR_SANDBOX_LOGINS=<reviewer>,<author> go test ./internal/guards/
```

`gofmt -l .` must print nothing. CI runs the same checks on Linux, macOS and Windows with the two latest Go releases, plus staticcheck, govulncheck and gitleaks.

## Decisions to know

- **One judgement per change request:** the agent receives one work package and writes one judgement for each change request of a cascade.
- **Publishing cannot approve or merge:** the plan schema only expresses comments, issues and Related Changes ticks.
- **Skipped entries are kept:** the run state lists every Related Changes entry, including the skipped ones and why.
- **Schema patterns use Go regexp syntax**, so they have no lookahead.
- **Unknown accounts are anonymized:** the recorder replaces every user account it does not know with `sandbox-user-<n>`, because GitHub links commits to any account that verified the commit email.

## Decisions taken at the end of F0

- **Criteria edited in a change request (S6 A):** the review goes on with the target branch's criteria and the summary notes that the edit was not applied. Exit code 5 is reserved and unused.
- **Default cascade limit:** 2. A repository can change it with `cascadeLimit` in `.agent-code-review/config.json`.
- **Schemas ship:** `schemas/**` is in the release allowlist, so agents can read the judgement schema.
- **Sandbox commit email:** sandbox commits move to an address on a reserved domain that no account can verify, so GitHub stops linking them to an unrelated account. This rewrites the sandbox history once, as an explicit exception to the no-force-push rule, and the recordings and judgements are regenerated in their own pull request.

## Open items

- **Exit codes:** [exit-codes.md](exit-codes.md) stays a proposal until the first command returns them.
- **Sandbox checks:** `core-lib`, `kmp-shared` and `feature-lib` have no check runs on their pull requests, although their `checks` workflow exists. S5 only needs `sample-app`, but the cause is unknown.
- **Recorder token:** the recordings were made with the reviewer's `gh` token. The fine-grained, read-only token described in [sandbox.md](sandbox.md) is not tested yet, in particular whether it can read check runs.
- **Rule ids and severities** live in the example catalog `testdata/contracts/rules-catalog/valid/android.json`. They become real with the `android` pack.
- **Recordings age:** the S5 A recording shows `instrumented` in progress. Recording S5 again needs the job to be re-run first.

## Next

F1 starts the review flow on top of these contracts: resolving a change request and its cascade, collecting it through `ForgeProvider` for GitHub, and the `review`, `next` and `finalize` commands, tested offline against the recordings in `testdata/github/`.
