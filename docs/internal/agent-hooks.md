# Agent Hooks

Harness hooks that reach the failures CI structurally cannot. Detail for the
hook files under `.claude/hooks/` and `.codex/hooks/`.

Two of them block and the rest only speak. The tier column below says which,
and it is set by the exit code — do not assume a hook stops anything without
checking it there.

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

| Hook | Harness | Event | Tier | What it does |
|---|---|---|---|---|
| `eshu-doc-staleness.sh` | Claude, Codex | PostToolUse on edits | side effect | Rebuilds the `go/` doc-drift snapshot |
| `skill-nudge.sh` | Claude | PreToolUse on edits | **blocking** | Refuses an edit until the governing skill is loaded |
| `skill-loaded.sh` | Claude | PostToolUse on `Skill` | side effect | Records which skills were loaded this session |
| `guard-live-gate.sh` | Claude | PreToolUse on Bash | **blocking** | Blocks a second live gate on the default ports |
| `on-compact.sh` | Claude | SessionStart, compact or resume | advisory | Points a re-grounded session at `eshu-session-lifecycle` |

## Why the nudge blocks instead of suggesting

The tier is set by the exit code, and the difference is total. Exit 0 with
`additionalContext` puts a sentence in front of the agent and nothing more.
Exit 2 fails the tool call.

`skill-nudge.sh` started out advisory, and there is a measurement of what that
bought. In the session that first observed these hooks firing, the nudge fired
three times and the agent loaded zero of the named skills. One of the three was
genuinely unloadable; the other two were available and simply not acted on. An
advisory hook is prose delivered by a hook — the same rung of the
prose → skill → hook ladder the repo already found insufficient, just with
better timing.

It now exits 2, and the requirement is **loading, not acknowledgement**.
`skill-loaded.sh` records each `Skill` invocation as
`/tmp/claude-skill-loaded-<session>-<id>`, and the nudge lifts only when every
id its arm names has a marker. Retrying without loading is refused again, and a
multi-skill arm names only the ids still missing. Blocking once and then waving
everything through would be acknowledgement theatre, so the suite asserts the
repeat refusal directly.

Verified live, in that order, against `go/cmd/api/main.go`: edit refused with
exit 2 naming `golang-engineering`; `Skill(golang-engineering)` invoked;
marker `claude-skill-loaded-<session>-golang-engineering` written; the same
edit then passed with exit 0. The `Skill` payload puts the name at
`tool_input.skill`, read off a real invocation rather than assumed.

**Escape hatch.** A skill can be genuinely unloadable — most often when it lives
on a branch the session's project directory cannot see, which is the activation
split below. A permanent block on editing a whole surface is worse than the rule
it enforces, so `touch /tmp/claude-skill-override-<session>` lifts it for the
session. Nothing relaxes automatically: an auto-degrade would be a silent
fallback hiding exactly the case worth knowing about.

**Ordering.** `IDS=` holds the enforced skill ids and `NOTE=` the human half.
They are separate so that guidance prose cannot mint a skill id, and so
`verify-agent-canon.sh` can match on the enforced list alone.

Four of those are Claude-only today. Porting them to Codex needs someone to
pin the `apply_patch` payload shape for the Codex version in use and confirm
Codex's pre-tool and session-start equivalents. Do not write a Codex adapter
against a guessed payload shape; a hook that silently never fires is worse than
an absent one, because the gate reads as covered.

## The nudge table rots, so a gate watches it

`skill-nudge.sh` maps Eshu source paths to Eshu project skills. Every directory
move and every new skill can invalidate an arm, and the failure is silent: the
hook keeps exiting 0 and nobody learns the arm stopped matching.

`scripts/verify-agent-canon.sh` therefore asserts that every skill under
`.agents/skills/` is either assigned by an `IDS=` case arm or named in the
`NUDGE_EXEMPT` block. The check reads only those two regions, not the whole
file, so a passing mention in an unrelated comment cannot satisfy it — and it
reads `IDS=` rather than `NOTE=` so that guidance prose cannot mint an id.

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

The last arm is a `*.go` fallback to `golang-engineering`, and its position is
part of the contract. The specialist arms above it claim particular surfaces;
the fallback catches the rest of the Go tree, which nothing else covered —
`go/cmd/ci-gates`, `go/cmd/api`, `go/cmd/reducer`, `go/cmd/ingester`, and
`go/cmd/bootstrap-index` all produced no nudge at all before it existed. Hoisted
above the specialist arms it swallows `doc.go` and every Go surface arm, so
`test-agent-hooks.sh` pins three paths that flip to `golang-engineering` if
anyone reorders the case.

The fallback is bounded because the requirement is per `(session, skill)`, not
per edit: load `golang-engineering` once and every later Go edit in that session
passes untouched. Non-Go paths nobody claims stay silent, and that stays
deliberate — a hook that fires on everything gets ignored.

## Installing

The hook files and `.claude/settings.json` are committed, so once they are on
`main` any session rooted at the repo picks them up. Two rules decide whether
they actually load, and both were learned by watching them not load.

**Settings come from the session root, not the working directory.** Claude Code
reads project settings from the directory the session started in, and expands
`${CLAUDE_PROJECT_DIR}` to that same directory. A session rooted at the main
checkout that later moves into a worktree still uses the main checkout's
settings and hook paths. If the hooks exist only on a branch checked out in that
worktree, none of them register: the settings file that loaded never mentioned
them, and the paths it does mention resolve into a tree where they are absent.

Starting the session inside the worktree does not fix this, which cost two
attempts to learn: a session rooted at `.worktrees/<name>` still reported
`${CLAUDE_PROJECT_DIR}` as the main checkout. Project-scoped wiring therefore
cannot activate an unmerged hook from any worktree.

The scope that does work before merge is a **user-level install** in
`~/.claude/settings.json`, pointing at `~/.claude/hooks/eshu-hook.sh <name>`.
That dispatcher resolves the real script main-checkout-first and then the
worktree, and exits 0 quietly when it finds neither — so it keeps working after
the branch merges and the worktree is pruned, which a direct path into
`.worktrees/` does not: `bash` on a missing script exits 127 and prints on every
matching tool call, in every repository, starting the moment nobody is thinking
about hook wiring any more.

Note the consequence for iterating: a worktree session runs the **main
checkout's** copy of each hook, so a branch that edits a hook still executes
main's version. Script *content* is read at invocation, though, so editing a
hook the dispatcher already resolves takes effect immediately — only
`settings.json` changes need a reload.

**Settings are read at session start, and a resume counts.** A session already
running when the files arrive will not pick them up. Restarting the app and
resuming the same conversation does reload them, keeping the same
`CLAUDE_CODE_SESSION_ID` — that is how these hooks were first observed firing,
after four null results built on the assumption that only a brand-new
conversation could load them. Do not plan a hook test around that assumption;
check the stamp instead. Approving the hook permission prompt is part of
activation, and declining it looks exactly like a broken hook.

**The hook and the skill it names activate by different paths.** A user-level
install runs the hook from an absolute path, so it fires anywhere. The skill
*listing* still comes from the project directory, which for a worktree is the
main checkout. Before these skills reach `main`, a nudge can therefore name a
skill that `Skill(...)` reports as unknown. That is expected pre-merge; treat
the nudge as a pointer rather than a loadable reference until the branch lands.

Diagnosing a hook you think should have fired: `skill-nudge.sh` touches
`/tmp/claude-nudge-<session>-<skill>` **before** it prints. A missing stamp means
the hook never executed; a stamp with no visible reminder means it ran and the
output went somewhere you did not look. That distinction is the difference
between a wiring problem and a logic problem, so check it before editing
anything.

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
