# agent-code-review

`agent-code-review` is a CLI written in Go and shipped as an agent plugin. It reviews a pull or merge request and, in cascade, every change listed in its `## Related Changes` section, each one with the review criteria of its own repository. It writes a full report per change to a temporary work dir, shows only a summary in chat, and writes to the platform only on explicit request.

Status: under development, not usable yet.

## Commands

The command line is complete for the first stable release:

| Command | What it does | Documentation |
| --- | --- | --- |
| `version` | Prints the version. | |
| `auth login`, `auth status`, `auth logout` | Stores, shows the source of, or removes the token in the git credential helper. | |
| `queue [--limit N]` | Lists the pull requests that ask for your review, with the local state of each. | [queue.md](docs/queue.md) |
| `review <pull request>` | Collects a pull request and every change in its Related Changes. | [review-and-next.md](docs/review-and-next.md) |
| `next` | Prints the next part to judge. | [review-and-next.md](docs/review-and-next.md) |
| `finalize` | Validates the judgements and writes the reports. | [finalize-and-show.md](docs/finalize-and-show.md) |
| `show [<cr>]` | Prints the summary of the cascade or a report; `show --save` saves one. | [finalize-and-show.md](docs/finalize-and-show.md) |
| `publish --plan`, `publish --apply` | Plans, then writes to the platform what the user confirmed. | [publish.md](docs/publish.md) |
| `doctor [--repo owner/repo]` | Checks git, the token, the packs, the configuration and the work directory. | [doctor.md](docs/doctor.md) |
| `check-criteria [<dir>]` | Validates the repository criteria of a working copy, offline. | [check-criteria.md](docs/check-criteria.md) |

Every command ends with an exit code that scripts and agents can branch on: [docs/exit-codes.md](docs/exit-codes.md).
