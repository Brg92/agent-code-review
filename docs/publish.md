# publish

`publish` is the only command that writes to a platform, and it writes only what the user confirmed. It works on the most recent run that is `finalized` or `planned`, or on the run given with `--run`, and appends its events to the `run.log` of the run.

```text
agent-code-review publish --plan <comments|issues|both> [--verbose] [--run <run id>]
agent-code-review publish --apply [--verbose] [--run <run id>]
```

## The rule for the agent

1. **Never run `--apply` without an explicit yes from the user**, given after the user has seen the summary that `--plan` printed. A yes to something else, or a yes given before the plan, is not a yes.
2. **A request to "post", "publish" or "comment" that does not say what** is a question, not a guess: ask whether to plan `comments`, `issues` or `both`.
3. Run `publish --plan <choice>`, show the summary, and ask: "Apply this plan?". On a yes, run `publish --apply`. To change what is written, run `--plan` again with another choice; single actions cannot be left out.
4. After a failure (exit 7, 6 or 4) say what was done and what was not, as the command printed it. Exit 7 and 6 may be applied again with a new yes only if the user wants to; exit 4 means the change request has to be reviewed again.

## The plan

`--plan` reads the platform and writes nothing to it. For every reviewed change request of the run, in review order (related change requests first, the root last), it makes `plan.json` in the directory of the change request (`schemas/plan.schema.json`), writes the SHA-256 of each file in `state.json` (`planSha256`), moves the run to `planned` (`finalized → planned`, and `planned → planned` to make a new plan) and prints a summary of each plan: what would be written, with counts and rule ids, and what is skipped and why.

| Choice | Writes |
| --- | --- |
| `comments` | one inline comment for each BLOCKER of the change request, in one review; the note that nothing blocks when the verdict is OK; the tick in the Related Changes of the parent |
| `issues` | one issue for each of BLOCKER (existing code), MAJOR and MINOR; the label `review` when the project lacks it |
| `both` | everything above |

Each `plan.json` has the action chosen, the actions in the order they run, and the entries it skips. An action is one of:

| Kind | What it writes |
| --- | --- |
| `review-comment` | an inline comment, as part of the one review of the change request |
| `summary-comment` | the general comment `Review completed: no blocking issues found!` |
| `label-create` | the label `review` in the project, before the issues that use it |
| `issue-create` | a new issue, assigned to the author of the change request |
| `issue-update` | the title and body of an open issue that carries our fingerprint (with the issue number) |
| `related-tick` | the box of this change request in the Related Changes of its parent (with the parent and the entry) |

### Inline comments

- One for each **BLOCKER of the change request** (`origin: mr`, not existing code) whose file shows a line of the diff, with no limit per run, all in **one review** whose event is `COMMENT`: never an approval, never a request for changes, and the review has no text of its own.
- Findings of existing code are never inline comments.
- A finding on a line outside the hunks is anchored on the nearest line of the diff, and the text starts with "Refers to line N of `path`, outside the changed lines."
- A suggestion is a `suggestion` block only where the lines can be replaced by it (the added or unchanged lines of one hunk, on the right side); otherwise the code is a plain block at the end of the fix. A comment over several lines is sent as a range only when it has a suggestion.
- The text of a comment: severity, rule and title, why, **Fix:**, the suggestion, other places the finding relies on, and the fingerprint of the finding in an HTML comment. It is in English and says nothing of how it was made.
- A BLOCKER that no line of the diff can carry (its file shows none) is skipped as `no-anchor` and stays in the report; the verdict stays BLOCKED.

### The note that nothing blocks

When the verdict of a change request is OK, one general comment: `Review completed: no blocking issues found!` with the fingerprint `acr:v1:ok:<head8>`. It is never written when the verdict is BLOCKED, and no other general comment is ever written.

### Issues

For each severity of a change request, one issue in the project of the change request, with the label `review`, assigned to the author of the change request:

- **BLOCKER**: the findings of existing code that are BLOCKERs (the BLOCKERs of the change request itself are inline comments).
- **MAJOR** and **MINOR**: the findings of the change request first, then, under `### Existing code (not written in this change request)`, those of existing code.
- One entry for each occurrence, with its fingerprint `acr:item:...`; the fingerprint of the issue, `acr:v1:issue:<project>#<number>:<severity>`, closes the body. INFO findings are only in the report.
- An **open** issue with that fingerprint, written by a trusted account, is rewritten (`issue-update`: its body is made again from the current findings, its title and state stay). A **closed** one is not touched: a new issue is opened. A body that is already what would be written is skipped as `already-posted`.
- The label `review` missing in the project is a `label-create` action of the plan, before the issues.
- If the author of the change request cannot be assigned in the project, the issue is opened without an assignee, and the outcome says so (`posted-unassigned`).

### Related tick

With `comments` or `both`, for every change request of the cascade that has a parent in it: the box of its entry in the description of the parent goes from `[ ]` to `[x]`, by changing that one byte (`cascade.Tick`), only if all of these hold:

- its head is unchanged, and it is open and not the account's own;
- its verdict is OK (no BLOCKER of the change request, no existing finding that crashes);
- none of its review threads is unresolved with a first comment that carries a trusted fingerprint of one of our findings (inline comments are only BLOCKERs);
- the box in the parent is still empty, and the parent itself can be written to.

Otherwise the plan says `not-eligible` and why. The tick is checked again when applying, against the description of the parent as it is then.

## Statuses

Skipped entries of a plan, and outcomes in `published.json`:

| Status | Meaning |
| --- | --- |
| `already-posted` | a trusted fingerprint is already on the change request or in the issue; nothing is written for it |
| `ignored-untrusted` | a fingerprint was found, written by an account that is not trusted; it does not count and the write goes ahead |
| `head-changed` | the change request has new commits since it was reviewed; nothing is written for it |
| `not-open` | the change request is closed or merged; nothing is written for it |
| `own-cr` | the account of the token opened the change request: report only, nothing is written, and it is never ticked in a parent |
| `rejected` | the platform refused the position of an inline comment; it stays in the report and is never tried on another line |
| `secret-blocked` | a title or body matches a secret pattern; only the name of the pattern is printed |
| `too-long` | a body is longer than the platform accepts (GitHub: 65 536 characters); the findings stay in the report |
| `not-eligible` | the entry cannot be ticked, or an issue to rewrite is closed or gone; the reason is in `detail` |
| `no-anchor` | a BLOCKER that no line of the diff can carry |
| `posted`, `posted-unassigned` | written, the second one without an assignee (outcomes only) |
| `failed` | the platform failed; a later apply tries it again (outcomes only) |

The check of the change request, in this order: closed or merged (`not-open`), new commits (`head-changed`), the account's own (`own-cr`).

## Which fingerprints count

A fingerprint in a note, a review comment or an issue counts only if it was written by the account of the token, or by an account the platform shows as an owner, member or collaborator of the project (on GitHub: `author_association` in the payload), **and never if the author of the change request wrote it**. A fingerprint of any other account is ignored: the plan lists it as `ignored-untrusted` and the write goes ahead. Which accounts are trusted is the provider's to say; the core only sees "trusted" or "not trusted". The legacy `mr-review:v1:` form is recognised under the same rule.

## Secrets

Every title and body, of every action, is scanned in `--plan` and again in `--apply` for a fixed set of patterns compiled into the binary, which cannot be changed or turned off: GitHub tokens (`ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, `github_pat_`), AWS access key ids (`AKIA`, `ASIA`), PEM private keys, Slack tokens and the assignment of a quoted literal of 8 characters or more to `password`, `passwd`, `secret`, `token` or `api_key`. An action with a match is `secret-blocked` and the others go ahead. The text that matched is never printed, logged or written: only the change request, the action, the path and the name of the pattern.

## Applying

`--apply` runs only on a run that is `planned`, and executes **exactly the stored plans**:

1. For every change request it reads `plan.json` and checks its SHA-256 against the one in `state.json`. A plan that is missing, not valid, for another run or change request, or whose hash is not the recorded one is **exit 8**, listed with the others, and **nothing is written**.
2. It reads every change request again. One that is closed, whose head is not the reviewed one, or that is the account's own gets nothing; the others go ahead. When some head changed, the command ends with **exit 4** and lists them: review them again.
3. For the others, in review order, it writes the label, the issues, the review, the note and the tick, reading the platform again before each kind of write so that what an earlier apply wrote is not written twice.
4. The outcome of each action is written to `published.json` in the directory of the change request, atomically, as soon as it is known.
5. The run moves to `published` only when every action of every plan has a final outcome (anything but `failed`, or not tried). Otherwise it stays `planned`.

A write is retried only after a rate limit, which refused it. After a network error or a 5xx it is not retried, because the platform may have done it: the next apply finds it by its fingerprint.

### Decisions that are easy to miss

- **Writes are not retried after a network error or a 5xx**, only after a rate limit; reads are retried as before. What the platform did is unknown, so the next apply looks for the fingerprint before it writes.
- **`assignee` is not required in an `issue-create`**: it is absent when the author of the change request is not known. When it is there and the author cannot be assigned in the project, the outcome is `posted-unassigned`.
- **Trust is a flag the provider fills** on notes, issues and review threads (on GitHub from `author_association`, and from the login of the token, which is read once and only when a note needs it). The core only reads "trusted" or "not trusted". What a platform accepts in a body and how it writes a suggestion are capabilities, as data.
- **A box that is already ticked** is `not-eligible` when planning (the entry cannot be ticked) and `already-posted` when applying (an earlier apply, or someone, did it): applying again stays idempotent.
- **The check of a change request** is in this order: closed or merged (`not-open`), new commits (`head-changed`), the account's own (`own-cr`).

### Resume

After a failure (**exit 7**: the platform is unreachable, answers 5xx, or asks for a wait longer than 60 seconds; **exit 6**: the token is refused or lacks a permission) the command prints what was done and what was not. Running `--apply` again on the same plans skips the actions that have a final outcome, checks the trusted fingerprints again for the others, and writes only what is not there. `published.json` is for the plan it names: after a new `--plan` its outcomes are not used, and the platform tells what is posted.

### A position the platform refuses

The inline comments of a change request are one request, and GitHub answers a position outside the diff with a 422 for the whole review, in which nothing is posted. If the answer names the comments it refused, the command marks them `rejected`, sends the review once more without them, and marks the others `posted`; a second refusal is handled the same way and not tried a third time. If the answer does not say which comment it refused, nothing was posted: every comment of the review is `rejected` and the findings stay in the report. A comment is never tried on another line, and never sent again in a review of its own.

**The "not named" case is the one GitHub produces.** The answer of GitHub to a review with a comment on a line that is not in the diff was captured on the sandbox (S8, `tools/review422`): the status is 422 and the body is `{"message":"Unprocessable Entity","errors":["Line could not be resolved"],...}`. It does not name the comment: there is no `comments[N]` in it, and no index. With one comment in the review, that is all the capture shows; it is kept as `testdata/githubwrite/review-422-line-outside-diff.json` and replayed by a test. The parser still looks for `comments[N]` in the `errors`, so an answer that does name them would be used, but in practice every comment of a refused review is `rejected` and the findings stay in the report. Whether GitHub names a comment when several are sent and only one is outside the diff was not captured.

## Exit codes

| Code | When |
| --- | --- |
| 0 | the plan was made, or everything the apply had to do was done |
| 2 | no `--plan <comments\|issues\|both>` or `--apply`, both, an argument, an unknown flag, a `--run` that is not a run id |
| 4 | one or more change requests have new commits since they were reviewed: nothing was written for them, the others were |
| 6 | the token is missing, refused, or lacks a permission (fine-grained tokens need Pull requests and Issues set to read and write) |
| 7 | the platform is unreachable, answers 5xx after the retries, or asks for a wait longer than the longest accepted |
| 8 | the run is not `finalized` or `planned` (`--plan`), or not `planned` (`--apply`); a plan is missing or is not the one that was made |

See [exit-codes.md](exit-codes.md).
