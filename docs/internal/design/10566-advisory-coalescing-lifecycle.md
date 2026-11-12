# Advisory coalescing lifecycle design (10566-A)

Status: ACCEPTED design, implemented on branch drive/10566-a-lifecycle.
The independent re-review accepted the synthetic re-homing design
(issue boxes 6-7 checked on that verdict); box 7 live rollout
(backlog falling, two stable reads) stays open post-merge.

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
slot, reused by the next touch. No sweeper, TTL, or terminal-row
prune is needed because no per-touch residue exists. The A-2 proof
pins this: N touches across M scopes and generations leave exactly
one row per group, and success turnover never grows the set.

Terminal slots need a touch-path extension before implementation.
The current `RetouchWorkItem` set covers only
pending, claimed, running, retrying, and succeeded: a touch landing
on a failed or dead-letter slot hits `ON CONFLICT DO NOTHING` plus a
no-match retouch, and the touch is silently dropped (proven live,
see Proof status). The implementation must therefore reopen
failed and dead-letter slots on a new touch: status back to
pending, attempt count reset, failure evidence cleared, and
`reopened_at` advanced to the touch. This preserves today's
one-run-per-touch semantics at the same cost (a poison group touched
every pass retries every pass, exactly as its fresh rows do today);
the reset is principled because the new touch is new evidence, not
the same poison. Without this, a single dead-letter wedges its
group until an operator replays it, which is strictly worse than
today. The A-2 checklist test for this is specified in Proof status.

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

Accepted absorption: a touch landing on a retrying row does not
expedite it. Retouch keeps the row's `visible_at` (only a succeeded
reopen resets it), so the touch waits for the scheduled retry
instead of minting an immediately claimable row as today. Nothing is
lost: the touch still advances `reopened_at`, so the retry's bound
covers it (A-3 proof), at the cost of up to one retry-backoff delay.
Expediting would mean resetting backoff on every duplicate touch,
which reintroduces the hot-group busy loop coalescing removes.

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
`ON CONFLICT DO NOTHING` plus a payload-free retouch means only the
first touch ever stamps `origin_*`; later touches advance the bound
without rewriting origin.

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

## Proof status (2026-11-12, lane A)

Proven live on a scratch PostgreSQL 18 database through the shipped
queue API with synthetic slot values (throwaway probe, deleted after
its run; every check below is pinned durably afterwards):

- A-1: 16 concurrent enqueue-plus-retouch touches of one group from
  distinct owners converge to one row admitted exactly once, and two
  concurrent claimers yield exactly one live lease on that row.
- A-2: after advancing a real scope, the claim-time sweep supersedes
  its stale advisory row while the synthetic row on the
  always-active genesis is never superseded; 25 groups touched three
  times each leave exactly 25 rows; the shipped retention-candidate
  selection names the aged real superseded generation and never the
  active genesis.
- A-3: fail-after-touch with a retryable cause, a second touch, and
  a reclaim hit the same slot with `CycleStartedAt` at or after the
  second touch, then succeed on the single row.
- Terminal-slot gap (design change recorded above): a fatal fail
  dead-letters the slot, and a later touch inserts zero rows with a
  no-match retouch while the slot stays dead-letter. This is the
  required touch-path reopen, not a passing behavior.
- Guard confirmation: rows on a hand-superseded genesis are swept,
  so the liveness/retention `scope_kind='synthetic'` exclusions stay
  required defense-in-depth even though no current path supersedes
  the genesis.

Durable pins (all green on current code, all valid after the
implementation except where noted):

- `reducer_queue_advisory_coalescing_live_test.go`:
  `TestAdvisoryCoalescingLifecycleLive` (A-1 storm and lease, A-2
  sweep sparing plus real retention-candidate selection plus cap,
  A-3 retry and bound).
- `reducer_queue_advisory_coalescing_test.go`: slot-identity
  convergence, retouch-advances-bound and claim-derives-cycle-start
  derivation guards, sweep active-generation guard and retention
  superseded-only guard.
- Written with the implementation (terminal reopen shipped):
  touch-after-dead_letter (and after failed) re-runs the
  slot with attempt count reset, failure evidence cleared, and the
  bound at or after the new touch; a poison group touched every pass
  costs one run per touch and never wedges its neighbors.

## Implementation checklist (done on branch drive/10566-a-lifecycle)

- Bootstrap migration for the synthetic scope and genesis
  generation, with reserved-id collision handling.
- Enqueuer change plus origin payload fields; `ifa
  enqueue-advisory-groups` takes the same path.
- Touch-path terminal reopen (failed and dead-letter slots reopen
  on a new touch) with the test specified above.
- Liveness and retention synthetic-scope exclusions with tests.
- A-1/A-2/A-3 tests listed above, plus the existing handler and
  enqueue suites.
- Per-scope reader audit: claim `RelatedScope` derivation, status
  summary grouping, telemetry dimensions, admin-status surfaces,
  freshness/`changed_since` (no advisory participation expected;
  verify, do not assume).
- Rollout metrics: advisory backlog row count and oldest claimable
  age falling faster than new touches, two stable reads.

## Per-scope reader audit (implementation, 2026-11-12)

Every surface that reads advisory rows by scope was checked live on the
implementation branch. No advisory consumer filters or joins on the
origin scope: the domain's production readers are the enqueuer
(`supply_chain_impact_advisory_group_enqueue.go`), the conflict-key
partitioner (`reducer_queue_conflict.go`), and the handler
(`advisory/group/handler.go`), all keyed by group id.

| Surface | Finding | Disposition |
|---|---|---|
| Claim `RelatedScope` derivation (`reducer_queue_helpers.go`: `RelatedScopeIDs: []string{scopeID}`) | After re-homing the related scope is the synthetic slot scope. The advisory handler reads `EntityKeys` only and never consults `RelatedScopeIDs` (no use in `handler.go`); no other consumer keys advisory rows by related scope. | No code change. Origin rides the payload (`origin_scope_id`, `origin_generation_id`) for drill-down. |
| Status summary grouping (`status_active_work_summary.go`: `GROUP BY scope_id, generation_id, stage, status`) | Advisory backlog rows now attribute to `synthetic:advisory-groups` instead of the first toucher. | No code change: accepted by the scope-attributed-status decision above (today's attribution was already arbitrary). |
| Telemetry dimensions (`instruments_advisory_group_projection.go`) | Signals carry `{domain}` / `{domain,outcome}` only; no scope label, so re-homing moves nothing. The lag signal measured from `EnqueuedAt` alone, understating the wait the handler actually defers on (`max(EnqueuedAt, CycleStartedAt)`). | Code change: `winnersBoundStart` in `handler.go` shares one bound between the watermark deferral and `RecordAdvisoryGroupLag`; `TestAdvisoryGroupHandlerLagStartsAtCycleBound` pins 60s-from-bound vs 660s-from-enqueue. No new signals. |
| Admin-status surfaces (`status_hosted_readiness.go`) | The stall gate reads global queue `Outstanding`/`OldestOutstandingAge` plus per-domain backlogs keyed by domain (no scope). Advisory contributes by domain exactly as before. | No code change. |
| Freshness / `changed_since` (journal sweeper) | The generic journal sweeper participates: it journals the seeded synthetic genesis once as an empty scope (one activation row, one cursor, one empty link per DB lifetime — the same shape as the `eshu:global` anchor). | No production change (arbiter ruling ACCEPT-(a)); the 5 freshness count assertions name the synthetic row explicitly. |
| Status age (`status_queries.go` domain backlogs; `status_blockage.go` blockages) | Oldest outstanding age is `MIN(COALESCE(reopened_at, created_at))`; blockage age is `MIN(available_at)` with `available_at = COALESCE(visible_at, created_at)`. Retouch advances `reopened_at` on every touch and reopen sets `visible_at` to the touch, so a hot group's age reflects its least-recently-touched outstanding slot, never a pinned first enqueue. | No code change; ages stay honest under coalescing. |

## No-Regression Evidence (advisory coalescing 2026-11-12):

No hot-path statement changes shape. The claim query
(`reducer_queue_claim_query.go`, `reducer_queue_batch_query.go`) and the
supersede predicate (`reducer_generation_filter_sql.go`) are
byte-identical (no diff); each touch still costs one enqueue INSERT plus
one retouch UPDATE; the handler issues no new queries. The retention and
liveness sweeps gain one `scope.scope_kind <> 'synthetic'` AND each, a
no-op for every pre-existing scope (no synthetic rows existed before
migration 295) that only fences the new slot scope.
`TestGenerationRetentionCandidatePlanLive` stays green, pinning the
candidate plan shape.

Backend: PostgreSQL 18.6 on the local scratch database, synthetic slot
values (isolated schemas for the seed proof, the shared scratch schema
for the storm and reopen proofs). Baseline: live census 20,629 rows over
2,377 conflict keys (prior measured evidence from the issue, not
re-verified here). Wall times below are proof runtimes on tiny fixtures,
not throughput claims; the performance claim is bounded row counts plus
statement-count parity.

After (this commit, all green):

- 16 concurrent touches of one group converge to 1 row admitted once,
  with 1 live lease under 2 concurrent claimers
  (`TestAdvisoryCoalescingLifecycleLive`).
- 75 touches over 25 groups leave exactly 27 rows, and the shipped
  retention selection names the aged real generation and never the
  active genesis (same test).
- Fail-after-touch then retry reclaims the same slot with
  `CycleStartedAt` covering the second touch (same test).
- Dead-letter and failed slots reopen to the succeeded shape (pending,
  attempt 0, no failure evidence, `visible_at` at the touch,
  `next_attempt_at` NULL) and reclaim promptly on the same slot; a
  poison group costs one run per touch
  (`TestAdvisoryCoalescingTerminalReopenLive`).
- Migration 295 seeds the anchor with 0 work items and replays to
  convergence (`TestAdvisoryGroupsSyntheticSeedLive`).

Why safe: the per-domain row count is bounded by the group count by
construction (one `work_item_id` per group); success turnover reuses the
slot, terminal turnover reopens it; no sweeper, TTL, or new index.

## Observability Evidence (advisory coalescing 2026-11-12):

No new signals: `telemetry-coverage` passes unchanged. The lag signal
keeps its instrument and buckets; only its start moves from `EnqueuedAt`
to `max(EnqueuedAt, CycleStartedAt)` via `winnersBoundStart`, pinned by
`TestAdvisoryGroupHandlerLagStartsAtCycleBound` (60s from the bound vs
660s from the enqueue). Operator reads:
`eshu_dp_advisory_group_projection_lag_seconds` now tracks the true
watermark wait; domain backlog oldest age and blockage age keep riding
the touch, per the audit above.

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
