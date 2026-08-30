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
| `goal-continue.sh` | Claude | Stop | **blocking** | Refuses to end the turn while `.claude/active-goal` names unfinished work, and hands the goal back. |
| `goal-refresh.sh` | Claude | UserPromptSubmit | side effect | Sets a goal from `/goal <text>`, and re-injects the active goal into context on every prompt so it cannot go stale. |

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

**Compaction invalidates the markers**, and `on-compact.sh` is what does it. A
resume keeps the same session id while discarding loaded skill content, so
without that step a marker outlives the thing it stands for and the nudge waves
through an edit whose governing skill is no longer in context — the exact
failure the block exists to prevent, with the hook's own message ("having
loaded it earlier does not count") reduced to a lie. It clears only the current
session's markers, and does so before the Eshu scope check, because a stale
marker is wrong whatever repository the compaction happened in.

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

## The Stop hook, and why its escape is checkable

`goal-continue.sh` exists because prose did not fix idling. An agent with a
multi-step goal would finish one step, report, and wait for the owner to type
"continue" — over and over. AGENTS.md said not to; the model drifted past it.
A Stop hook is the enforcement point, because it runs whether or not the model
remembered the rule.

It reads `.claude/active-goal` (or `$CLAUDE_GOAL_FILE`, or `~/.claude/active-goal`)
and, if that file names unfinished work, returns
`{"decision":"block","reason":...}` with the goal text and the three legitimate
reasons to stop: the goal is met, an irreversible act needs consent the owner
has not already given, or the work is blocked on something no local action
clears.

The design problem is that **the agent writes the goal file**. Every escape has
to survive that:

| Escape | Why it is safe to trust |
|---|---|
| `DONE` on the first line | Checkable against the work itself. |
| `BLOCKED: <reason>` | The reason is echoed to stderr, so the owner reads the exact claim rather than only seeing the turn end. |
| `BLOCKED: … WATCH=<pid>` | The hook verifies the process is alive. **A dead watcher REFUSES the stop** — nothing would wake the agent, so waiting is the bug rather than the excuse. |
| `CLAUDE_GOAL_OFF=1` | Owner-side, not agent-side. |
| budget | At most three continuations per `prompt_id`, so a new owner message resets it and a stuck agent cannot spin on one prompt. |

### Consent the owner already gave

The consent reason was unconditional, and that is right exactly once. An owner
who has already said "yes, push" says it into a chat turn; the next Stop hands
the agent the same *an irreversible act needs consent* bullet, and the agent
stops again — on work it had been told to finish. Observed on a six-lane PR
train: the owner typed consent, the turn ended anyway, and the owner typed it
again.

`CONSENT: <acts>` on a line of the goal file is that answer written where the
hook can read it. `/goal consent push, pr-open` writes it, `/goal revoke-consent`
removes it, and `CLAUDE_GOAL_CONSENT` is the launcher-side form for a run whose
permissions are known before it starts. Both hooks read it: the refusal and the
per-turn restatement each name the granted acts and say that asking again for
one of them is not a reason to stop. `CONSENT: all` retires the bullet
outright; a named list narrows it to everything not on the list.

This grants nothing on its own — the permission is the owner's, and the file is
the owner's. What the hook removes is the chance to forget it. The escapes
above are checked *because the agent writes them*; consent is checked by
nobody, because a consent line the agent wrote for itself only ever unblocks
what the owner has to review anyway on the PR. An empty `CONSENT:` grants
nothing, the same way an empty `BLOCKED:` releases nothing.

The general rule, worth applying to any gate with an override: ask who authors
the override value. If it is the party being constrained, attach evidence the
gate can verify itself, and prefer refusing on a failed check over allowing on
an unchecked assertion.

Every parse failure allows the stop — no goal file, an empty one, malformed
JSON, empty stdin, no `python3`. A hook that can refuse to end a session must
never be the reason a session breaks. `scripts/test-goal-continue-hook.sh`
covers all of it, and the `agent-canon` gate runs it.

## Why the goal is restated every turn

The Stop hook alone does not solve idling, and the first version of it proved
that twice over.

It shipped with **no producer**. It read `.claude/active-goal`; nothing wrote
one. Measured on a second machine that had pulled the merge: the hook was
installed, registered, executable, its suite green -- and it had never fired,
on any session, because no goal file existed anywhere. A consumer without a
producer is not a feature, and every gate passed while it did nothing.

The second problem is staleness. A goal set once, forty turns ago, is not a
goal the model is still holding: compaction summarizes it away, and attention
drifts. Setting it is not the same as keeping it.

`goal-refresh.sh` fixes both, on `UserPromptSubmit`:

- **Producer.** A prompt beginning `/goal ` or `GOAL: ` writes the goal file.
  That is the single write path. There is deliberately no fuzzy inference --
  guessing that some sentence was an objective is how a hook starts enforcing
  a goal nobody set. `/goal consent <acts>` and `/goal revoke-consent` edit the
  `CONSENT:` line only; they are matched before the generic producer, because
  read as an objective `/goal consent push` would replace the real goal with
  the word "consent" and grant nothing.
- **Refresher.** On *every* prompt, an active goal is injected back into
  context via `additionalContext`, together with the rule that the owner should
  not be asked what the code, a local doc, a loaded skill, or a Deep-tier
  dispatch can settle. The objective is therefore never more than one turn old,
  and it survives compaction because it is re-added afterwards too.

A `SESSION:` header binds a goal to the session that set it, so a goal cannot
outlive its session and nag the next one about finished work. `/goal done`
retires it. An unheaded file is the owner's own hand-written goal and is
honoured as-is.

This complements the built-in `/goal`, which evaluates whether a condition is
*met*; these hooks keep the objective and the rules *in front of the model*.
They are not alternatives to each other.

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
make an edit on a governed surface and see whether it is refused. Approving the
hook permission prompt is part of activation, and declining it looks exactly
like a broken hook.

**The hook and the skill it names activate by different paths.** A user-level
install runs the hook from an absolute path, so it fires anywhere. The skill
*listing* still comes from the project directory, which for a worktree is the
main checkout. Before these skills reach `main`, a nudge can therefore name a
skill that `Skill(...)` reports as unknown. That is expected pre-merge; treat
the nudge as a pointer rather than a loadable reference until the branch lands.

Diagnosing a hook you think should have fired. When the nudge was advisory this
needed a stamp, because silence was ambiguous — it could mean the hook never
ran or that its output went somewhere you did not look. Blocking removes that
ambiguity for free: **a firing nudge fails your edit**, so you cannot miss it.

The question worth asking now is the opposite one. An edit on a governed
surface that is *not* refused has three explanations, in the order worth
checking:

1. The skill is already loaded. `ls /tmp/claude-skill-loaded-<session>-*` —
   `skill-loaded.sh` writes one marker per skill, and the nudge lifts when
   every id its arm names has one.
2. An override is in force: `/tmp/claude-skill-override-<session>` exists.
3. The hook is not wired. Check `.claude/settings.json` for the
   `(event, matcher, command)` triple, not just the filename —
   `scripts/test-agent-hooks.sh` asserts all four, and a hook attached to the
   wrong event is present, silent, and easy to mistake for absent.

Marker names take the first 12 characters of the session id, so a real one
looks like `claude-skill-loaded-aab79782-96c-golang-engineering`. Scope any
check to your own session id: markers are shared in `/tmp`, and reading another
session's is how a "confirmed" result turns out to be someone else's.

Nothing here bypasses `scripts/dev/bootstrap-hooks.sh`, which installs the git
pre-commit and pre-push hooks. The two sets are unrelated: git hooks gate
commits and pushes, agent hooks gate tool calls.

## Escape hatches

`guard-live-gate.sh` blocks a `make pre-pr`, `make pre-pr-full`, or
`verify-golden-corpus-gate` run while a `ci-gates` process is up or port 15432
is bound. Those are two independent signals, and only one of them can be
waived:

- **Each port override waives only its own probe.** The guard checks Postgres
  15432, Bolt 7687, HTTP 7474, API 18080 and MCP 18091, skipping any whose
  variable the command sets. So `ESHU_POSTGRES_PORT=15532` still gets caught by
  the Bolt probe — moving one port is not a parallel stack, and a blanket
  waiver on one override is how a real collision slipped through an earlier
  revision. Prometheus 19090 and Ask 19191 are omitted deliberately: mock
  providers rather than the contended backend, and each probe costs an `lsof`
  on a hook that runs on every Bash call.
- **A running `ci-gates` process blocks regardless**, and no override reaches
  it. Be precise about what it covers, though: `ci-gates` runs during the
  fast-gate phase only. The live lane is `scripts/verify-golden-corpus-gate.sh`,
  invoked straight from `scripts/dev/pre-pr.sh` with no `ci-gates` process at
  all — so during the phase that actually binds these ports, this check matches
  nothing. The port probes are what cover that, which is why they are per-port.
- **`CLAUDE_HOOK_ALLOW=1`** prefixed on the command waives both, for one call.
  It is the only full override, which is why it is a conscious per-call act
  rather than an environment setting.

The guard exists because a contended run produces a red that belongs to the
machine rather than to your diff, and reading it as a defect costs more than
the wait did.
