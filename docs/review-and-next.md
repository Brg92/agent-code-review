# `review` and `next`

The first two commands of the flow. `review` reads a pull request and everything its Related Changes section lists, and stops when every change request is **collected**. `next` hands the agent one part of one work package at a time. Neither writes to a platform. The exit codes are in [exit-codes.md](exit-codes.md).

```bash
agent-code-review review [--verbose] <pull request>
```

The pull request is its URL or `owner/repo#N`.

```bash
agent-code-review next [--verbose] [--run <run id>] [--part <n>]
```

## `review`

1. Removes the work directories of runs older than `keepDays` (7 by default).
2. Creates the run, `runs/<run id>/` in the work directory, and its `run.log`.
3. Reads the root and the criteria of its target branch, which may set the cascade limits, then resolves the cascade. Links to another platform are skipped as `other-platform`.
4. For each change request, root first: the changed files (those with status `unchanged` are left out), the criteria of its target branch (an invalid file is left out and listed under "Repository criteria ignored"), the packs, the profile and dimensions, the rules, the signals, the checks of the head commit. The work package is written to `<project>-<number>-<head>/workpackage.json`.
5. Moves the run to `collected` and prints the summary: the run id, the change requests in review order (related first, root last) with their titles, the ones skipped with their reason, the parts to judge and `Next: agent-code-review next`.

Titles are written by the authors of the change requests, so the summary puts them between the delimiters of untrusted content that the briefs use, and says so in its first line.

Only the root stops the review when it cannot be read (exit 6 or 7). A related change request that the platform says is gone, or that the token cannot read, after the cascade read it, is skipped as `not-found`: the cascade is resolved again without it, so what it listed is not in the run, the cascade is INCOMPLETE and `run.log` records it. A failure that is not about that change request, such as the network or the rate limit, still stops the review. A read that the core turns into Not verified does not stop it: a tree cut short, checks that cannot be read, a search the platform cannot run, a changed file whose contents the platform does not give (`content-unavailable`, left out of the rules). They are listed in the work package.

The notes of reading the criteria, such as criteria edited by the change request that are not applied, are in `criteria.notes` of the work package and in its brief; `finalize` copies them into the findings.

## `next`

Chooses the most recent run that is not `published`, by run id, or the run given with `--run`. It prints one line, then the brief of a part:

```text
Judge github:github.com/o/r#3 part 1 of 2 — write the judgement to <work dir>/runs/<run id>/o_r-3-1a2b3c4d/judgement.json
```

The change request is the first one in review order without a judgement that parses. Its parts are served in order, one per call, and `next` only moves forward: the count of the parts served is kept in the file `served` of the change request's directory and never goes back. Once every part was served, `next` repeats one line until the judgement is there, then moves to the next change request:

```text
Write the judgement to <work dir>/runs/<run id>/o_r-3-1a2b3c4d/judgement.json, then run agent-code-review next
```

When every change request has a judgement it prints `Done: run agent-code-review finalize`. `next --part N` prints part N of the change request that is up again, without moving the count; a part it does not have is a usage error (exit 2). `next` never changes the phase of the run, and whether a judgement is valid is for `finalize` to say. Every part served, and every time `next` waits, is a line of `run.log`.

## `run.log`

`runs/<run id>/run.log` is appended by every command that works on the run. One line per event, fields separated by tabs, `-` for a field that does not apply:

```text
2026-11-20T12:00:05Z	review	github:github.com/o/r#7	GET /repos/o/r/pulls/7/files	200	120ms	1	ok
```

The fields are the UTC time, the command, the change request, the operation, the HTTP status, the duration, the attempts and the outcome. The outcome is `ok`, `retry`, `rate-limited`, `error` or `not-verified: <reason>`. A request that is retried has one line per retry and a last line with the total. The path has no query. The log never holds a token, a header, a query string or a response body. It is removed with its run. `--verbose` writes the same lines to stderr as they happen.

## Errors

A failure prints five lines on stderr, the last only when a run exists:

```text
agent-code-review: could not read the root change request github:github.com/o/r#7
  where: github:github.com/o/r#7, change request github:github.com/o/r#7
  why: forbidden (needs pull_requests: read), status 403
  what to do: the token lacks pull_requests: read; add it to the fine-grained token, or run `agent-code-review auth login` to store another
  log: /home/user/.cache/agent-code-review/runs/20261120T120000Z-52fdfc07/run.log
```
