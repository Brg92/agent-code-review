# Exit codes

> **Status.** A code is **binding** once a command returns it; the others are proposals until their first command is implemented. Binding now: 0, 1, 2, 6, 7, 8, 10 and 130, used by `version`, `auth`, `review` and `next`.

Every `agent-code-review` command ends with one of these codes. Agents and scripts branch on the code, never on the text of the message. The message on stderr explains the cause and, where possible, names the file and field involved.

| Code | Status | Meaning | Typical cause |
| --- | --- | --- | --- |
| 0 | Binding | Success | The command did what was asked. A review that finds blockers still succeeds: the verdict is in the report, not in the exit code. |
| 1 | Binding | Unexpected error | A bug or an environment problem the CLI does not classify, such as a full disk, a work directory or run log that cannot be used, packs that cannot be found or loaded, a user `config.json` that is not valid (until code 3 is binding), or a terminal that cannot hide the typed token: `auth login` run in a Git Bash (MSYS or Cygwin) terminal, or where the echo cannot be turned off. |
| 2 | Binding | Usage error | Unknown command or flag, missing argument, an invalid `--host`, or an argument where `auth login` expects the token on stdin; for `review`, a reference that is not a pull request or `owner/repo#N`, or a root on a host or platform this release does not read (only github.com is); for `next`, a `--run` that is not a run id. The usage is printed on stderr for a malformed command line. |
| 3 | Proposal | Validation error | A file does not match its schema: a judgement written by the agent, used by `finalize`. Every invalid field is listed. A repository criteria file that is not valid, or criteria that cannot apply, such as a rule id a pack already uses, never stop a review: they are listed under "Repository criteria ignored". |
| 4 | Proposal | Head changed | The change request has new commits since it was collected or planned. `publish --apply` writes nothing; review again. |
| 5 | Proposal | Reserved | Not used. A change request that edits its own criteria is still reviewed, with the target branch's criteria, and the summary notes that the edit was not applied (scenario S6 A). |
| 6 | Binding | Authentication error | No token, an expired or refused token, or a token without the needed permission, found by `review` on the first request that needs it (the message names the permission the platform said was missing). For `auth`: no token available or read from stdin, a token with whitespace, git missing, or git failing to read, store or remove the token, including a git command that does not finish within 30 seconds, for example a credential helper waiting for an unlock. |
| 7 | Binding | Platform or API error | `review`: the platform is unreachable, answers 5xx after the retries, or asks for a wait longer than the longest one accepted (60 seconds) for its rate limit; the root change request is missing or the token cannot read it; a change request lists more changed files than the platform returns, or holds a path with a control character and so is not reviewed. A related change request that cannot be read is not an error: it is skipped as `not-found` and the cascade is INCOMPLETE. |
| 8 | Binding | Invalid state | `next`: there is no run, or none that is not published; the run is not in phase `collected` (it was not finished, or it is already judged); a work package is missing or damaged; or the state file has an unknown version or is damaged. `finalize` before every change request is judged will use it too. |
| 9 | Proposal | Path not allowed | A path points outside the run's work directory, or is absolute or contains `..`, or the folder named for saved reports is inside a git work tree. |
| 10 | Binding | Unsafe credential storage | `auth login` refuses to store the token: git has no credential helper, so nothing would be stored, or `store` is among the helpers, which writes it in plain text on disk. The message says what to configure. |
| 130 | Binding | Interrupted | Ctrl+C or a termination request while `auth login` reads the token; the terminal echo is restored first. 130 is the shell convention for SIGINT, outside the result codes above. |

Rules:

- Codes are stable once released. A new condition gets a new code; an existing code never changes its meaning.
- The CLI never prints a token, also not in error messages.
- Code 1 always means something the CLI should classify better: report it.
