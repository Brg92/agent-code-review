# S1: coroutine and nullability defects

| | |
| --- | --- |
| Repository | `acr-plugin-sandbox/sample-app` |
| Branch | `feat/home-s1-session-prefetch` → `main` |
| Pull request | [sample-app#2](https://github.com/acr-plugin-sandbox/sample-app/pull/2) |
| Criteria | `.agent-code-review/` overlay on `main` (profile `app`) |

The change refreshes the session in `MainActivity.onCreate`, adds `SessionRepository.prefetch()` and `ProfileStore.hasDisplayName()`, and removes the `warmUp()` call from `HomeViewModel`.

All paths below are under `app/src/main/java/com/example/sample/`.

## Expected findings

Rule ids are provisional until the rules catalog exists.

| File | Code | Rule | Severity | Where |
| --- | --- | --- | --- | --- |
| `MainActivity.kt` | `runBlocking { SessionManager.instance.refresh() }` | `run-blocking-prod` | BLOCKER | added line |
| `data/SessionRepository.kt` | `GlobalScope.launch {` in `prefetch()` | `global-scope` | BLOCKER | added line |
| `data/SessionRepository.kt` | `sessionManager.current()!!.userName` | `no-double-bang` | MAJOR | added line |
| `ui/HomeViewModel.kt` | `repository.requireUser()` in `refresh()` | `logic.always-throws` | BLOCKER | **outside the hunks**: unchanged, but always throws now that `warmUp()` is never called |

`runBlocking` blocks the main thread. The outside-hunk BLOCKER is relocated to line 16, the nearest line of the diff of `ui/HomeViewModel.kt`, and its text starts with "Refers to line 27 of `app/src/main/java/com/example/sample/ui/HomeViewModel.kt`, outside the changed lines."

## Expected pre-existing defects

These already exist on `main` and are unchanged by the pull request.

| File | Code | Rule | Severity | Crash | Where |
| --- | --- | --- | --- | --- | --- |
| `data/ProfileStore.kt` | `prefs.getString("display_name", null)!!` | `no-double-bang` | BLOCKER | yes, when no name is stored | context line inside a hunk |
| `data/SessionRepository.kt` | `GlobalScope.launch {` in `warmUp()` | `global-scope` | BLOCKER | no | outside the hunks, relocated to line 15 |

Pre-existing defects are reported apart from new findings. They change the verdict only when they are a crash, and a crash is always a BLOCKER. A finding keeps the severity of its rule in the catalog, unless the rule needs judgement: `global-scope` is a BLOCKER both in `prefetch()` and in `warmUp()`. The pre-existing one does not block, because it is not a crash. `no-double-bang` needs judgement; the crash makes it a BLOCKER.

## Expected verdict

**BLOCKED**, because of the three BLOCKERs. The pre-existing crash in `ProfileStore.kt` would block it on its own as well.
