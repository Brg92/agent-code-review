# Sandbox

The sandbox is a set of synthetic GitHub repositories with scenario pull requests that exercise the review flow end to end. The recorder (`tools/recorder`) captures read-only API responses from them into `testdata/github/<scenario>/`, which the tests then replay offline.

Everything in the sandbox is original and synthetic: package names are `com.example.*`, commits use the identity `ACR Sandbox Author <author@example.com>`, and no code or text comes from a real project.

## Organization and accounts

| Name | Role |
| --- | --- |
| `acr-plugin-sandbox` | GitHub organization that owns the sandbox repositories |
| *reviewer* | The maintainer's account, organization owner. It creates the repositories, receives review requests, and runs the recorder and the plugin. |
| *author* | A second account with write access to the sandbox repositories. It pushes the branches and opens every scenario pull request, so they are not authored by the reviewer. |

Scenario pull requests are never merged, and their branches are never deleted.

## Repositories

| Repository | Content | Scenarios |
| --- | --- | --- |
| `sample-app` | Android app with Compose and the `free`/`paid` flavors; consumes `core-lib` | S1, S2, S5, S6 |
| `core-lib` | Android library with Views and `maven-publish`; public API `SessionManager` | S2, S3, S6 |
| `kmp-shared` | Kotlin Multiplatform library (`commonMain`, `androidMain`, `iosMain`, `expect`/`actual`) and a SwiftUI app in `iosApp/` | S2, S6 |
| `feature-lib` | Android library for edge cases | S2, S4 |

Each repository has a `checks` workflow made of fast shell steps:

- **`lint`**: fails on trailing whitespace.
- **`test`**: checks that sources exist.

S5 adds a long-running job on its own branch.

The repositories are built locally in `../acr-plugin-sandbox/` next to this checkout. `../acr-plugin-sandbox/PR_PLAN.md` describes how to publish them and in which order to open the pull requests. The scenarios themselves are described in `testdata/scenarios/S1` to `S6`.

## Tokens

Use fine-grained personal access tokens, limited to the organization and the four repositories, with a short expiration (30 days or less).

### Reviewer: recorder token (`ACR_GITHUB_TOKEN`)

Read-only. Settings:

- **Resource owner:** `acr-plugin-sandbox`. The organization must allow fine-grained tokens.
- **Repository access:** only select repositories, the four sandbox repositories.
- **Repository permissions, all read-only:**
  - Metadata (mandatory)
  - Contents
  - Pull requests
  - Issues
  - Commit statuses
  - Checks, if the form offers it. Otherwise confirm, the first time you record, that check runs can still be read.
- **No write permission and no account permissions.** The review-requested search runs as the token's user.

Set `ACR_SANDBOX_LOGINS` to the comma-separated logins of the two accounts when you run the tests. The test that keeps real logins out of `testdata/` skips itself when the variable is unset.

### Author: publishing token

Only needed when the author account does not use `gh auth login`. Settings:

- **Resource owner:** `acr-plugin-sandbox`.
- **Repository access:** the four sandbox repositories.
- **Repository permissions:**
  - Contents: read and write
  - Pull requests: read and write
  - Issues: read and write
  - Workflows: read and write, which is required to push `.github/workflows/checks.yml`

Later phases that write review comments need their own reviewer token with Pull requests and Issues set to read and write. Never reuse the recorder token for that.

Tokens never go on disk, in logs or in `testdata/`. The recorder reads `ACR_GITHUB_TOKEN` from the environment, and the sanitizer removes logins, ids, emails and avatar URLs from the recorded data.
