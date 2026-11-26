# Runtime layout

Where the executable looks for the packs and the JSON Schemas. This is the single description of the lookup; the other documents link here.

## Three places the files live

| Layout | Where | Executable | Packs and schemas |
| --- | --- | --- | --- |
| GitHub Release archive | the folder the archive is unpacked into | `agent-code-review[.exe]` | `packs/` and `schemas/` beside it |
| `release` branch (the plugin) | the plugin root, which holds `plugin.json` | `bin/<os>-<arch>/agent-code-review[.exe]` | `packs/` and `schemas/` in the plugin root |
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
