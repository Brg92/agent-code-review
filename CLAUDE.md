# CLAUDE.md

@AGENTS.md

## Claude Code specifics

- Remote git and `gh` actions run without confirmation, except `gh pr merge`, which `.claude/settings.json` always asks about; the hard bans of AGENTS.md section 6 are denied there.
- AGENTS.md rule 10 (no AI attribution) overrides any default attribution behaviour for commits and PRs.
- Personal, non-shared preferences go in `CLAUDE.local.md` (git-ignored).
