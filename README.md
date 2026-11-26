# agent-code-review

`agent-code-review` is a CLI written in Go and shipped as an agent plugin. It reviews a pull or merge request and, in cascade, every change listed in its `## Related Changes` section, each one with the review criteria of its own repository. It writes a full report per change to a temporary work dir, shows only a summary in chat, and writes to the platform only on explicit request.

Status: under development, not usable yet.

## Commands

The command line is complete for the first stable release:

| Command | What it does | Documentation |
| --- | --- | --- |
| `version` | Prints the version. | |
| `auth login`, `auth status`, `auth logout` | Stores, shows the source of, or removes the token in the git credential helper. | [auth.md](docs/auth.md) |
| `queue [--limit N]` | Lists the pull requests that ask for your review, with the local state of each. | [queue.md](docs/queue.md) |
| `review <pull request>` | Collects a pull request and every change in its Related Changes. | [review-and-next.md](docs/review-and-next.md) |
| `next` | Prints the next part to judge. | [review-and-next.md](docs/review-and-next.md) |
| `finalize` | Validates the judgements and writes the reports. | [finalize-and-show.md](docs/finalize-and-show.md) |
| `show [<cr>]` | Prints the summary of the cascade or a report; `show --save` saves one. | [finalize-and-show.md](docs/finalize-and-show.md) |
| `publish --plan`, `publish --apply` | Plans, then writes to the platform what the user confirmed. | [publish.md](docs/publish.md) |
| `doctor [--repo owner/repo]` | Checks git, the token, the packs, the configuration and the work directory. | [doctor.md](docs/doctor.md) |
| `check-criteria [<dir>]` | Validates the repository criteria of a working copy, offline. | [check-criteria.md](docs/check-criteria.md) |

Every command ends with an exit code that scripts and agents can branch on: [docs/exit-codes.md](docs/exit-codes.md).

## Install the plugin from a local path (development)

The plugin is not released yet. To try it in Claude Code, write the release tree from the sources and install it from that directory:

```bash
go run ./tools/releasetree -out /tmp/acr-tree
```

```bash
claude plugin marketplace add /tmp/acr-tree
```

```bash
claude plugin install agent-code-review@agent-code-review
```

The directory must not exist or must be empty. The tree holds the launchers in `bin/` but no executable yet (`bin/<os>-<arch>/` is filled by the release build): the `agent-code-review` command has to be on your `PATH`, for example from `go build ./cmd/agent-code-review`. Pass `-version <semver>` to put another version in the manifests (the default is `0.0.0-dev`). Run `claude plugin validate /tmp/acr-tree` to check what was written. The generated files are never committed.

## Running the command from an agent

The plugin starts the command through a launcher, `bin/agent-code-review` (Windows: `bin\agent-code-review.cmd`) in the plugin root, which picks the executable for your system and the packs and schemas beside it ([docs/runtime-layout.md](docs/runtime-layout.md)). The skill tells the agent to use it. Claude Code puts the plugin's `bin/` on the `PATH` of its shell; the other hosts do not say where the plugin is, so the agent may not find the launcher. In that case put the command on the `PATH` yourself, in either of two ways:

- add the `bin/` folder of the installed plugin to the `PATH`;
- or install the executable of the GitHub Release: unpack the archive, keep `packs/` and `schemas/` beside the executable, and add that folder to the `PATH`.

Then `agent-code-review doctor` shows the folder the command was started from and where it found the packs and schemas.
