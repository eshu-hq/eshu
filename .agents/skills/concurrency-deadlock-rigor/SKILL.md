---
name: concurrency-deadlock-rigor
description: Use when designing, debugging, refactoring, or reviewing Eshu workers, queues, leases, transactions, retries, batching, fan-out/fan-in, shared state, lock ordering, database writes, or distributed coordination where deadlocks, races, contention, starvation, or unsafe overlap may exist.
---

# Concurrency And Deadlock Rigor

Use this skill when correctness depends on ordering, isolation, coordination, or
shared-state access. Do not change concurrent behavior from intuition.

## Mandatory Reasoning

Before proposing or implementing a non-trivial change, MUST identify:

- workflow stages and entrypoint
- shared state and conflict domains
- transaction boundaries
- retry boundaries
- lock, claim, or write ordering
- idempotency keys
- deadlock, race, starvation, duplication, and stale-work hazards
- observability that reveals contention and progress
- verification that proves the unsafe overlap is gone

## Workflow

1. Draw the sequence. Include producers, queues, leases, workers, graph/Postgres
   writes, retries, acknowledgements, and downstream consumers.
2. List contested resources at the resource level, not just service level:
   tables, rows, keys, graph nodes/relationships, leases, files, caches, or
   external APIs.
3. Separate transaction scope from retry scope. Name what can repeat and what
   must be idempotent.
4. Analyze ordering. Write down the bad interleaving when a deadlock or race is
   plausible.
5. Check actual system semantics. MUST use official docs or source for database,
   queue, or runtime guarantees when they affect the design.
6. Prefer fixes that remove unsafe overlap by design: readiness gates,
   ownership separation, conflict-domain partitioning, durable coordination, or
   explicit sequencing.
7. Preserve useful concurrency. Lower worker counts, smaller batches, longer
   timeouts, and more retries are diagnostics or temporary mitigations unless
   evidence proves they are the right architecture.

## Replay And Retry Matrix

For queue, recovery, replay, or dead-letter changes, tests MUST cover every
owned dispatch variant, not one representative query or handler. Include:

- duplicate delivery of already-succeeded work
- stale state replay after partial projection
- retry preserving transient failures without overwriting terminal state
- dead-letter replay returning to the intended queue state
- concurrent workers touching the same conflict domain
- empty or already-drained queue state

## Observability

Concurrency-sensitive paths MUST expose enough telemetry to answer:

- what is blocked or retrying
- which conflict domain is hot
- how long claims, transactions, and retries take
- whether stale or duplicate work committed
- whether progress resumed after coordination

## Repo Enforcement

Concurrency changes are not complete with only a safety argument. If the diff
touches workers, leases, queues, batching, channels, goroutines, retry loops, or
claim behavior, run:

```bash
scripts/test-verify-performance-evidence.sh
scripts/verify-performance-evidence.sh
```

The PR must include a tracked evidence note with one of `Performance Evidence:`,
`Benchmark Evidence:`, or `No-Regression Evidence:` plus either
`Observability Evidence:` or `No-Observability-Change:`. The note must name the
conflict domain, worker/lease settings, before/after queue or row counts, and
the metric/span/log/status signal that shows progress, blocking, retries, and
failure class.

## Done Criteria

- The actual conflict domain is named.
- Transaction and retry scopes are separated.
- The fix removes unsafe overlap rather than only reducing its frequency.
- Useful unrelated concurrency is preserved where possible.
- Tests, load checks, or failure injections prove the design.
- Remaining risk is stated plainly.
