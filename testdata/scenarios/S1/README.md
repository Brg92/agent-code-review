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
| `MainActivity.kt` | `runBlocking { SessionManager.instance.refresh() }` | `coroutines.run-blocking` | BLOCKER | added line |
| `data/SessionRepository.kt` | `GlobalScope.launch {` in `prefetch()` | `coroutines.global-scope` | MAJOR | added line |
| `data/SessionRepository.kt` | `sessionManager.current()!!.userName` | `kotlin.not-null-assertion` | MAJOR | added line |
| `ui/HomeViewModel.kt` | `repository.requireUser()` in `refresh()` | `logic.always-throws` | BLOCKER | **outside the hunks**: unchanged, but always throws now that `warmUp()` is never called |

`runBlocking` blocks the main thread. The outside-hunk BLOCKER cannot be posted inline; it must appear in the summary.

## Expected pre-existing defects

These already exist on `main` and are unchanged by the pull request.

| File | Code | Rule | Crash | Where |
| --- | --- | --- | --- | --- |
| `data/ProfileStore.kt` | `prefs.getString("display_name", null)!!` | `kotlin.not-null-assertion` | yes, when no name is stored | context line inside a hunk |
| `data/SessionRepository.kt` | `GlobalScope.launch {` in `warmUp()` | `coroutines.global-scope` | no | outside the hunks |

Pre-existing defects are reported apart from new findings. They change the verdict only when they are a crash.

## Expected verdict

**BLOCKED**, because of the two BLOCKERs. The pre-existing crash in `ProfileStore.kt` would block it on its own as well.
