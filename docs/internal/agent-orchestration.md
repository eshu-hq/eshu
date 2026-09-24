# Agent Orchestration Model

This document is the canon for **how Eshu work is executed across multiple
harnesses and models without losing quality.** It applies whether the work
runs in Claude Code, Codex, opencode, pi, or any future harness, and regardless
of provider, model family, or reasoning tier.

It exists because Eshu is worked through a **tiered model economy**: expensive
reasoning is reserved for decisions that need it, while bounded implementation
can use an execution-focused model. The workflow must hold the same quality bar
regardless of provider or price tier.

> This is a design contract, not a tutorial. Harness-specific wiring lives in
> each harness's config; the binding rules live in `AGENTS.md`.

## The one principle

**With execution-focused agents, prose is never the quality mechanism.** A model
will paraphrase, skim, or ignore parts of any rule file. Quality therefore
comes from two things a model cannot paraphrase away:

1. **The gate floor** — CI (and local hooks) that pass or fail the same way no
   matter who wrote the code. See [Gate Floor](#the-gate-floor).
2. **Scope and tool boxing** — a weak model receives a *narrow, pre-verified
   task* and *restricted tools*, never trust. The user or active primary agent
   scopes the work; a reviewer checks the judgment; the gates catch the mechanics.

Build for the assumption that an executor may miss half the prose. The agent
prose only routes role → permissions → tools; it is not the thing that protects
the codebase.

## Three layers

| Layer | Artifact | Property |
| --- | --- | --- |
| **Constant floor** | CI workflows (`.github/workflows/`), local hooks | Runs identically for every harness and model. The only truly model-independent guarantee. |
| **Shared brain** | `AGENTS.md` (≡ `CLAUDE.md`), `.agents/skills/`, `.agents/roles.json` | One rule and method canon, plus one role/model manifest. |
| **Role shims** | Per-harness agent configs (`.opencode/agent/*.md`, `.claude/agents/*.md`, `.codex/agents/*.toml`) and Codex/Muse launchers | Thin `(role + permissions + model)` bundles. No rulebook copies — the method lives in the skill they load. |

The shared brain is loaded by every harness through its native mechanism:
Claude reads `CLAUDE.md`; Codex and opencode read `AGENTS.md` (plus opencode's
`instructions` array); per-directory `AGENTS.md` files scope rules for Codex.
Skills are symlinked into `.claude/skills/` and `.codex/skills/`, pointed at
by opencode's `skills.paths`, and discovered directly by Muse from `.agents/skills/`.

## Roles, models, and tools

An agent earns its existence when it has a distinct **job or tool boundary**.
Knowledge differences belong in skills. The `-deep` variants are explicit
escalation points for the same job and skill body, so the coordinator can choose
a stronger model without copying the method. OpenCode model and provider
selection remains a user/runtime concern: use opencode's
active model, `opencode run --model`, `/models`, `OPENCODE_CONFIG_CONTENT`, or a
personal config directory to override the model without changing tracked role
files. Under that test, the opencode roster is:

| Role | Runtime model binding | Tools | Responsibility |
| --- | --- | --- | --- |
| **Scanner** (`scan-eshu`) | user's selected fast model | read-only | Gather bounded evidence and return a cited handoff. |
| **Executor** (`develop-eshu`) | user's selected implementation model | full write, **one surface at a time** | Implement one scoped task and run focused proof; the coordinator owns promotion and publication. |
| **Debugger** (`debug-eshu`) | user's selected diagnostic model | **read, no write** | Diagnose to root cause from available observations; request command results from the coordinator when the harness blocks shell execution. |
| **Performance engineer** (`perf-eshu`) | user's selected performance model | **read, no write** | Analyze measurements through `eshu-performance-rigor`; route proven code changes to the executor. Loads [`performance-map.md`](performance-map.md). |
| **Reviewer** (`review-eshu`) | user's selected review model | **read, no write** | Run `eshu-code-review` against final diffs and PR evidence. Keeps judgment separate from authorship. |

`debug-eshu-deep` and `perf-eshu-deep` inherit their base role's method and
access but use the deep tier. The manifest is the only place to change their
model choices or shared role instructions.

Concurrency is a conditional method, not a separate job: the debugger,
performance agent, developer, or reviewer loads `concurrency-deadlock-rigor`
when the surface involves races, leases, queues, or shared-state ordering.

`ask-eshu` (read-only Q&A) is intentionally **deferred**: it overlaps
opencode's built-in `explore`/`plan` agents and its name collides with Eshu's
own Ask product surface (`POST /api/v0/ask`). Add it only when a distinct need
appears.

The repo does not pin personal model economics into tracked opencode role files.
If a task needs a stronger or cheaper provider, choose it in the opencode
session or with a higher-precedence local override. opencode does not provide
credit-aware automatic provider failover; when a provider is exhausted, switch
the active model or restart with another override.

Examples, with placeholder model IDs that must be replaced by `opencode models`
output from the local machine:

```bash
OPENCODE_CONFIG_CONTENT='{"agent":{"perf-eshu":{"model":"openai/<gpt-5.6-sol-id>","variant":"high"}}}' opencode
OPENCODE_CONFIG_CONTENT='{"agent":{"perf-eshu":{"model":"anthropic/<opus-4.8-id>","variant":"high"}}}' opencode
OPENCODE_CONFIG_CONTENT='{"agent":{"perf-eshu":{"model":"deepseek/<deepseek-pro-id>","variant":"high"}}}' opencode
```

The same override shape works for any tracked OpenCode role; change the agent
key, not the tracked role file.

### Default model bindings by tier

The tracked choices live in `.agents/roles.json`; the table below describes
the routing rule. Match model capability to task difficulty, then pick the
role for the harness in play:

| Tier | Reach for it when | Role examples |
| --- | --- | --- |
| **Deep** | difficult cross-system root cause, architecture, intermittent performance | `debug-eshu-deep`, `perf-eshu-deep` |
| **Workhorse** | default implementation, diagnosis, performance, review | `develop-eshu`, `debug-eshu`, `perf-eshu`, `review-eshu` |
| **Fast** | bounded evidence scans and lookups | `scan-eshu` |

**Kimi K3** sits outside the ladder — it has no cheaper variant to downshift to,
so run it **always at high effort**, reached for deliberately as a strong
cross-family workhorse or as an independent verifier in adversarial-verification
passes (a different model lineage catches what a single family rationalizes
away).

The manifest currently maps these tiers to Claude Haiku/Sonnet/Opus, Codex
Luna/Terra/Sol, and Muse Spark at low/high/xhigh effort. A caller's explicit
model override can still win per task or session. The coordinator selects the
tier when it dispatches; a leaf agent does not downgrade its own model.
Muse currently uses one model across the tiers, so its savings come from
reasoning effort and bounded scopes rather than selecting a cheaper model.

### Goal and skill prompts

The normal entry point is a goal that names project skills. The user does not
need to name a role or invoke `agent-roles.py`. The three project
`UserPromptSubmit` hooks run `scripts/goal-role-router.py` for explicit
`/goal` or `GOAL:` prompts with named Eshu skills. It reads
`.agents/roles.json` and injects candidate phase roles with model, effort,
and access into the coordinator's context. The hook does not launch a worker;
an explicit `/goal goal.txt` or `goal.md` reads up to 64 KiB from the
workspace or the matching Claude session scratchpad. The Claude/Muse goal
refresher stores its contents for later turns.
The coordinator owns the goal and loads its skills. At each bounded phase it
checks the actual work against the candidates and applies the selected role's
model, effort, access, and instructions when delegating. A long issue goal can use
`scan-eshu` for evidence, `debug-eshu` for an unknown cause, `develop-eshu` for
a proved fix, and `review-eshu` for an independent final diff. It does not
assign the entire goal to the first matching role.

Skill names describe methods, not automatic model switches. For example,
`eshu-issue-driver` stays with the coordinator, `eshu-diagnostic-rigor` helps
identify a diagnosis phase, and `concurrency-deadlock-rigor` refines the worker
handling a race or lease. The coordinator preserves explicit phase ordering,
ownership, and model choices in the goal. Small coupled work can stay in the
main session; its selected model remains unchanged. Model savings come from
bounded child work routed to the manifest tier.
OpenCode is deliberately session-selected and is outside this manifest's
model bindings.

### Where the model binds

A tier is repo policy; the binding is per-harness and lives on the **role**, not
on the skill. The canonical skill body stays one byte-identical file behind
the Claude and Codex discovery links. `scripts/agent-roles.py generate` renders
the native role shims from `.agents/roles.json`, including OpenCode's permission
frontmatter. OpenCode read roles have shell execution denied, so a coordinator
supplies any command output their proof needs.
`scripts/agent-roles.py check` is part of `verify-agent-canon.sh`, so a stale
binding fails locally and in CI.

| Harness | Role artifact | Model binding | Read-only boxing |
| --- | --- | --- | --- |
| Claude Code | `.claude/agents/*.md` | `model:` and `effort:` in the role frontmatter | withheld `Edit`/`Write` tools |
| opencode | `.opencode/agent/*.md` | deliberately unpinned; chosen per session | `permission.edit/write/bash: deny` for every read role |
| Codex | `.codex/agents/*.toml`; `scripts/agent-roles.py codex-exec ROLE TASK` when custom-role selection is unavailable | `model` and `model_reasoning_effort` in the role file or launcher arguments | `sandbox_mode = "read-only"` in the role file; `--sandbox read-only` in the launcher |
| Muse Code | `scripts/agent-roles.py muse-exec ROLE TASK` | `--model` and `--reasoning-effort` from the manifest | `--permission-profile :read-only` for read roles |

Muse's launcher runs one role as a headless session, rather than registering a
native subagent. Its read-only profile may prevent a diagnostic or performance
role from running a proof that writes local artifacts. OpenCode's read roles
cannot run shell commands. In either case, the coordinator should run blocked
proof separately and pass the result back. When a Muse goal and skill prompt
calls for a bounded role, the coordinator selects the manifest tier and either
uses its native child tool with that configuration or invokes `muse-exec`
itself; the user does not run the launcher.

Codex custom role files bind models only when the active spawn tool can select
the named role. The tested Codex 0.156.1 CLI/app schema exposes a task
name and optional model override, but no custom-role selector. A child merely
named `debug_eshu_deep` inherits its parent's model; that name does not load
`debug-eshu-deep.toml`. The coordinator must pass the manifest's model, effort,
access, and instructions explicitly to a native child, or invoke
`scripts/agent-roles.py codex-exec ROLE TASK` itself. The user still supplies
only the goal and skills. The launcher starts a separate headless Codex session
with the model, effort, role instructions, and sandbox read from
`.agents/roles.json`; it is not a spawned child of the coordinator. Check the
CLI startup banner for the resolved model and effort. Do not report task-name
dispatch as role routing.

The reviewer uses the same skill in all four:
[`.claude/agents/review-eshu.md`](../../.claude/agents/review-eshu.md),
[`.opencode/agent/review-eshu.md`](../../.opencode/agent/review-eshu.md), and
[`.codex/agents/review-eshu.toml`](../../.codex/agents/review-eshu.toml) —
tracked bindings running the same `eshu-code-review` skill. Muse loads that
skill through workspace discovery when the launcher passes `--trust-workspace`.

Codex discovers a role file from each config layer's `<config_folder>/agents/`
directory. The repo's own layer is the `.codex/` folder at the checkout root
(the same layer `.codex/config.toml` already uses), so a role committed there is
project-scoped and tracked.

**A directory-discovered role file must define a non-blank
`developer_instructions`.** That path parses with `role_name_hint: None`
(`codex-rs/agent-roles/src/loader.rs:303`), which sets `require_present` on the
validator (`agent_role_config.rs:67-71`, `:134-157`). A role missing the field
does not fail loudly: it is logged as a startup warning and **silently dropped**
(`loader.rs:305-308`), so its `model` never binds and the role simply is not
there. Treat a Codex role that appears to do nothing as this defect until
proven otherwise. The field is also where a Codex role's prose belongs — it is
the counterpart of the Markdown body in the Claude and opencode twins. That layer is **disabled while the checkout is
untrusted**: add the worktree path under `[projects."<path>"] trust_level =
"trusted"` in `~/.codex/config.toml`, and note that trust is keyed by absolute
path, so a second checkout of the same repo needs its own entry. A
`[profiles.<name>]` in `~/.codex/config.toml` remains the way to run a whole
Codex *session* at a chosen tier; the role file binds a spawned reviewer only
when the spawn tool offers a custom-role selector.

Codex's `read-only` preset gates internet access behind approval as well as
writes, and a reviewer needs the network for the live GitHub truth the skill
requires. A user-level `approval_policy = "never"` returns that call as a
failure rather than prompting, so the role sets `approval_policy =
"on-request"`. That does not widen the write boundary: read-only still refuses
edits, and an approval prompt for one is the signal that the reviewer is doing
something it should not.

The Claude and Codex roles pin a model while the opencode role stays unpinned.
That is not an inconsistency: Workhorse is the tier this repo already declares
for review in the table above, so those roles transcribe repo policy rather than
one contributor's economics. opencode stays unpinned because its per-session
override path is the documented one and costs nothing to use; the other two have
no equivalent per-session role override, so an unpinned role there would
silently review on whatever the caller happens to be.

## The handoff contract

This is where a multi-model pipeline lives or dies. The user, built-in planning
agent, or any other coordinator must hand the executor a **machine-followable
task spec**, not prose. A loose handoff makes an executor flail; a tight one
makes it reliable.

Every implementation handoff MUST contain:

1. **Surface** — the exact file(s) to touch, one ownership boundary only.
2. **Acceptance test** — the failing test that defines "done" (the TDD seed).
3. **Gate commands** — the exact commands to run and paste before claiming
   done (the relevant subset of [Verification Defaults](#the-gate-floor)).
4. **Out of scope** — explicit boundaries the executor must not cross.
5. **Ownership / parallel-work note** — which other surfaces are active, read
   live (`gh pr list`, `git worktree list`); never hard-coded issue numbers.
6. **Performance packet when applicable** — primary metric boundaries,
   baseline manifest, proven hypothesis, exactness/concurrency evidence, current
   total, target gap, candidate-stage seconds, maximum/expected recoverable
   seconds, minimum worthwhile win, measured resource envelope, reference
   profile, and absolute-target applicability.

The raw material already exists in the project skills and `eshu-issue-driver`.
Render that spec format on every handoff before dispatching implementation.

## Dispatch

The user or active primary agent delegates through the harness's subagent
mechanism (in opencode, the **Task tool**) or invokes a role directly. The
executor, debugger, performance engineer, and reviewer are leaf agents — they
run as `mode: all` (both directly selectable and dispatchable) and their own
`task` permission is denied, so they cannot dispatch further. Aggregation and
sequencing stay with the user or primary agent.

Routing: bounded lookup → `scan-eshu`; implementation → `develop-eshu`; unknown-cause failure → `debug-eshu`
(returns a root cause, then `develop-eshu` implements the fix); bottleneck /
regression / tuning → `perf-eshu` (returns measurements, then any code change
routes to `develop-eshu`); final diff / PR readiness → `review-eshu`. One
surface per dispatch, always with the full handoff contract, sequenced
accuracy-before-performance per the Life Motto.

For performance investigation and debugging, the coordinator MUST use subagents
or role agents when the work can be safely split into bounded lanes: live issue
state, caller inventory, old/new query-shape proof, test discovery, and log or
CI review are good splits. Do not delegate implementation until the cheap proof
has proven the theory and the handoff packet names the exact surface, failing
test, gate commands, required project skills, and out-of-scope boundary.

Judgment-heavy performance and debugging work MUST escalate to
`perf-eshu-deep` or `debug-eshu-deep`. If its mapped model is unavailable, use
the nearest high-reasoning model and record the substitution in the handoff. Long waits,
build polling, and GitHub bookkeeping remain coordinator or script work, not
frontier-model work.

Commit before dispatching any subagent. A subagent scoped as read-only has
still reverted a production fix while probing whether a test was tautological
— the reviewer's own tools were not the boundary that mattered, and a
committed HEAD was the only reason the fix was recoverable. Never hand a
subagent a working tree the dispatcher cannot restore.

Re-verify a subagent's findings before acting on them, and before relaying
them to another agent as verified. A subagent's report is a claim, not
evidence: the dispatcher checks it against the source, the diff, or a rerun
before treating it as settled, and never forwards it as "confirmed" on the
strength of the subagent's own confidence.

While a dispatched agent is in flight, check its liveness at most every 60
seconds. Liveness means its process is still running, it has produced new
commits, or the harness reports it active — never a file's mtime, which a
thinking agent leaves untouched for long stretches while still working.

## The gate floor

The floor is **strong**: the following dimensions are enforced by a blocking CI
gate on every PR, so a defect from any model is caught regardless of its
discipline.

- Go unit tests, the **race detector**, `golangci-lint` (incl. the custom
  500-line file-cap plugin), and `gofumpt` formatting (`test.yml`,
  `race-graph-writes.yml`).
- Structural drift gates: OpenAPI ↔ handler, telemetry coverage, route
  coverage, contract source-of-truth, operator dashboard, and skill roundtrip
  now run as dynamic path-filtered gates inside `static-contract-gates.yml`,
  alongside MCP schema + capability inventory (`mcp-schema-drift.yml`) and
  golden-corpus correlation edges (`golden-corpus-gate.yml`).
- Security: trivy (fs), gosec, govulncheck, nancy (`security-scan.yml`).
- Docs build `mkdocs --strict`, license headers, whitespace hygiene
  (`test.yml`).
- Frontend typecheck / lint / test / e2e-mock (`frontend.yml`, path-filtered).

CI is the mechanical backstop, not proof that an implementation is
architecturally or semantically correct. `verify-agent-hygiene.yml` now blocks
root-canon drift, AI attribution, missing shared-skill discovery links, and
OpenCode Git-policy contradictions. The mandatory independent
`eshu-code-review` remains the judgment gate for final diffs.

## Where rules live

Rules live **once**, in `AGENTS.md` (mirrored byte-identical to `CLAUDE.md`).
Agent files do **not** restate the rulebook — that is the drift hazard, and it
multiplies with every new agent.

There is one deliberate exception, governed by a single rule:

> **Inline only role boundaries or actions whose ambiguity can mutate external
> state before CI runs. Everything CI enforces, let CI enforce.**

CI already hammers `rg`-not-`grep`, the 500-line cap, formatting, tests, root
canon, skill discovery, and attribution, so those need no inline repetition.
Push target/transport, worktree discipline, external writes, and
ask-when-unclear remain worth inlining because a wrong action happens before
CI can reject it.

The same test decides what belongs in a harness hook. A hook fires on an action,
before a diff exists, which is the only way to reach a failure CI cannot see —
a skill nobody loaded, two live gates contending for one port, a session that
came back from compaction without its rules. [Agent Hooks](agent-hooks.md)
covers the files, why they are per-harness rather than symlinked, and the gate
that keeps the skill-nudge table from rotting.

## Token-budget optimization

Use focused local proof for discovery and reserve the expensive promotion gate
for a branch that has already survived design review. The order is:

1. Executor completes TDD implementation and focused local proof.
2. Run a preliminary full `eshu-code-review` on the final rebased diff. Fix every
   P0, P1, and blocking P2 finding and repeat until the verdict is
   `P0=0, P1=0, P2-blocking=0`, with every deferred P2 tracked in a linked
   issue with the owner's agreement quoted in the PR, and named there with its
   severity-table category.
3. Capture a `ci-gates review-attest` receipt for the clean preliminary review.
4. Only when the branch is otherwise ready to push, run `make pre-push` once
   (add `make pre-pr` for the risky change classes in CLAUDE.md).
5. Verify the receipt against the exact post-preflight inputs. A match replaces
   a duplicate full semantic review. Any changed base, diff, worktree, claims,
   packet, or verdict invalidates it and restarts the affected proof and review.
   A deferred P2 is already tracked and a P3 is cosmetic; neither does.
6. Push the reviewed diff, open or update the PR, then use CI and external
   reviews as authoritative post-push gates. No edit may occur between the final
   clean review and push.

This maximizes the value of every frontier token: judgment, not janitorial.

For performance work, the strongest diagnostic model stops after it localizes
the bottleneck, proves or rejects the theory, interprets the evidence, and
writes the implementation packet. An execution-focused model owns bounded TDD
implementation. Scripts or the coordinator own builds, remote/CI polling,
GitHub bookkeeping, and cleanup. Long waits are not frontier-model work.

## Per-harness wiring

The pattern is identical for every harness — thin config pointing at the shared
brain and the same gate floor:

- **opencode** — `.opencode/opencode.jsonc` (`instructions` → `AGENTS.md`;
  `skills.paths` → `.agents/skills`) + `.opencode/agent/*.md` role/permission
  shims. Tracked shims do not pin personal model choices.
- **Codex** — root + per-directory `AGENTS.md`, `.codex/skills/`,
  `.codex/agents/*.toml`, `.codex/hooks.json`.
- **Claude Code** — `CLAUDE.md`, `.claude/skills/`, `.claude/agents/*.md`.
- **Muse Code** — `AGENTS.md`, project skills in `.agents/skills/`, and
  `python3 scripts/agent-roles.py muse-exec ROLE 'task'` for model and permission
  routing. `--dry-run` prints the resolved invocation without starting a model.
- **pi / future** — same: an instructions pointer at `AGENTS.md`, a skills
  pointer at `.agents/skills/`, and reliance on the CI floor.

No harness gets a private copy of the rules. New harness = new thin pointer,
nothing more.
