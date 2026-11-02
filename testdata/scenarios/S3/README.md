# S3: file change kinds and diff positions

| | |
| --- | --- |
| Repository | `acr-plugin-sandbox/core-lib` |
| Branch | `refactor/session-s3-reorganize-files` → `main` |
| Pull request | [core-lib#1](https://github.com/acr-plugin-sandbox/core-lib/pull/1) |
| Criteria | legacy `.github/skills/core-lib-code-review/SKILL.md` on `main` (profile `library`) |

All paths below are under `src/main/`.

## File changes

| Status | Path | Notes |
| --- | --- | --- |
| added | `java/com/example/core/session/SessionExpiry.kt` | new file |
| removed | `java/com/example/core/session/LegacySessionStore.kt` | deleted file |
| renamed, 100% | `java/com/example/core/ui/StatusBadgeView.kt` → `java/com/example/core/ui/badge/StatusBadgeView.kt` | no patch, nothing to review |
| renamed, 75% | `java/com/example/core/session/TokenCodec.kt` → `java/com/example/core/session/codec/TokenCodec.kt` | package changed, one line removed |
| added, binary | `res/drawable-nodpi/badge_background.png` | 1×1 PNG, no patch |
| modified | `java/com/example/core/session/SessionManager.kt` | hunks `@@ -1,5 +1,6 @@`, `@@ -11,6 +12,8 @@`, `@@ -30,6 +33,7 @@` |

`SessionManager.kt` checks line mapping: from the second hunk on, the context lines have different old and new line numbers.

## Expected findings

| File | Code | Rule | Severity | Where |
| --- | --- | --- | --- | --- |
| `session/codec/TokenCodec.kt` | removed `require(token.isNotBlank()) { "token must not be blank" }` | `logic.removed-validation` | MAJOR | **removed line**: left side, old line 7 |
| `session/SessionExpiry.kt` | `if (...) { return true } else { return false }`, 5 lines | `style.redundant-if` | MINOR | added lines, with a **multi-line suggestion** |

Expected multi-line suggestion, replacing the five lines of `isExpired`'s body:

```kotlin
        return session.expiresAtMillis <= nowMillis
```

The binary file and the 100% rename produce no findings. The deleted file produces no findings either, since it is internal and deprecated.

## Expected verdict

**CHANGES REQUESTED**: one MAJOR, no BLOCKER.
