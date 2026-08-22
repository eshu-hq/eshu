# Agent Hooks

Harness hooks that enforce the rules CI structurally cannot reach. Detail for
the hook files under `.claude/hooks/` and `.codex/hooks/`.

CI fires on a diff. These fire on an action, before the diff exists. That is the
whole reason they are here rather than in a workflow, and it matches the canon
rule in [Agent Orchestration Model](agent-orchestration.md): inline only the
boundaries whose ambiguity can mutate state before CI runs, and let CI enforce
everything else.

## Layout

Hooks are a real file per harness, not a shared file with symlinks. The skills
under `.agents/skills/` use symlinks because their content is harness-neutral
prose. Hook payloads are not: Claude Code delivers `tool_input.file_path`, while
Codex's `apply_patch` shape varies by version and names several files at once.
`.codex/hooks/eshu-doc-staleness.sh` handles that by ignoring the payload and
rebuilding the whole snapshot, which only works for a hook that needs no path.

| Hook | Harness | Event | What it does |
|---|---|---|---|
| `eshu-doc-staleness.sh` | Claude, Codex | PostToolUse on edits | Rebuilds the `go/` doc-drift snapshot |
| `skill-nudge.sh` | Claude | PreToolUse on edits | Names the project skill governing the edited path |
| `guard-live-gate.sh` | Claude | PreToolUse on Bash | Blocks a second live gate on the default ports |
| `on-compact.sh` | Claude | SessionStart, compact or resume | Points a re-grounded session at `eshu-session-lifecycle` |

Three of those are Claude-only today. Porting them to Codex needs someone to
pin the `apply_patch` payload shape for the Codex version in use and confirm
Codex's pre-tool and session-start equivalents. Do not write a Codex adapter
against a guessed payload shape; a hook that silently never fires is worse than
an absent one, because the gate reads as covered.

## The nudge table rots, so a gate watches it

`skill-nudge.sh` maps Eshu source paths to Eshu project skills. Every directory
move and every new skill can invalidate an arm, and the failure is silent: the
hook keeps exiting 0 and nobody learns the arm stopped matching.

`scripts/verify-agent-canon.sh` therefore asserts that every skill under
`.agents/skills/` is either assigned by a `SKILL=` case arm or named in the
`NUDGE_EXEMPT` block. The check reads only those two regions, not the whole
file, so a passing mention in an unrelated comment cannot satisfy it.

Skills with no characteristic file path belong in `NUDGE_EXEMPT` with a reason.
A code review runs against a diff, a release against intent, the humanizer
against prose. No `Edit|Write` arm could ever fire for them.

### Arm ordering is load-bearing

`case` takes the first match, so a narrow arm placed below a broad one never
runs. Both mis-routes found when this table moved into the repo were exactly
that: `.github/workflows/security-scan.yml` and
`scripts/verify-telemetry-coverage.sh` each matched a broad arm and nudged
`generator-script-discipline` instead of the skill that owns them.

`scripts/test-agent-hooks.sh` pins both, and pins a control case that fails if
someone repairs a future mis-route by widening a narrow arm rather than
ordering it above the broad one. The suite runs in CI as part of
`verify-agent-hygiene.yml`.

The nudge deliberately stays quiet on ordinary Go files. A hook that fires on
every edit gets ignored, so only specialist surfaces are mapped.

## Installing

The hook files and `.claude/settings.json` are committed, so a checkout of a
branch containing them is already wired. Claude Code reads `settings.json` at
session start: a session already running when the files arrive will not pick
them up until it restarts, and it may ask you to approve the new hook commands
the first time.

Nothing here bypasses `scripts/dev/bootstrap-hooks.sh`, which installs the git
pre-commit and pre-push hooks. The two sets are unrelated: git hooks gate
commits and pushes, agent hooks gate tool calls.

## Escape hatches

`guard-live-gate.sh` blocks a `make pre-pr`, `make pre-pr-full`, or
`verify-golden-corpus-gate` run while a `ci-gates` process is up or port 15432
is bound. Two ways past it, both deliberate:

- Run on an alternate port set. The repo defines 15532, 15635, and 15636 for
  exactly this. A command carrying its own `ESHU_POSTGRES_PORT` is opting into
  a parallel stack and is not blocked.
- Prefix the command with `CLAUDE_HOOK_ALLOW=1` for a single call.

The guard exists because a contended run produces a red that belongs to the
machine rather than to your diff, and reading it as a defect costs more than
the wait did.
