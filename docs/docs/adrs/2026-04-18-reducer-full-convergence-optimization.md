# ADR: Reducer Full-Convergence Optimization

**Status:** Superseded
**Date:** 2026-04-18
**Related:** `2026-04-18-e2e-validation-atomic-writes-deferred-backfill.md`

## Status Review (2026-05-03)

**Current disposition:** Superseded.

This ADR correctly separated reducer convergence from bootstrap performance, but
its optimization plan has been replaced by the broader NornicDB/reducer
throughput workstream. The active evidence, benchmark target, rejected
hypotheses, and merged closeout now live in
`2026-04-28-reducer-throughput-and-nornicdb-concurrency-plan.md`.

**Remaining work:** none in this ADR. Keep it as historical context for why
bootstrap success did not imply reducer convergence success.

## Decision

Treat reducer convergence as a separate optimization track from the bootstrap
ADR, and sequence the work in this order:

1. Keep the already-landed label-constrained hot-path lookups.
2. Next, reduce **Neo4j transaction footprint** for the shared-projection hot
   domains:
   - `inheritance_edges`
   - `sql_relationships`
3. Only after re-measuring that change, decide whether reducer queue fairness
   needs a second implementation step.

This ADR does **not** recommend leading with higher worker counts, larger retry
buckets, or scheduler complexity before the remaining long transaction path is
reduced.

## Why This ADR Exists

The deferred bootstrap backfill ADR improved correctness and materially reduced
bootstrap helper cost, but it did not remove the dominant end-to-end tail:

- bootstrap helper completed in about `29m 42s`
- full reducer convergence still took about `5h 10m`
- a small set of pathological repositories dominated wall time
- observed Neo4j write transactions lasted about `17m` to `51m`
- reducer workers were pinned behind those writes while other work aged in the
  queue

The remaining problem is therefore not primarily bootstrap commit behavior. It
is reducer write-path behavior under skewed repositories and large shared-domain
rebuilds.

## Code-Truth Current Flow

This section is derived from the current Go implementation, not from prior ADR
text.

### Shared projection path

The shared-projection runner processes these domains:

- `platform_infra`
- `repo_dependency`
- `workload_dependency`
- `inheritance_edges`
- `sql_relationships`

Per domain and partition, the runner does:

1. claim a partition lease
2. read pending intents ordered by `created_at ASC, intent_id ASC`
3. filter to the current partition
4. discard stale or superseded generations
5. gate eligible domains on semantic-node readiness
6. retract existing canonical edges for the selected repo set
7. write replacement canonical edges
8. mark processed intents completed

In current practice, the shared runner applies that readiness gate to:

- `inheritance_edges`
- `sql_relationships`

The shared readiness utility also includes `code_calls`, but `code_calls` is
executed by the dedicated acceptance-unit runner instead of this shared
partition loop.

### Code-call path

`code_calls` already run in a dedicated acceptance-unit lane, not in the shared
partition loop. That runner:

1. claims a single `code_calls` lease
2. scans oldest pending code-call intents
3. selects one acceptance unit at a time
4. loads the full bounded acceptance-unit slice
5. retracts code-call edges for the affected repositories
6. writes code-call edges grouped by evidence source
7. marks processed intents completed

That path also gates accepted work on the same semantic-node-readiness phase,
but it does so inside the dedicated code-call runner rather than through the
shared partition schedule.

### Reducer queue path

Reducer work claims still use FIFO-like ordering:

- `ORDER BY updated_at ASC, work_item_id ASC`
- `FOR UPDATE SKIP LOCKED`

That matters because long-running work sits at the front of the visible queue
and can delay later fast work even when the fast work does not share the same
dominant cost.

## Verified Findings From Code

### 1. Option 1 is already in place

The batch Cypher used by the hot edge domains is now label constrained:

- `code_calls` uses `Function|Class|File`
- `inheritance_edges` uses `Function|Class|Interface|Trait|Struct|Enum|Protocol`
- `sql_relationships` uses `SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn`

That was the right first step because it is low-risk, correctness preserving,
and reduces planner ambiguity on the hottest lookup path.

### 2. The remaining asymmetry is transaction footprint

`EdgeWriter.WriteEdges` behaves differently by domain:

- for `code_calls`, if `CodeCallGroupBatchSize > 0`, batched statements are
  executed as multiple `ExecuteGroup` calls
- the default `code_calls` group size is already `1`
- for all other domains using `GroupExecutor`, all batched statements still go
  through a single `ExecuteGroup(ctx, stmts)` call

This means the shared hot domains still have unbounded managed-group execution
for one selected batch, while `code_calls` already has a built-in escape hatch
for limiting transaction size.

### 3. The next best target is `inheritance_edges` and `sql_relationships`

After the current label-constrained changes, the code-backed bottleneck is no
longer "unlabeled match everywhere." The more specific remaining issue is:

- `inheritance_edges` and `sql_relationships` still rebuild through a single
  grouped write call per selected batch
- those domains run inside the shared-projection path that also services other
  domains
- long grouped writes therefore pin shared workers and amplify queue age for
  unrelated work

### 4. Queue fairness is real, but it is downstream of the long-write problem

The FIFO-like claim behavior is a real fairness concern, but the code shows
that it is being amplified by the write path:

- old slow work stays visible first
- shared workers remain occupied by long Neo4j groups
- later fast work inherits the delay

Improving fairness before reducing transaction duration risks adding scheduler
complexity around the same bottleneck instead of shrinking the bottleneck
itself.

## Correctness And Edge-Case Constraints

### Shared projection is already not globally atomic

The shared-projection path already performs:

1. `RetractEdges(...)`
2. `WriteEdges(...)`
3. `MarkIntentsCompleted(...)`

Those are separate steps, not one all-or-nothing rebuild transaction. So the
system already relies on retry-driven convergence rather than true atomic
replacement of a repo's canonical shared edges.

That matters because reducing write-group size does **not** introduce a brand
new consistency model. It does, however, increase the number of intermediate
states that can exist before the final retry or completion.

### Safe chunking requirements

Any transaction-footprint reduction for `inheritance_edges` or
`sql_relationships` must preserve these invariants:

- chunking applies to the write groups, not to a sequence of
  `retract-subset -> write-subset` pairs
- failed work must remain uncompleted so the same batch is retried
- retries must continue to retract by repo set and evidence source before
  replaying writes
- chunking must stay deterministic so repeated retries do not reorder or drift
- observability must reveal partial-progress failures clearly

The intended Option 2 shape is therefore:

1. retract once for the selected repo set and evidence source
2. write replacement edges in bounded groups
3. mark intents completed only after all groups succeed

That is the smallest change from the current implementation shape. It reduces
write-group duration without changing repo selection, readiness gating, or
completion semantics.

### Partial-state window and failure signature

Today, the shared hot domains already expose a visible rebuild window:

- retract can succeed before any replacement edges are written
- a whole-write failure after retract leaves the repo with zero rebuilt edges
  for that evidence source until retry
- in the validated run, the long write groups for monster repos lasted about
  `17m` to `51m`, so that zero-edge window can already be measured in tens of
  minutes

Option 2 changes that failure signature:

- after the initial retract, some bounded groups may succeed before a later
  group fails
- readers can therefore observe a repo with only a subset of the rebuilt edges
  present until retry completes
- this is not a brand new class of visible intermediate state, but it is a
  broader set of intermediate signatures than the current "all missing or all
  rewritten" tendency

The return we want from Option 2 is to exchange one repo-sized `17m` to `51m`
write group for multiple bounded groups whose duration is short enough to
measure in seconds or low minutes. If tuning still produces multi-minute groups
that pin workers for long periods, the chunk size is still wrong.

### Domain-specific label sets must stay exact

The current label sets are not arbitrary hints. They encode real edge cases:

- `inheritance_edges` must include `Function` because PHP trait alias handling
  can emit `ALIASES` edges between method nodes
- `sql_relationships` must include `SqlIndex` because it is part of the
  canonical SQL entity family even if some relationships use it less often

Any future tightening of label sets must be proven against these cases.

## Options Compared

### Option 1: Label-constrained hot-path lookups

Pros:

- highest confidence first move
- low correctness risk
- directly reduces planner ambiguity and broad label scans

Cons:

- already landed, so it is no longer the main remaining lever
- does not by itself cap transaction duration for shared hot domains

**Result:** Correct first move. Keep it.

### Option 2: Bounded managed-group execution for shared hot domains

Examples:

- add per-domain group-size control for `inheritance_edges`
- add per-domain group-size control for `sql_relationships`
- instrument group count and duration per domain while keeping retry-driven
  convergence semantics

Pros:

- directly attacks the remaining long-transaction path
- aligns shared hot domains with an approach already used by `code_calls`
- should reduce worker pin time, lock footprint, and tail latency
- is a smaller and more evidence-backed change than scheduler redesign

Cons:

- increases the temporary partial-state window during a rebuild
- changes the dominant partial-failure signature from "retract succeeded, full
  write failed" to "retract succeeded, some write groups succeeded, later group
  failed"
- needs explicit telemetry so operators can distinguish retryable partial
  rebuilds from stuck convergence
- requires domain-specific validation under large repositories

**Recommendation:** Best next step.

### Option 3: Queue fairness or scheduler changes first

Examples:

- domain-aware claim priority
- a fast lane for reopened work
- separate worker pools for slow shared domains

Pros:

- can reduce starvation symptoms
- can improve operator experience for fast work

Cons:

- does not shorten the dominant shared write transaction
- can mask root cause with scheduler complexity
- may still leave workers pinned by the same long Neo4j groups

**Recommendation:** Defer until after Option 2 is measured.

## Expected Return

This ADR does **not** claim a precise percentage improvement yet. The current
code and validation evidence are strong enough to rank the levers, but not to
honestly promise a number without a controlled re-run.

What we can say with confidence:

- the landed label-constrained lookup change was the right first move, but it
  is unlikely to be the entire answer because shared hot domains still execute
  one large grouped write
- Option 2 has the highest remaining expected wall-clock return because it
  shortens the time a shared worker can be pinned behind one repo-sized write
- Option 3 is more likely to improve fairness than absolute throughput unless
  the long-write path is reduced first

So the expected sequence of returns is:

1. Option 1: moderate hot-path improvement, low risk, already landed
2. Option 2: best remaining chance at meaningful convergence reduction
3. Option 3: fairness and tail smoothing only after the new baseline is known

## Observability Requirements

Before or with Option 2, add or confirm telemetry for:

- shared-domain write duration by domain
- grouped-write statement count by domain
- grouped-write transaction count by domain
- retry count and retry age for shared hot domains
- queue oldest age and queue depth during shared convergence

The operator-facing question is simple:

"At 3 AM, can we tell whether the reducer is making forward progress through
bounded groups, or whether it is stalled inside one pathological write?"

If the answer is still "not clearly," the implementation is incomplete.

## Success Metrics

Track these before and after Option 2:

- full reducer convergence wall clock
- per-domain convergence completion time
- count and duration of long Neo4j write groups
- `eshu_dp_queue_oldest_age_seconds`
- `eshu_dp_queue_depth`
- reducer worker utilization during shared projection
- number of manual reducer restarts

Success means:

- fewer multi-minute shared write groups
- lower queue oldest age during convergence
- fewer workers pinned behind one repository batch
- no correctness regressions in shared edge rebuilds

## Controlled Measurement Plan

Use the same validation shape as the 2026-04-18 E2E run captured in
`2026-04-18-e2e-validation-atomic-writes-deferred-backfill.md`:

- same 896-repository corpus
- same remote instance class and Neo4j deployment
- same reducer worker count of 4 unless the test is explicitly isolating worker
  count as a separate variable
- same bootstrap and reducer runtime shape so the only intended delta is the
  shared hot-domain write-path behavior

Capture the same before/after timestamps plus the new grouped-write telemetry.
Do not change queue policy, worker count, or corpus shape in the same run if
the goal is to measure Option 2 cleanly.

## Non-Goals

This ADR does not propose:

- revisiting the bootstrap helper flow again
- raising worker counts as the primary fix
- masking long writes with retries alone
- making the reducer globally sequential
- landing queue fairness changes before the write-path baseline is known

## Next Steps

1. Keep the current label-constrained lookup changes.
2. Add bounded managed-group execution controls for `inheritance_edges` and
   `sql_relationships`.
3. Add per-domain telemetry for grouped write count and duration.
4. Re-run the same large corpus and measure convergence deltas.
5. Only then decide whether queue fairness deserves its own follow-up
   implementation ADR.
