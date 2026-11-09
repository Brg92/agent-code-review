# Exit codes

> **Status.** A code is **binding** once a command returns it; the others are proposals until their first command is implemented. Binding now: 0, 1, 2, 6, 10 and 130, used by `version` and `auth`.

Every `agent-code-review` command ends with one of these codes. Agents and scripts branch on the code, never on the text of the message. The message on stderr explains the cause and, where possible, names the file and field involved.

| Code | Status | Meaning | Typical cause |
| --- | --- | --- | --- |
| 0 | Binding | Success | The command did what was asked. A review that finds blockers still succeeds: the verdict is in the report, not in the exit code. |
| 1 | Binding | Unexpected error | A bug or an environment problem the CLI does not classify, such as a full disk. |
| 2 | Binding | Usage error | Unknown command or flag, missing argument, an invalid `--host`, or an argument where `auth login` expects the token on stdin. The usage is printed on stderr. |
| 3 | Proposal | Validation error | A file does not match its schema: a judgement written by the agent, the repository's `.agent-code-review/config.json` (scenario S6 B) or legacy `.github/mr-review.json`, or the user's `config.json`. Every invalid field is listed. Ignored repository criteria, such as a rule id a pack already uses, are only noted. |
| 4 | Proposal | Head changed | The change request has new commits since it was collected or planned. `publish --apply` writes nothing; review again. |
| 5 | Proposal | Reserved | Not used. A change request that edits its own criteria is still reviewed, with the target branch's criteria, and the summary notes that the edit was not applied (scenario S6 A). |
| 6 | Binding | Authentication error | No token, an expired token, or a token without the needed permission. For `auth`: no token available or read from stdin, a token with whitespace, git missing, or git failing to read, store or remove the token. |
| 7 | Proposal | Platform or API error | The platform is unreachable or answers with an error that is not about authentication, including rate limits. |
| 8 | Proposal | Invalid state | The command does not fit the run's current phase, for example `finalize` before every change request is judged, or a work directory with a state file of an unknown version. |
| 9 | Proposal | Path not allowed | A path points outside the run's work directory, or is absolute or contains `..`, or the folder named for saved reports is inside a git work tree. |
| 10 | Binding | Unsafe credential storage | `auth login` refuses to store the token: git has no credential helper, so nothing would be stored, or `store` is among the helpers, which writes it in plain text on disk. The message says what to configure. |
| 130 | Binding | Interrupted | Ctrl+C or a termination request while `auth login` reads the token; the terminal echo is restored first. 130 is the shell convention for SIGINT, outside the result codes above. |

Rules:

- Codes are stable once released. A new condition gets a new code; an existing code never changes its meaning.
- The CLI never prints a token, also not in error messages.
- Code 1 always means something the CLI should classify better: report it.
