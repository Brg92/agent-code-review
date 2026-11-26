# Plugin formats

What each host needs from the release tree, read from the vendors' own
documentation on 2026-11-26. This file fixes the manifests, agent files and
capability names that the plugin sources and the release tree generator use. Where
a host's documentation is silent it says "not documented"; nothing here is
guessed.

Verification status (D251): Claude Code on Linux and GitHub Copilot in Android
Studio are the verified hosts. Kiro, Codex and Cursor are **not verified**: they
get only what manifest validation needs.

How it was read: pages were fetched through a tool that cuts long pages and
summarises the rest. Facts marked *[summary]* come from such a summary and must be
re-checked before they carry weight. Everything else was read in full.

## 1. What ships, per host

| Host | Status | Manifest | Marketplace | Skill | Agent |
|---|---|---|---|---|---|
| Claude Code | verified | `.claude-plugin/plugin.json` | `.claude-plugin/marketplace.json` | `skills/code-review/SKILL.md` | `agents/code-reviewer.md` |
| GitHub Copilot (JetBrains) | verified at beta | `plugin.json` (Agent Plugins 1.0) | `.claude-plugin/marketplace.json` (4th in its lookup order) | `skills/code-review/SKILL.md` | `com.github.copilot/agents/code-reviewer.agent.md` |
| Kiro | not verified | `plugin.json` (Agent Plugins 1.0) | none | `skills/code-review/SKILL.md` | none |
| Codex | not verified | `plugin.json` (Agent Plugins 1.0) | `.claude-plugin/marketplace.json` (accepted as legacy) | `skills/code-review/SKILL.md` | none |
| Cursor | not verified | `plugin.json` (Agent Plugins 1.0) | none | `skills/code-review/SKILL.md` | none |

Consequences:

- One root `plugin.json` in the Agent Plugins 1.0 format serves Copilot, Kiro,
  Codex and Cursor. Copilot gives it precedence over `.claude-plugin/plugin.json`,
  so the two manifests do not collide.
- One `.claude-plugin/marketplace.json` serves Claude Code and Copilot.
- Skills are the same file everywhere (`SKILL.md`, Agent Skills format).
- Agents are not part of Agent Plugins 1.0 (see section 2), so each host that
  takes an agent in a plugin has its own file. Kiro, Codex and Cursor have no
  documented plugin slot for one; they get the skill only.

## 2. Agent Plugins 1.0

Exists and is published: <https://agent-plugins.org/specification>, source
<https://github.com/agentplugins/agent-plugins-spec/blob/main/spec/1.0.0.md>,
schemas <https://agent-plugins.org/schemas/1.0.0/plugin.schema.json> and
<https://agent-plugins.org/schemas/1.0.0/mcp.schema.json>. A 1.1.0 release also
exists (Copilot CLI accepts both); it was not read. The spec text sets who uses
it: Core Maintainers come from Amazon, Cursor, Microsoft, OpenAI and Vercel.
Claimed by the hosts themselves: GitHub Copilot (CLI reference), VS Code, Kiro,
Cursor, OpenAI Codex. Claude Code does not mention it.

Version 1 covers **skills and MCP only**. Agents and hooks are outside it; a host
puts them in a *client extension directory* named after its reverse-domain
namespace (Copilot: `com.github.copilot/`).

`plugin.json` at the plugin root:

- required: `$schema` (exactly `https://agent-plugins.org/schemas/1.0.0/plugin.schema.json`)
  and `name` (1–64 chars, `a-z 0-9 - .`, starts and ends alphanumeric, no `--`
  or `..`);
- optional: `version` (SemVer recommended), `description`, `author`
  (`name`, `email`, `url`, nothing else), `homepage`, `repository`, `license`,
  `keywords`, `extensions`;
- the schema is closed (`additionalProperties: false`); the spec text says
  unknown top-level fields are only reported, and that the text wins. We emit
  only the fields above, so both readings pass;
- no component path fields: `skills/` is fixed, and `agents`, `hooks`,
  `mcpServers` are not manifest fields.

`mcp.json` at the plugin root (only when MCP is on): required `$schema`
(`.../schemas/1.0.0/mcp.schema.json`, same version as `plugin.json`) and
`mcpServers`. Details in section 8.

## 3. Claude Code

Sources (read 2026-11-26): <https://code.claude.com/docs/en/plugins-reference>,
<https://code.claude.com/docs/en/plugin-marketplaces>,
<https://code.claude.com/docs/en/plugins/components>,
<https://code.claude.com/docs/en/plugins/loading>,
<https://code.claude.com/docs/en/sub-agents>,
<https://code.claude.com/docs/en/skills> (partly *[summary]*),
<https://code.claude.com/docs/en/hooks> (partly *[summary]*).

**Manifest** `.claude-plugin/plugin.json`. Only `name` is required (kebab-case; the
names `claude`, `anthropic`, `claude-code` and anything starting `claude-` or
`anthropic-` are reserved). Fields we use: `name`, `version`, `description`,
`author` (`name`), `homepage`, `repository`, `license`, `keywords`, `agents`.
Component paths must start with `./`, stay inside the plugin root and exist.
`agents` replaces the default `agents/` scan and takes `.md` files, not
directories: we set `"agents": ["./agents/code-reviewer.md"]`. `skills` adds to the
default `skills/` scan, so we leave it out. Unknown top-level keys are stripped
with a warning. Every component lives at the plugin root, never inside
`.claude-plugin/`.

**Marketplace** `.claude-plugin/marketplace.json`. Required: `name`, `owner`
(`owner.name`), `plugins[]`; each entry needs `name` and `source`. We use one
entry with `source: "./"` and a `description`; **no `version` in the entry**
(plugin.json wins and `validate` warns on both). `source: "./"` (the plugin at the
marketplace root) is not shown in the docs' examples; it is checked in M6.
Add and install from a git repository or a local path:

```bash
claude plugin marketplace add <owner>/<repo>          # GitHub
claude plugin marketplace add https://host/team/repo.git
claude plugin marketplace add ./path/to/release-tree  # local, development
claude plugin install agent-code-review@agent-code-review
claude plugin validate ./path/to/release-tree
```

**Skill** `skills/<name>/SKILL.md`; all frontmatter is optional, `description`
recommended; description plus `when_to_use` is cut at 1,536 characters in the
listing *[summary]*. We write only `name` and `description` (the portable subset).

**Agent** `agents/*.md`. Frontmatter: `name` and `description` required; `tools`
(comma list or YAML list; all tools inherited if omitted), `model`, `skills`, and
others. Plugin agents ignore `permissionMode`, `hooks` and `mcpServers`.

- Tool names: `Bash`, `Read`, `Write` (also `Edit`, `Grep`, `Glob`, `Skill`, ...).
- Skill from an agent: **yes**. `skills:` preloads the named skills' full content
  at startup *[summary]*. Whether the plugin-qualified name is needed is checked
  in M6.

**Finding files in the plugin.** `${CLAUDE_PLUGIN_ROOT}` is replaced anywhere in
skill, command and agent Markdown, in hook commands and in MCP server config. It
is **not** set in the environment of Bash commands the agent runs. A plugin
`bin/` directory is **on the Bash tool's PATH** while the plugin is enabled, so
`bin/agent-code-review` runs as a bare command. Installed plugins are copied to
`~/.claude/plugins/cache/<marketplace>/<plugin>/<version>/`.

**Updates.** With `version` set, users stay on that version until the string
changes; without it they track the commit. `claude plugin update
<plugin>@<marketplace>` or `/plugin marketplace update <name>`; auto-update is off
for third-party marketplaces unless the user turns it on. The old version
directory is removed 14 days later.

**Hooks** (record only, M4). `hooks/hooks.json` wraps the event map in a
top-level `hooks` key (a file without it fails to load): `{"hooks": {"PreToolUse":
[{"matcher": "Bash", "hooks": [{"type": "command", "command": "...", "args":
[...], "timeout": 30}]}]}}`. The page lists 34 events, among them `SessionStart`,
`PreToolUse`, `PostToolUse`, `Stop`. A command hook receives the event JSON on
stdin; exit 2 blocks on events that can block; with `args` present no shell is
involved. Hooks get `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PLUGIN_DATA`,
`CLAUDE_PROJECT_DIR`.

**JSON Schema.** None published (`$schema` is accepted for editors, no URL given).
`claude plugin validate <path>` validates a plugin or a marketplace; it needs the
CLI, so it runs locally and in M6, not in the generator's tests.

## 4. GitHub Copilot in JetBrains IDEs (Android Studio)

Sources (read 2026-11-26): docs.github.com pages
[plugins](https://docs.github.com/en/copilot/concepts/customizations/plugins),
[plugin and marketplace reference](https://docs.github.com/en/copilot/reference/plugin-and-marketplace-reference),
[CLI plugin reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-plugin-reference),
[custom agents configuration](https://docs.github.com/en/copilot/reference/custom-agents-configuration),
[agent skills](https://docs.github.com/en/copilot/concepts/customizations/agent-skills),
[hooks reference](https://docs.github.com/en/copilot/reference/hooks-reference),
[MCP in the IDE](https://docs.github.com/en/copilot/how-tos/provide-context/use-mcp/extend-copilot-chat-with-mcp),
[enterprise MCP allowlist](https://docs.github.com/en/copilot/how-tos/administer-copilot/manage-mcp-usage/configure-enterprise-allowlist),
[feature matrix](https://docs.github.com/en/copilot/reference/copilot-feature-matrix).

**Plugins in JetBrains are not documented.** The plugin pages list VS Code, the
Copilot CLI, the Copilot app and the cloud agent. The feature matrix has no
"plugins" or "hooks" row for JetBrains. What it does list for JetBrains: custom
agents (preview), agent skills (preview), custom instructions (preview), MCP
(supported). The developer's legacy plugin was installed from a git repository
marketplace in Android Studio; that experience, not the documentation, is why
this host is kept. Verification: M6 on the company PC.

What the plugin format is, per the CLI reference (the format the IDE is expected
to share):

- **Manifest lookup.** Agent Plugins 1.0 `plugin.json` at the root "takes
  precedence over `.plugin/plugin.json` and `.claude-plugin/plugin.json`"; legacy
  order: `.plugin/plugin.json`, `plugin.json`, `.github/plugin/plugin.json`,
  `.claude-plugin/plugin.json`. Fields we use: those of section 2.
- **Marketplace lookup**, in order: `marketplace.json`,
  `.plugin/marketplace.json`, `.github/plugin/marketplace.json`,
  `.claude-plugin/marketplace.json`. Required: `name`, `owner`, `plugins[]`;
  entry: `name`, `source`. The plugin id is `plugin@marketplace`. CLI: `copilot
  plugin marketplace add owner/repo[#ref] | URL | path`, `copilot plugin install
  plugin@marketplace`, `copilot plugin update`. The settings UI in JetBrains is
  not documented. Installed to `~/.copilot/installed-plugins/<marketplace>/<plugin>`.
- **Skills**: `skills/<name>/SKILL.md` (no root-`SKILL.md` fallback in Agent
  Plugins). Frontmatter: `name`, `description` required, `license`, and
  `allowed-tools`.
- **Agents**: `com.github.copilot/agents/*.agent.md` (the `.agent.md` pattern is
  inferred from the docs' examples; the agent id is the file name without it).
  In JetBrains the documented location is `.github/agents/` in the workspace.
  Frontmatter: `name`, `description` required; `tools`, `model`,
  `disable-model-invocation`, `user-invocable`, `target`, `mcp-servers` and
  `metadata` (the last two are not used by IDE agents).
- **Tool aliases** (case-insensitive): `execute` (= `shell`, `Bash`, `powershell`),
  `read` (= `Read`, `NotebookRead`), `edit` (= `Edit`, `MultiEdit`, `Write`,
  `NotebookEdit`), `search`, `agent`, `web`, `todo`. The docs give the mapping to
  concrete tools for the cloud agent only; the JetBrains mapping is not
  documented.
- **Skill from an agent: not documented.** Agent frontmatter has no skills
  property; skills are chosen by description and "a skill's directory files are
  made available to the agent". This is the fallback case of D254.
- **Finding files in the plugin.** `${PLUGIN_ROOT}` (aliases `${CLAUDE_PLUGIN_ROOT}`,
  `${COPILOT_PLUGIN_ROOT}`) is expanded in stdio MCP `args`, `env` values and
  `cwd`, in LSP config, and in the `mcp-servers` block of a plugin agent. It is
  **not documented for skill or agent bodies**, and there is no documented `bin/`
  on PATH. The skill's "base directory" is mentioned for skill-relative files.
- **Hooks**: documented for the Copilot CLI and cloud agent only, not JetBrains.
  Plugin location `com.github.copilot/hooks/hooks.json`; shape `{"version": 1,
  "hooks": {"preToolUse": [{"type": "command", "bash": "...", "powershell":
  "...", "timeoutSec": 30}]}}`, camelCase events.
- **Updates**: `copilot plugin update`; a marketplace added by the user can opt
  into session-start updates with `autoUpdate`. Nothing is documented for the IDE.
- **JSON Schema**: Agent Plugins 1.0 (section 2). None for the legacy manifest or
  the marketplace file.

## 5. Kiro, Codex, Cursor (not verified)

Only what manifest validation needs: the shared root `plugin.json`, and
`skills/`. Each host says it accepts it.

| Host | Says it accepts the root Agent Plugins `plugin.json` | Source |
|---|---|---|
| Kiro ("Powers") | yes, explicitly | <https://kiro.dev/docs/powers/> |
| Codex | yes, called the portable format ("new packages should use this format") | <https://developers.openai.com/plugins/build/plugins> |
| Cursor | yes, next to its own `.cursor-plugin/plugin.json` format | <https://cursor.com/docs/plugins> |

Where they differ, recorded and **not** emitted:

- Kiro agents live in `.kiro/agents/*.md|json` (`tools: ["read", "write",
  "shell"]`), outside a Power's layout. Install from a repository URL in the Powers
  panel.
- Codex agents are TOML files in `.codex/agents/`; no agent slot in a plugin is
  documented. Its marketplace file `.agents/plugins/marketplace.json` requires
  `policy.installation`, `policy.authentication` and `category` per entry; it also
  reads `.claude-plugin/marketplace.json` as a legacy file, which we do not
  extend. Plugin root: `PLUGIN_ROOT`/`PLUGIN_DATA` environment variables
  (hooks, desktop only). Legacy manifest: `.codex-plugin/plugin.json`.
- Cursor agents: `.cursor/agents/<name>.md` (`name`, `description`, `model`,
  `readonly`, `is_background`); no tool allow-list. Plugin root in MCP config is
  `${CURSOR_PLUGIN_ROOT}`, not the standard's `${PLUGIN_ROOT}`.

## 6. Capability translation (D254)

Agents are written once, neutral, with the capabilities `shell`, `read`, `write`.
The generator turns them into each host's tool names:

| Capability | Claude Code `tools` | Copilot `tools` |
|---|---|---|
| `shell` | `Bash` | `execute` |
| `read` | `Read` | `read` |
| `write` | `Write` | `edit` |

Only these two hosts get an agent file. The table is the single place that grows
when a host that takes plugin agents is added.

## 7. Questions asked in the brief

**Can an agent load or reference a skill?**
Claude Code: yes, `skills:` preloads it. Copilot: not documented, skills are
selected by description (fallback case of D254). Kiro, Codex, Cursor: not
applicable, no agent is shipped.

**How does the agent find files inside the installed plugin?**
Claude Code: `${CLAUDE_PLUGIN_ROOT}` in skill and agent text; `bin/` is on PATH.
Copilot: `${PLUGIN_ROOT}` only in MCP/LSP config; skill base directory; no
documented `bin/` PATH. The launcher lookup is M2's problem; this is the
constraint it starts from.

**Which tool names map to `shell`, `read`, `write`?** Section 6.

**How is an MCP server declared, and is it blocked?** Section 8. Policy:
Copilot's "MCP servers in Copilot" policy is **off by default for Copilot Business
and Enterprise** and does not govern Free, Pro, Pro+ and Max; the JetBrains
prerequisites say it must be enabled for Business and Enterprise members. On a
company PC MCP is therefore probably blocked until an administrator enables it.
This agrees with D257 (no MCP in 1.0.0). Claude Code's own MCP policy was not
researched; it is not needed while MCP stays off.

**Which paths must be added to `docs/release-allowlist.txt`?** None. Every path in
section 1 is already covered by `plugin.json`, `.claude-plugin/**`,
`com.github.copilot/**`, `skills/**`, `agents/**`, `packs/**`, `schemas/**`,
`bin/**`, `LICENSE` and `README.md`; `hooks/**` is already there for M4. `mcp.json`
and `.mcp.json` are **not** added (D257, D63); neither are `.codex-plugin/`,
`.cursor-plugin/`, `.kiro/`.

## 8. MCP declaration (record only, D257)

Off by default; never emitted in 1.0.0. The generator can emit it from a test
input. The server name and the command below are placeholders.

- Claude Code: inline in `plugin.json`, or `.mcp.json` at the root:
  `"mcpServers": {"<name>": {"command": "${CLAUDE_PLUGIN_ROOT}/bin/<launcher>",
  "args": ["mcp"], "env": {}}}`. The generator uses the inline field. A `url`
  entry without `type` is read as stdio and fails. The full stdio table was read
  only as a *[summary]* (`command`, `args`, `env`, `type`, `timeout`).
- Agent Plugins hosts (Copilot, Kiro, Codex, Cursor): root `mcp.json`, no
  manifest field: `{"$schema": ".../schemas/1.0.0/mcp.schema.json", "mcpServers":
  {"<name>": {"type": "stdio", "command": "./bin/<launcher>", "args": ["mcp"]}}}`.
  `command` is one bare executable name or a `./` path relative to the plugin
  root, never a shell string, no placeholder in it; `args`, `env` values and `cwd`
  accept `${PLUGIN_ROOT}` and `${PLUGIN_DATA}`. Cursor says to write
  `${CURSOR_PLUGIN_ROOT}` instead.
- Copilot in the IDE (user config, not the plugin): `{"servers": {"<name>":
  {"command": "...", "args": [...]}}}`. Not documented: whether the IDE reads a
  plugin's `mcp.json`.
- Policy: see section 7.

## 9. Validation we can run

- Agent Plugins `plugin.json` and `mcp.json`: official schemas exist. The standard
  library has no JSON Schema validator, so a test checks the schema's constraints
  directly (required keys, closed objects, `$schema` constant, `name` pattern,
  `author` keys, server `type` and `command` rules) against what the generator
  writes.
- Claude Code: `claude plugin validate` (local and M6).
- Copilot legacy manifest, marketplace file, Kiro, Codex, Cursor: no schema;
  structural checks only (required fields from this file, referenced paths
  exist).

## 10. Where this differs from the plan

1. **Copilot plugins in JetBrains are not documented** (section 4). The plan
   assumed the git-repository marketplace works in Android Studio; that rests on
   the legacy plugin having been installed that way, and is verified only in M6.
2. **Agent Plugins 1.0 has no agents or hooks.** The plan's "Agent Plugins 1.0
   manifests" cover the manifest and skills; Copilot's agent and hooks go in
   `com.github.copilot/`, Claude Code's in `agents/` and `hooks/`.
3. **Only two hosts take an agent in a plugin** (Claude Code, Copilot). Kiro,
   Codex and Cursor get the shared `plugin.json` and the skill. The planned test
   "every capability has a translation for every host" becomes "for every host
   that gets an agent".
4. **No separate manifests for Kiro, Codex, Cursor** were needed: all three accept
   the root `plugin.json`.
5. **The marketplace file is shared** between Claude Code and Copilot, which
   removes one manifest from the plan.
6. **Copilot has no documented skill reference for agents**, so it is the D254
   fallback case from the start.
7. **No `bin/` on PATH outside Claude Code** is documented; this is a constraint
   for M2.

## 11. Open points to settle before the generator is written

- Kiro, Codex and Cursor ship the skill only, with no agent file (section 5).
- Schema checks are hand-coded in a test, not a vendored schema copy (section 9).
- `source: "./"` and `owner.name` in the marketplace file are checked with
  `claude plugin validate` and a local install once the generator exists.
- Plugin-qualified skill name for `skills:` in the Claude agent is checked in M6.
