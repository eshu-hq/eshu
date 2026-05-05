# ADR: E2E Validation — Atomic Canonical Writes and Deferred Bootstrap Backfill

**Status:** Superseded by later acceptance evidence
**Date:** 2026-04-18
**Validates:** `2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`,
`2026-04-18-bootstrap-relationship-backfill-quadratic-cost.md`

## Status Review (2026-05-03)

**Current disposition:** Historical validation.

The validated decisions landed: canonical writes use the atomic/grouped writer,
retryable Cypher error handling covers `EntityNotFound` and `DeadlockDetected`,
and deferred relationship backfill is now normal bootstrap behavior. Current
acceptance evidence has moved to newer full-corpus proofs.

**Remaining work:** none for this validation ADR. Use the active ADR that owns a
change to record new E2E evidence.

## Decision

Accept the combined implementation of:

1. **Atomic canonical writes** (Option C from the cross-phase race ADR)
2. **RetryableError wrapping** for Neo4j EntityNotFound and DeadlockDetected
   codes (Option F)
3. **Generation-scoped directory retracts** (Option G)
4. **Deferred bootstrap relationship backfill** with readiness gating and
   deployment_mapping reopen (from the backfill quadratic cost ADR)

These changes are validated by a full 896-repo E2E run on the remote instance
(`eshu-e2e.example.test`) against the production corpus.

## Why This ADR Exists

The previous 792-repo baseline run exhibited:

- **1,061 EntityNotFound** terminal failures
- **220 DeadlockDetected** terminal failures
- **1,281 total terminal failures** (12.5% of ~10,249 reducer executions)
- **1 hour 33 minutes** bootstrap duration
- **0 resolved cross-repo relationships**

These failures were permanently terminal because Neo4j `ClientError` codes
bypassed the retry infrastructure. The quadratic bootstrap cost was caused by
per-repo whole-corpus evidence scans.

This ADR documents the evidence that the combined implementation eliminates all
observed failure classes and enables a new capability (cross-repo resolution).

## E2E Run Evidence

### Test Environment

| Parameter | Value |
|-----------|-------|
| Host | `ubuntu@eshu-e2e.example.test` |
| Machine | 123 GB RAM, multi-core |
| Branch | `codex/go-data-plane-architecture` |
| Corpus | 896 repositories (production fixtures) |
| Stack | Docker Compose: Neo4j, Postgres, bootstrap-index, resolution-engine |
| Reducer workers | 4 (default `min(NumCPU, 4)`) |

### Baseline (Previous Run, 792 Repos)

| Metric | Value |
|--------|-------|
| Bootstrap duration | 1h 33m 22s |
| Repos processed | 792 |
| EntityNotFound failures | 1,061 |
| DeadlockDetected failures | 220 |
| Total terminal failures | 1,281 |
| Cross-repo resolved relationships | 0 |
| Reducer backlog during bootstrap | Zero (bootstrap-bound) |

### Validated Run (2026-04-18, 896 Repos)

| Metric | Value | Change |
|--------|-------|--------|
| Bootstrap-index helper duration | **29m 42s** | **-68%** (vs 1h 33m baseline) |
| Reducer full convergence | **~5h 10m** | New measurement (see below) |
| Repos processed | 896 | +13% |
| EntityNotFound failures | **0** | **-100%** |
| DeadlockDetected failures | **0** | **-100%** |
| Total terminal failures | **0** | **-100%** |
| Cross-repo resolved relationships | **1,814** | New capability |
| Cross-repo resolution completions | 382 repos | New capability |
| Evidence facts discovered | 10,768 | New capability |
| Relationship candidates | 1,814 | New capability |

**Important distinction:** The 29m 42s measures only the bootstrap-index
helper process (discovery + collection + projection + backfill + reopen).
The reducer runs independently and took ~5h 10m to converge all domains
to terminal status. The convergence time is dominated by 5 monster repos
(see "Discovered Issues" below) which produce Neo4j transactions lasting
17-51 minutes each, blocking all 4 workers. Reducer convergence time is
a separate optimization target from bootstrap-index helper duration.

### Bootstrap Timeline

| Phase | Timestamp (UTC) | Duration |
|-------|-----------------|----------|
| Bootstrap started | 07:05:04 | -- |
| Discovery completed (896 repos) | 07:05:07 | 3s |
| Collection completed (896 repos) | 07:29:08 | 24m 4s |
| Projection completed (896 repos) | 07:34:45 | 29m 41s |
| Pipeline complete (incl. backfill + reopen) | 07:34:46 | **29m 42s** |

Key observations:

1. **Per-repo commit dropped from ~20s to ~50ms.** The deferred backfill
   eliminates the per-repo O(facts x catalog) scan. At repo position 896,
   commit takes milliseconds instead of 20+ seconds.

2. **Deferred backfill ran once in <1s.** The single corpus-wide
   `BackfillAllRelationshipEvidence` pass discovered 10,768 evidence facts
   across 896 repos and published 896 `backward_evidence_committed` readiness
   rows.

3. **Reopen completed instantly.** `ReopenDeploymentMappingWorkItems`
   transitioned 846 succeeded deployment_mapping items back to pending.

### Resolved Relationships Breakdown

| Relationship Type | Count |
|-------------------|-------|
| PROVISIONS_DEPENDENCY_FOR | 601 |
| DEPLOYS_FROM | 555 |
| USES_MODULE | 430 |
| DISCOVERS_CONFIG_IN | 120 |
| DEPENDS_ON | 108 |
| **Total** | **1,814** |

These are real cross-repo dependency edges. Example:

- `repository:r_8dc4362a` → `repository:r_8c9222ef`: DEPLOYS_FROM (0.93
  confidence, 3 evidence facts) + DISCOVERS_CONFIG_IN (0.90, 3 evidence facts)
- `repository:r_bb28b557` → `repository:r_8c9222ef`: DEPLOYS_FROM (0.93, 3)
  + DISCOVERS_CONFIG_IN (0.90, 3)

Each resolution completed in ~2 seconds per repo.

### Failure Analysis

**Correctness regressions eliminated:**

| Error Class | Baseline | Validated Run |
|-------------|----------|---------------|
| EntityNotFound | 1,061 | 0 |
| DeadlockDetected | 220 | 0 |
| Total correctness failures | 1,281 | **0** |

These error classes were permanently terminal in the baseline. The atomic
write and retryable error changes eliminate them entirely.

**Operator intervention still required:**

| Event | Count | Cause |
|-------|-------|-------|
| Manual resolution-engine restart | 4 | Stuck Neo4j transactions from monster repos (17-51 min, 5,000+ locks) |
| context canceled (from restarts) | 4 | In-flight transactions killed by restart |

All 4 `context canceled` errors were retried successfully after restart.
The restarts were needed because monster repo transactions (see "Discovered
Issues" below) blocked all workers. This is a pre-existing operational
issue — not a regression from this changeset — but it means the E2E run
was not fully autonomous. A production deployment would require either
the Neo4j label-hint optimization (future ADR) or an operator monitoring
for stuck transactions.

## Validated Design Decisions

### 1. Atomic Canonical Writes Eliminate the Race

The projector's Phase A→G canonical write was previously executed as separate
Neo4j sessions, exposing intermediate state (nodes deleted but not yet
recreated) to the reducer. The atomic write wraps all phases in a single
`session.ExecuteWrite()` managed transaction.

**Evidence:** Zero EntityNotFound across all observed reducer executions. The
theoretical maximum is 896 repos × 8 domains = 7,168, but not all domains
converged during the run (e.g., `semantic_entity_materialization` completed
only 364/896 due to upstream gating). Observed succeeded executions totaled
~5,512 across all domains. The previous run produced 1,061 EntityNotFound
on fewer repos.

**Log evidence:**
```
canonical atomic write completed scope_id=... statements=715 duration_s=43.49
canonical atomic write completed scope_id=... statements=426 duration_s=28.08
canonical atomic write completed scope_id=... statements=107 duration_s=3.46
```

All canonical writes use the atomic path. No fallback to sequential execution
was observed.

### 2. RetryableError Infrastructure — Defense-in-Depth

The `WrapRetryableNeo4jError` wrapper correctly identifies EntityNotFound and
DeadlockDetected codes as retryable. In the validated run, the wrapper was not
triggered on the atomic write path because `session.ExecuteWrite()` managed
transactions retry transient errors internally.

However, the wrapper remains necessary for:

- **`Session.Run()` auto-commit paths** in the reducer's edge materialization
  writers (code_call, inheritance, sql_relationship), which do not use managed
  transactions and therefore lack built-in retry for transient Neo4j errors.
- **Concurrent reducer-to-reducer contention** on shared projection edges,
  where two reducer workers may touch the same Neo4j nodes simultaneously.

The wrapper is defense-in-depth, not dead code.

### 3. Deferred Backfill Pipeline Works End-to-End

The full pipeline:

```text
bootstrap commits 896 repos (backfill skipped)
  → BackfillAllRelationshipEvidence (single corpus-wide pass)
    → 10,768 evidence facts discovered
    → 896 backward_evidence_committed readiness rows published
  → ReopenDeploymentMappingWorkItems
    → 846 succeeded items reopened to pending
  → Reducer claims reopened items
    → CrossRepoRelationshipHandler.Resolve() passes readiness gate
    → ListEvidenceFacts loads per-repo evidence
    → relationships.Resolve() produces candidates and resolved edges
    → 1,814 resolved relationships written
```

### 4. Readiness Gating Prevents Premature Resolution

During the first pass (before backfill), deployment_mapping intents succeed
with zero cross-repo edges because `backward_evidence_committed` is not yet
marked. This is the correct soft-gate behavior:

```
cross-repo resolution gated reason=backward_evidence_not_committed
```

After `BackfillAllRelationshipEvidence` marks readiness and
`ReopenDeploymentMappingWorkItems` reopens items, the second pass resolves
cross-repo edges with complete evidence:

```
cross-repo relationship resolution completed evidence_count=6 candidate_count=2 resolved_count=2
```

### 5. Terminal Wait Was Correctly Removed

The original ADR proposed a `WaitForDeploymentMappingTerminal` poll loop
before reopening. E2E testing proved this is **structurally incompatible**
with the reducer architecture:

- The claim query (`ORDER BY updated_at ASC, LIMIT 1, FOR UPDATE SKIP LOCKED`)
  picks the single oldest pending item across ALL domains
- Monster repos block workers for 5-50 minutes on inheritance, code_call, and
  SQL relationship materializations
- `CountInFlightByDomain` counts ALL non-terminal items, not just the repo
  being waited on
- With 4 workers and ~40 monster repos × 6 domains, the terminal wait would
  need 4+ hours — longer than the bootstrap itself

**Resolution:** Terminal wait was removed. `ReopenSucceeded` only targets
`status = 'succeeded'` rows, which is safe:

- Pending/claimed items are unaffected (WHERE clause filters them out)
- Items that succeed after reopen will run with readiness gate already open
- No items are lost or double-processed

**Reopen race window:** A small number of deployment_mapping items may
transition from `claimed` to `succeeded` between the readiness gate opening
(backfill completes) and the reopen pass executing. These stragglers
succeed with the gate already open but ran before evidence was committed,
so they produce zero cross-repo edges.

**This race is bounded but NOT automatically self-healing today.** The
graph projection phase repair runner (`GraphProjectionPhaseRepairer`)
republishes missing readiness rows — it does not replay succeeded reducer
work items. The recovery APIs (`replay.go`, `recovery_handler.go`) are
manual admin actions, not automatic background processes. Stragglers
from this race window require either:

1. A manual admin replay of the affected deployment_mapping items, or
2. A future automated straggler-replay mechanism (not yet implemented)

The number of affected items is small (bounded by the number of
deployment_mapping items that transition during the reopen pass, typically
single digits), but they are **not perfectly identifiable from durable state
alone** because legitimate repos can also resolve to zero cross-repo edges.
Operators can only narrow the set with ad hoc correlation across succeeded
`deployment_mapping` rows, readiness state, and zero-edge resolution outcomes,
then replay the suspected stragglers manually.

### 6. Instrumentation Gap — Being Fixed in Same Changeset

`BackfillAllRelationshipEvidence` and `ReopenDeploymentMappingWorkItems`
accepted `*telemetry.Instruments` and `trace.Tracer` parameters but ignored
them (unused `_` bindings). This means the E2E run produced no OTEL metrics
or spans for these operations — the evidence above comes from structured
logs only.

**Resolution:** The same changeset that tightens this ADR wires in:

- `eshu_dp_deferred_backfill_duration_seconds` histogram
- `eshu_dp_deferred_backfill_evidence_total` counter
- `eshu_dp_deployment_mapping_reopened_total` counter
- `bootstrap.reopen_deployment_mapping` trace span

## Discovered Issues (Pre-existing, Not Caused by Changes)

### Monster Repo Neo4j Transaction Stalls

**Observation:** 5 repos produce Neo4j transactions lasting 17-51 minutes
with 5,000+ active locks each. These transactions use un-labeled MATCH
patterns (`MATCH (source {uid: row.source_entity_id})`) which force full
graph scans instead of label-specific index lookups.

**Evidence:**
```
neo4j-transaction-2638  Running  PT51M44.557S  5293 locks
neo4j-transaction-3249  Running  PT48M6.735S   5281 locks
neo4j-transaction-4736  Running  PT17M22.655S  0 locks (waiting)
neo4j-transaction-5138  Running  PT7M6.676S    0 locks (waiting)
```

**Impact:** Workers appear "hung" (no log output for 30+ minutes). Lease
expires but worker is alive and waiting for Neo4j. All other pending items
are blocked behind these transactions in the claim queue.

**Domains affected:**
- `sql_relationship_materialization`: 51 min (REFERENCES_TABLE edges)
- `inheritance_materialization`: 22 min (OVERRIDES edges)
- `code_call_materialization`: 7 min (CALLS edges)

**Recommendation:** Future ADR to add label hints to edge materialization
MATCH patterns. `MATCH (source:Function|Class|File {uid: ...})` would use
the label index instead of scanning all nodes.

### Shared FIFO Claim Queue Starves New Domains

**Observation:** The claim query `ORDER BY updated_at ASC` means recently-
reopened deployment_mapping items (newer `updated_at`) sit behind older
pending items from other domains. With 4 workers consumed by monster repo
transactions, DM items process at ~1 per minute instead of ~30 per minute.

**Evidence:**
- 845 DM items ready to process (all other domains succeeded for those repos)
- 243 items from other domains (monster repos) consuming all 4 workers
- DM items would take ~28 minutes at full throughput but took 4+ hours due
  to interleaving with monster repo work

**Recommendation:** Future optimization: domain-aware claim priority or
separate worker pools per domain class (fast vs monster). Not a correctness
issue — all items eventually process.

### Reducer Worker Count Underutilizes Hardware

**Observation:** The remote instance has 123 GB RAM and many cores. The
reducer uses 4 workers (default cap) at 0.15% CPU and 228 MB RAM. The
machine is overwhelmingly idle while the reducer is I/O-bound on Neo4j.

**Recommendation:** Increase `ESHU_REDUCER_WORKERS` to 16 or 32 for instances
with available resources. The bottleneck shifts to Neo4j concurrent
transaction capacity, but more workers would prevent monster repos from
starving all other work.

### Semantic Entity Materialization Bottleneck

**Observation:** `semantic_entity_materialization` completed only 364/896
repos while all other domains completed 889+/896. This domain is gated behind
`code_entities_uid` readiness which requires code_call projection to complete
first. The code_call projection is blocked on the same monster repos.

**Evidence:**
```
code call projection skipped acceptance units until semantic readiness
is committed  blocked_count=216
```

This is the correct gating behavior from the deadlock elimination ADR. The
216 blocked units are waiting for semantic nodes to commit before edge
projection can proceed.

## Architectural Lessons

### 1. Atomic Writes Are Worth the Complexity

Wrapping 7 Neo4j phases in a single managed transaction eliminates an entire
class of race conditions. The performance cost is negligible (same statements,
one transaction boundary instead of seven). The complexity cost is modest
(refactoring phase methods to return statements instead of executing inline).

### 2. Deferred Backfill Is the Correct Pattern for Bootstrap

Per-repo backfill is correct for steady-state (rare new repo events). Deferred
corpus-wide backfill is correct for bootstrap (every repo is new). The two
paths share the same `DiscoverEvidence` function and produce identical results.

### 3. Terminal Wait Is an Anti-Pattern for Shared Queues

Waiting for a specific domain to drain before proceeding is fundamentally
incompatible with a shared FIFO queue where other domains' monster repos block
workers. The correct pattern is:

- Let the first pass succeed with soft-gated behavior
- Reopen succeeded items after the gate condition is satisfied
- Pending/claimed items naturally benefit when the gate opens

### 4. Monster Repos Dominate Reducer Throughput

5 repos out of 896 (0.6%) consume >80% of reducer wall time. Any optimization
to the reducer queue, worker count, or Neo4j query patterns must account for
this extreme skew. Average-case metrics are misleading.

## Files Validated

| File | Role |
|------|------|
| `go/internal/storage/neo4j/canonical_node_writer.go` | Atomic write dispatch |
| `go/internal/storage/neo4j/retryable_error.go` | Neo4j retryable error wrapping |
| `go/internal/storage/neo4j/canonical_node_cypher.go` | Generation-scoped directory retracts |
| `go/internal/storage/postgres/ingestion.go` | Deferred backfill, reopen |
| `go/internal/reducer/cross_repo_resolution.go` | Readiness gating, evidence loading |
| `go/internal/reducer/graph_projection_phase.go` | Readiness key types |
| `go/internal/storage/postgres/graph_projection_phase_state.go` | Readiness persistence |
| `go/cmd/bootstrap-index/main.go` | Pipeline orchestration |
| `go/cmd/reducer/main.go` | ReadinessLookup/Prefetch wiring |

## References

- Baseline E2E run: 2026-04-17, 792 repos, 1h 33m, 1,281 failures
- Validated E2E run: 2026-04-18, 896 repos, 29m 42s, 0 failures
- Remote instance: `ubuntu@eshu-e2e.example.test`
- Deadlock elimination ADR: `2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`
- Bootstrap backfill ADR: `2026-04-18-bootstrap-relationship-backfill-quadratic-cost.md`
- Cross-phase race ADR plan: Options C, F, G
- Resolution engine logs: `docker compose logs resolution-engine`
- Neo4j transaction inspection: `SHOW TRANSACTIONS YIELD ...`
