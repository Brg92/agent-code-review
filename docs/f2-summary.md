# Phase F2 summary

Phase F2 gives `agent-code-review` its platform side: a read-only transport, the token from the OS credential helper, the GitHub provider, the effective rules and the work package, and the commands `auth`, `review`, `next`, `finalize`, `show`, `publish`, `queue`, `doctor` and `check-criteria`. It is the first phase in which the product reads a real platform, and, with `publish`, the first in which it writes to one. F2 ends with a complete review of a sandbox scenario, from `review` to a confirmed `publish --apply`, run for real on Linux ([e2e.md](e2e.md)).

## What was delivered

| Pull request | Result |
| --- | --- |
| [#32](https://github.com/Brg92/agent-code-review/pull/32) | `internal/forges/base`: the read side of `ForgeProvider`, its capabilities and the contract suite |
| [#33](https://github.com/Brg92/agent-code-review/pull/33) | Effective rules (criteria applied to the packs), the work package and the brief |
| [#34](https://github.com/Brg92/agent-code-review/pull/34) | Read-only transport (retries, rate limits, ETags), token resolution through `git credential` or `ACR_GITHUB_TOKEN`, the `auth` command |
| [#35](https://github.com/Brg92/agent-code-review/pull/35) | The S1 contract example aligned with the engine, pack ids reserved |
| [#36](https://github.com/Brg92/agent-code-review/pull/36) | The recorder records trees and code searches; scenario S7 |
| [#37](https://github.com/Brg92/agent-code-review/pull/37) | `gh api` and `gh repo edit` always ask; more pushes denied |
| [#38](https://github.com/Brg92/agent-code-review/pull/38) | Hardening of the brief, keys, fingerprints, transport and auth |
| [#39](https://github.com/Brg92/agent-code-review/pull/39) | The read side of the GitHub provider |
| [#40](https://github.com/Brg92/agent-code-review/pull/40) | The recorder records repositories, criteria listings and the S1 comment |
| [#41](https://github.com/Brg92/agent-code-review/pull/41) | `review` and `next` |
| [#42](https://github.com/Brg92/agent-code-review/pull/42) | `finalize` and `show` |
| [#43](https://github.com/Brg92/agent-code-review/pull/43) | One set of hand-written judgements for the scenarios, validated against the product flow |
| [#44](https://github.com/Brg92/agent-code-review/pull/44) | `publish --plan` and `--apply` |
| [#45](https://github.com/Brg92/agent-code-review/pull/45) | `queue`, `doctor` and `check-criteria` |
| this pull request | Scenario S8, the end-to-end run, the capture of a real 422, the closure cleanups and this summary |

## What exists

### Commands

| Command | Reads | Writes to the platform | Documentation |
| --- | --- | --- | --- |
| `auth login`, `auth status`, `auth logout` | the credential helper | nothing | |
| `queue [--limit N]` | one search | nothing | [queue.md](queue.md) |
| `review <pull request>` | the cascade and, for each change request, its files, criteria, checks and tree | nothing | [review-and-next.md](review-and-next.md) |
| `next` | the run | nothing | [review-and-next.md](review-and-next.md) |
| `finalize` | the run | nothing | [finalize-and-show.md](finalize-and-show.md) |
| `show` | the run | nothing | [finalize-and-show.md](finalize-and-show.md) |
| `publish --plan <comments\|issues\|both>` | the platform | nothing | [publish.md](publish.md) |
| `publish --apply` | the platform | exactly the stored plans, only after a yes | [publish.md](publish.md) |
| `doctor [--repo owner/repo]` | the environment and the repository | nothing | [doctor.md](doctor.md) |
| `check-criteria [<dir>]` | a working copy, offline | nothing | [check-criteria.md](check-criteria.md) |

A state machine drives the flow (`resolved → collected → judged → finalized → planned → published`); the agent only fills the `judgement` file. [docs/e2e.md](e2e.md) lists the commands in order for Linux, macOS and Windows.

### Contracts

Schemas at `schema: 1` that F2 added or changed, each with valid and invalid examples in `testdata/contracts/`: `workpackage`, `findings`, `judgement`, `plan`, `state` and the new `published`. Merged with `!`:

- [#33](https://github.com/Brg92/agent-code-review/pull/33): the work package drops the patch and the head and base copy paths from `files[]`, and requires `checklist`, `notVerified`, `cascade`, `parts`, the criteria's `profileReason`, `dimensions` and `ignored`, and `source` and `needsJudgement` on rules.
- [#38](https://github.com/Brg92/agent-code-review/pull/38): `signalsDismissed` entries require `defaultSeverity`, and a project key with a `.` or `..` segment no longer matches.
- [#41](https://github.com/Brg92/agent-code-review/pull/41): the work package requires `ci` and `criteria.notes`; the Not verified reasons gain `content-unavailable`.
- [#42](https://github.com/Brg92/agent-code-review/pull/42): every signal has a required `lineText`; an invalid user `config.json` exits 3.
- [#44](https://github.com/Brg92/agent-code-review/pull/44): a new shape of `plan.json` (`issue-create` and `issue-update`), `planSha256` in the state, text limits in the judgement, the new `published.json`, and the rule for which fingerprints count.

### Exit codes

Binding: 0, 1, 2, 3, 4, 6, 7, 8, 9, 10 and 130. 5 stays reserved and is never used. The table, with the commands that return each code, is in [exit-codes.md](exit-codes.md); a guard and a table test keep it true.

### Coverage per package

`tools/covergate` over `go test -coverprofile ./...`; the threshold is 90% for every package under `internal/` except `internal/testutil/...` and `internal/guards`. The module as a whole, test helpers and tools included, is at 89.1%.

| Package | Statements | Covered | Coverage |
| --- | ---: | ---: | ---: |
| `internal/auth` | 187 | 180 | 96.2% |
| `internal/cascade` | 110 | 105 | 95.4% |
| `internal/cli` | 654 | 615 | 94.0% |
| `internal/core` | 1523 | 1488 | 97.7% |
| `internal/forges/base` | 74 | 74 | 100.0% |
| `internal/forges/github` | 759 | 708 | 93.2% |
| `internal/forges/gitlab` | 34 | 34 | 100.0% |
| `internal/lexer` | 317 | 314 | 99.0% |
| `internal/packs` | 458 | 444 | 96.9% |
| `internal/refs` | 72 | 70 | 97.2% |
| `internal/render` | 777 | 765 | 98.4% |
| `internal/review` | 1508 | 1374 | 91.1% |
| `internal/rules` | 1199 | 1151 | 95.9% |
| `internal/runlog` | 57 | 55 | 96.4% |
| `internal/state` | 367 | 341 | 92.9% |
| `internal/symbols` | 411 | 400 | 97.3% |
| `internal/transport` | 429 | 416 | 96.9% |
| `internal/version` | 1 | 1 | 100.0% |

## End-to-end result

Scenario S8 ([testdata/scenarios/S8/README.md](../testdata/scenarios/S8/README.md)): [`sample-app#9`](https://github.com/acr-plugin-sandbox/sample-app/pull/9), the root, lists [`core-lib#4`](https://github.com/acr-plugin-sandbox/core-lib/pull/4) in its Related Changes. Both were opened by the author account, with a review requested from the reviewer, and are never merged or recorded.

### Linux

Run on the developer's machine, with the commands of [e2e.md](e2e.md), the reviewer's fine-grained token stored by `auth login`, and the hand-written judgements of `testdata/scenarios/S8/judgements/`.

| Step | Result |
| --- | --- |
| `doctor --repo`, both repositories | exit 0; the two write checks say they cannot be made without writing |
| `review` | `core-lib#4` first, `sample-app#9` last, 2 parts, nothing written |
| `next`, judgements, `finalize` | `core-lib#4` OK; `sample-app#9` BLOCKED with 1 blocker, 2 major and 1 existing finding |
| `publish --plan both` | `core-lib#4`: the note and the tick. `sample-app#9`: the label, a BLOCKER issue, a MAJOR issue and 1 inline comment |
| `publish --apply` | exit 0, everything `posted` |
| second plan (after a new `review` of the root) | `nothing to write`: `already-posted` for both issues and the inline comment; `core-lib#4` skipped as `already done` |
| `run.log` | one line per request, no token, header, query or body: no match for `gh[pousr]_`, `github_pat_` or `Bearer` in either run |

What was written:

- On `sample-app#9`: [one review](https://github.com/acr-plugin-sandbox/sample-app/pull/9#discussion_r4701789338) with the event `COMMENT` and one inline comment on line 13 of `MainActivity.kt` (the `runBlocking`); the label `review`; [issue 10](https://github.com/acr-plugin-sandbox/sample-app/issues/10), the BLOCKER of existing code (`global-scope`), and [issue 11](https://github.com/acr-plugin-sandbox/sample-app/issues/11), the MAJOR findings (`missing-tests`, `no-double-bang`), both assigned to the author; and the box of `core-lib#4` ticked in its description.
- On `core-lib#4`: [the note](https://github.com/acr-plugin-sandbox/core-lib/pull/4#issuecomment-7176838934) `Review completed: no blocking issues found!`.

What the run showed:

- **A token without write permission fails cleanly.** The first `--apply` ran with a token that could read but not write: it stopped at its first write with exit 6 and `forbidden (needs issues: write, pull_requests: write), status 403`, wrote nothing, and said what was left. `doctor` cannot see this for a fine-grained token (it says so); the token was fixed on GitHub and the same plan applied, once, with no write repeated.
- **A published run cannot be planned again.** `publish --plan` on a `published` run exits 8, so the "nothing is planned twice" check is a second `review` of the root, the same judgements and a new plan ([e2e.md](e2e.md) step 9).
- **Linux needs a credential helper that stores.** The helper that `gh` configures ignores `store`, so `auth login` ended with "the git credential helper did not keep the token". The run used git's in-memory `cache` helper, selected through the environment of the commands, which `doctor` reports as an unknown helper. The message does not say what to install; see the open questions.

### Windows and macOS

Not run by hand in F2: CI covers them on the recordings, on all three OSes with both Go versions. For the merge commit of M9 (`538a010`, [run 45707541556](https://github.com/Brg92/agent-code-review/actions/runs/45707541556)) every job passed: `lint`, `security`, `test` on `ubuntu-latest`, `macos-latest` and `windows-latest` with Go `stable` and `oldstable`, and the six builds. The CI result of this pull request is in its checks.

The live test of the OS keychains (Git Credential Manager on Windows, Keychain on macOS) is not part of F2. It is the open item "Before the beta" of [e2e.md](e2e.md), together with the same end-to-end commands on each OS.

## What changed from the plan, and why

- **Exit criterion on one OS (D230).** The run was done on Linux only, the developer's only machine. Windows and macOS are covered by CI on the recordings; the keychains are checked by hand before the beta.
- **S8 is never recorded (D58, D201).** `publish` writes to its pull requests on purpose, so the recorder does not list it; a test fails if it does. The review-requested search of the `queue` recording would list S8 whenever it is recorded again; the recording in `testdata/github/queue/` was made before S8 existed.
- **The real 422 does not name the comment (D231).** The body that GitHub answered to a review with a comment outside the diff, captured with `tools/review422` and kept as `testdata/githubwrite/review-422-line-outside-diff.json`, is `"errors":["Line could not be resolved"]`. The behaviour of `publish` did not change: that is the "not named" case it already handled (the review is refused as a whole, every comment is `rejected`, the findings stay in the report). A test replays the real body, and the synthetic server words its unnamed refusal like GitHub.
- **The S1 contract example had two findings fewer than the product (D200).** `TestScenarioS1` skipped the two automatic `missing-tests` findings. The example now has them, the test compares every finding, and a guard keeps their text equal to the catalog.
- **The gitleaks allowlist for a deleted commit is gone (D232).** A scan of every ref reports no leaks without it.
- **Documentation is tested against the binary.** A test compares every command line of `README.md` and `docs/` with the usage the binary prints; it found four synopses that had drifted.
- **More pull requests than the F1 estimate listed.** Hardening (#38), the judgement set (#43), the S7 recording (#36) and the rule about `gh api` (#37) were not milestones of that estimate.

## Known limits

- **Code search on the sandbox (D202).** GitHub's index does not serve the sandbox repositories: every code search returns `total_count` 0 and `incomplete_results` true. A detector reads that as "search unavailable", never as "no use", and the recordings hold no result. Record the searches again when the index serves the repositories.
- **GitHub Enterprise is out of scope (D210).** `github.com` is the only host; a link to another host is skipped as `other-platform`.
- **One comment in the 422 capture.** Whether GitHub names a comment when several are sent and only one is outside the diff was not captured.
- **`doctor` cannot check writes for a fine-grained token.** It says so; `publish` names the permission it lacks.
- **No second plan on a published run.** To repeat a plan, review again.
- **Keychains of Windows and macOS not tested live.** See above.
- **The sandbox keeps what the run wrote.** The plugin never deletes anything; `docs/e2e.md` says how to repeat the run.
- **GitLab** is prepared behind `ForgeProvider` and starts after the first stable release.

## What F3 starts from

F3 is the plugin and the release (launchers and the release branch, manifests, thin skills, agents and hooks, verification on the hosts). It starts from a complete, tested CLI whose contracts are binding and whose exit codes are documented. What it needs to decide or do first:

- **Where the packs are found at runtime.** `doctor` and `review` look next to the executable or in `ACR_PACKS_DIR`; the launcher has to put them there, or they have to be embedded. Settled in F3/M2: see [runtime-layout.md](runtime-layout.md).
- **The keychain checks before the beta** (Windows with Git Credential Manager, macOS with Keychain), then the end-to-end commands on each.
- **The message of `auth login` when the helper does not store.** On Linux it should say what to install, as it does for no helper or for `store`.
- **The `queue` recording and S8.** Decide whether the recorder should leave S8 out of the review-requested search, or the recording should simply be kept as it is.

## How to verify

```bash
gofmt -l .
```

```bash
go vet ./...
```

```bash
go test -race -coverprofile=coverage.out ./...
```

```bash
go run ./tools/covergate coverage.out
```
