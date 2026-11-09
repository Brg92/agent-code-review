# Phase F1 summary

Phase F1 builds the offline core of `agent-code-review` on the F0 contracts: the neutral model and the run state machine, a table-driven lexer, the rule engine, the `android` and `kotlin-multiplatform` packs, Related Changes parsing and the cascade, findings, verdicts and fingerprints, the report, the changed-symbol index, the work dir, the user configuration and the repository criteria. None of it talks to a platform: every input comes from interfaces that tests fill with synthetic data or with the recordings in `testdata/github/`. F1 ends with a blocking per-package coverage gate.

## What was delivered

| Milestone | Pull request | Result |
| --- | --- | --- |
| — | [#13](https://github.com/Brg92/agent-code-review/pull/13), [#14](https://github.com/Brg92/agent-code-review/pull/14), [#15](https://github.com/Brg92/agent-code-review/pull/15) | Remote-action rules for the agent, the fine-grained recorder token, no `Refs:` footer in commits |
| M2 | [#16](https://github.com/Brg92/agent-code-review/pull/16), [#17](https://github.com/Brg92/agent-code-review/pull/17) | Cascade limits, `BLOCKED`/`OK` verdicts, `unknown` CI state; pre-existing crashes |
| M3 | [#18](https://github.com/Brg92/agent-code-review/pull/18) | Neutral model (`internal/core`) and the run state machine (`internal/state`) |
| M4 | [#19](https://github.com/Brg92/agent-code-review/pull/19) | Table-driven lexer with the Kotlin and Java tables |
| M5 | [#20](https://github.com/Brg92/agent-code-review/pull/20) | Rule engine, pack loading and profile resolution |
| — | [#21](https://github.com/Brg92/agent-code-review/pull/21) | CI failures are fixed without weakening checks (`AGENTS.md` §5) |
| M6 | [#23](https://github.com/Brg92/agent-code-review/pull/23) | `android` pack, dimensions, `group` and `when` on rules |
| M7 | [#24](https://github.com/Brg92/agent-code-review/pull/24) | `kotlin-multiplatform` pack, per-file `platform`, pack priority |
| M8 | [#25](https://github.com/Brg92/agent-code-review/pull/25) | Related Changes parsing and cascade resolution |
| M9a | [#26](https://github.com/Brg92/agent-code-review/pull/26) | Diff map, anchors, judgement validation, findings, verdicts, fingerprints |
| M9b | [#27](https://github.com/Brg92/agent-code-review/pull/27) | Report, cascade summary and final message |
| M9c | [#28](https://github.com/Brg92/agent-code-review/pull/28) | Change-request checks: missing tests, source-set and variant asymmetry |
| M10 | [#29](https://github.com/Brg92/agent-code-review/pull/29) | Changed-symbol index and candidates across the cascade |
| M11 | [#30](https://github.com/Brg92/agent-code-review/pull/30) | Work dir, user config and repository criteria |
| M12 | this pull request | Per-package coverage gate and this summary |

## What exists

### Packages

| Package | Purpose |
| --- | --- |
| `internal/cascade` | Walks Related Changes depth first from the root (`Resolve`), gives discovery and review order, INCOMPLETE causes and the cascade verdict; ticks a Related Changes checkbox byte for byte (`Tick`) |
| `internal/cli` | The command line; today only `version` and `--version` |
| `internal/core` | Neutral model: change request keys, forges, cascade entries and limits, diffs and anchors, judgement validation, findings, verdicts, fingerprints, the findings document and the repository criteria |
| `internal/forges/github` | GitHub references to pull requests in descriptions (a `refs.Registry`); no I/O |
| `internal/forges/gitlab` | GitLab references to merge requests in descriptions, prepared for after the first stable release; no I/O |
| `internal/lexer` | Splits source into tokens from a language table loaded at runtime (`LoadTable`, `Tokenize`, `Clean`); never fails |
| `internal/packs` | Loads packs, activates them on a repository, assigns each changed file a pack and a language, resolves profile and dimensions |
| `internal/refs` | Finds and classifies the lines of the Related Changes section and turns references into keys through a per-platform `Registry` (`Related`, `Compose`) |
| `internal/render` | Report of a change request, cascade summary and final chat message, rendered only from findings documents |
| `internal/rules` | Loads rules catalogs and runs their checks on one file (`Run`) or on a whole change request (`RunChangeRequest`); builds checklists |
| `internal/state` | Run state and its phases, `state.json`, the work dir, cleanup of old runs, the user configuration, saved reports |
| `internal/symbols` | Declarations of a file from its tokens (`Extract`), the public API a change request adds, removes or changes (`IndexOf`), uses of a name (`Uses`) |
| `internal/version` | The build version, `0.0.0-dev` unless injected |
| `internal/guards` | Test-only structural guards (stdlib only, no shell, no write methods on the forge interface, allowlist, clean testdata, contracts) |
| `internal/testutil/jsonschema` | Test-only validator for the JSON Schema subset the contracts use |
| `internal/testutil/sandbox` | Test-only reader of the recorded GitHub responses |
| `internal/testutil/scenario` | Test-only builder of finalize inputs from recordings and hand-written judgements |

`internal/forges/base` (the `ForgeProvider` interface) does not exist yet; its guard is ready for it.

### Schemas

Eleven JSON Schemas, all `schema: 1`, each with valid and invalid examples in `testdata/contracts/`: `config`, `findings`, `judgement`, `lexer-table` (new in F1), `pack-manifest`, `plan`, `repo-criteria`, `rules-catalog`, `state`, `user-config` (new in F1) and `workpackage`. All of them ship: `schemas/**` is in `docs/release-allowlist.txt`. Go loaders reject unknown fields and agree with the examples, so no schema validator runs in production.

### Packs

**`android`**

- 119 rules: 18 with `match`, 101 without (judged only, listed in the checklist). Of the 18, 15 are token patterns, one uses the `line-length` detector (`line-too-long`, whose `max` repositories may set from 60 to 200), one `missing-doc` and one the change-request detector `missing-test`. Two matched rules need judgement: `no-double-bang` and `missing-kdoc`.
- Severities: 21 BLOCKER, 60 MAJOR, 38 MINOR. 55 rules carry a `when` condition: 22 apply only to `platform: [android]`, 31 only to some UI toolkits (13 of them also android-only) and 15 only to the `library` profile.
- Languages: lexer tables for Kotlin, Java, Groovy, TOML and properties; 110 rules apply to Kotlin, 65 to Java.
- Dimension `ui-toolkit`: `views`, `compose`, `both` (default and combined value `both`), detected from build files and the version catalog. File dimension `platform`: every file is `android`.
- Profiles `app` (default) and `library`; app and library criteria templates.

**`kotlin-multiplatform`**

- 6 rules, all Kotlin: `kmp-jvm-api-in-common` (pattern, MAJOR), `kmp-missing-throws` and `kmp-swift-unfriendly-api` (judged only), `kmp-expect-without-actual` (BLOCKER, `expect-actual`, needs judgement), `kmp-source-set-asymmetry` (MAJOR, `counterpart`, needs judgement), `kmp-missing-tests` (MAJOR, `missing-test`). The first three only apply to `common` files.
- File dimension `platform`: `common`, `android`, `ios`, mapped from source sets (`commonMain`/`commonTest`, `androidMain`/`androidUnitTest`, `ios*Main`/`ios*Test`), default `common`.
- Priority 10, so it comes before `android` in a repository where both are active. Its Kotlin lexer table and symbols are identical to the android ones, which a test enforces.
- Profiles `library` (default) and `app`.

**Core catalog** (embedded): `logic-defect`, `uses-removed-api`, `missing-required-argument` (BLOCKER, judged), `variant-asymmetry` (MAJOR, `counterpart` on `config.variants`), `dependency-mr-undeclared` and `contract-misuse` (MAJOR, judgement only).

### Change-request detectors

Besides token patterns and the file-level `line-length` and `missing-doc` detectors, four detectors see the whole change request. Every name, suffix and path comes from the catalog:

| Detector | Signal | Reads |
| --- | --- | --- |
| `missing-test` | An added or modified source with a matching suffix has no test in the change request or under a test root of its module | `Tree` |
| `counterpart` | A file changed under one source set or variant whose counterpart under another group exists and is not changed | `Tree` |
| `symbol-usage` | Public API removed, or given a new required parameter, while a change request above still uses it | `Search` on each ancestor's target branch |
| `expect-actual` | An `expect` added or changed in `commonMain` without a matching `actual` on a platform that has files | changed files, `Tree`, `Search` |

A failed or missing `Tree` or `Search` never produces a signal: the file or declaration is reported as not verified (`tree-unavailable`, `search-unavailable`).

### Scenarios

| Scenario | What the offline tests check with it |
| --- | --- |
| S1 | Coroutine and nullability findings, a pre-existing crash that alone makes the change request `BLOCKED`, the neutral criteria layout, anchors on recorded patches, the golden report |
| S2 | The cascade from the recorded descriptions (five reviewed, a cycle back to the root, a tracking issue, a missing number, a ticked entry), low limits, `uses-removed-api` on core-lib below sample-app, cascade summary and final message |
| S3 | Added, removed, modified, renamed (with and without a patch) and binary files, line mapping across hunks, multi-line suggestions, the legacy criteria layout |
| S4 | A generated file without a patch (`patch-unavailable`), noise globs; reached at depth 3 of S2 |
| S5 | CI failing and pending on the head (A), no checks (B) |
| S6 | Criteria edited by the change request are not applied (A), an invalid overlay stops the review with every field named (B), legacy criteria (C), no criteria with Swift files not verified and the KMP default profile (D) |

The `queue` recording is not used yet.

### CI and the coverage gate

CI is unchanged except for the coverage gate ([ci.md](ci.md)): `tools/covergate` reads the profile of `go test -race -coverprofile` in `test (ubuntu-latest, go stable)` and fails the job if a package under `internal/` is below 90% statement coverage. `internal/testutil/...` and `internal/guards` are excluded. The gate truncates to one decimal, so it can show 0.1 less than `go test`.

| Package | Statements | Covered | Coverage |
| --- | ---: | ---: | ---: |
| `internal/cascade` | 110 | 105 | 95.4% |
| `internal/cli` | 5 | 5 | 100.0% |
| `internal/core` | 952 | 927 | 97.3% |
| `internal/forges/github` | 31 | 30 | 96.7% |
| `internal/forges/gitlab` | 32 | 32 | 100.0% |
| `internal/lexer` | 317 | 314 | 99.0% |
| `internal/packs` | 417 | 401 | 96.1% |
| `internal/refs` | 72 | 70 | 97.2% |
| `internal/render` | 287 | 281 | 97.9% |
| `internal/rules` | 1197 | 1149 | 95.9% |
| `internal/state` | 367 | 341 | 92.9% |
| `internal/symbols` | 411 | 400 | 97.3% |
| `internal/version` | 1 | 1 | 100.0% |

`internal/cli` was at 0% because only the binary test in `cmd/agent-code-review` ran it; it now has in-process tests. The total over the module is 95.7%.

## How a review flows through the core today

Each step exists and is tested on its own or across a few steps; nothing yet runs them in sequence for a real change request.

1. **Cascade.** `refs.Related` finds the Related Changes section of a description and classifies its lines (followed, not followed, unparsed); the `forges/github` and `forges/gitlab` registries turn references into `core.Key`s, and `refs.Compose` adds registries for links to other platforms. `cascade.Resolve` walks a `cascade.Source` depth first within `core.Limits` and returns a `cascade.Result`: entries in discovery order, the review order (related first, root last), skipped entries with their reason, INCOMPLETE causes and the lines not followed. `state.New` records the run in phase `resolved`; `state.Save` and `state.Load` keep `state.json`.
2. **Criteria.** `core.ReadCriteria` reads each repository through `core.Files` at the **target branch** of its change request: neutral `.agent-code-review/` or legacy `.github/skills/` plus `.github/mr-review.json`. It returns `core.RepoCriteria` (profile, packs, variants, dimensions, disabled rules, rule options, repository rules, noise, public API, consumers, shared libraries, cascade limits, notes). An invalid configuration returns a `core.CriteriaError`; ignored items and criteria edited by the change request become notes.
3. **Packs and profile.** `packs.Load` loads the built-in packs and the user's `packPaths` (from `state.LoadUserConfig`) with the core catalog. `Set.Activate` activates packs by their `detect` signs through a `packs.Reader`; `packs.ResolveProfile` (config, then cascade dependency, then pack signs, then pack default, with the reason) and `packs.ResolveDimensions` settle the context. `Set.ActiveRules`, `Rule.WithOptions`, `rules.Applicable` and `rules.Checklist` give the rules and the grouped checklist.
4. **File signals.** `core.NewFileDiff` parses each patch. `Set.Assign` gives a file its pack, language and file dimensions, or marks it as noise or not verified. `Assignment.File` and `Assignment.Context` build the input of `rules.Run`, which tokenizes with `lexer.Tokenize`, matches on code tokens within a step budget and returns signals (origin `mr` on added lines, `existing` elsewhere) and not-verified files.
5. **Change-request signals.** `ChangedFile.RulesFile` builds the input of `rules.RunChangeRequest`, which runs the four detectors with a `rules.Tree`, a `rules.Search` and the ancestors from the cascade, using `symbols.Extract`, `symbols.IndexOf` and `symbols.Uses`. `core.NumberSignals` merges file and change-request signals and numbers them `s1`, `s2`, …
6. **Work package.** The contract is `schemas/workpackage.schema.json`; `state.CreateChangeRequestDir` makes the directory that holds `workpackage.json` (`state.WorkPackageFile`) and the judgement. No code builds the work package yet.
7. **Judgement validation.** `core.DecodeJudgement` reads the agent's judgement; `core.ValidateJudgement` checks it against the `core.Review` (head, files, rules, signals) and reports every error: the change request and head it judges, rules, files, sides and lines, catalog severities, signal decisions and the limits on notes.
8. **Findings.** `core.Finalize` turns a valid judgement into a `core.Result`: findings with anchors (`FileDiff.Anchor`, relocated inline when outside the hunks), title, why and fix from the rule, fingerprints (`core.FindingFingerprint`), suggestions that fit (`FileDiff.SuggestionFits`), pre-existing defects and dismissed signals.
9. **Verdicts.** `core.ChangeRequestVerdict` gives `BLOCKED` or `OK` per change request; `cascade.Result.Verdict` gives the overall `BLOCKED`, `INCOMPLETE` (with causes) or `OK`.
10. **Report and cascade summary.** `Result.Document` produces the findings document (`core.Document`), which `render.Report` turns into the report. `render.Cascade` and `render.Final` write the cascade summary and the final chat message. `state.ReportName`, `state.RootReport` and `state.SaveReport` save reports outside any git work tree; `state.CleanRuns` removes old runs. `State.Advance` moves the run through `collected`, `judged`, `finalized`, `planned` and `published`.

### Missing to run it end to end (F2)

- A GitHub provider behind `ForgeProvider` that implements `cascade.Source`, `core.Files`, `packs.Reader`, `rules.Tree` and `rules.Search`, plus pull request metadata, files, commits, checks and review comments.
- Composition of the effective rule set from `RepoCriteria`: pack and core rules, minus `disabledRules`, with `ruleOptions` applied through `Rule.WithOptions`, plus repository rules. Today `ReadCriteria` returns these values and nothing applies them.
- A builder of the work package from the review (files, checklist, signals, criteria) and the `review`, `next`, `finalize`, `show`, `publish`, `queue`, `auth`, `doctor` and `check-criteria` commands.
- The plan (`schemas/plan.schema.json`), secret scanning of every outgoing write, the head check before `publish --apply`, and tokens from the OS keychain via `git credential`.

## Contracts changed in F1

Merged with `!`:

- [#16](https://github.com/Brg92/agent-code-review/pull/16) `feat(schemas)!`: `findings.verdict` is only `BLOCKED` or `OK`; `cascadeLimit` becomes `cascade.maxDepth` and `cascade.maxChangeRequests`; the `over-limit` skip reason becomes `over-depth-limit` and `over-count-limit`.
- [#26](https://github.com/Brg92/agent-code-review/pull/26) `feat(core)!`: finding `message` becomes the required `title`, `why` and `fix`; inline reported findings need an anchor; finding ids and plan `findingId`s are `acr:v1:<rule>:<path>:<h12>` fingerprints.
- [#27](https://github.com/Brg92/agent-code-review/pull/27) `feat(render)!`: matched rules without `needsJudgement` need `why` and `fix`; findings documents require `title`, `baseRef`, `files`, `agentNotVerified`, `signalsDismissed`, `questions`, `notes` and the criteria's `profileReason` and `dimensions`; every pre-existing finding is placed in the summary.
- [#30](https://github.com/Brg92/agent-code-review/pull/30) `feat(core)!`: `repo-criteria` documents require `dimensions`, `ruleOptions` and `rules`.

Three PRs merged without `!` also broke a schema, as their descriptions say: [#20](https://github.com/Brg92/agent-code-review/pull/20) (file extensions only in the pack manifest), [#23](https://github.com/Brg92/agent-code-review/pull/23) (manifest `checklists` removed) and [#24](https://github.com/Brg92/agent-code-review/pull/24) (profile `kmp` removed). No release exists, so nothing outside the repository depends on the old forms.

## Open questions for F2

- **Provider reads lead to Not verified, never to false findings.** The GitHub provider must implement `Source`, `Files`, `Tree` and `Search`. Every failure (permission, rate limit, truncated tree, network) must surface as a not-verified reason, never as an empty result that a detector reads as "no test" or "no use". GitHub code search only indexes the default branch, so a `Search` on any other target branch must report `search-unavailable` rather than return nothing.
- **Reference registries.** `review` must build the registry with `refs.Compose`: the platform of the root first, then the others, so links to another platform are recognised and skipped as `other-platform`.
- **Recordings.** The recordings have no repository trees and no code searches. Record trees (and searches where the provider uses them) and update the scenarios so `missing-tests`, `kmp-missing-tests`, `kmp-source-set-asymmetry` and `variant-asymmetry` run from recordings, not only from synthetic trees. The S5 A recording still shows `instrumented` in progress.
- **Built-in packs on disk.** The CLI must find `packs/` at runtime. Options: next to the binary in the release layout, located by the launcher; or embedded with `embed` and written out on demand. A pack loaded from disk can be inspected and overridden by `packPaths`; an embedded one cannot drift from the binary.
- **Judgement-only rules.** `contract-misuse` and `dependency-mr-undeclared` stay without a detector. `dependency-mr-undeclared` gets its input from `sharedLibraries`, but only the agent decides.
- **Effective rule set and work package** (see above): neither is built yet, and both are needed before `review` can produce its first work package.
- **Exit codes** ([exit-codes.md](exit-codes.md)) are still a proposal; they become binding with the first F2 command.
- **Commit scope for dev tools.** `tools` is not in the scope list of `AGENTS.md` §4, though this PR's first commit uses it; earlier tools used `recorder`. Either add `tools` to the list or keep one scope per tool.
- **Gate scope.** `cmd/` and `tools/` are outside the gate: `cmd/agent-code-review` is tested through a built binary and shows 0% in-process, and `tools/recorder` is at 78.2%. Keep the gate on `internal/` only, since `cmd/` will stay a one-line `main`.

## Re-estimate of F2 to F4

Sizes: S is up to about a week of milestones of the F1 kind, M two to three, L more.

### F2: GitHub provider and commands (L)

| Milestone | Size | Risks |
| --- | --- | --- |
| `forges/base`: `ForgeProvider` and its capabilities, with the existing guard | S | Capabilities shaped only by GitHub, then wrong for GitLab |
| Transport and auth: HTTP client with retries, rate limits and ETags; token from `git credential` or the environment; the `auth` command | M | Credential helpers differ per OS; a token must never reach a log or an error |
| GitHub provider, read side: change request, files, commits, checks, comments, contents, `Source`, `Files`, `Tree`, `Search` | L | Code search limits and its default-branch index; truncated trees on large repositories; pagination |
| Recorder and scenarios: record trees and searches, refresh S2 and S5, scenarios for missing tests and asymmetry | M | New sandbox scenario PRs; recordings age; check runs from apps other than Actions |
| `review` and `next`: effective rule set, built-in packs, registries, work package, state up to `collected` | L | The largest integration step; work package size for large changes |
| `finalize` and `show` | S | Re-validating against a head that moved |
| `publish --plan` and `--apply`: plan, head check, secret scan, comments, issues, Related ticks, idempotency by fingerprint | L | The product invariants live here; secret-scan false positives; partial failures mid-apply |
| `queue`, `doctor`, `check-criteria`; exit codes made binding | M | `doctor` checks differ per host and OS |

### F3: plugin and release (M)

| Milestone | Size | Risks |
| --- | --- | --- |
| Launchers and the release branch: per-OS binaries, the allowlist enforced on `release`, versions injected at build | M | macOS Gatekeeper and Windows SmartScreen on unsigned binaries |
| Manifests (Agent Plugins 1.0, `.claude-plugin/`), thin skills, agents and hooks | M | Host formats still moving; keeping the skills thin |
| Verification on Claude Code, Codex, Cursor, Kiro and Copilot in Android Studio | M | Kiro and Copilot plugin support and shell access may differ from the others |

### F4: stabilisation and 1.0.0 (M)

| Milestone | Size | Risks |
| --- | --- | --- |
| Use on the sandbox and on real public repositories; false-positive review of every matched rule | M | Rule tuning can cascade into catalog changes |
| Performance and limits: lexer and rule budgets on large changes, long cascades | S | Budgets too tight give not-verified files; too loose give slow reviews |
| Contract freeze for `schema: 1`, docs, release notes, `1.0.0` | S | Late breaking changes found during use |

GitLab starts after 1.0.0, behind the same `ForgeProvider`.

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
