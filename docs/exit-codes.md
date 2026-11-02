# Exit codes

> **Status: to be reviewed.** These codes are a proposal. They become binding when the first command that returns them is implemented.

Every `agent-code-review` command ends with one of these codes. Agents and scripts branch on the code, never on the text of the message. The message on stderr explains the cause and, where possible, names the file and field involved.

| Code | Meaning | Typical cause |
| --- | --- | --- |
| 0 | Success | The command did what was asked. A review that finds blockers still succeeds: the verdict is in the report, not in the exit code. |
| 1 | Unexpected error | A bug or an environment problem the CLI does not classify, such as a full disk. |
| 2 | Usage error | Unknown command or flag, missing argument. The usage is printed on stderr. |
| 3 | Validation error | A file does not match its schema: a judgement written by the agent, or the repository's `.agent-code-review/config.json` (scenario S6 B). Every invalid field is listed. |
| 4 | Head changed | The change request has new commits since it was collected or planned. `publish --apply` writes nothing; review again. |
| 5 | Reserved | Not used. A change request that edits its own criteria is still reviewed, with the target branch's criteria, and the summary notes that the edit was not applied (scenario S6 A). |
| 6 | Authentication error | No token, an expired token, or a token without the needed permission. |
| 7 | Platform or API error | The platform is unreachable or answers with an error that is not about authentication, including rate limits. |
| 8 | Invalid state | The command does not fit the run's current phase, for example `finalize` before every change request is judged, or a work directory with a state file of an unknown version. |
| 9 | Path not allowed | A path points outside the run's work directory, or is absolute or contains `..`. |

Rules:

- Codes are stable once released. A new condition gets a new code; an existing code never changes its meaning.
- The CLI never prints a token, also not in error messages.
- Code 1 always means something the CLI should classify better: report it.
