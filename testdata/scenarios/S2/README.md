# S2: Related Changes cascade

| | |
| --- | --- |
| Root | `acr-plugin-sandbox/sample-app` `feat/home-s2-session-expiry` → `main`, [sample-app#7](https://github.com/acr-plugin-sandbox/sample-app/pull/7) |
| Depth 1 | `acr-plugin-sandbox/core-lib` `refactor/session-s2-remove-legacy-token` → `main`, [core-lib#3](https://github.com/acr-plugin-sandbox/core-lib/pull/3) |
| Depth 2 | `acr-plugin-sandbox/kmp-shared` `feat/shared-s2-session-countdown` → `main`, [kmp-shared#2](https://github.com/acr-plugin-sandbox/kmp-shared/pull/2) |
| Ticked | `acr-plugin-sandbox/feature-lib` `feat/toggle-s2-remote-defaults` → `main`, [feature-lib#2](https://github.com/acr-plugin-sandbox/feature-lib/pull/2) |

Each change request in the cascade is reviewed with the criteria from its own repository's target branch. The exact descriptions are in `../acr-plugin-sandbox/PR_PLAN.md`.

## `## Related Changes` entries in sample-app S2

| Entry | Case | Expected |
| --- | --- | --- |
| `- [ ] https://github.com/acr-plugin-sandbox/core-lib/pull/N` | full URL | reviewed |
| `- [x] acr-plugin-sandbox/feature-lib#N` | `owner/repo#N`, ticked | not reviewed again, listed as already done |
| `- [ ] #N` (S1) | `#N`, same repository | reviewed |
| `- [ ] #N` (tracking issue) | `#N` that is an issue | skipped, reported as not a change request |
| `- https://github.com/acr-plugin-sandbox/kmp-shared/pull/N` (S6) | line without a checkbox | ignored |
| `- [ ] #N` (S2 itself) | self-reference | ignored, never reviewed twice |
| `- [ ] https://gitlab.com/...` | other platform | skipped, because one cascade uses one platform |
| `- [ ] acr-plugin-sandbox/core-lib#9999` | non-existent pull request | reported as not found; the cascade goes on |

## Other cases

- **Heading variant:** core-lib S2 uses `## Related PRs`, which is accepted as a variant.
- **Cycle:** core-lib S2 links back to sample-app S2. The cycle is detected and the root is not reviewed again.
- **Over the cascade limit:** kmp-shared S2 links `feature-lib#N` (S4) at depth 3. With a cascade limit of 2 it is listed as not reviewed, over the limit.
  - **Open question:** the default limit is not defined yet. Tests for this scenario set the limit to 2.

## Expected findings

| Change request | File | Code | Rule | Severity |
| --- | --- | --- | --- | --- |
| core-lib S2 | `src/main/java/com/example/core/session/SessionManager.kt` | removed `fun legacyToken(): String?` | `uses-removed-api` | BLOCKER |

`sample-app` `main` still calls `sessionManager.legacyToken()` in `SessionRepository.authHeader()`, and sample-app S2 does not remove that call. The finding sits on the removed line. It points to the consumer in the other repository, and core-lib's legacy criteria (S6) also call it a BLOCKER.

sample-app S2 uses `SessionManager.expiresAt()`. That method is added by core-lib S2, which is in the same cascade, so it is not a finding.

S1 is reviewed at depth 1 through its `#N` entry, so its findings (see `../S1/README.md`) are part of the cascade too.

## Expected verdict

- **Cascade:** BLOCKED, because of core-lib S2 and S1.
- **sample-app S2 and kmp-shared S2:** no findings.
