# Sandbox

The sandbox is a set of synthetic GitHub repositories with scenario pull requests that exercise the review flow end to end. The recorder (`tools/recorder`) captures read-only API responses from them into `testdata/github/<scenario>/`, which the tests then replay offline.

Everything in the sandbox is original and synthetic: package names are `com.example.*`, commits use the identity `ACR Sandbox Author <author@acr-sandbox.invalid>`, and no code or text comes from a real project.

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

In a newly published repository GitHub may never start the workflow, even though Actions is enabled and the workflow is listed as active. Closing and reopening a pull request starts it without changing any commit. Before recording, check that every head that should have checks has a completed run.

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

Set `ACR_SANDBOX_LOGINS` to the logins of the two accounts, reviewer first: `<reviewer>,<author>`. The recorder requires it. The test that keeps real logins out of `testdata/` also uses it, and skips itself when the variable is unset.

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

## Recording

Run the recorder from the repository root:

```bash
ACR_GITHUB_TOKEN=<token> ACR_SANDBOX_LOGINS=<reviewer>,<author> go run ./tools/recorder all
```

Pass scenario names instead of `all` to record only some of them: `S1` to `S6`, or `queue` for the review-requested search. Each scenario is recorded into `testdata/github/<scenario>/`, and the recordings of the previous run are removed first.

What the recorder does:

- **Read-only:** it only sends `GET` requests, and follows pagination only within the API base URL.
- **Requests:** for each pull request of the scenario, it reads what a review reads:
  - the pull request, its files, commits, reviews and comments;
  - the check runs and the combined status of the head commit;
  - the criteria files on the target branch;
  - the contents of every changed file that has a patch.
  A number that is not a pull request is also looked up as an issue, which tells an issue from a missing number. Missing files are recorded with their `404`.
- **One file per response page:** method, path, query, status, the `Link` and `ETag` headers only, and the body.
- **Sanitized:** the two sandbox logins become `sandbox-reviewer` and `sandbox-author`, with ids 1001 and 1002. Their avatar URLs and node ids are replaced. Email addresses outside `example.com` and the reserved `.invalid` domain, and token-like strings, are redacted. Any other user account becomes `sandbox-user-<n>`.

Check before you record:

- **S5 A:** the `instrumented` job only stays `in_progress` for 45 minutes after it starts. Re-run it right before you record S5.
- **S4:** the files API must return no `patch` for `IconCatalog.kt`.

Commit the recorded data on its own, in a `test(recorder): ...` commit, after `go test ./...` passes with `ACR_SANDBOX_LOGINS` set.
