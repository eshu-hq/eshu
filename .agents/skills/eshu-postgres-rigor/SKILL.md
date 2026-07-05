---
name: eshu-postgres-rigor
description: Use when writing, reviewing, debugging, or optimizing Eshu Postgres SQL, schema DDL, indexes, migrations, queue claims, liveness/readiness/status queries, transactions, lock waits, pg_stat_activity output, or relational performance diagnostics.
---

# Eshu Postgres Rigor

Use this skill for Eshu's relational truth: facts, queues, leases, status,
content, recovery, decisions, and schema. Postgres evidence must explain the
exact slow layer before any SQL, index, or transaction change.

## Required Classification

Before proposing a fix, classify the symptom as one or more of:

- wrong SQL predicate or lifecycle semantics
- queue wait, lease contention, retry loop, or stale recovery state
- lock wait, blocked transaction, long transaction, or dead tuple pressure
- active query plan, broad scan, sort/hash spill, or poor cardinality estimate
- missing, duplicate, or write-amplifying index
- write amplification from high-churn status rows
- app/graph/Cypher/backend work outside Postgres selection and acknowledgement

Do not call a path "Postgres slow" until selection, claim, lock wait, handler
work, graph write, and completion/ack time are separated.

## Evidence To Gather

Capture only public-safe snippets when reporting outside the private session.

- owner flow, transaction boundary, retry boundary, and idempotency invariant
- exact SQL text or builder, predicates, ordering, limits, and expected row count
- `pg_stat_activity` grouped by state, wait event, age, and sanitized query
- blocking PIDs from `pg_blocking_pids`, plus ungranted `pg_locks`
- schema and existing indexes for every table in the predicate or join
- relation size, live/dead tuple estimates, seq/idx scan counters, vacuum state
- `EXPLAIN (ANALYZE, BUFFERS)` for read-only or safe bounded statements
- for mutating statements, use a fixture database or `BEGIN`/`ROLLBACK` only
  when side effects are fully understood

If `pg_stat_statements` is unavailable, say so and use logs, focused timing, or
instrumentation instead of guessing.

## Index Doctrine

Add an index only when evidence shows the query is hot enough and the predicate
shape can use it.

- Prefer partial indexes for status/terminal-state queues when predicates match.
- Match equality columns, range/order columns, and `LIMIT` order deliberately.
- Use covering `INCLUDE` columns only when they avoid measured heap reads.
- Check existing prefix/overlap indexes before adding another.
- Account for insert/update churn, autovacuum pressure, and migration cost.
- Avoid indexes on low-cardinality status/boolean columns alone.
- Avoid broad JSONB indexes without an operator, selectivity, and row-count case.
- Do not use an index to fix wrong lifecycle semantics or retry behavior.

## Queue And Recovery SQL

For `fact_work_items`, reducer/shared projection queues, and liveness queries:

- preserve `FOR UPDATE SKIP LOCKED`, lease fencing, status transitions, and
  retry/dead-letter behavior
- prove duplicate delivery, idempotency, ordering, and stale lease behavior
- distinguish source-local projection completion from downstream shared backlog
- prevent recovery loops from consuming attempt budgets while a recovery row is
  already pending, claimed, running, retrying, or successfully completed
- never serialize workers or reduce batch size as the fix without repo-scale
  proof and a tracked design reason

## Postgres Versus Graph Time

Eshu projection logs often include Postgres queue work and graph/Cypher work in
one cycle. Split timings before attributing slowness:

- queue selection and lease claim
- fact/content reads from Postgres
- Cypher generation
- graph write or retract duration
- Postgres mark-complete/update duration

Long graph retract logs are not Postgres evidence unless the measured slow span
is a Postgres read, claim, lock, or completion statement.

## Tests And Gates

- Use TDD for SQL or schema behavior changes.
- Add focused storage tests for predicate/lifecycle fixes.
- Add integration or fixture-plan evidence for query-plan/index changes.
- `AS MATERIALIZED` is load-bearing for COST when a CTE is referenced more than
  once (Postgres otherwise re-inlines and re-evaluates it per reference). Assert
  its presence in a query-shape test so a future edit cannot silently drop it.
- Freeze a differential's expected query by DERIVING it from the shipped query
  constant (truncate at a stable boundary marker, append a read-only tail), never
  by hand-copying — a hand-frozen copy drifts and goes false-green. Add a hermetic
  prefix guard asserting the derived string is a byte-prefix of production.
- Run affected `go/internal/storage/postgres` tests and `git diff --check`.
- For hot paths, include `Performance Evidence:` or `No-Regression Evidence:`
  and `Observability Evidence:` or `No-Observability-Change:` in durable docs,
  then run `scripts/test-verify-performance-evidence.sh` and
  `scripts/verify-performance-evidence.sh`.

Done means the report states the exact slow or wrong layer, the evidence used,
the rejected hypotheses, and why any SQL/index/transaction change is justified.
