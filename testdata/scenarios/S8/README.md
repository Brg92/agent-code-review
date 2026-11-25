# S8: the end-to-end run of publish

| | |
| --- | --- |
| Repositories | `acr-plugin-sandbox/sample-app` (root) and `acr-plugin-sandbox/core-lib` (related) |
| Branches | `feat/home-s8-session-refresh` → `main`, and `feat/session-s8-display-label` → `main` |
| Pull requests | [sample-app#9](https://github.com/acr-plugin-sandbox/sample-app/pull/9) and [core-lib#4](https://github.com/acr-plugin-sandbox/core-lib/pull/4) |
| Criteria | `.agent-code-review/` overlay of `sample-app`, and the legacy `.github/skills/core-lib-code-review/SKILL.md` of `core-lib` |

**S8 is never recorded.** Its pull requests collect the comments, the review and the issues that `publish --apply` writes, on purpose, and publishing is the only thing they are for. Recording them would change what the recorder reads and break the recordings of the other scenarios, so the recorder does not list S8 and a test fails if it does (see [docs/sandbox.md](../../../docs/sandbox.md)). There is no `testdata/github/S8`.

Both pull requests were opened by the author account, with a review requested from the reviewer, and are never merged. `docs/e2e.md` runs `review`, `publish --plan both` and `publish --apply` on them.

## The change

`sample-app#9` restores the session when the app starts. Paths are under `app/src/main/java/com/example/sample/`.

| File | Change |
| --- | --- |
| `MainActivity.kt` | `onCreate` calls `runBlocking { SessionRepository().refreshNow() }` |
| `data/SessionRepository.kt` | adds `suspend fun refreshNow()`, which ends with `cachedUser!!` |

Its description lists `core-lib#4` in `## Related Changes`, unticked. `core-lib#4` adds `sessionLabel(session)` and its test in `com.example.core.session`; it has no finding.

## Expected result

| Change request | Finding | Severity | What `publish` writes |
| --- | --- | --- | --- |
| `sample-app#9` | `run-blocking-prod` on the `runBlocking` line of `MainActivity.kt` (change request) | BLOCKER | one inline comment, in one review with the event `COMMENT` |
| `sample-app#9` | `no-double-bang` on `cachedUser!!` (change request, confirmed as MAJOR) | MAJOR | in the MAJOR issue |
| `sample-app#9` | `missing-tests` on `SessionRepository.kt` (automatic, change request) | MAJOR | in the MAJOR issue |
| `sample-app#9` | `global-scope` on `warmUp()` (existing code, not a crash) | BLOCKER | the BLOCKER issue, which is for existing code only |
| `core-lib#4` | none | | the note `Review completed: no blocking issues found!`, and the tick of its entry in the description of `sample-app#9` |

Verdict of `sample-app#9`: **BLOCKED**. Verdict of `core-lib#4`: **OK**, which is why the note and the tick are exercised. The hand-written judgements are in `judgements/`; they hold the head commit of each pull request, so a new head needs new judgements.
