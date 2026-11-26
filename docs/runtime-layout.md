# Runtime layout

Where the executable looks for the packs and the JSON Schemas, and what the launchers of the plugin do. This is the single description of the lookup; the other documents link here.

## Three places the files live

| Layout | Where | Executable | Packs and schemas |
| --- | --- | --- | --- |
| GitHub Release archive | the folder the archive is unpacked into | `agent-code-review[.exe]` | `packs/` and `schemas/` beside it |
| `release` branch (the plugin) | the plugin root, which holds `plugin.json` | `bin/<os>-<arch>/agent-code-review[.exe]`, started through the launcher `bin/agent-code-review` or `bin\agent-code-review.cmd` | `packs/` and `schemas/` in the plugin root |
| A checkout of this repository | the repository | `go run` or a build | `ACR_PACKS_DIR=<checkout>/packs`, with `schemas/` beside it |

## Lookup of the packs

In this order; the first of 1 to 3 that holds a pack is the **root**:

1. `ACR_PACKS_DIR`, an absolute path to a folder of packs. When it is set, 2 and 3 are not looked at. Its parent is the root.
2. `packs/` next to the executable, after its symbolic links are resolved. The folder of the executable is the root.
3. `packs/` of the plugin root two levels above the folder of the executable (`bin/<os>-<arch>/` is two levels below it), **only if that folder holds `plugin.json`**. Without `plugin.json` it is just a folder and its packs are not used.
4. Each of `packPaths` of the user configuration. They add packs; they never move the root.

Nothing is looked for further up. A folder counts when a subfolder of it holds a pack manifest; with none found, the command fails and says where it looked.

## Lookup of the schemas

`schemas/` is looked for in the same root as the packs, never elsewhere. `next` prints the absolute path of `schemas/judgement.schema.json` on the line under the one that says where to write the judgement:

```text
Judge github:github.com/o/r#3 part 1 of 2 — write the judgement to <work dir>/runs/<run id>/o_r-3-1a2b3c4d/judgement.json
Judgement schema: /opt/agent-code-review/schemas/judgement.schema.json
```

The same line follows the line `next` repeats while it waits for the judgement. When the schema is not found, the line is left out, nothing else changes, and `doctor` warns (see [doctor.md](doctor.md)). The brief keeps its own line, `Judgement: write <path> as defined by schemas/judgement.schema.json.`

## `doctor`

It prints the folder the executable was started from and the root where the packs and schemas were found, so a wrong installation is visible in one line.

## Launchers

`plugin/bin/agent-code-review` (POSIX `sh`) and `plugin/bin/agent-code-review.cmd` (Windows) are copied to `bin/` of the release tree, the first with the executable bit (git mode `100755`). Each one:

- picks `<os>-<arch>/agent-code-review[.exe]` in the folder it is in, `bin/`, also when started through a symbolic link: `uname -s` and `uname -m` on POSIX (`x86_64` is `amd64`, `aarch64` and `arm64` are `arm64`), `%PROCESSOR_ARCHITECTURE%` and `%PROCESSOR_ARCHITEW6432%` on Windows. The six targets are `linux`, `darwin` and `windows` on `amd64` and `arm64`;
- passes the arguments unchanged and ends with the exit code of the executable;
- on an unsupported system or architecture, or a missing executable, ends with 1 and a message that says what is wrong, why and what to do;
- does nothing else: it sets no environment variable and looks for no pack. The executable finds the packs by the lookup above.

## When the host does not say where the plugin is

Claude Code puts the plugin's `bin/` on the `PATH` of the shell it gives the agent. The other hosts do not document that (see [plugin-formats.md](plugin-formats.md)). The skill tells the agent to run `bin/agent-code-review` of the plugin root, two folders above its own `SKILL.md`, and to use `agent-code-review` when that is on the `PATH`. If the host does not tell the agent where the skill is, put the command on the `PATH` yourself, in either of two ways:

- add the plugin's `bin/` to the `PATH`. The launcher is there, and it finds the executable and the packs beside it;
- or install the executable of the GitHub Release: unpack the archive, so that `packs/` and `schemas/` stay beside the executable, and put that folder on the `PATH`.
