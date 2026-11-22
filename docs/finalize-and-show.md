# `finalize` and `show`

The last two commands of the review. `finalize` checks the judgements the agent wrote, turns them into the findings and the report of each change request, and `show` reads them back. Neither reads from or writes to a platform. The exit codes are in [exit-codes.md](exit-codes.md); the files they read are described in [review-and-next.md](review-and-next.md).

```bash
agent-code-review finalize [--verbose] [--run <run id>]
```

```bash
agent-code-review show [--verbose] [--run <run id>] [<cr>]
agent-code-review show --save [<folder>] [<cr>] [--verbose] [--run <run id>]
```

Both work on the most recent run that is not `published`, or on the run given with `--run`, and append their events to its `run.log`.

## `finalize`

1. Reads the work package and the judgement of every reviewed change request of the run. Skipped entries have neither.
2. Validates every judgement against its work package: the schema, the change request and head, the rules, the files and lines, the signals that need a decision. A finding, new or pre-existing, must be on a line the agent was shown: any line of a file shown whole, the lines of its regions, and the lines of the diff of its side (the removed lines on the left side). A finding on any other line, or a range from `startLine` to `line` that leaves them, is a validation error that names the path and the line. On a file the work package shows no line of (no patch, placed in the summary) every finding that has a line is refused the same way: what could not be seen goes in `notVerified` or `questions`. The signals of the engine on such a file are not affected: they carry their `lineText`. The text fields have limits, in characters: a finding's `title` is one line of at most 120, `why` and `fix` at most 1000 each, a `suggestion` at most 50 lines, each entry of `questions` at most 500, and `notes` stay at 3 of 200 each. Each is a problem with the pointer of the field and the length it has. A judgement that is missing is a problem of the same list: `missing judgement for <cr>, run agent-code-review next`.
3. If any judgement is invalid or missing, **writes nothing** and leaves the phase as it is. It exits 3 and prints every problem, grouped by change request, each with the pointer of its field, so that the agent fixes everything in one round:

```text
agent-code-review: the judgements are not valid, nothing was finalized
  where: 2 change requests
  why: 3 problems
  problems:
    github:github.com/o/lib#3 — <work dir>/runs/<run id>/o_lib-3-1a2b3c4d/judgement.json
      /signals: signal s2 (missing-kdoc at src/A.kt:4) needs judgement: confirm or dismiss it
    github:github.com/o/app#7 — <work dir>/runs/<run id>/o_app-7-5e6f7a8b/judgement.json
      /headSha: judges head 000…, the work package is for head e9009ec0…
      /findings/0/path: nope.kt is not a changed file of this change request
  what to do: fix every problem in the judgement file of its change request, then run `agent-code-review finalize` again
  log: <work dir>/runs/<run id>/run.log
```

4. Otherwise writes `findings.json` and `report.txt` in the directory of each change request, and moves the run from `collected` through `judged` to `finalized`, saving the state once, last. `findings.json` holds the findings with their anchors and fingerprints, the verdict, and the checks and criteria notes copied from the work package; the report is made from `findings.json` alone.
5. Prints the final message: that nothing was written to the platform, the path of every `report.txt` in review order, the summary of the cascade (the overall verdict BLOCKED, INCOMPLETE or OK with its causes, the counts of each change request, and how many dismissed signals had a BLOCKER by default), and the actions that follow. It never prints a full report.

A run that is already finalized (or later) exits 8. To change a judgement after `finalize`, run `review` again.

`finalize` does not check that the change request is still at the commit that was reviewed: every report says `reviewed at head <head8>`, and `publish` checks the head before it writes anything.

The fingerprint of a finding starts from the text of its line, and `finalize` has no file contents: it takes the text from the file contents or the regions of the work package, then from the diff, then, for a signal, from the `lineText` that every signal of the work package carries (the raw text of its line, written by `review` and not shown in the brief). A work package written before `lineText` existed is refused: run `review` again.

Rules and options come from the packs, not from the platform: the work package names the rules, and `finalize` takes their text from the packs it loads. A pack that no longer defines a rule of the work package is a failure (exit 1) that names the rule; run `review` again with the packs it was made with.

## `show`

Requires a run that is `finalized` or later (exit 8 otherwise), and never changes it.

- Without an argument it prints the summary of the cascade again.
- With a change request, given as its key or as `<owner>/<repo>#<number>`, it prints its full report, made again from its `findings.json`. A change request that is not in the run, or was skipped, exits 2.
- With `--save` it writes a report to a new file and prints only its path: the one of the root, which includes the summary of the cascade, or the one of the change request named. The file goes to the folder given or, without one, to `agent-code-review/reports` in the home directory, and is named `<repo>-<number>-<head8>_<yyyy-MM-dd_HHmm>.txt`; if the name is taken, `_2`, `_3` and so on are added. The folder must be outside any git work tree and must not contain `..`, and it must be writable: otherwise exit 9, and nothing is written. An argument that ends in `#<number>` is the change request; any other is the folder.
