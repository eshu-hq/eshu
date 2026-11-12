# Advisory coalescing lifecycle design (10566-A)

Status: DESIGN, not implemented. No production change lands until an
independent re-review accepts this design (issue boxes 6-7 stay unchecked).

## Problem

Each touch of an advisory group inserts one queue row per (scope,
generation, group). Live census: 20,629 rows over 2,377 conflict keys;
19,806 pending rows over 2,211 distinct groups. The handler reconciles
global winners by group id and is scope-blind except for logging, so one
live row per group would cap the backlog at the group count. Coalescing
as originally specified ("retouch any live row for the group") is
blocked: a row hosted on a foreign (scope, generation) is destroyed by
the claim-time supersede sweep when its host scope advances (P0), by
the retention-prune FK cascade while pending (P1), and concurrent
touches from two scopes can double-insert with no unique key (P1).

## Decision

Re-home `advisory_group_projection` rows to a synthetic long-lived
scope/generation owned by the supply-chain domain. Do not exempt the
domain from the supersede or prune paths.

Rejected alternative: exempting the domain from generation-keyed
supersede and retention prune. It keeps the TOCTOU enqueue race (a new
partial unique index plus locking, i.e. new concurrency machinery on
the hottest claim path), it pins real host generations: a pending
coalesced row keeps its host generation unprunable, and a hot group
retouched forever pins that generation forever. The exemption also
edits the supersede CTE predicate every claim executes (plan risk),
while re-homing touches no hot-path SQL at all.

## Mechanism

A bootstrap migration inserts one reserved scope and one genesis
generation, for example scope `synthetic:advisory-groups`
(`scope_kind='synthetic'`, owned by no collector) with generation
`synthetic:advisory-groups:genesis` in status `active`, referenced by
`ingestion_scopes.active_generation_id`. The advisory enqueuer builds
its `ReducerIntent` with that scope/generation instead of the origin
pass's, keeping domain, entity key (`advisory_group:<id>`), cause, and
source system as today.

What changes and what does not:

- `work_item_id` becomes a pure function of the group:
  `reducer_<synthetic>_<genesis>_advisory_group_projection_advisory_group:<id>`.
  The existing `ON CONFLICT (work_item_id) DO NOTHING` plus
  `RetouchWorkItem` is then already the race-free coalescing
  primitive; no new index, lock, or dedupe query.
- Conflict key is unchanged (`advisory_group` domain, hashed entity
  key), so same-group serialization and cross-group parallelism are
  exactly as today, and the live-lease unique index keeps fencing the
  same keys.
- The handler needs no logic change: winners read and graph write are
  keyed by group id; only log lines observe the synthetic scope.
- The supersede sweep never fires on these rows by its existing guard
  (a row is a candidate only when its generation is older than its
  scope's active generation; the synthetic generation is always the
  active one). No predicate change.
- Retention never considers the synthetic generation (candidates are
  `status='superseded'` rows; it stays `active`). No query change. A
  one-line defense-in-depth exclusion of `scope_kind='synthetic'`
  from the eligible set is recommended so a mistaken future
  supersede of the genesis generation fails safe instead of
  cascading.
- The liveness sweep gets a matching one-line exclusion for the
  synthetic scope (it is perpetually old and active, which is normal
  for it, not a wedge).

## No-leak argument

Row count for the domain is bounded by the group count, permanently.
Each group maps to exactly one `work_item_id`; touches insert or
retouch that single row, which cycles
pending to claimed to succeeded and back to pending on retouch.
Succeeded rows are not garbage to collect: they are the per-group
slot, reused by the next touch. Failed and dead-letter rows are
likewise one slot per group, retouched (reopened) by the next touch
rather than duplicated. No sweeper, TTL, or terminal-row prune is
needed because no per-touch residue exists. The A-2 proof pins this:
N touches across M scopes and generations leave exactly one row per
group, and success/terminal turnover never grows the set.

## Watermark preservation (I10)

`RetouchWorkItem` sets `reopened_at` on every live-state touch, and
the claim read derives `CycleStartedAt` from `COALESCE(reopened_at,
created_at)` while the handler waits for winners newer than
`max(EnqueuedAt, CycleStartedAt)`. Every coalesced touch therefore
advances the bound to the latest touch. Concurrent retouches
serialize in Postgres; the last committer carries the latest
timestamp, so the bound is at least the maximum touch time. The
fan-out ordering (enqueue before the holder debt clears, fail the
pass while the owed marker stands) is untouched.

## Race safety (A-1 proof sketch)

Two concurrent touches build byte-identical intents (same synthetic
scope, genesis generation, domain, entity key, hence same
`work_item_id` and same conflict key). One `INSERT` wins, the other
hits `DO NOTHING`; both retouch, so the bound advances once. The
live-lease unique index still rejects a second concurrent lease on
the group key. Test: concurrent-enqueue storm from distinct scopes
for one group asserts exactly one row, `reopened_at` at least the
max touch, and exactly one live lease under concurrent claimers.

## Supersede and retention proof (A-2 sketch)

Seed a synthetic row, advance several real scopes through new
generations, run claims and retention passes: the synthetic row is
never superseded and its generation is never a prune candidate,
while real stale rows still supersede and prune as today.
Cap test: N touches across M scopes leave one row per group.
Guard test: the `scope_kind='synthetic'` exclusions hold even if the
genesis generation were marked superseded by hand.

## Retry after partial success (A-3 sketch)

Reconcile is idempotent per group (same node and `(Repository,
Package)`-keyed edges rewritten), so a retried intent rewrites the
same state. A pass whose enqueue succeeded but whose fan-out failed
fails pre-ack with the owed marker standing; the retry re-enqueues
and retouches, advancing the bound without duplicating edges. Test:
fail-after-enqueue then retry asserts one reconcile outcome and a
bound at least the retry's touch.

## Generation-liveness decision

Accepted: advisory rows stop contributing to real scopes'
"draining, not wedged" signal. The liveness query counts any
non-terminal reducer row per (scope, generation); today an advisory
row makes a generation look draining. That signal was spurious: a
global group reconcile never gated a real scope's activation, and
the scope's own `supply_chain_impact` and projector rows still
represent its drain. Net effect is a more honest liveness signal,
not a lost one.

## Scope-attributed-status decision

Accepted: the group's backlog attributes to the synthetic scope
instead of the first toucher. Today's attribution is already
arbitrary (whichever scope touched first). The synthetic owner is
stable and self-describing in status views. For drill-down, the
enqueuer stamps the origin scope and generation into the payload as
informational fields (`origin_scope_id`, `origin_generation_id`);
they affect no key, predicate, or handler read.

## Arbiter questions

1. Exempt or re-home? Re-home, for the reasons above: it reuses the
   proven PK idempotency primitive (solving the P1 race with zero
   new machinery), needs no hot-path predicate change (dodging the
   P0/P1 by construction rather than by exemption), and bounds rows
   by group count with no generation pinning (the exemption's leak).
2. Close as disproven? No. The design is implementable pending the
   A-1/A-2/A-3 proofs and the per-scope reader audit below. If the
   audit finds a consumer that requires advisory rows to ride real
   scopes, that finding returns here before any implementation.

## Implementation checklist (later PR, not this lane)

- Bootstrap migration for the synthetic scope and genesis
  generation, with reserved-id collision handling.
- Enqueuer change plus origin payload fields; `ifa
  enqueue-advisory-groups` takes the same path.
- Liveness and retention synthetic-scope exclusions with tests.
- A-1/A-2/A-3 tests listed above, plus the existing handler and
  enqueue suites.
- Per-scope reader audit: claim `RelatedScope` derivation, status
  summary grouping, telemetry dimensions, admin-status surfaces,
  freshness/`changed_since` (no advisory participation expected;
  verify, do not assume).
- Rollout metrics: advisory backlog row count and oldest claimable
  age falling faster than new touches, two stable reads.

## Evidence

- Handler scope-blind except logging:
  `go/internal/reducer/advisory/group/handler.go` (`groupIDsFromIntent`
  reads only `EntityKeys`; winners read by group id; `ScopeID` used
  only in the completion log).
- Watermark bound `max(EnqueuedAt, CycleStartedAt)`:
  `handler.go` `awaitWinnersWatermark`.
- `CycleStartedAt` derives from `COALESCE(reopened_at, created_at)`:
  comment on `supply_chain_impact_advisory_group_enqueue.go` and the
  reopen-anchor live test.
- Retouch advances `reopened_at` on every live state:
  `go/internal/storage/postgres/reducer_queue_retouch.go`.
- Deterministic id and `DO NOTHING`:
  `reducer_queue_helpers.go` `reducerWorkItemID`,
  `reducer_queue.go` `enqueueReducerBatchSuffix`.
- Group-partitioned conflict key:
  `reducer_queue_conflict.go` `reducerConflictDomainKey`
  (`advisory_group` arm).
- Live-lease uniqueness per conflict key:
  `migrations/005_fact_work_items.sql`
  (`fact_work_items_reducer_live_lease_uniq`).
- Supersede has no advisory exemption and its candidate set is
  older-generation unleased rows:
  `reducer_generation_filter_sql.go`
  (`supersedeInactiveReducerGenerationsPredicate`).
- Prune live-work guard covers claimed/running/retrying only, so a
  pending foreign-hosted row is unprotected; generation FK cascade
  deletes work rows:
  `generation_retention_sql.go` (`live_work` CTE),
  `migrations/005_fact_work_items.sql` (`ON DELETE CASCADE`).
- Liveness counts any non-terminal reducer row per (scope,
  generation) as draining:
  `generation_liveness_sql.go` (reducer-backlog `NOT EXISTS`).
- `superseded` is terminal and unreplayable (only `succeeded`
  reopens): sweep comment in `reducer_generation_filter_sql.go`,
  `reducer_queue_replay_test.go`.
- Census andBlocked verdict: issue text and
  `~/eshu-findings/03-design-corrected-flow.md` section 4 plus
  `~/eshu-findings/06-review-disposition.md` (A-1..A-3); census
  figures are prior measured evidence, not re-verified here.
- Advisory-domain storage readers are enqueue, conflict key, and
  retouch only (no per-scope filtering to migrate): search for
  `DomainAdvisoryGroupProjection` under `go/internal/storage/postgres`.
