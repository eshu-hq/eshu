# ADR: Neo4j Deadlock Elimination via Code-Entity Keyspace Readiness

**Status:** Accepted  
**Date:** 2026-04-17  
**Supersedes:** Previous deadlock assessment (`61132c4b`) and the earlier
batch-isolation draft that treated this as a code-call batching problem

## Status Review (2026-05-03)

**Current disposition:** Complete.

The durable readiness contract, repair queue, and bounded code-entity write
shape have been absorbed into the reducer and storage contracts. Later ADRs
still tune reducer throughput, but they no longer reopen this deadlock decision.

**Remaining work:** none for this ADR. Future deadlock or graph-write
contention work belongs in a new, evidence-backed ADR or in the active NornicDB
throughput track.

## Decision

Adopt a **durable graph-readiness contract** for the `uid`-keyed code-entity
keyspace, gate edge projection on that readiness, and harden the contract with
an **exact repair queue** plus **smaller managed code-call transaction groups**.

For a given bounded unit:

- `scope_id`
- `acceptance_unit_id` (repository)
- `source_run_id`
- `generation_id`
- `keyspace = code_entities_uid`

the system will enforce this phase order:

1. `canonical_nodes_committed`
2. `semantic_nodes_committed`
3. `code_entity_edges_committed`

`code_calls`, `inheritance_edges`, and `sql_relationships` become eligible only
after the matching `semantic_nodes_committed` phase exists for the same bounded
unit and generation.

This is the recommended path because it removes the observed deadlock class by
construction while preserving concurrency across different repositories and
bounded units. It does **not** introduce a global single-threaded Neo4j writer.

## Why This ADR Exists

The deadlocks are real, deterministic, and expensive:

- the same `INDEX_ENTRY` lock IDs recur across runs
- the failures cluster around `parser/code-calls`
- a single repository can spend minutes in `DeadlockDetected` /
  `TransactionExecutionLimit` churn

This is not acceptable to leave as a retry tax or "tune later" issue.

The earlier batch-isolation framing was useful as a symptom analysis, but it
did not fully identify the architectural fault line. Reducing code-call batch
size may lower contention, but it does not explain or eliminate why the
conflicting transactions are allowed to overlap in the first place.

## Verified Findings

### 1. Neo4j says frequent deadlocks indicate a concurrent write pattern problem

The Neo4j Operations Manual states that frequent deadlocks mean concurrent write
requests are happening in a way that cannot satisfy isolation and consistency at
the same time. Neo4j documents two relevant remedies:

- make updates happen in the same order
- make sure concurrent transactions do not perform conflicting writes to the
  same nodes or relationships

Source:
https://neo4j.com/docs/operations-manual/current/database-internals/concurrent-data-access/

This is the core architectural signal: the system must either guarantee a safe
order or remove the overlap.

### 2. `MERGE` is not a reliable lock-ordering primitive

Neo4j documents that `MERGE` takes locks out of order to enforce uniqueness.
That means client-side sorting is not a deadlock elimination strategy for
workloads built around `MERGE`.

Source:
https://neo4j.com/docs/operations-manual/current/database-internals/concurrent-data-access/

### 3. Auto-commit `Session.Run()` does not get driver retry semantics

The Go driver documentation is explicit:

- managed transactions are retried on transient failures
- implicit / auto-commit transactions run through `Session.Run()`
- implicit transactions are **not** automatically retried

Sources:

- https://neo4j.com/docs/go-manual/current/transactions/
- https://neo4j.com/docs/go-manual/current/query-advanced/
- https://neo4j.com/docs/go-manual/current/performance/

This matters because the previous batch-isolation idea assumed per-batch
`Execute()` would preserve retry semantics. That is false on the reducer path as
currently wired.

### 4. The current system models freshness, not graph-write readiness

`shared_projection_acceptance` stores the authoritative generation per bounded
unit:

- `go/internal/storage/postgres/shared_projection_acceptance.go:16`
- `go/internal/storage/postgres/accepted_generation.go:10`
- `go/internal/reducer/shared_projection_worker.go:127`

`CodeCallIntentWriter` upserts shared intents and acceptance rows together at
intent-emission time:

- `go/internal/storage/postgres/code_call_intent_writer.go:13`
- `go/internal/storage/postgres/code_call_intent_writer.go:41`

`CodeCallProjectionRunner` treats "accepted generation matches" as sufficient
eligibility:

- `go/internal/reducer/code_call_projection_runner.go:173`
- `go/internal/reducer/code_call_projection_runner.go:187`

So the system currently says:

`facts are current` => `edge projection may start`

But that is not the same as:

`the graph keyspace is safe for edge projection`

### 5. The conflicting keyspace is the `uid`-keyed code-entity space

The shared lock domain is the uniqueness-constrained `uid` keyspace for code
entities:

- `go/internal/graph/schema.go:99`

Current producers touching that keyspace:

- canonical node projection MERGEs `Function`, `Class`, and other entity labels
  by `uid`
  - `go/internal/storage/neo4j/canonical_node_cypher.go:96`
  - `go/internal/storage/neo4j/canonical_node_writer.go:47`
- semantic entity materialization MERGEs semantic `Function`, `Module`, and
  related nodes by `uid`
  - `go/internal/storage/neo4j/semantic_entity.go:210`
  - `go/internal/storage/neo4j/semantic_entity.go:269`

Current consumers touching that same keyspace:

- code calls
  - `go/internal/storage/neo4j/canonical.go:159`
- inheritance edges
  - `go/internal/storage/neo4j/canonical.go:186`
- SQL relationship edges
  - `go/internal/storage/neo4j/canonical.go:213`

These consumers `MATCH` nodes by `uid` and then `MERGE` relationships. The
observed deadlock class appears when those reads and edge writes overlap with
producer transactions that are still acquiring `uid` uniqueness locks.

### 6. The normal projector path already encodes one important dependency

The projector writes canonical nodes before enqueuing reducer intents:

- `go/internal/projector/runtime.go:109`
- `go/internal/projector/runtime.go:161`

That means the normal path already treats canonical projection as an upstream
phase. The problem is that this phase ordering is not modeled durably for the
downstream graph consumers that share the same keyspace.

### 7. `semantic_entity_materialization` is the unguarded overlap

`semantic_entity_materialization` loads facts and writes semantic nodes directly
through an atomic Neo4j grouped transaction:

- `go/internal/reducer/semantic_entity_materialization.go:45`
- `go/internal/storage/neo4j/semantic_entity.go:269`

`code_call_materialization`, by contrast, does not write edges directly. It
emits shared intents and immediately advances bounded-unit acceptance:

- `go/internal/reducer/code_call_materialization.go:31`
- `go/internal/storage/postgres/code_call_intent_writer.go:41`

This is the missing contract:

- semantic node writes are still running for a repo / source run / generation
- edge consumers become eligible anyway because acceptance already says "current"

That is the architectural bug.

## Root Cause

The root cause is **not** "the code-call batch is too large."

The root cause is:

> The platform lacks a durable phase-readiness contract for the `uid`-keyed
> code-entity graph keyspace, so edge consumers can start while a node-producing
> writer for the same bounded unit is still holding uniqueness locks.

More concretely:

- `shared_projection_acceptance` answers "which generation is authoritative?"
- it does **not** answer "which graph-write phases have committed?"
- code-entity edge consumers read `uid`-constrained nodes
- semantic node writers create or enrich those same `uid`-constrained nodes
- the two are allowed to overlap for the same repo / source run / generation

That is exactly the kind of concurrent-write pattern Neo4j identifies as a
deadlock source.

## Scope of Elimination

This ADR is specifically about eliminating the observed deadlock class on the
code-entity `uid` keyspace.

It is **not** a promise that no Neo4j deadlock of any kind can ever happen in
the system. It is a design that removes the currently observed producer /
consumer deadlock class by construction.

Once the recommended architecture is in place:

- node producers for the `code_entities_uid` keyspace do not overlap with
  edge consumers for the same bounded unit and generation
- the observed `INDEX_ENTRY` cycle between `MERGE`-driven node upserts and
  `MATCH` / relationship `MERGE` consumers is no longer possible

## Implemented Architecture

### 1. Durable keyspace phase state lives in Postgres

The implementation adds a dedicated readiness table:

- `schema/data-plane/postgres/012_graph_projection_phase_state.sql`
- `go/internal/storage/postgres/graph_projection_phase_state.go`

Concrete key:

- `scope_id`
- `acceptance_unit_id`
- `source_run_id`
- `generation_id`
- `keyspace`
- `phase`

Concrete payload:

- `committed_at`
- `updated_at`

Concrete reducer-side contract:

- `GraphProjectionKeyspace`
- `GraphProjectionPhase`
- `GraphProjectionPhaseKey`
- `GraphProjectionPhaseState`
- `GraphProjectionReadinessLookup`
- `GraphProjectionReadinessPrefetch`
- `GraphProjectionPhasePublisher`

Concrete values implemented for this deadlock class:

- `keyspace = code_entities_uid`
- `phase = canonical_nodes_committed`
- `phase = semantic_nodes_committed`

Why a new table instead of overloading `shared_projection_acceptance`:

- acceptance still expresses freshness only
- phase state now expresses graph-write readiness only
- the two contracts are separate in both schema and code

### 2. Projector completion publishes canonical readiness

The projector now publishes `canonical_nodes_committed` after successful
canonical writes in:

- `go/internal/projector/runtime.go`
- `go/cmd/projector/runtime_wiring.go`

The publication is scoped to the bounded unit plus
`keyspace = code_entities_uid` and happens only after canonical node commit
success. That makes the existing projector ordering durable and queryable
instead of implicit.

### 3. Semantic completion publishes semantic readiness

The reducer now publishes `semantic_nodes_committed` after successful semantic
entity writes in:

- `go/internal/reducer/semantic_entity_materialization.go`
- `go/internal/reducer/defaults.go`
- `go/cmd/reducer/main.go`

This is the concrete readiness signal that code-entity edge consumers now use.

### 4. Publish failures enqueue exact readiness repairs

The hardening pass adds a second durability layer for the case where the graph
write succeeded but the readiness publication failed afterward:

- `schema/data-plane/postgres/013_graph_projection_phase_repair_queue.sql`
- `go/internal/storage/postgres/graph_projection_phase_repair_queue.go`
- `go/internal/reducer/graph_projection_phase_repair_runner.go`

Concrete behavior:

- projector canonical publish failure enqueues the exact
  `canonical_nodes_committed` repair row
- semantic publish failure enqueues the exact
  `semantic_nodes_committed` repair row
- the reducer runs a bounded repair loop that:
  - lists due repair rows
  - skips stale generations
  - skips rows whose readiness already exists
  - republishes only the exact missing readiness row
  - backs off failed republishes without dropping the row

This keeps readiness recovery precise. The repairer does not scan the world or
speculate. It only retries a publication that was already proven true by a
successful upstream graph write.

### 5. Code-entity edge consumers are gated on semantic readiness

The gating now happens at work-selection time, before Neo4j retract/upsert
work starts:

- `go/internal/reducer/code_call_projection_runner.go`
- `go/internal/reducer/shared_projection_worker.go`
- `go/internal/reducer/shared_projection_runner.go`

Concrete behavior:

- `CodeCallProjectionRunner` requires accepted generation plus matching
  `semantic_nodes_committed` readiness for `code_calls`
- `SharedProjectionRunner` applies the same readiness gate only for
  `inheritance_edges` and `sql_relationships`
- `platform_infra`, `repo_dependency`, and `workload_dependency` remain
  ungated
- blocked units stay pending and are not marked completed just because
  readiness is missing
- the selectors keep scanning for other ready work deeper in the slice, so the
  fix does not collapse concurrency to one unit at a time

### 6. Preserve concurrency by scoping the gate to bounded units

This is not a global serializer.

Concurrency is preserved because:

- different repositories still progress independently
- different source runs still progress independently
- unrelated domains still run independently
- only the same code-entity keyspace for the same bounded unit is phase-gated

In other words:

- **same repo / same run / same generation / same keyspace:** ordered
- **different repo or different run:** concurrent

That is exactly the concurrency pattern Neo4j recommends: no conflicting writes
to the same nodes or relationships across concurrent transactions.

## Why This Eliminates the Observed Deadlock

The observed cycle requires both of these conditions:

1. a producer transaction is still acquiring or holding `uid` uniqueness locks
2. an edge consumer starts reading and writing against that same `uid` keyspace

The new contract removes condition 2.

If the edge consumer cannot start until the producer has durably published
`semantic_nodes_committed`, then for the same bounded unit:

- there is no overlap window
- there is no AB/BA lock cycle between producer and consumer
- there is no need to rely on luck, retries, or smaller batches

That is elimination by construction for this deadlock class.

## Alternatives Considered

### A. Smaller code-call batches in separate transactions

Rejected as the primary design, adopted only as a scoped secondary
optimization after readiness gating.

Why:

- reduces lock footprint but does not remove the overlap
- changes correctness semantics to partial edge visibility
- on the current reducer path, `Execute()` maps to auto-commit `Session.Run()`,
  which does not get driver-managed retries
- the repo's own Neo4j research notes there is no authoritative batch-size
  guidance that proves deadlock elimination

This is now implemented as a **secondary** safeguard in the reducer edge
writer:

- `DomainCodeCalls` uses a smaller row batch size than other domains
- `DomainCodeCalls` also splits grouped Neo4j writes into smaller
  `ExecuteGroup(...)` chunks
- grouped managed transactions are preserved, so we keep retry semantics
- the reducer emits `eshu_dp_code_call_edge_batches_total` and
  `eshu_dp_code_call_edge_batch_duration_seconds` so operators can observe the
  isolated batch path directly

It reduces lock footprint and tail latency, but it is still not the elimination
strategy. Readiness gating remains the correctness contract.

### B. Global Neo4j write serializer

Rejected.

Why:

- eliminates deadlocks by brute force
- destroys throughput and multi-repo concurrency
- violates the requirement to preserve concurrency

### C. Client-side UID sorting

Rejected.

Why:

- Neo4j documents that `MERGE` takes locks out of order
- sorting input rows does not guarantee actual lock order

### D. Freshness-only gating with no graph readiness

Rejected.

Why:

- this is the current bug
- freshness is not graph-write safety

## Implementation Reality

### Schema and store

The readiness contract is implemented by:

- `schema/data-plane/postgres/012_graph_projection_phase_state.sql`
- `schema/data-plane/postgres/013_graph_projection_phase_repair_queue.sql`
- `go/internal/storage/postgres/graph_projection_phase_state.go`
- `go/internal/storage/postgres/graph_projection_phase_repair_queue.go`
- `go/internal/storage/postgres/schema.go`

The Postgres store now provides:

- durable upsert through `GraphProjectionPhaseStateStore`
- exact lookup through `Lookup`
- cycle-local batching through `NewGraphProjectionReadinessPrefetch`
- direct runner lookup through `NewGraphProjectionReadinessLookup`
- exact repair queue persistence through `GraphProjectionPhaseRepairQueueStore`

### Runtime telemetry

The reducer now emits dedicated `code_call` batch-isolation telemetry:

- `eshu_dp_code_call_edge_batches_total`
- `eshu_dp_code_call_edge_batch_duration_seconds`

These signals are emitted from the Neo4j edge writer when isolated `code_call`
batches execute, so staging and production can validate that the deadlock fix
is active and measure its latency envelope directly.

### Runtime publications

Canonical readiness publication is wired through the projector runtime:

- `go/internal/projector/runtime.go`
- `go/cmd/projector/runtime_wiring.go`

Semantic readiness publication is wired through reducer handler defaults and
the semantic materialization path:

- `go/internal/reducer/semantic_entity_materialization.go`
- `go/internal/reducer/defaults.go`
- `go/cmd/reducer/main.go`

When either publication fails after a successful graph write, the same runtime
path now enqueues an exact repair row instead of leaving the bounded unit
blocked forever.

### Reducer-side repair

The reducer now owns a dedicated repair loop:

- `go/internal/reducer/graph_projection_phase_repair_runner.go`
- `go/cmd/reducer/main.go`
- `go/internal/reducer/service.go`

This loop is intentionally bounded. It consumes queued repair rows rather than
scanning accepted generations or rebuilding readiness from inference.

### Runner gating

Readiness-aware selection is implemented in:

- `go/internal/reducer/code_call_projection_runner.go`
- `go/internal/reducer/shared_projection_worker.go`
- `go/internal/reducer/shared_projection_runner.go`

### Telemetry

The hardened implementation adds structured logs for:

- bounded units skipped because semantic readiness is still missing
- exact readiness publication failures that were queued for repair
- exact readiness rows successfully repaired
- repair cycles with non-zero repaired row counts

This keeps observability tied to the same bounded-unit contract instead of
inventing a generic second workflow layer.

### Code-call write-path optimization

The reducer Neo4j edge writer now applies smaller managed transaction groups to
`DomainCodeCalls` only:

- `go/internal/storage/neo4j/edge_writer.go`
- `go/internal/storage/neo4j/edge_writer_test.go`
- `go/cmd/reducer/config.go`
- `go/cmd/reducer/main.go`

Concrete runtime defaults:

- `CodeCallBatchSize = 50`
- `CodeCallGroupBatchSize = 1`

That means a large code-call slice is still executed through managed
`ExecuteGroup(...)` retries, but each grouped transaction holds a much smaller
lock footprint than the old "all statements in one grouped transaction" shape.

## Verification and Exit Criteria

The elimination bar is not "seems better." It is all of the following.

### Unit and package verification

At minimum, for the code paths touched:

- `cd go && go test ./cmd/projector ./cmd/reducer ./internal/projector ./internal/reducer ./internal/storage/postgres ./internal/storage/neo4j -count=1`
- `cd go && go vet ./internal/reducer ./internal/storage/postgres ./internal/projector`
- docs gate from the local testing runbook:
  - `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`

### New correctness tests

Add tests that prove:

- code-call acceptance can exist before semantic readiness
- code-call runner does not select that bounded unit until semantic readiness is
  present
- inheritance and SQL relationship runners behave the same way
- semantic completion for generation `G` does not unlock edge work for a
  different generation
- readiness is scoped by `scope_id + acceptance_unit_id + source_run_id`
- failed canonical readiness publication enqueues an exact repair row
- failed semantic readiness publication enqueues an exact repair row
- reducer repair republishes missing readiness and deletes stale queued rows
- reducer repair backs off failed republishes without dropping the repair row
- code-call edge writes use smaller managed grouped transactions than other
  domains

### Contention proof

Add an integration / stress proof that intentionally stretches the semantic
writer transaction while code-call intents are pending for the same repo and
generation.

Expected result:

- edge projection does not start early
- no `DeadlockDetected`
- no `TransactionExecutionLimit`

### Elimination acceptance criteria

The ADR is considered validated only when we have:

1. `0` observed `DeadlockDetected` events for this code-entity producer /
   consumer class across repeated full runs
2. `0` `TransactionExecutionLimit` failures on the same path
3. bounded readiness-wait telemetry showing work is waiting for phase
   completion instead of burning retries

## Consequences

### Positive

- eliminates the observed deadlock class by design, not by probability
- preserves concurrency across repositories
- gives the platform an explicit ownership model for the `uid` keyspace
- creates a reusable contract for future graph consumers
- closes the liveness hole where a publish failure could otherwise block work
  indefinitely
- narrows post-readiness code-call lock footprint without giving up grouped
  retry semantics

### Costs

- requires a schema addition
- requires projector and reducer refactors to publish / consult phase state
- broadens the solution from one runner to a cross-runtime contract

These costs are appropriate. The problem is architectural, so the solution must
also be architectural.

## Sources

Official Neo4j documentation:

- Operations Manual, Concurrent Data Access:
  https://neo4j.com/docs/operations-manual/current/database-internals/concurrent-data-access/
- Go Driver Manual, Transactions:
  https://neo4j.com/docs/go-manual/current/transactions/
- Go Driver Manual, Further Query Mechanisms:
  https://neo4j.com/docs/go-manual/current/query-advanced/
- Go Driver Manual, Performance Recommendations:
  https://neo4j.com/docs/go-manual/current/performance/

Repository evidence:

- `go/internal/storage/postgres/code_call_intent_writer.go:41`
- `go/internal/reducer/code_call_projection_runner.go:173`
- `go/internal/storage/neo4j/semantic_entity.go:269`
- `go/internal/storage/neo4j/canonical.go:159`
- `go/internal/projector/runtime.go:109`
