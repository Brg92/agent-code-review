# `doctor`

Checks the environment the other commands depend on and says what to fix. It reads, and writes nothing to the platform. The exit codes are in [exit-codes.md](exit-codes.md).

```bash
agent-code-review doctor [--repo owner/repo] [--verbose]
```

## Output

One line for each check, in a fixed order: `ok`, `warn` or `fail`, what was checked and what was found, and for `warn` and `fail` what to do. It never prints a token, or part of one: it only says where the token comes from.

```text
ok    version: 0.0.0-dev
ok    git: git version 2.43.0
ok    credential storage: libsecret (OS keychain)
ok    token for github.com: from the environment variable ACR_GITHUB_TOKEN
ok    GET /user: logged in as ann; classic token (scopes: repo)
ok    packs: android, kotlin-multiplatform (from /opt/agent-code-review/packs)
ok    user config: /home/ann/.config/agent-code-review/config.json is not there, the defaults apply
ok    work directory: /home/ann/.cache/agent-code-review is writable
```

Every check runs, whatever failed before it. A check that needs what a failed one gives, such as the account without a token, is a `fail` that says it was not checked.

## The checks

1. **version** of the binary.
2. **git** is present, with its version: the token is read through `git credential`.
3. **credential storage** is secure, by the rule of `auth login`: git has a credential helper, and none of the helpers is `store`, which keeps the token in plain text. When the token comes from `ACR_GITHUB_TOKEN` nothing is stored, and a storage that would be refused is a `warn`. A helper that is not known is a `warn`.
4. **token** for github.com is available, from `ACR_GITHUB_TOKEN` or from `git credential`, and which one. If `GITHUB_TOKEN` or `GH_TOKEN` is set and `ACR_GITHUB_TOKEN` is not, an extra `warn` line says they are never read as a token, and how to pass the token. In GitHub Actions: `ACR_GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`.
5. **`GET /user`** works: the login, and the kind of token as far as the headers say. A classic token has an `X-OAuth-Scopes` header, and its scopes are listed. Any other token has none, and is reported as not classic (a fine-grained token, for example).
6. **With `--repo`**, what the token can do in the repository, one line each, by reading it: the repository, its pull requests, its contents, its issues and its check runs (a fine-grained token needs `Actions: read` for the last). When the platform refuses and says which permission was needed, the line names it, from `X-Accepted-GitHub-Permissions`. Then whether the token can **write** to pull requests and to issues, which only `publish` needs. Nothing is written to find out: a classic token writes with the `repo` scope, or `public_repo` on a public repository, and a missing one is a `warn`. For a token that is not classic only a write would show it, so the line is `ok` and says it cannot be checked; `publish` names the permission it lacks.
7. **packs** are found and valid: their ids, and where they were loaded from. A pack manifest has no version, so none is shown.
8. **user config** is valid: `config.json` in the user configuration directory. A file that is not there is valid.
9. **work directory** is writable: it is created if it is not there, and a file is written in it and removed.

There is no option for another host: github.com is the only one.

## Exit code

`0` when no check is a `fail`; warnings do not count. Otherwise the code of the first of these that one of the failed checks has:

| Code | Cause |
| --- | --- |
| 6 | git missing or a failing credential helper, no token, a token the platform refuses or one that lacks a permission. |
| 10 | Insecure credential storage. |
| 7 | The platform is unreachable or answers with an error, or the repository is not there or cannot be seen by the token. |
| 3 | The user configuration or a pack is not valid. |
| 1 | Anything else: no packs found, a work directory that is not writable. |

The same condition may give another code to another command: `review` ends with 1 for a pack that cannot be loaded, `doctor` with 3 when the pack is invalid and 1 when none is found.
