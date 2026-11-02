# S6: where review criteria come from

Review criteria are always read from the **target branch**, never from the branch under review.

| Case | Repository | Branch → base | Pull request |
| --- | --- | --- | --- |
| A | `sample-app` | `chore/criteria-s6-relax-overlay` → `main` | [sample-app#5](https://github.com/acr-plugin-sandbox/sample-app/pull/5) |
| B | `sample-app` | `feat/home-s6-invalid-criteria` → `chore/criteria-s6-invalid-overlay-base` | [sample-app#6](https://github.com/acr-plugin-sandbox/sample-app/pull/6) |
| C | `core-lib` | `feat/session-s6-legacy-criteria` → `main` | [core-lib#2](https://github.com/acr-plugin-sandbox/core-lib/pull/2) |
| D | `kmp-shared` | `feat/shared-s6-ios-greeting` → `main` | [kmp-shared#1](https://github.com/acr-plugin-sandbox/kmp-shared/pull/1) |

## A: valid overlay, changed only in the pull request

`sample-app` `main` has a valid overlay:
- `.agent-code-review/config.json`: `schema` 1, profile `app`, pack `android`, `variants` `["free", "paid"]`;
- `.agent-code-review/criteria.md`.

The pull request adds `"disabledRules": ["kotlin.not-null-assertion"]` to its own copy of `config.json`, and adds `ProfileStore.initials()` with `prefs.getString("display_name", null)!!`.

Expected:
- **Overlay:** the change is ignored, since criteria come from `main`.
- **Finding:** `kotlin.not-null-assertion` (MAJOR) on the added line.
- **Summary:** notes that the overlay changed in the pull request and was not applied.
- **Variants:** `free` and `paid` come from `main`'s `config.json`.

The review does not fail because of the edited overlay: a change request may legitimately change its repository's criteria, which apply once it is merged.

## B: invalid overlay on the target

The base branch `chore/criteria-s6-invalid-overlay-base` has a `config.json` with:
- `"schema": 99`;
- `"profile": "spaceship"`;
- `"variants": "free"`, a string instead of an array.

Expected: the review stops before judging, with a criteria validation error: exit code 3. The error names the file and each invalid field. There is no silent fallback to the defaults.

## C: legacy criteria

`core-lib` `main` has no `.agent-code-review/`. It has instead:
- `.github/skills/core-lib-code-review/SKILL.md`;
- `.github/mr-review.json`, with profile `library`, a `publicApi` glob and `consumers`.

Expected: the legacy files are loaded and used as the criteria, the profile is `library`, and the summary says legacy criteria are in use. The change adds `SessionManager.isSignedIn()`, so there are no findings.

## D: no criteria and Swift files

`kmp-shared` `main` has no criteria files at all.

Expected:
- **Profile:** KMP, detected from `commonMain`/`androidMain`/`iosMain`, with the default criteria.
- **`Greeting.kt` (common):** reviewed.
- **`iosApp/iosApp/ContentView.swift`:** listed as **Not verified**, because no pack covers Swift.
- **Findings:** none.
