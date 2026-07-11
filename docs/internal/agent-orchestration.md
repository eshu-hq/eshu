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
| **Shared brain** | `AGENTS.md` (≡ `CLAUDE.md`), `.agents/skills/` | One canon. Each harness points at it; rules are never re-stated per harness. |
| **Role shims** | Per-harness agent configs (e.g. `.opencode/agent/*.md`) | Thin `(role + permissions + prompt)` bundles. No rulebook copies. |

The shared brain is loaded by every harness through its native mechanism:
Claude reads `CLAUDE.md`; Codex and opencode read `AGENTS.md` (plus opencode's
`instructions` array); per-directory `AGENTS.md` files scope rules for Codex.
Skills are symlinked into `.claude/skills/` and `.codex/skills/` and pointed at
by opencode's `skills.paths`.

## Roles, models, and tools

An agent earns its existence when its **tools or permissions** differ — not when
only its prose flavor or model preference differs. Knowledge differences belong
in skills. Model and provider selection is a user/runtime concern: use opencode's
active model, `opencode run --model`, `/models`, `OPENCODE_CONFIG_CONTENT`, or a
personal config directory to override the model without changing tracked role
files. Under that test, the opencode roster is:

| Role | Runtime model binding | Tools | Responsibility |
| --- | --- | --- | --- |
| **Executor** (`develop-eshu`) | user's selected implementation model | full write, **one surface at a time** | Implement one scoped task, TDD-first, run and paste the gates. |
| **Debugger** (`debug-eshu`) | user's selected diagnostic model | **read + run, no write** | Diagnose to root cause. The no-write boxing physically prevents the "fix before you understand" failure mode. |
| **Performance engineer** (`perf-eshu`) | user's selected performance model | **read + run + measure, no write** | Prove bottlenecks and candidate fixes through `eshu-performance-rigor`; routes proven code changes to the executor. Loads [`performance-map.md`](performance-map.md). |
| **Reviewer** (`review-eshu`) | user's selected review model | **read + run, no write** | Run `eshu-code-review` against final diffs, PR updates, and merge-readiness claims. Keeps judgment separate from authorship. |

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

The same override shape works for `develop-eshu`, `debug-eshu`, and
`review-eshu`; change the agent key, not the tracked role file.

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
   baseline manifest, proven hypothesis, exactness/concurrency evidence, target,
   and minimum worthwhile win.

The raw material already exists in the project skills and `eshu-issue-driver`.
Render that spec format on every handoff before dispatching implementation.

## Dispatch

The user or active primary agent delegates through the harness's subagent
mechanism (in opencode, the **Task tool**) or invokes a role directly. The
executor, debugger, performance engineer, and reviewer are leaf agents — they
run as `mode: all` (both directly selectable and dispatchable) and their own
`task` permission is denied, so they cannot dispatch further. Aggregation and
sequencing stay with the user or primary agent.

Routing: implementation → `develop-eshu`; unknown-cause failure → `debug-eshu`
(returns a root cause, then `develop-eshu` implements the fix); bottleneck /
regression / tuning → `perf-eshu` (returns measurements, then any code change
routes to `develop-eshu`); final diff / PR readiness → `review-eshu`. One
surface per dispatch, always with the full handoff contract, sequenced
accuracy-before-performance per the Life Motto.

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

## Token-budget optimization

Push **mechanical** quality down to the automated gates so independent review never
spends tokens catching a lint error or a missing test. The order is:

1. Executor runs local gates + opens a PR.
2. CI must be green (mechanical correctness proven for free).
3. *Only then* run an independent full `eshu-code-review` — for **design**
   judgment on mechanically green code, never as a substitute for local proof.

This maximizes the value of every frontier token: judgment, not janitorial.

## Per-harness wiring

The pattern is identical for every harness — thin config pointing at the shared
brain and the same gate floor:

- **opencode** — `.opencode/opencode.jsonc` (`instructions` → `AGENTS.md`;
  `skills.paths` → `.agents/skills`) + `.opencode/agent/*.md` role/permission
  shims. Tracked shims do not pin personal model choices.
- **Codex** — root + per-directory `AGENTS.md`, `.codex/skills/`,
  `.codex/hooks.json`.
- **Claude Code** — `CLAUDE.md`, `.claude/skills/`.
- **pi / future** — same: an instructions pointer at `AGENTS.md`, a skills
  pointer at `.agents/skills/`, and reliance on the CI floor.

No harness gets a private copy of the rules. New harness = new thin pointer,
nothing more.
