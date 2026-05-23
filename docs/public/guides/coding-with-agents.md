# Code With Agents On Eshu

Eshu is heavily developed with AI agents. Treat the agents as fast pair
programmers that still have to follow the same engineering bar as humans:
accuracy first, performance second, reliability third.

## Start Every Agent Session Here

1. Read the root `AGENTS.md`.
2. Read the nearest scoped `AGENTS.md` in the package or directory you will
   touch.
3. Read the package `README.md` and `doc.go` when they exist.
4. Trace the runtime flow before editing code.
5. Write or update tests before changing behavior.

The root guide is mandatory operating policy, not optional style advice.

## Tell The Agent The Job

Good:

```text
Review go/internal/query for stale docs. Read the code first, then update only
the docs that contradict implementation. Keep diagrams that help future agents
reason about flow.
```

Better:

```text
Fix this reducer bug with TDD. First trace raw facts -> queued work -> reducer
decision -> graph write -> query response. Show the failing test before the
implementation, then run the focused package test.
```

Avoid:

```text
Clean this up.
```

That leaves too much room for speculative edits.

## Required Proof

| Change type | Proof to ask for |
| --- | --- |
| bug fix | failing regression test first, then focused passing test |
| graph/query/correlation truth | fixture intent, reducer graph truth, and API or MCP truth agreement |
| performance-sensitive path | before/after benchmark or runtime measurement plus observability evidence |
| concurrency or queue behavior | idempotency, retry, ordering, contention, and dead-letter proof |
| docs-only change | code/config source checked plus strict docs build when navigation changes |
| Helm or Compose change | render/lint or focused Compose proof |

## Useful Agent Prompts

```text
Act as a first-time engineer. Start at README.md and follow the docs to run,
query, monitor, and debug Eshu. List every point where the path breaks or
requires hidden project knowledge.
```

```text
Read the code and docs for this package. Keep accurate diagrams, delete stale
duplication, and make the README explain what future agents need before editing.
```

```text
Before touching code, map entry point, data input, transformation, persistence,
consumer, transaction boundary, async boundary, retry path, and invariants.
```

```text
Run the smallest verification that proves this change. If it is performance
sensitive, include a no-regression measurement and the exact observability
signals an operator would use.
```

## What Agents Must Not Do

- Do not remove Mermaid diagrams that explain runtime, data, or concurrency
  flow unless you replace them with a clearer and still accurate diagram.
- Do not replace package-local agent rules with vague summaries.
- Do not add AI attribution to commits, PRs, or docs.
- Do not push to `main` or `master`.
- Do not ship serialization as a concurrency fix.
- Do not claim work is ready without listing the commands or runtime proof run.

## Where To Put Documentation

| Need | Location |
| --- | --- |
| first-time user path | `docs/public/getting-started/` |
| product workflow | `docs/public/use/` |
| operator workflow | `docs/public/operate/` |
| deployment workflow | `docs/public/deploy/` |
| durable reference | `docs/public/reference/` |
| package-local implementation truth | package `README.md`, `doc.go`, scoped `AGENTS.md` |
| maintainer-only cleanup notes | `docs/internal/` |

Prefer one canonical human page plus links to deeper references. Do not copy
the same runbook across several pages.
