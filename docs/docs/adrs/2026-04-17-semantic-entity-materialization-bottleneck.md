# ADR: Semantic Entity Materialization Throughput Bottleneck

**Status:** Accepted; follow-up superseded by 2026-04-28
**Date:** 2026-04-17  
**Related:** `2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`

## Status Review (2026-05-03)

**Current disposition:** Implemented, with follow-up owned by the reducer
throughput ADR.

The acceptance-unit fix is present in code: semantic intents are keyed by
repository acceptance unit, the reducer filters materialization to that target
repository, and reducer tests cover readiness publication plus target-repo
filtering.

**Remaining work:** semantic throughput remains a measured NornicDB/full-corpus
hot path, but that work moved to
`2026-04-28-reducer-throughput-and-nornicdb-concurrency-plan.md`.

## Decision

Adopt **acceptance-unit scoped semantic work identity** as the primary fix for
semantic materialization throughput:

1. emit semantic reducer intents per bounded repository acceptance unit, not
   per semantic entity
2. execute semantic materialization exactly for that bounded unit, not for the
   full scope slice
3. publish `semantic_nodes_committed` exactly for that bounded unit after the
   bounded semantic write commits
4. keep readiness repair and generation guards unchanged

This ADR does **not** recommend higher reducer worker counts as the primary
solution. Worker tuning is secondary and should be re-measured only after work
identity and execution scope are aligned.

## Context

During end-to-end validation of the cross-phase entity-not-found race fix
(896 repositories, full bootstrap), semantic entity materialization emerged as
the dominant throughput bottleneck in the reducer path.

The deadlock-elimination work succeeded at its primary goal:

- 0 `EntityNotFound` terminal failures
- 0 permanently failed intents
- retryable deadlocks only

That success exposed a second problem that had previously been masked by
terminal failure noise:

- semantic work identity is too fine-grained at enqueue time
- semantic execution scope is too broad at handler time
- increasing reducer concurrency alone is therefore not a principled fix

This ADR records the current flow, the actual bottleneck, the edge cases that
must shape the design, and the recommended structural change.

## Why This ADR Exists

The current semantic path is paying large-repo costs repeatedly while exposing
operators to a misleading tuning story.

Without this change:

- queue depth can look like an entity-count problem
- handler cost can behave like a repo-scope rewrite problem
- worker tuning can appear to help while still preserving duplicate semantic
  rewrites

That combination makes it too easy to choose operational mitigation where an
architectural correction is required.

## Current Flow

The current semantic pipeline is not actually "one small Neo4j write per
entity." It is:

1. The projector emits one reducer intent per qualifying semantic entity fact.
2. Each emitted intent is keyed by the entity ID.
3. The reducer claims one semantic intent.
4. The semantic handler loads all facts for the full
   `scope_id + generation_id`.
5. The handler extracts every semantic entity row present in that full fact
   slice.
6. The Neo4j semantic writer retracts and rewrites the semantic-entity graph
   state for every touched repo in that slice.
7. The handler publishes `semantic_nodes_committed` for every touched repo in
   that slice.
8. The reducer acknowledges the single claimed intent.

So for a repository with 900 semantic entities, the system can enqueue roughly
900 distinct semantic work items even though each claimed work item is already
performing a scope-wide semantic reload and repo-wide retract-plus-rewrite.

That means the system currently has a work-identity mismatch:

- queue identity is per entity
- execution scope is per scope/generation, often spanning the full repo slice

This is the key architectural finding.

## Implementation Reality

The current implementation has three important properties:

1. **Intent emission is entity keyed.** The projector emits semantic intents
   keyed by semantic entity ID.
2. **Queue identity follows that entity key.** Reducer work item IDs are built
   from `scope_id + generation_id + domain + entity_key`.
3. **Handler execution is broader than queue identity.** The semantic handler
   loads all facts for the current scope and generation, extracts all semantic
   rows from that slice, and the semantic writer retracts and rewrites the
   semantic graph state for all touched repos in that slice.

So the queue currently models "one semantic entity" while the handler models
"one scope-wide semantic refresh." That difference is the main design flaw.

## Evidence

### E2E Run Observations (2026-04-17, 896 repos)

| Metric | Value |
|--------|-------|
| Bootstrap progress during observation | 840 / 896 (94%) and still running |
| Semantic entities pending | 1,132 in a new large-repo batch |
| Code-call edges pending | 15,841 |
| Code-call edges completed | 27,386 |
| Deadlocks | 32 retryable, all retried, 0 permanent |
| Reducer workers | 4 (`ESHU_REDUCER_WORKERS=4`) |
| Large repo semantic volume | 900+ entities in one repo slice |

### E2E Run Observations (2026-04-18, live follow-up run)

| Metric | Value |
|--------|-------|
| Semantic entities pending | 24,778 |
| Semantic entities succeeded | 24,423 |
| Semantic entities claimed | 37 |
| Per-intent duration | about 1.8 to 2.0 seconds |
| Downstream blocked domains | code-call backlog still gated, with ~30 pending in the observed slice |
| Deadlocks | 0 observed in this snapshot |
| Permanently failed | 0 |
| Notable repo behavior | repeated entity-keyed processing for the same large repo slice (`r_bb28b557`) |

This second run strengthens the same conclusion as the first run. The system is
not merely slow because semantic volume is high; it is reprocessing the same
large repository acceptance unit through many entity-keyed intents, each taking
roughly the same wall time. That is direct evidence of duplicate-rewrite
amplification rather than a pure lack-of-workers problem.

### What These Numbers Prove

The current observations are strong evidence of two things:

1. semantic materialization is the active bottleneck while bootstrap continues
   to advance
2. downstream code-entity edge domains accumulate behind semantic readiness
   even when canonical progress is otherwise healthy

### What These Numbers Do Not Yet Prove

The current observations do **not** justify a higher worker count as a
decision-grade immediate recommendation.

Specifically, these numbers do not yet prove:

1. that reducer workers are the true limiting factor rather than duplicate
   semantic rewrites
2. that increasing workers from 4 to 8 improves useful throughput more than it
   increases overlapping semantic write contention
3. that any projected 10x to 20x speedup estimate is accurate enough to treat
   as a committed outcome before implementation and measurement

The observations are therefore sufficient to justify the structural fix, but
not to skip the architectural analysis and jump straight to worker tuning.

### What The Current Cost Model Really Is

The expensive loop is not:

```text
claim 1 entity -> write 1 entity -> ack 1 entity
```

The expensive loop is closer to:

```text
claim 1 entity-keyed intent
  -> load all facts for scope/generation
    -> extract all semantic rows for touched repos
      -> retract and rewrite semantic graph state for touched repos
        -> publish readiness for touched repos
          -> ack 1 entity-keyed intent
```

For large repos, this means the system can repeat nearly the same semantic
retract-plus-rewrite work many times while only the queue identity changes.

### Downstream Impact

Semantic entity materialization gates all code-entity edge domains through the
readiness contract:

```text
semantic_entity_materialization
  -> publishes semantic_nodes_committed
    -> unblocks code_call projection
    -> unblocks inheritance_edges
    -> unblocks sql_relationships
```

When semantic materialization drains slowly, downstream edge work stays blocked
even though canonical nodes are already committed.

## Root Cause

The root cause is not only "too many reducer intents."

The root cause is:

> Semantic entity materialization is queued with per-entity identity but
> executed with scope-wide, repo-slice semantics, so the reducer pays queue,
> fact-loading, retract, rewrite, and readiness-publication cost repeatedly for
> work that is already logically acceptance-unit scoped.

That mismatch creates three concrete problems:

1. Duplicate work amplification.
2. Poor scaling on large repos.
3. Unsafe optimism about worker-based tuning.

## Why Worker Count Alone Is Not The Primary Fix

Increasing `ESHU_REDUCER_WORKERS` may improve throughput in some environments,
but it is not a safe primary recommendation for the current architecture.

Reasons:

1. More workers increase concurrent semantic transactions.
2. The current semantic writer performs retract-plus-rewrite for touched repos
   in one grouped Neo4j transaction.
3. If multiple entity-keyed intents for the same repo slice are active at the
   same time, higher worker counts can increase overlap between duplicate
   repo-scope semantic rewrites.
4. Neo4j's own guidance is that frequent deadlocks indicate a concurrent write
   pattern problem and should be addressed by ordering or by removing
   conflicting overlap, not by assuming retries are an acceptable steady-state
   tax.

Worker tuning should therefore be treated as a bounded post-fix measurement
exercise after work identity and execution scope are aligned, not as the
headline mitigation.

## Non-Goals

This ADR intentionally does not do the following:

1. introduce a global single-threaded semantic writer
2. weaken the graph-readiness contract for downstream code-entity edge domains
3. rely on retries or worker-count changes as the primary elimination mechanism
4. turn semantic materialization into a generic workflow engine for unrelated
   reducer domains
5. choose a final internal Neo4j chunk size for very large repos before the
   acceptance-unit work identity change lands

## Design Goal

Align semantic work identity with semantic execution scope.

For semantic materialization, the natural bounded unit is the same one used by
the readiness contract:

- `scope_id`
- `acceptance_unit_id` (repository)
- `source_run_id`
- `generation_id`

The system should enqueue and execute semantic work at that bounded-unit level.

## Options

### Option A: Increase Reducer Workers Now

Set `ESHU_REDUCER_WORKERS=8` or higher without changing semantic work identity.

**Pros:**

- zero code change
- may improve throughput on some runs

**Cons:**

- does not remove duplicate semantic rewrites
- can increase overlapping retract-plus-rewrite transactions for the same repo
- can increase retryable deadlock churn
- makes batching and queue depth look better without fixing the architectural
  mismatch

**Verdict:** not decision-grade as an immediate recommendation. At most, use as
a bounded staging experiment after explicitly accepting the risk that higher
concurrency may amplify overlapping semantic repo rewrites before the
acceptance-unit fix lands.

### Option B: Acceptance-Unit Scoped Semantic Intents (Recommended)

Emit semantic reducer intents per bounded repository unit instead of per
entity, and make the handler execute only that bounded repository unit.

**Target flow:**

```text
claim repo-scoped semantic intent
  -> load facts for scope/generation
  -> filter to the target acceptance unit
  -> extract semantic rows for that repo only
  -> retract and rewrite semantic graph state for that repo only
  -> publish semantic_nodes_committed for that repo only
  -> ack repo-scoped intent
```

**Pros:**

- removes the per-entity identity mismatch
- eliminates repeated queue claim and ack cost for duplicate entity-scoped
  semantic work
- reduces repeated fact loading for the same repo slice
- makes readiness publication deterministic at the same bounded unit used by
  downstream gating
- makes concurrency reasoning tractable because one semantic work item maps to
  one acceptance unit

**Cons:**

- requires code changes in intent emission and handler filtering
- a single repo-scoped transaction can be large for very large repositories
- retry granularity becomes per acceptance unit instead of per entity

**Verdict:** recommended primary fix.

### Option C: Repo-Scoped Intents With Internal Chunking

Use acceptance-unit scoped semantic intents, but chunk the Neo4j write inside
the bounded unit when the semantic row count crosses a threshold.

**Pros:**

- preserves acceptance-unit alignment
- avoids one unbounded Neo4j transaction for very large repos
- provides a cleaner path for future tuning than raw worker increases

**Cons:**

- more design work than pure repo-scoped intent alignment
- must preserve correctness for retract and readiness publication across
  multiple internal write chunks

**Verdict:** strong follow-up if repo-scoped transactions prove too large in
practice.

### Option D: Batch Claim Existing Entity-Scoped Intents

Keep per-entity semantic intents but group them during claim or execution.

**Pros:**

- less change to projector emission

**Cons:**

- keeps the wrong work identity
- still requires deduplication logic at claim time
- still leaves the semantic handler broader than the queue identity
- improves mechanics without fixing the model

**Verdict:** not preferred.

## Edge Cases And Design Constraints

The recommended change must handle these explicitly:

1. Empty semantic repo slice. If a repo-scoped semantic intent finds no
   semantic rows for its target acceptance unit, the system must decide whether
   that means "nothing to write" or "retract stale semantic nodes and still
   publish readiness." The contract must be explicit.
2. Multiple repos in one scope/generation. The handler must not keep
   scope-wide behavior after repo-scoped intent emission, or the main benefit
   is lost.
3. Duplicate repo-scoped intent emission. The queue identity must remain
   deterministic so duplicate projector emissions collapse safely.
4. Stale generations. Repo-scoped semantic work must continue to respect the
   existing generation-freshness guard.
5. Publish failure after successful write. The repair-queue path for readiness
   publication must remain intact, because this is still a graph-write phase.
6. Very large repos. One repo can still have hundreds or thousands of semantic
   rows. If a single repo-scoped transaction is too large, chunking must happen
   inside the bounded unit rather than by reintroducing per-entity queue
   identity.
7. Readiness exactness. `semantic_nodes_committed` must be published only for
   the acceptance unit whose semantic graph state actually committed.
8. Concurrency preservation. The design should preserve concurrency across
   different repositories and generations. It should not collapse the whole
   reducer into a single global semantic lane.

## Recommendation

Adopt **Option B** as the structural fix:

1. Emit semantic intents per acceptance unit, not per entity.
2. Make the semantic handler execute exactly that acceptance unit.
3. Keep readiness publication exact and repo-scoped.
4. Keep the repair queue for post-write publication failures.

Treat worker tuning as secondary:

1. Do not make `ESHU_REDUCER_WORKERS=8` the main recommendation in this ADR.
2. Re-measure worker count only after acceptance-unit scoped semantic execution
   is in place.
3. If large repo-scoped transactions remain a problem, add internal chunking
   within the bounded repo unit rather than restoring per-entity work identity.

## Implementation Sketch

### 1. Intent emission

Replace per-entity semantic reducer emission with one semantic work item per
acceptance unit.

Intent identity should be based on the bounded repo unit, not on individual
entity IDs.

### 2. Handler scope

`SemanticEntityMaterializationHandler.Handle()` must stop treating every
claimed semantic intent as "load full scope and rewrite every touched repo."

Instead it should:

1. identify the target repo acceptance unit from the intent
2. load facts for the scope/generation
3. filter envelopes and rows to that repo
4. write semantic graph state for that repo only
5. publish readiness for that repo only

### 3. Queue identity

The reducer work-item ID should collapse duplicate semantic emissions for the
same bounded repo unit and generation.

### 4. Telemetry

Before rollout, add or confirm operator-visible signals for:

- semantic rows written per intent
- repos touched per semantic intent
- semantic write duration
- semantic retries and deadlocks
- blocked readiness age for downstream code-entity edge domains

## Rollout Plan

### Phase 1: Align work identity

1. Change semantic intent emission from entity scoped to acceptance-unit scoped.
2. Preserve deterministic queue identity for duplicate repo-scoped emissions.
3. Add focused projector tests for the new semantic intent identity.

### Phase 2: Align handler scope

1. Make the semantic handler resolve and execute only the targeted acceptance
   unit.
2. Limit retract, rewrite, and readiness publication to that exact repo slice.
3. Keep generation freshness and repair-queue behavior intact.

### Phase 3: Measure and tune

1. Compare semantic queue depth and drain time before and after the change.
2. Re-measure downstream blocked readiness age and code-call backlog drain.
3. Only after that, evaluate whether worker-count changes or internal chunking
   improve throughput without increasing contention.

## Verification and Exit Criteria

Minimum verification for the eventual code change:

1. projector tests proving semantic intent emission is repo scoped
2. reducer tests proving the semantic handler filters to the target repo
3. readiness tests proving only the target repo publishes
4. stale-generation tests proving superseded semantic work is skipped
5. retry and repair tests proving post-write readiness publication failures are
   still repaired correctly
6. end-to-end measurement comparing:
   - semantic queue depth
   - semantic drain duration
   - blocked code-call backlog age
   - retryable deadlock count

Exit criteria for this ADR's implementation:

1. one bounded repository acceptance unit produces at most one semantic work
   item per generation
2. one claimed semantic work item rewrites at most one repository acceptance
   unit
3. downstream readiness publication is exact for that same bounded unit
4. large-repo semantic drain time drops materially relative to the current
   duplicate-rewrite path
5. retryable deadlocks do not regress materially after the identity/scope fix
6. worker tuning decisions are made from post-fix measurements, not pre-fix
   queue shape

## References

- E2E run evidence: 896-repo bootstrap, 2026-04-17
- Related ADR: `2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`
- Projector semantic intent emission:
  `go/internal/projector/semantic_entity_intents.go`
- Projector intent assembly:
  `go/internal/projector/runtime.go`
- Semantic reducer handler:
  `go/internal/reducer/semantic_entity_materialization.go`
- Semantic Neo4j writer:
  `go/internal/storage/neo4j/semantic_entity.go`
- Reducer queue identity:
  `go/internal/storage/postgres/reducer_queue.go`
