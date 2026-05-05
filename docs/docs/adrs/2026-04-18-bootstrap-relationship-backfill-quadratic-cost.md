# ADR: Bootstrap Relationship Backfill Quadratic Cost

**Status:** Implemented with follow-up
**Date:** 2026-04-18
**Related:** `2026-04-17-semantic-entity-materialization-bottleneck.md`

## Status Review (2026-05-03)

**Current disposition:** Implemented; ADR workstream closed.

The deferred relationship-backfill design is in the current runtime.
`SkipRelationshipBackfill` suppresses per-commit bootstrap and ingester
backfill, bootstrap and ingester wiring run one deferred
`BackfillAllRelationshipEvidence` pass plus
`ReopenDeploymentMappingWorkItems`, and the implementation publishes
`backward_evidence_committed` readiness through
`graph_projection_phase_state`.

Read-only validation on 2026-05-03 found the current code and tests at the
expected ownership points:

- `go/internal/storage/postgres/ingestion.go` owns
  `SkipRelationshipBackfill`, `BackfillAllRelationshipEvidence`, readiness
  publication, and `ReopenDeploymentMappingWorkItems`.
- `go/cmd/bootstrap-index/main.go` waits for source-local projector drain
  before reopening succeeded `deployment_mapping` rows.
- `go/cmd/ingester/wiring.go` defers ingester relationship maintenance to the
  collector batch-drain hook.
- `go/internal/storage/postgres/ingestion_test.go` covers skipped unknown
  generations and generation-scoped deferred evidence persistence.
- `go/cmd/ingester/wiring_test.go` covers deferred ingester wiring.

**Remaining work:** only the documented automatic replay for the narrow reopen
straggler window remains. The quadratic backfill fix itself is done.

## Decision

Adopt deferred bootstrap backfill with an explicit correctness-preserving
gating design for cross-repo resolution.

Specifically:

1. Add a `SkipRelationshipBackfill` option to `IngestionStore` that suppresses
   the per-repo backfill inside `CommitScopeGeneration` during bootstrap.
2. After all repos are committed, run a single corpus-wide
   `DiscoverEvidence(allFacts, fullCatalog)` pass and persist evidence under
   each target repo's actual generation ID.
3. Gate cross-repo resolution in `deployment_mapping` behind a
   `backward_evidence_committed` readiness phase so that intents processed
   before the deferred pass completes do not finalize against incomplete
   backward evidence.
4. After the deferred pass completes and the readiness phase is marked,
   reopen already-`succeeded` `deployment_mapping` intents so cross-repo
   resolution runs again with complete evidence.
5. Keep the existing per-repo backfill intact for steady-state ingestion where
   it is correctly scoped to rare new-repo events.

This ADR does **not** recommend removing the backfill from the steady-state
ingestion path. The per-repo backfill is correct and efficient when new repos
appear infrequently. The problem is exclusively the bootstrap context where
every repo is new.

## Context

After resolving the semantic entity materialization bottleneck
(acceptance-unit scoped intents) and the cross-phase EntityNotFound race
(retryable error wrapping + atomic canonical writes), the 792-repo E2E
bootstrap completed with zero failures and zero deadlocks.

That success exposed the next bottleneck: the bootstrap itself takes
**1 hour 33 minutes** for 792 repos. The reducer has zero backlog throughout
the run, meaning the pipeline is entirely bootstrap-bound.

## Why This ADR Exists

The bootstrap duration is dominated by a per-repo cost that grows linearly
with the number of already-committed repos. At the start of bootstrap, each
repo commits in ~10ms. By the end, each repo takes ~20 seconds. This is not a
constant overhead that can be tuned away with workers or batch size. It is an
algorithmic complexity problem that will get worse as the repo count grows.

Without this change:

- bootstrap duration grows quadratically with repo count
- adding more repos to the corpus makes every repo slower, not just the new
  ones
- operators cannot predict bootstrap duration from repo count alone because
  the growth is non-linear

## Research Methodology

Root cause was identified by tracing the full data flow end-to-end, not by
assumption:

1. **Observed symptom:** every repo takes ~20s regardless of fact count at the
   end of bootstrap. 3-fact repos and 2,736-fact repos both take ~20s.
2. **Hypothesis 1 (disproved):** OTEL exporter shutdown per repo. Verified by
   reading `go/cmd/bootstrap-index/main.go:91` and
   `go/internal/telemetry/provider.go` — `providers.Shutdown()` is called once
   at process exit, not per repo.
3. **Hypothesis 2 (disproved):** Snapshot/parse time per repo. Verified by
   comparing `collector snapshot completed` timestamps — snapshots complete in
   milliseconds even at the end of the run. Workers are blocked on channel
   send, not on parsing.
4. **Hypothesis 3 (disproved):** Postgres query degradation under load.
   Verified with `EXPLAIN ANALYZE` on the two largest queries
   (`loadRepositoryCatalog` = 13ms, `loadLatestRelationshipFacts` = 465ms).
   Total query time ~500ms, not 20s.
5. **Hypothesis 4 (confirmed):** CPU-bound relationship evidence discovery.
   Traced through `ingestion.go:279` →
   `backfillRelationshipEvidenceForNewRepositories` →
   `loadLatestRelationshipFacts` (465ms) → `DiscoverEvidence(91649 facts, 791
   catalog entries)` → `discoverFromEnvelope` → `matchCatalog` → 72.6M
   comparisons at ~200-300ns each = 15-20s. Matches observed timing exactly.
6. **Verified growth pattern:** Compared first 20 repos (0.007-0.44s each)
   against last 20 repos (20.0-23.7s each). Duration correlates with committed
   repo count, not with current repo's fact count.

## Data Flow: Current vs Proposed

### Current: Per-Repo Backfill (repeated whole-corpus scans)

```text
drainCollector loop (sequential, one repo at a time):
  +-----------+     +------------------------+     +------------------+
  | source.   | --> | CommitScopeGeneration  | --> | projector queue  |
  | Next()    |     |                        |     | (enqueued)       |
  +-----------+     +------------------------+     +------------------+
                           |
                           | for EVERY new repo:
                           v
                    +-----------------------------------+
                    | backfillRelationshipEvidence       |
                    |   1. load ALL content+file facts   |  <-- grows each repo
                    |   2. load full catalog              |  <-- grows each repo
                    |   3. DiscoverEvidence(all, all)     |  <-- O(facts x catalog)
                    |   4. filter + persist evidence      |
                    +-----------------------------------+
                    Cost at repo N: O(N * avg_facts)
                    Total across N repos: sum(1..N) = N repeated scans
```

### Proposed: Deferred Single Pass (one corpus-wide scan)

```text
drainCollector loop (sequential, backfill SKIPPED):
  +-----------+     +------------------------+     +------------------+
  | source.   | --> | CommitScopeGeneration  | --> | projector queue  |
  | Next()    |     | (backfill skipped)     |     | (enqueued)       |
  +-----------+     +------------------------+     +------------------+
                           |
                           | afterBatch still runs (forward evidence only)
                           v
                    +-----------------------------------+
                    | afterBatch: DiscoverEvidence       |
                    |   (current batch, catalog)         |  <-- O(batch x catalog)
                    +-----------------------------------+

After ALL repos committed (runs exactly once inside runPipelined):
  +---------------------------------------------------+
  | BackfillAllRelationshipEvidence                    |
  |   1. load ALL content+file facts (once)            |
  |   2. load full catalog (once)                      |
  |   3. DiscoverEvidence(all, all) (once)             |
  |   4. group evidence by target repo                 |
  |   5. persist under each target repo's generation   |
  |   6. mark backward_evidence_committed readiness    |
  |   7. reopen succeeded deployment_mapping intents   |
  +---------------------------------------------------+
  Cost: one corpus-wide scan (facts x catalog), ~20s at 792 repos

Reducer (resolution-engine, separate container):
  deployment_mapping handler:
    -> check backward_evidence_committed readiness
    -> if NOT ready: skip cross-repo resolution, return 0 edges
    -> if ready: load evidence by generation, resolve normally
```

## Current Flow

`CommitScopeGeneration` in `go/internal/storage/postgres/ingestion.go`
processes each repo in a single Postgres transaction:

```text
1. validate generation input                          ~0ms
2. check freshness hint (skip if unchanged)           ~1ms
3. BEGIN transaction
4. upsert ingestion scope                             ~1ms
5. upsert scope generation                            ~1ms
6. load repository catalog (all repo facts)           ~13ms (at 792 repos)
7. upsert streaming facts (batched multi-row INSERT)  ~varies by fact count
   -> afterBatch: DiscoverEvidence(batch, catalog)    ~fast (small batch)
8. backfill relationship evidence for new repos:
   a. reload repository catalog                       ~13ms
   b. load ALL latest content+file facts              ~465ms (at 91,649 facts)
   c. DiscoverEvidence(allFacts, fullCatalog)          ~15-20s (at 792 repos)
   d. filter evidence to new repo targets             ~fast
   e. persist evidence facts                          ~fast
9. enqueue projector work item                        ~1ms
10. COMMIT                                            ~1ms
```

Step 8c is the bottleneck. During bootstrap, every repo is "new" (not in the
prior catalog snapshot loaded at step 6), so step 8 executes for **every
single repo**.

## Evidence

### E2E Run Timing (2026-04-18, 792 repos, full bootstrap)

Per-repo commit duration measured from `bootstrap scope collected` logs:

| Repo position | Facts | Catalog size | Duration |
|---------------|-------|--------------|----------|
| 2 (start) | 14 | 2 | 0.007s |
| 6 (start) | 3 | 6 | 0.012s |
| 12 (early) | 729 | 12 | 0.44s |
| 19 (early) | 1,120 | 19 | 0.21s |
| ~780 (end) | 3 | ~780 | 20.17s |
| ~782 (end) | 14 | ~782 | 20.15s |
| ~785 (end) | 67 | ~785 | 20.02s |
| ~787 (end) | 1,085 | ~787 | 21.06s |
| ~790 (end) | 2,736 | ~790 | 23.75s |

Key observations:

1. **Fact count does not determine duration.** A 3-fact repo at position ~780
   takes 20.17s while the same 3-fact repo at position 6 takes 0.012s. The
   duration is determined by how many repos are already committed, not by the
   current repo's size.

2. **The growth is linear in committed repos.** At position 780, the backfill
   loads ~90,000 content+file facts and runs `DiscoverEvidence` against ~780
   catalog entries. At position 6, it loads ~50 facts against 6 entries.

3. **Total bootstrap work is dominated by repeated whole-corpus scans.** Each
   repo at position N pays a scan cost proportional to the data already
   committed. Summing across all N repos: the same corpus is scanned N times
   with growing size, producing quadratic total work.

### Query Timing (EXPLAIN ANALYZE on completed 792-repo database)

| Query | Execution time |
|-------|---------------|
| `loadRepositoryCatalog` (all repository facts) | 13ms |
| `loadLatestRelationshipFacts` (all content+file facts) | 465ms |

### CPU Cost Analysis

`DiscoverEvidence` iterates every loaded fact envelope
(`evidence.go:26`). For each envelope, `discoverFromEnvelope` checks artifact
type and routes to type-specific discoverers (Terraform, Helm, ArgoCD,
Kustomize, Jenkins, Dockerfile, GitHub Actions, Docker Compose). Each
discoverer calls `matchCatalog` which iterates all catalog entries
(`evidence.go:491`), performing `strings.Contains` alias matching per entry.

At 792 repos with 91,649 content+file facts:

- 91,649 envelopes x 792 catalog entries = **~72.6 million comparisons**
- At ~200-300ns per comparison (lowercase + Contains): **~15-20s of CPU**
- Plus 465ms SQL query time
- **Total: ~20s per call.** Matches observed timing exactly.

### Snapshot Timing Proof

Snapshot workers (parsing repos) complete in milliseconds even at the end of
bootstrap. The `collector snapshot completed` logs show sub-second times
throughout the run. Workers are blocked waiting for the consumer
(`drainCollector`) to drain the channel, not doing work. The snapshot-to-
channel architecture is not the bottleneck.

### Total Run Duration

| Phase | Start | End | Duration |
|-------|-------|-----|----------|
| First repo committed | 01:17:41 | -- | -- |
| Last repo committed | -- | 02:51:02 | -- |
| Last reducer completed | -- | 02:51:03 | -- |
| **Total wall clock** | **01:17:41** | **02:51:03** | **1h 33m 22s** |

The reducer maintained zero backlog throughout. The pipeline was entirely
bootstrap-bound.

## Root Cause

The root cause is:

> `backfillRelationshipEvidenceForNewRepositories` executes once per new repo
> inside `CommitScopeGeneration`, loading ALL historical content+file facts
> and running O(facts x catalog) relationship discovery each time. During
> bootstrap, every repo is new, turning per-repo backfill into N repeated
> whole-corpus scans where each scan grows with the data already committed.

The backfill exists to solve a real problem: when a new repo appears, existing
facts from other repos may already reference it (e.g., Terraform modules
pointing at the new repo's name). The backfill scans historical facts to find
these backward references.

In steady-state ingestion (one new repo per hour), this is correct and
efficient: one 20s scan for one new repo is acceptable. During bootstrap (792
new repos in sequence), the same mechanism becomes a 792x repeated full scan
where each scan grows with the data already committed.

## Why the Forward Pass (afterBatch) Is Not Sufficient Alone

The `afterBatch` callback in `upsertStreamingFacts` (ingestion.go:259) calls
`DiscoverEvidence(batch, catalog)` on each batch of incoming facts. This
discovers **forward evidence**: references FROM the current repo's facts TO
already-cataloged repos.

The backfill discovers **backward evidence**: references FROM already-committed
facts TO the new repo.

During bootstrap, repos are committed in arbitrary order. Repo A's
`afterBatch` at position 100 cannot discover that repo B (committed at
position 50) has Terraform that references repo A, because repo A was not in
the catalog when repo B was committed. The backfill at position 100 catches
this by scanning repo B's facts.

Both directions are needed for correctness. The question is **when** the
backward scan runs, not **whether** it runs.

## Correctness Constraint: Concurrent Reducer

**Critical design constraint.** The bootstrap runtime (`bootstrap-index`) is a
one-shot producer. The reducer (`resolution-engine`) is a separate container
that drains work concurrently (`docs/docs/deployment/service-runtimes.md`).

Inside `PlatformMaterializationHandler.Handle()`
(`go/internal/reducer/platform_materialization.go:95`), `deployment_mapping`
calls `CrossRepoRelationshipHandler.Resolve()`. The resolver loads evidence
strictly by the generation ID of the intent being processed
(`go/internal/reducer/cross_repo_resolution.go:87`):

```go
evidenceFacts, err := h.EvidenceLoader.ListEvidenceFacts(ctx, generationID)
```

If backward evidence is deferred, early `deployment_mapping` intents will be
reduced against incomplete evidence and will never be revisited unless the
design includes an explicit gating or replay mechanism.

This is not a theoretical concern — it is the actual deployed topology.

## Storage Constraint: Evidence is Append-Only

`UpsertEvidenceFacts` uses `INSERT ... ON CONFLICT (evidence_id) DO NOTHING`
(`go/internal/storage/postgres/relationship_schema.go:159`). The evidence ID
is content-based, derived from generation ID plus evidence kind,
relationship type, repo/entity IDs, confidence, rationale, and serialized
details (`go/internal/storage/postgres/relationship_store.go:146`).

This means:

- Re-runs with the same evidence content are idempotent even if discovery
  ordering changes.
- Stale evidence rows are never retracted.
- A synthetic generation ID like `"bootstrap-backfill"` would not be found by
  `ListEvidenceFacts(ctx, generationID)` which filters by the repo's actual
  generation ID.

The deferred pass must store evidence under each target repo's **actual
generation ID**, not a synthetic one. And the storage model requires a
retraction strategy for interrupted bootstrap + restart scenarios.

## Options

### Option A: Do Nothing

Accept 1.5 hour bootstrap for 792 repos.

**Pros:**

- zero code change
- current relationship evidence is complete and correct

**Cons:**

- bootstrap duration grows quadratically and will worsen as repos are added
- at 1,500 repos, bootstrap would take ~6+ hours
- blocks operator workflows that depend on fast re-bootstrap (schema migration,
  disaster recovery, fixture rebuild)
- wastes CPU on redundant work (scanning the same facts N times)

**Verdict:** not acceptable as the corpus grows.

### Option B: Deferred Backfill With Cross-Repo Resolution Gating (Recommended)

Skip per-repo backfill during bootstrap. After all repos are committed, run a
single corpus-wide `DiscoverEvidence` pass, persist evidence under each target
repo's actual generation ID, mark a readiness phase, and reopen already-
`succeeded` `deployment_mapping` intents.

**Pros:**

- removes N repeated whole-corpus scans from the per-repo commit path and
  collapses them to one corpus-wide pass
- per-repo commit drops from ~20s back to ~50ms (measured at bootstrap start)
- bootstrap helper duration drops materially; the validated 896-repo run
  completed bootstrap-index in 29m 42s versus the earlier 792-repo 1h 33m
  baseline
- cross-repo resolution correctness is preserved by the readiness gate
- no change to steady-state ingestion path

**Cons:**

- requires readiness gating for `deployment_mapping` cross-repo resolution
- requires reopening already-succeeded `deployment_mapping` intents after the
  deferred pass
- the deferred pass itself takes ~20s (one corpus-wide scan), but runs once
- adds complexity to bootstrap orchestration

**Verdict:** recommended. The gating cost is bounded and the correctness
guarantee is explicit.

### Option C: Incremental Backfill With Inverted Index

Build a pre-computed inverted index mapping repo name aliases to fact IDs.
When a new repo appears, look up only the facts that mention its aliases
instead of scanning all facts.

**Pros:**

- makes per-repo backfill proportional to matching facts, not all facts
- works for both bootstrap and steady-state
- no deferred pass or gating needed

**Cons:**

- significant design complexity: must maintain the inverted index across
  commits, handle new aliases, support all artifact types
- regex-based discoverers (Terraform patterns, GitHub URLs) don't reduce to
  simple alias lookup
- the inverted index itself must be rebuilt or incrementally maintained
- overkill for bootstrap where a single deferred pass is simpler

**Verdict:** strong long-term optimization if steady-state backfill becomes a
problem at very large scale (5,000+ repos). Not justified as the first fix.

### Option D: Parallelize Backfill Across Workers

Run `DiscoverEvidence` on a worker pool, partitioning facts across goroutines.

**Pros:**

- reduces wall-clock time per backfill call by N workers
- no change to correctness or data flow

**Cons:**

- does not reduce total CPU work (still N repeated scans)
- improves wall time by a constant factor (e.g., 4x with 4 workers)
- at 1,500 repos, even 4x parallelism still means ~1.5+ hours
- does not address the repeated-scan root cause

**Verdict:** not a substitute for fixing the repeated scans. Can be combined
with Option B for the deferred pass if the single-pass ~20s grows.

### Option E: Move Backfill to Async Reducer Domain

Make relationship backfill a queued reducer domain that runs asynchronously
after all facts are committed, similar to semantic entity materialization.

**Pros:**

- fully decouples evidence discovery from the commit path
- runs at reducer concurrency (multiple workers)
- naturally deferred without bootstrap-specific flags

**Cons:**

- significant architectural change: new reducer domain, new queue, new handler
- still needs gating for cross-repo resolution
- adds operational complexity for a rarely-run bootstrap path

**Verdict:** architecturally clean but over-engineered for the current problem.
Revisit if relationship evidence needs real-time incremental maintenance.

## Recommendation

Adopt **Option B** as the primary fix with three mandatory sub-designs:

### Sub-design 1: Deferred backfill with per-generation evidence storage

The deferred pass must:

1. Load all content+file facts once.
2. Load the full catalog once.
3. Run `DiscoverEvidence(allFacts, fullCatalog)` once.
4. Group resulting evidence by `TargetRepoID`.
5. For each target repo, look up its active generation ID from
   `ingestion_scopes`.
6. Store evidence under that repo's **actual generation ID** (not a synthetic
   one) so `ListEvidenceFacts(ctx, generationID)` finds it.

### Sub-design 2: Cross-repo resolution readiness gating

Add a `backward_evidence_committed` readiness phase, following the existing
pattern used by `semantic_nodes_committed` → code-call gating in
`graph_projection_phase_state`:

1. During bootstrap, `backward_evidence_committed` starts as NOT committed for
   each scope/generation.
2. After the deferred pass stores evidence for a generation, mark the phase as
   committed.
3. In `CrossRepoRelationshipHandler.Resolve()`, check the readiness phase
   before loading evidence. If not ready, return 0 edges (the intent succeeds
   but writes no cross-repo edges).

This is a soft gate: the `deployment_mapping` intent itself succeeds (platform
edges and infrastructure edges still write), only the cross-repo resolution
sub-step is skipped.

#### Readiness key shape

The `graph_projection_phase_state` primary key is `(scope_id,
acceptance_unit_id, source_run_id, generation_id, keyspace, phase)` — all
fields are validated non-blank (`go/internal/reducer/graph_projection_phase.go:51`).

For `backward_evidence_committed`, the key components are derived from the
`deployment_mapping` intent context:

| Field | Value | Source |
|-------|-------|--------|
| `ScopeID` | repo's scope_id | `intent.ScopeID` in `Resolve()` |
| `AcceptanceUnitID` | repo's scope_id | repo-level acceptance (no sub-units for cross-repo) |
| `SourceRunID` | repo's generation_id | `intent.GenerationID` passed to `Resolve()` |
| `GenerationID` | repo's generation_id | same as SourceRunID for single-generation intents |
| `Keyspace` | `"cross_repo_evidence"` | new constant in `graph_projection_phase.go` |
| `Phase` | `"backward_evidence_committed"` | new constant in `graph_projection_phase.go` |

The deferred pass publishes one readiness row per target repo's
scope/generation. The resolver checks readiness before loading evidence:

```go
key := reducer.GraphProjectionPhaseKey{
    ScopeID:          scopeID,
    AcceptanceUnitID: scopeID,
    SourceRunID:      generationID,
    GenerationID:     generationID,
    Keyspace:         reducer.GraphProjectionKeyspaceCrossRepoEvidence,
}
ready, found := readinessLookup(key, reducer.GraphProjectionPhaseBackwardEvidenceCommitted)
if !ready || !found {
    slog.InfoContext(ctx, "cross-repo resolution gated",
        slog.String(telemetry.LogKeyScopeID, scopeID),
        slog.String(telemetry.LogKeyGenerationID, generationID),
        slog.String("reason", "backward_evidence_not_committed"),
    )
    return 0, nil
}
```

The existing `GraphProjectionPhaseRepairer` (`graph_projection_phase_repair_runner.go`)
handles republication if the initial readiness write fails — no new repair
infrastructure is needed.

### Sub-design 3: Reopen succeeded deployment_mapping work items

The current reducer queue uses `INSERT ... ON CONFLICT (work_item_id) DO NOTHING`
(`go/internal/storage/postgres/reducer_queue.go:27`). The `work_item_id` is
deterministic: `reducer_` + `scopeID` + `_` + `generationID` + `_` + `domain` +
`_` + `entityKey` (`reducer_queue.go:456`). Once a `deployment_mapping` intent
succeeds with zero cross-repo edges (soft-gated), a naive re-INSERT of the same
identity is silently dropped.

**Mechanism:** Add a `ReopenSucceeded` SQL path to `ReducerQueue`:

```sql
UPDATE fact_work_items
SET status = 'pending',
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work_item_id = $2
  AND status = 'succeeded'
```

This is the minimal change that fits the existing queue model:

- No new domain, entity key suffix, or second-pass identity needed.
- Only reopens `succeeded` items — does not touch in-progress or failed ones.
- The reopened intent runs identically to the first pass; the only difference is
  that `backward_evidence_committed` readiness is now marked, so cross-repo
  resolution proceeds with complete evidence.
- Platform and infrastructure edges written during the first pass are idempotent
  (retract-then-write pattern in `PlatformMaterializationHandler`), so the
  second pass does not produce duplicates.

**Why not a retryable error at the readiness gate?** The `deployment_mapping`
handler writes platform edges and infrastructure edges *before* cross-repo
resolution (`platform_materialization.go:69-91`). A retryable failure at the
cross-repo step would cause the entire handler to retry, re-doing platform and
infrastructure writes unnecessarily. The soft gate + reopen approach avoids this
by letting the first pass succeed for platform/infrastructure and only replaying
for the cross-repo resolution step.

**Why not a distinct second-pass identity?** A new domain or entity-key suffix
would create a second work item that shares no history with the original. The
reducer runtime would need special-casing to know that this second item is a
cross-repo-only replay, not a full deployment_mapping pass. Reopening the
original item is simpler and uses the exact same handler code path.

After the deferred pass completes and readiness phases are marked:

1. Query all `deployment_mapping` work items with `status = 'succeeded'` for
   the generations processed during bootstrap.
2. Call `ReopenSucceeded` for each, setting status back to `pending`.
3. The reducer claims and re-processes them. This time, the readiness check
   passes, evidence is loaded, and cross-repo edges are resolved.

The second pass is **mandatory for bootstrap completeness**, not optional
cleanup. If the reopen step fails, bootstrap must exit non-zero (see
Implementation §3 below).

### Why Not the Others

| Option | Rejection reason |
|--------|-----------------|
| A (do nothing) | Quadratic cost not acceptable as corpus grows |
| C (inverted index) | Correct but over-engineered for a problem solved by deferral |
| D (parallelize) | Constant-factor improvement, does not fix repeated scans |
| E (reducer domain) | Architectural overkill for a bootstrap-specific problem |

## Edge Cases and Design Constraints

1. **Evidence completeness.** The deferred pass calls the same
   `DiscoverEvidence` function with the same inputs (all content+file facts,
   full catalog) and produces the same evidence as the per-repo path would
   produce in aggregate. The only difference is timing.

2. **Concurrent reducer during bootstrap.** The resolution-engine runs as a
   separate container and processes `deployment_mapping` intents concurrently
   with bootstrap. The `backward_evidence_committed` readiness gate prevents
   cross-repo resolution from finalizing against incomplete backward evidence.
   Forward evidence (from `afterBatch`) is still available during bootstrap, so
   forward-direction edges are not affected.

3. **Interrupted bootstrap + restart.** If bootstrap is killed after 400 of
   792 repos are committed but before the deferred pass runs, bootstrap exits
   non-zero (deferred pass failure is fatal). The operator must re-run.

   **Storage reality:** `UpsertEvidenceFacts` uses
   `INSERT ... ON CONFLICT (evidence_id) DO NOTHING` with a content-based
   evidence ID. Re-runs with the same evidence content are idempotent, but
   stale evidence rows are not automatically retracted if the preserved run
   restarts from a different generation state.

   **Required handling:**
   - **Clean restart (volumes wiped):** full re-bootstrap, deferred pass runs
     on all repos at the end. Correct.
   - **Resume restart (volumes preserved):** existing active generations and
     evidence rows must be treated carefully. Content-based IDs avoid duplicate
     inserts for identical evidence, but stale evidence tied to superseded
     generations still relies on the broader generation lifecycle and recovery
     semantics.

   Since the deferred pass is fatal, an interrupted bootstrap always results in
   either a clean restart or a resume restart — never a silent partial
   completion.

4. **Forward evidence during bootstrap.** The `afterBatch` callback still runs
   during bootstrap (it is not gated by `SkipRelationshipBackfill`). Forward
   evidence (current repo references existing repos) is discovered at commit
   time. Only backward evidence (existing facts reference new repo) is
   deferred.

5. **Deferred pass cost at scale.** At 792 repos with 91,649 facts, the single
   deferred pass takes ~20s. At 2,000 repos with ~250,000 facts, the cost is
   proportional to `total_facts * catalog_size` and may take ~60-90s. This is a
   one-time cost and can be parallelized (Option D) if it grows further.

6. **Steady-state ingestion unaffected.** The per-repo backfill in the
   ingester path (`SkipRelationshipBackfill = false`) remains unchanged. New
   repos during normal ingestion still trigger immediate backward evidence
   discovery, and `backward_evidence_committed` is marked immediately. The
   `ReopenSucceeded` path is not used in steady-state — it is
   bootstrap-specific.

8. **Reopen races with in-flight reducer claims.** `ReopenSucceeded` only
   updates rows with `status = 'succeeded'`. If a `deployment_mapping` intent
   is still `claimed` or `running` when the deferred pass completes, the
   reopen UPDATE is a no-op for that row.

   The earlier version of this ADR proposed waiting for all
   `deployment_mapping` intents to reach terminal status before reopening.
   The validated implementation removed that terminal wait because it is
   structurally incompatible with the shared FIFO reducer queue: monster repos
   can occupy all workers for hours, making the wait longer than bootstrap
   itself.

   Current implementation rule:
   - reopen only rows already in `succeeded`
   - allow pending/claimed rows to continue naturally with the gate now open
   - accept that a small reopen race window remains for rows that transition
     to `succeeded` during the reopen pass

   Those race-window stragglers are **not** automatically replayed today.
   They require manual admin replay or a future automated straggler-replay
   path.

9. **Reopen idempotency.** If `ReopenSucceeded` runs twice (e.g., retry
   after a transient Postgres error), it is safe: reopening an already-pending
   item is a no-op (`WHERE status = 'succeeded'` does not match `pending`).
   Reopening an item that the reducer has already re-claimed is also safe
   (status is `claimed`, not `succeeded`).

7. **Evidence scoped to actual generation IDs.** The deferred pass must store
   evidence under each target repo's actual generation ID, not a synthetic one.
   `ListEvidenceFacts(ctx, generationID)` filters by generation ID, so evidence
   stored under a different ID would be invisible to the resolver.

## Implementation Sketch

### 1. IngestionStore option

`IngestionStore` currently carries `db`, `beginner`, and `Now`
(`go/internal/storage/postgres/ingestion.go:142`). The deferred backfill
function needs a tracer and instruments reference. Two options:

- **Option A:** Add `Tracer` and `Instruments` fields to `IngestionStore`.
- **Option B:** Accept tracer and instruments as parameters to the deferred
  backfill function (avoids struct change, keeps it a bootstrap-specific call).

Prefer Option B to minimize IngestionStore surface change:

```go
type IngestionStore struct {
    db                        ExecQueryer
    beginner                  Beginner
    Now                       func() time.Time
    SkipRelationshipBackfill  bool
}
```

In `CommitScopeGeneration`, guard the backfill:

```go
if !s.SkipRelationshipBackfill {
    if err := backfillRelationshipEvidenceForNewRepositories(...); err != nil {
        return err
    }
}
```

### 2. Deferred backfill function

Accepts tracer and instruments as parameters. Stores evidence under each
target repo's actual generation ID:

```go
func (s IngestionStore) BackfillAllRelationshipEvidence(
    ctx context.Context,
    tracer trace.Tracer,
    instruments *telemetry.Instruments,
) error {
    var span trace.Span
    if tracer != nil {
        ctx, span = tracer.Start(ctx, "relationship.backfill_deferred")
        defer span.End()
    }
    start := time.Now()

    catalog, err := loadRepositoryCatalog(ctx, s.db)
    // ... load facts, run DiscoverEvidence once ...
    // Group evidence by TargetRepoID
    // For each target repo, look up active generation ID
    // Store evidence under that generation ID
    // Mark backward_evidence_committed readiness for each generation
    // Reopen succeeded deployment_mapping intents
}
```

### 3. Bootstrap integration

The deferred pass must be wired into `runPipelined` in
`go/cmd/bootstrap-index/main.go`. The current structure:

```go
func runPipelined(ctx, cd, pd, workers, tracer, instruments, logger) error {
    collectorDone := make(chan struct{})
    errc := make(chan error, 2)

    go func() {
        defer close(collectorDone)
        err := drainCollector(...)
        errc <- err
    }()

    go func() {
        err := drainProjectorPipelined(...)
        errc <- err
    }()

    collectorErr := <-errc
    // ... wait for projector ...
}
```

The deferred pass runs **after the collector goroutine completes** (all facts
committed) **but before `runPipelined` returns** (projector may still be
draining). Insert between collector completion and projector wait:

```go
collectorErr := <-errc
if collectorErr != nil {
    cancel()
    projectorErr := <-errc
    return errors.Join(collectorErr, projectorErr)
}

// Deferred relationship backfill: single corpus-wide pass.
// This is FATAL if it fails. Without the deferred pass:
//   - backward evidence is never stored
//   - backward_evidence_committed readiness is never marked
//   - the soft gate permanently skips cross-repo resolution
//   - ReopenSucceeded never runs
//   - cross-repo edges are permanently missing
// There is no durable repair path that survives bootstrap-index
// process exit. The operator must re-run bootstrap.
if err := cd.committer.BackfillAllRelationshipEvidence(
    ctx, tracer, instruments,
); err != nil {
    logger.ErrorContext(ctx, "deferred relationship backfill failed",
        slog.String("error", err.Error()),
        telemetry.FailureClassAttr("backfill_deferred_failure"),
    )
    cancel()
    projectorErr := <-errc
    return fmt.Errorf("deferred backfill fatal: %w",
        errors.Join(err, projectorErr))
}

// Wait for the source-local projector to drain before reopening reducer work.
// Otherwise deployment_mapping items emitted after reopen starts could miss
// reopening and remain soft-gated.
projectorErr := <-errc
if projectorErr != nil {
    return projectorErr
}

// Reopen succeeded deployment_mapping work items so the reducer re-processes
// them with complete backward evidence. In-flight stragglers are NOT
// automatically replayed today and require manual admin replay or a future
// automated straggler-replay mechanism.
if err := cd.committer.ReopenDeploymentMappingWorkItems(
    ctx, tracer, instruments,
); err != nil {
    logger.ErrorContext(ctx, "reopen deployment_mapping work items failed",
        slog.String("error", err.Error()),
        telemetry.FailureClassAttr("reopen_deployment_mapping_failure"),
    )
    cancel()
    projectorErr := <-errc
    return fmt.Errorf("reopen deployment_mapping fatal: %w",
        errors.Join(err, projectorErr))
}

// Bootstrap helper is complete. Reopened reducer work drains independently.
projectorErr := <-errc
```

### 4. Reopen succeeded items after projector drain

The implemented bootstrap flow does **not** wait for every
`deployment_mapping` item to reach terminal status before reopening. Instead it:

- runs the deferred backfill
- waits for the source-local projector to drain
- reopens already-`succeeded` `deployment_mapping` rows

This matches the validated runtime shape and avoids a terminal wait that can
last longer than bootstrap itself when monster repos occupy all reducer
workers.

### 5. ReopenSucceeded SQL path

Add to `ReducerQueue`:

```go
const reopenSucceededReducerWorkQuery = `
UPDATE fact_work_items
SET status = 'pending',
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work_item_id = $2
  AND status = 'succeeded'
`

func (q ReducerQueue) ReopenSucceeded(ctx context.Context, workItemID string) error {
    _, err := q.db.ExecContext(ctx, reopenSucceededReducerWorkQuery, q.now(), workItemID)
    if err != nil {
        return fmt.Errorf("reopen succeeded reducer work: %w", err)
    }
    return nil
}
```

### 6. Evidence ID stability

`relationship_store.go` already derives evidence IDs from content-bearing
fields plus serialized details rather than loop index. That makes
`ON CONFLICT (evidence_id) DO NOTHING` stable across discovery ordering and
lets the deferred pass reuse the existing evidence store semantics without a
bootstrap-specific ID scheme.

### 7. Observability (mandatory per telemetry contract)

Every code path introduced by this ADR must be operator-visible at 3 AM.

**OTEL spans:**

| Span name | When | Key attributes |
|-----------|------|----------------|
| `relationship.backfill_deferred` | deferred pass | `fact_count`, `catalog_size`, `evidence_count`, `duration_seconds` |

**Metrics (registered in `telemetry/instruments.go`, `eshu_dp_` prefix):**

| Metric | Type | When |
|--------|------|------|
| `eshu_dp_deferred_backfill_duration_seconds` | Histogram | deferred pass duration |
| `eshu_dp_deferred_backfill_evidence_total` | Counter | evidence facts produced by deferred pass |
| `eshu_dp_deployment_mapping_reopened_total` | Counter | succeeded work items reopened after deferred pass |

**Structured logs (slog):**

| Message | Level | Key fields |
|---------|-------|------------|
| `deferred_backfill_completed` | INFO | `evidence_facts`, `readiness_rows`, `duration_s` |
| `cross-repo resolution gated` | INFO | `scope_id`, `generation_id`, `reason=backward_evidence_not_committed` |
| `deferred relationship backfill failed` | ERROR | `error`, `failure_class=backfill_deferred_failure` (fatal) |
| `deployment_mapping_reopened` | INFO | `count` |
| `reopen deployment_mapping work items failed` | ERROR | `error`, `failure_class=reopen_deployment_mapping_failure` (fatal) |

**Operator questions this answers:**

- "Did bootstrap produce complete relationship evidence?" — check
  `eshu_dp_deferred_backfill_evidence_total` and bootstrap exit code.
- "How long did the deferred pass take?" — check
  `eshu_dp_deferred_backfill_duration_seconds`.
- "Were any cross-repo resolution intents gated?" — check
  `cross-repo resolution gated` logs during the first pass.
- "Did the reopen step succeed?" — check
  `eshu_dp_deployment_mapping_reopened_total` and bootstrap exit code.
- "Are cross-repo edges complete?" — after reopened intents drain, cross-repo
  edge count should match the evidence count.

## Expected Impact

| Metric | Before | After |
|--------|--------|-------|
| Per-repo commit (end of bootstrap) | ~20s | ~50ms |
| Bootstrap-index helper duration | 1h 33m 22s at 792 repos | 29m 42s at 896 repos |
| Deferred backfill pass | N/A | ~20s (once) |
| Relationship evidence correctness | Complete | Complete (gated) |
| Steady-state ingestion | Unchanged | Unchanged |
| Bootstrap scaling | N repeated growing scans | One corpus-wide scan |

Note: the deferred pass cost is proportional to `total_facts * catalog_size`.
At 792 repos this is ~20s. At 2,000 repos it may be ~60-90s. The main gain is
eliminating repeated scans from the per-repo commit path: bootstrap now pays
for one corpus-wide scan instead of N growing scans.

## Verification and Exit Criteria

1. Unit test: `CommitScopeGeneration` with `SkipRelationshipBackfill=true`
   does not call `backfillRelationshipEvidenceForNewRepositories`.
2. Unit test: deferred backfill produces evidence matching the per-repo path
   for a multi-repo dataset, stored under correct per-repo generation IDs.
3. Unit test: `CrossRepoRelationshipHandler.Resolve()` returns 0 edges when
   `backward_evidence_committed` readiness phase is not marked.
4. Unit test: `CrossRepoRelationshipHandler.Resolve()` loads evidence and
   resolves edges when `backward_evidence_committed` readiness phase IS marked.
5. Unit test: evidence ID remains content-based and idempotent across re-runs.
6. Unit test: `ReopenSucceeded` transitions `succeeded` items to `pending` and
   is a no-op for items in other states (`claimed`, `pending`, `dead_letter`).
7. Unit test: deferred backfill failure causes `runPipelined` to return a
   non-nil error (fatal).
8. Unit test: reopen failure causes `runPipelined` to return a non-nil error
   (fatal).
9. Unit test: readiness key shape for `backward_evidence_committed` matches
   the key shape used by `CrossRepoRelationshipHandler.Resolve()` — same
   `ScopeID`, `AcceptanceUnitID=scopeID`, `SourceRunID=generationID`,
   `GenerationID=generationID`, `Keyspace=cross_repo_evidence`.
10. Integration test: bootstrap with skip + deferred pass produces the same
    logical `relationship_evidence_facts` set as bootstrap without skip.
11. E2E measurement: bootstrap helper duration drops materially versus the
    pre-change baseline while preserving relationship correctness.
12. E2E measurement: cross-repo relationship edge count matches previous run
    after reopened `deployment_mapping` intents are re-processed by the reducer.
13. Steady-state regression test: ingester path with
    `SkipRelationshipBackfill=false` still runs per-repo backfill correctly.

Exit criteria:

1. Bootstrap removes repeated whole-corpus scans from the per-repo commit path.
2. Relationship evidence is complete and stored under correct generation IDs.
3. No `deployment_mapping` intent finalizes cross-repo edges against incomplete
   backward evidence (gated by `backward_evidence_committed` readiness phase).
4. Succeeded `deployment_mapping` work items are reopened after the deferred
   pass, not silently dropped by `ON CONFLICT DO NOTHING`.
5. Deferred backfill failure is fatal for bootstrap (non-zero exit).
6. Reopen failure is fatal for bootstrap (non-zero exit).
7. Steady-state ingestion is not affected.
8. The deferred pass is instrumented with span, duration histogram, and
   evidence count.
9. Evidence IDs remain content-based and idempotent.
10. Readiness key shape for `backward_evidence_committed` is documented and
    matches between the publisher (deferred pass) and the consumer (resolver).

## References

- E2E run evidence: 792-repo bootstrap, 2026-04-18
- Related ADR: `2026-04-17-semantic-entity-materialization-bottleneck.md`
- Ingestion commit boundary:
  `go/internal/storage/postgres/ingestion.go` (lines 174-301)
- Relationship backfill:
  `go/internal/storage/postgres/ingestion.go` (lines 400-443)
- Relationship evidence storage:
  `go/internal/storage/postgres/relationship_store.go` (lines 135-171)
- Evidence INSERT semantics:
  `go/internal/storage/postgres/relationship_schema.go` (line 159,
  `ON CONFLICT (evidence_id) DO NOTHING`)
- Evidence ID derivation:
  `go/internal/storage/postgres/relationship_store.go` (line 146, content-based
  digest)
- Cross-repo resolution:
  `go/internal/reducer/cross_repo_resolution.go` (line 87,
  `ListEvidenceFacts(ctx, generationID)`)
- Platform materialization handler:
  `go/internal/reducer/platform_materialization.go` (line 96, calls
  `CrossRepoResolver.Resolve`)
- Reducer queue identity and enqueue semantics:
  `go/internal/storage/postgres/reducer_queue.go` (line 19, INSERT ... ON
  CONFLICT DO NOTHING; line 456, `reducerWorkItemID` derivation)
- Reducer queue Ack (succeeded):
  `go/internal/storage/postgres/reducer_queue.go` (line 75, `ackReducerWorkQuery`)
- Graph projection readiness phase key:
  `go/internal/reducer/graph_projection_phase.go` (line 34,
  `GraphProjectionPhaseKey` struct; line 51, `Validate()`)
- Graph projection readiness store:
  `go/internal/storage/postgres/graph_projection_phase_state.go` (line 19,
  schema; line 137, `PublishGraphProjectionPhases`)
- Graph projection repair runner:
  `go/internal/reducer/graph_projection_phase_repair_runner.go` (line 72,
  `Run()` loop)
- Deployment mapping fact emission:
  `go/internal/collector/git_fact_builder.go` (line 444,
  `entity_key = deployment:<repoBaseName>`)
- Bootstrap orchestration:
  `go/cmd/bootstrap-index/main.go` (lines 162-209, `runPipelined`)
- Relationship evidence discovery:
  `go/internal/relationships/evidence.go` (lines 18-32)
- Catalog matching:
  `go/internal/relationships/evidence.go` (lines 479-533)
- Latest relationship facts query:
  `go/internal/storage/postgres/ingestion.go` (lines 24-63)
- Service runtime topology:
  `docs/docs/deployment/service-runtimes.md`
