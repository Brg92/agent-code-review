# End-to-end run on the sandbox

The run that closes F2: a complete review of the scenario S8, from `review` to a confirmed `publish --apply`, and a second plan that proves nothing is written twice. It writes comments, a review and issues to the sandbox on purpose, so it uses pull requests of its own that are never recorded (see S8 in [sandbox.md](sandbox.md) and `testdata/scenarios/S8/README.md`).

| | |
| --- | --- |
| Root | [`acr-plugin-sandbox/sample-app#9`](https://github.com/acr-plugin-sandbox/sample-app/pull/9), whose Related Changes lists `core-lib#4` |
| Related | [`acr-plugin-sandbox/core-lib#4`](https://github.com/acr-plugin-sandbox/core-lib/pull/4), expected verdict OK |
| Judgements | `testdata/scenarios/S8/judgements/sample-app-9.json` and `core-lib-4.json`, written by hand |

Expected: `sample-app#9` is BLOCKED by one BLOCKER of the change request, so the plan has one inline comment in one review, one issue for the MAJOR findings and one for the BLOCKER of existing code. `core-lib#4` is OK, so the plan has the note `Review completed: no blocking issues found!` and the tick of its entry in the description of `sample-app#9`.

The plugin never deletes anything on the platform. To repeat the run, see [Run it again](#run-it-again).

## What you need

- The reviewer account of the sandbox ([sandbox.md](sandbox.md)), with a **fine-grained token** limited to `acr-plugin-sandbox/sample-app` and `acr-plugin-sandbox/core-lib`, with Contents, Metadata, Actions and Commit statuses read-only and **Pull requests and Issues read and write**: `doctor` cannot check the two writes of a fine-grained token, and without them `publish --apply` stops at its first write with exit 6 and `forbidden (needs issues: write, pull_requests: write), status 403`, having written nothing. Check the token in the active list of the organization (`Settings → Personal access tokens`) if that happens. It is not the recorder token and it is not for the author account: the author's pull requests are what the reviewer reviews, and a token of the author turns both of them into `own-cr`.
- git with a secure credential helper (Git Credential Manager on Windows, Keychain on macOS, libsecret on Linux), and Go for the build. The packs of the checkout are used through `ACR_PACKS_DIR`.
- **On Linux, a keychain helper.** `auth login` refuses to store the token unless git has a helper that keeps it. The helper `gh auth git-credential`, which `gh auth setup-git` configures, ignores `store`, so `auth login` ends with "the git credential helper did not keep the token". Install a Secret Service helper (`git config --global credential.helper libsecret`, with `git-credential-libsecret` built from `contrib/credential/libsecret` of git). For one session only, without changing `~/.gitconfig`, you can select git's in-memory `cache` helper through the environment of the command (`doctor` warns that it does not know where `cache` keeps the token, which is memory only); this is what the F2 run on Linux did:

  ```bash
  export GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=credential.https://github.com.helper GIT_CONFIG_VALUE_0= \
         GIT_CONFIG_KEY_1=credential.https://github.com.helper GIT_CONFIG_VALUE_1='cache --timeout=14400'
  ```

  The empty first value resets the helpers that the configuration lists, so the token is neither asked of nor stored by `gh`. Unset the four variables afterwards, and run `git credential-cache exit` to drop the token from memory.
- The token goes into the keychain with `auth login` and nowhere else. **Do not export it in a profile.** If `ACR_GITHUB_TOKEN` is set for another reason, `unset` it (`Remove-Item Env:ACR_GITHUB_TOKEN`) for this run, or `doctor` will say the token comes from there.

## Commands

Run them in order, from the root of a checkout of this repository, on the commit to be checked. Each command says what to look at before the next one.

### Linux and macOS

```bash
go build -o ./agent-code-review ./cmd/agent-code-review
export ACR_PACKS_DIR="$PWD/packs"
./agent-code-review auth login
./agent-code-review doctor --repo acr-plugin-sandbox/sample-app
./agent-code-review doctor --repo acr-plugin-sandbox/core-lib
./agent-code-review review https://github.com/acr-plugin-sandbox/sample-app/pull/9
./agent-code-review next
```

### Windows, PowerShell

```powershell
go build -o .\agent-code-review.exe .\cmd\agent-code-review
$env:ACR_PACKS_DIR = "$PWD\packs"
.\agent-code-review.exe auth login
.\agent-code-review.exe doctor --repo acr-plugin-sandbox/sample-app
.\agent-code-review.exe doctor --repo acr-plugin-sandbox/core-lib
.\agent-code-review.exe review https://github.com/acr-plugin-sandbox/sample-app/pull/9
.\agent-code-review.exe next
```

### Windows, Git Bash

Run `auth login` in PowerShell: a Git Bash terminal cannot hide the typed token, and the command exits with 1 there. The other commands run in Git Bash as on Linux, with the `.exe` name:

```bash
go build -o ./agent-code-review.exe ./cmd/agent-code-review
export ACR_PACKS_DIR="$PWD/packs"
./agent-code-review.exe doctor --repo acr-plugin-sandbox/sample-app
./agent-code-review.exe doctor --repo acr-plugin-sandbox/core-lib
./agent-code-review.exe review https://github.com/acr-plugin-sandbox/sample-app/pull/9
./agent-code-review.exe next
```

The rest of this document writes `agent-code-review` for the binary of your shell.

### What to check, step by step

1. **`auth login`** reads the token from the terminal, hidden, and stores it in the credential helper. Then `agent-code-review auth status` says the token comes from the credential helper.
2. **`doctor --repo`**, once for each repository: every line is `ok`, and the last two say the token can write to pull requests and issues (for a fine-grained token this line says that it cannot be checked: `publish` names the permission it lacks). Exit code 0.
3. **`review <URL of sample-app#9>`** collects the root and, through its Related Changes, `core-lib#4`. The summary lists `core-lib#4` first and `sample-app#9` last, no change request skipped, and ends with `Next: agent-code-review next`. Nothing is written to the platform.
4. **`next`** prints `Judge github:github.com/acr-plugin-sandbox/core-lib#4 part 1 of 1 — write the judgement to <path>`, then the brief. The path is the `judgement.json` in the directory of that change request in the run, named `<owner>_<repo>-<number>-<head8>`. Copy the hand-written judgement there: the file is the change request, the path is the one printed.

   ```bash
   cp testdata/scenarios/S8/judgements/core-lib-4.json "<path printed by next>"
   ```

   ```powershell
   Copy-Item testdata\scenarios\S8\judgements\core-lib-4.json "<path printed by next>"
   ```

   Run `agent-code-review next` again: it now asks for `sample-app#9`. Copy `sample-app-9.json` to the path it prints in the same way, and run `next` once more: it prints `Done: run agent-code-review finalize`. A judgement holds the head commit it was written for: after a new head, write it again with the new `headSha`.
5. **`finalize`** validates both judgements, writes `findings.json` and `report.txt` for each and moves the run to `finalized`. The summary says BLOCKED for the cascade, with one BLOCKER in `sample-app#9`, and OK for `core-lib#4`.
6. **`publish --plan both`** reads the platform and writes nothing to it. For `core-lib#4`: the note and the tick. For `sample-app#9`: one `review-comment`, the `label-create` of `review` if the project has no such label, and the issues. Read the summary.
7. **The confirmation.** Nothing is written until you say yes to what the plan printed. Only then:
8. **`publish --apply`** writes exactly that plan, and nothing else. It ends with exit code 0 and prints what it posted, with the numbers of the issues (`posted  label review`, `posted  issue BLOCKER (new) (#10)`, `posted  issue MAJOR (new) (#11)`, `posted  inline comment run-blocking-prod ...MainActivity.kt:13`, and for `core-lib#4` the note and the tick). Open the links on GitHub: the review on `sample-app#9` has one inline comment on the `runBlocking` line, with the event `COMMENT`; `core-lib#4` has the note; the description of `sample-app#9` has its Related Changes entry ticked; and the issues exist, assigned to the author.
9. **A second plan** proves that nothing is planned twice. The run is `published` now, and `publish --plan` refuses a published run (exit 8, `no run that is not published`), so review the root again, give the same judgements to the new run and plan it:

   ```bash
   agent-code-review review https://github.com/acr-plugin-sandbox/sample-app/pull/9
   ```

   `core-lib#4` is skipped as `already done`, because its note is posted and its entry is ticked. For `sample-app#9`, `next` asks for its judgement once more: copy `sample-app-9.json` to the path it prints, run `next` (`Done: run agent-code-review finalize`), then `finalize` and `publish --plan both`. The plan says `nothing to write`, with `already-posted` for the two issues (`issue #10 is up to date`, `issue #11 is up to date`) and for the inline comment. Do not apply it.
10. **The log.** `run.log` of the run, in `runs/<run id>/` of the work directory (`~/.cache/agent-code-review` on Linux, `~/Library/Caches/agent-code-review` on macOS, `%LOCALAPPDATA%\agent-code-review` on Windows; `doctor` prints it), has one line per request and no token, header, query or body:

    ```bash
    grep -c -E 'gh[pousr]_|github_pat_|Bearer' runs/<run id>/run.log   # must print 0
    ```

    ```powershell
    (Select-String -Path runs\<run id>\run.log -Pattern 'gh[pousr]_|github_pat_|Bearer').Count   # must print 0
    ```

## Run it again

The second `publish --plan both` of a finished run is empty, and so is any later one: what is posted stays posted. To run the whole flow again on the same pull requests, do one of these by hand, because the plugin never deletes anything:

- **Delete what was written.** On `sample-app#9`: the review comment (a review comment is deleted from its menu on the conversation), the issues opened by the run (close them and delete them from their menu, or delete the label `review` last), and untick the box of `core-lib#4` in the description. On `core-lib#4`: the note `Review completed: no blocking issues found!`. A closed issue is not touched by a new run, which opens a new one, so a closed issue left in place is harmless; an open one would be rewritten instead.
- **Push a new head.** A new commit on `feat/home-s8-session-refresh` or on `feat/session-s8-display-label` (from the local `acr-plugin-sandbox` checkout, as the author account) gives a head that nothing was written for: the plugin checks the head of every change request before it writes, and a plan made for the old head ends with exit 4. What was written for the old head stays where it is, and the plan finds it by its fingerprints. Write the judgements again with the new `headSha` (and the signal ids that `review` printed), and untick the box in the description of `sample-app#9` to see the tick again.

## Capturing a 422

`publish` sends the inline comments of a change request as one review, and GitHub refuses the whole review with a 422 when a comment is on a line that is not in the diff. `tools/review422` sends such a review to S8, on purpose, and keeps the answer as a synthetic fixture. It is a development tool, it ships nowhere, and it saves nothing unless the answer is a 422. Run it only after the plan of the run above is applied, so that it does not change what the plan found, and only when you want the capture again:

```bash
go run ./tools/review422 -repo acr-plugin-sandbox/sample-app -pr 9 \
  -path app/src/main/java/com/example/sample/MainActivity.kt -line 1 \
  -out testdata/githubwrite/review-422-line-outside-diff.json
```

It uses the token of `auth login` (the same environment as above on a Linux without a keychain helper). Line 1 is not in the diff of `MainActivity.kt`, whose hunk starts at line 3. The F2 capture is in `testdata/githubwrite/review-422-line-outside-diff.json`: status 422, `"errors":["Line could not be resolved"]`, no comment named. If GitHub accepts the review instead, the tool says so and saves nothing: delete the review comment by hand.

## Before the beta

The run above is done on Linux in F2. The checks of the keychains are not part of F2 and have to be done by hand before the beta, because the CI has no Git Credential Manager or Keychain. Each one ends with the end-to-end commands of this document, on that OS.

- **Windows** (a Windows 11 evaluation VM on the Linux machine is enough):
  1. `auth login` with Git Credential Manager: the token is stored, and `auth status` and `doctor` find it.
  2. A second credential for `acr-api` does not make an ordinary `git push` or `git fetch` show the account picker of Git Credential Manager: run both against a repository of the sandbox with the credential stored, and check that neither asks which account to use.
  3. The end-to-end commands above, once in PowerShell and once in Git Bash (with `auth login` in PowerShell).
- **macOS** (a real Mac): `auth login` with Keychain, `auth status`, `doctor`, then the end-to-end commands above.
