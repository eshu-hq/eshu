# Agent Orchestration Model

This document is the canon for **how Eshu work is executed across multiple
harnesses and models without losing quality.** It applies whether the work
runs in Claude Code, opencode, pi, or any future harness, and whether the
model is a frontier model (Claude) or a cheaper executor (Codex, MiniMax,
DeepSeek, OpenAI).

It exists because Eshu is worked by a **tiered model economy**: frontier
tokens are scarce and reserved for judgment (design and PR review), while
high-volume implementation is delegated to cheaper models. The workflow must
hold the same quality bar regardless of who or what is doing the work.

> This is a design contract, not a tutorial. Harness-specific wiring lives in
> each harness's config; the binding rules live in `AGENTS.md`.

## The one principle

**With weak executors, prose is never the quality mechanism.** A cheaper model
will paraphrase, skim, or ignore parts of any rule file. Quality therefore
comes from two things a model cannot paraphrase away:

1. **The gate floor** — CI (and local hooks) that pass or fail the same way no
   matter who wrote the code. See [Gate Floor](#the-gate-floor).
2. **Scope and tool boxing** — a weak model receives a *narrow, pre-verified
   task* and *restricted tools*, never trust. A coordinator scopes the work; a
   reviewer checks the judgment; the gates catch the mechanics.

Build for the assumption that the executor ignores half the prose. The agent
prose only routes role → model → tools; it is not the thing that protects the
codebase.

## Three layers

| Layer | Artifact | Property |
| --- | --- | --- |
| **Constant floor** | CI workflows (`.github/workflows/`), local hooks | Runs identically for every harness and model. The only truly model-independent guarantee. |
| **Shared brain** | `AGENTS.md` (≡ `CLAUDE.md`), `.agents/skills/` | One canon. Each harness points at it; rules are never re-stated per harness. |
| **Role shims** | Per-harness agent configs (e.g. `.opencode/agent/*.md`) | Thin `(role + model + tools)` bundles. No rulebook copies. |

The shared brain is loaded by every harness through its native mechanism:
Claude reads `CLAUDE.md`; Codex and opencode read `AGENTS.md` (plus opencode's
`instructions` array); per-directory `AGENTS.md` files scope rules for Codex.
Skills are symlinked into `.claude/skills/` and `.codex/skills/` and pointed at
by opencode's `skills.paths`.

## Roles, models, and tools

An agent earns its existence when its **model, tools, or permissions** differ —
not when only its prose flavor differs. Knowledge differences belong in skills.
Under that test, the roster is:

| Role | Tier / model | Tools | Responsibility |
| --- | --- | --- | --- |
| **Coordinator** (`plan-eshu`) | Codex-class | read + run, **no code edits** | Decompose, design, and emit a scoped task spec (see [Handoff Contract](#the-handoff-contract)). Never assigns work it cannot define a gate for. |
| **Executor** (`develop-eshu`) | MiniMax / DeepSeek-class | full write, **one surface at a time** | Implement one scoped task, TDD-first, run and paste the gates. |
| **Debugger** (`debug-eshu`) | any (cheap acceptable) | **read + run, no write** | Diagnose to root cause. The no-write boxing physically prevents the "fix before you understand" failure mode. |
| **Performance engineer** (`perf-eshu`) | Frontier / strong | **read + run + measure, no write** | Find bottlenecks and regressions, tune the graph/storage stack. Measures and recommends; routes code changes to the executor. Loads [`performance-map.md`](performance-map.md). |
| **Reviewer** | Frontier (Claude) | full | Design review and PR review — judgment work. Token-expensive, used sparingly, after the cheap gates are green. |

`ask-eshu` (read-only Q&A) is intentionally **deferred**: it overlaps
opencode's built-in `explore`/`plan` agents and its name collides with Eshu's
own Ask product surface (`POST /api/v0/ask`). Add it only when a distinct need
appears.

The model assignment **is** the tiering. Pinning Codex to the coordinator and
MiniMax/DeepSeek to the executor is the entire reason these are separate
agents rather than one. The reviewer tier is normally a harness choice (run
Claude Code), not an opencode agent.

## The handoff contract

This is where a multi-model pipeline lives or dies. The coordinator must hand
the executor a **machine-followable task spec**, not prose. A loose handoff
makes a weak executor flail; a tight one makes it reliable.

Every coordinator → executor handoff MUST contain:

1. **Surface** — the exact file(s) to touch, one ownership boundary only.
2. **Acceptance test** — the failing test that defines "done" (the TDD seed).
3. **Gate commands** — the exact commands to run and paste before claiming
   done (the relevant subset of [Verification Defaults](#the-gate-floor)).
4. **Out of scope** — explicit boundaries the executor must not cross.
5. **Ownership / parallel-work note** — which other surfaces are active, read
   live (`gh pr list`, `git worktree list`); never hard-coded issue numbers.

The raw material already exists: the `writing-plans` / `executing-plans`
skills and `eshu-issue-driver`. The coordinator's job is to render that spec
format on every handoff.

## Dispatch

The coordinator delegates through the harness's subagent mechanism (in
opencode, the **Task tool**). The executor, debugger, and performance engineer
are leaf agents — they run as `mode: all` (both directly selectable and
dispatchable) and their own `task` permission is denied, so they cannot
dispatch further. Aggregation and sequencing stay with the coordinator.

Routing: implementation → `develop-eshu`; unknown-cause failure → `debug-eshu`
(returns a root cause, then the coordinator dispatches `develop-eshu` to fix);
bottleneck / regression / tuning → `perf-eshu` (returns measurements, then any
code change routes to `develop-eshu`). One surface per dispatch, always with the
full handoff contract, sequenced accuracy-before-performance per the Life Motto.

In opencode the coordinator's reach is pinned with `permission.task` (allowlist
the three leaf agents, deny the rest). Other harnesses use their own delegation
primitive over the same canon.

## The gate floor

The floor is **strong**: the following dimensions are enforced by a blocking CI
gate on every PR, so a defect from any model is caught regardless of its
discipline.

- Go unit tests, the **race detector**, `golangci-lint` (incl. the custom
  500-line file-cap plugin), and `gofumpt` formatting (`test.yml`,
  `race-graph-writes.yml`).
- Structural drift gates: OpenAPI ↔ handler (`verify-openapi.yml`), MCP schema
  + capability inventory (`mcp-schema-drift.yml`), telemetry coverage
  (`verify-telemetry-coverage.yml`), route coverage
  (`verify-route-coverage.yml`), golden-corpus correlation edges
  (`golden-corpus-gate.yml`), contract source-of-truth
  (`contract-source-of-truth.yml`), operator dashboard
  (`generate-operator-dashboard.yml`), skill roundtrip
  (`verify-skill-roundtrip.yml`).
- Security: trivy (fs), gosec, govulncheck, nancy (`security-scan.yml`).
- Docs build `mkdocs --strict`, license headers, whitespace hygiene
  (`test.yml`).
- Frontend typecheck / lint / test / e2e-mock (`frontend.yml`, path-filtered).

**Conclusion: it is safe to let a weak executor produce Go/backend/structural
code**, because CI is the backstop. Two gaps remain where a weak model could
merge a defect with no automated catch — these are exactly the rules a cheaper
model is most likely to break, and they are the next gates to add:

| Gap | Risk | Planned gate |
| --- | --- | --- |
| **`AGENTS.md` ⇄ `CLAUDE.md` lockstep** | The mandated byte-for-byte parity is enforced only by a local pre-commit hook, not CI. A drifted commit merges. | A CI step asserting `diff AGENTS.md CLAUDE.md` is empty. |
| **No-AI-attribution** | No gate at all. Cheaper models routinely add "Generated by …" / `Co-Authored-By` footers. | A CI grep over the diff / commit trailers for AI-attribution markers. |

Until those land, the reviewer (Claude) and the pre-commit hooks are the only
catch for these two dimensions.

## Where rules live

Rules live **once**, in `AGENTS.md` (mirrored byte-identical to `CLAUDE.md`).
Agent files do **not** restate the rulebook — that is the drift hazard, and it
multiplies with every new agent.

There is one deliberate exception, governed by a single rule:

> **Inline only the non-negotiables that have no CI gate. Everything CI
> enforces, let CI enforce.**

CI already hammers `rg`-not-`grep`, the 500-line cap, formatting, and tests, so
those need no inline repetition. The rules CI *cannot* catch — no AI
attribution, the push mechanism, never-push-to-main, worktree discipline,
ask-when-unclear — are the only ones worth inlining in the executor shim, and
only because they are the actual gaps. This keeps the inline set small (less
drift) and focuses weak-model attention exactly where the floor is thin.

## Token-budget optimization

Push **mechanical** quality down to the cheap gates so a frontier review never
spends tokens catching a lint error or a missing test. The order is:

1. Executor runs local gates + opens a PR.
2. CI must be green (mechanical correctness proven for free).
3. *Only then* request a Claude review — for **design** judgment, on
   correct-but-maybe-wrong code, never on broken code.

This maximizes the value of every frontier token: judgment, not janitorial.

## Per-harness wiring

The pattern is identical for every harness — thin config pointing at the shared
brain and the same gate floor:

- **opencode** — `.opencode/opencode.jsonc` (`instructions` → `AGENTS.md`;
  `skills.paths` → `.agents/skills`) + `.opencode/agent/*.md` role shims.
- **Codex** — root + per-directory `AGENTS.md`, `.codex/skills/`,
  `.codex/hooks.json`.
- **Claude Code** — `CLAUDE.md`, `.claude/skills/`.
- **pi / future** — same: an instructions pointer at `AGENTS.md`, a skills
  pointer at `.agents/skills/`, and reliance on the CI floor.

No harness gets a private copy of the rules. New harness = new thin pointer,
nothing more.
