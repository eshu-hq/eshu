# #7187: claim sweep and Ack obsolete take the claim fence before generation rows

## Problem

The projector claim's #7108 maintenance lock,
`locked_stale_scope_generations`
(`FOR NO KEY UPDATE OF stale_generation SKIP LOCKED` in
`claimProjectorWorkSweepSQL`), can wait despite SKIP LOCKED. That wait
puts the claim into a deadlock cycle with Ack: the claim waits on an
open generation updater while holding maintenance rows that Ack's
`supersedeProjectorObsoleteGenerationsQuery` needs.

Mechanism (PostgreSQL heap_lock_tuple): when the row version in the
statement's snapshot has a multixact xmax with a running compatible
locker (an FK-child insert's KEY SHARE) and a committed updater, the
lock follows the update chain in heap_lock_updated_tuple_rec and
waits on an in-progress updater of the next version. SKIP LOCKED
does not cover that transactionid wait. `scope_generations` meets
every condition (~25 FK children take KEY SHARE during ingestion;
Ack and ingestion update it).

Root-cause evidence: a raw-psql shim on PostgreSQL 18 (parent table
with one FK child; K = open child INSERT holding KEY SHARE; C fixes
a repeatable-read snapshot at v1; U1 commits a non-key v1→v2; A2
holds an open UPDATE on v2) shows C's
`SELECT ... FOR NO KEY UPDATE SKIP LOCKED` in `pg_stat_activity` as
`active | Lock | transactionid`, with the server logging
`still waiting for ShareLock on transaction ...`. SKIP LOCKED skips
nothing on the chain path.

The deterministic chain test,
`TestProjectorClaimDoesNotWaitOnGenerationUpdateChain`
(`projector_queue_claim_generation_chain_live_test.go`), stages the
same alignment through the shipped claim (repeatable-read claimer
snapshot at v1, KEY SHARE holder, committed non-key update, open
Ack-shaped fence-plus-generation update). On the base tree the
claimer is observed in `Lock/transactionid` and the test fails RED
with that observation; the existing
`TestProjectorClaimDoesNotWaitOnLockedMaintenanceRows` only holds
latest-version locks, so it cannot catch chain walks.

## Design decision

The issue offers two outs: lock a claim-only target with no FK
children, or decide from a non-locking snapshot. Verdict: lock the
claim fence first (#7115 shape). A non-locking generation read
would lose the #7469/#7820 lock-time marker recheck that the
marked-guard and obsolete-marker EPQ tests pin, and dropping the
generation write would move (not remove) the retirement contract
the maintenance-semantics tests pin. The fence
(`projector_scope_claim_fences`) has no referencing tables, so no
KEY SHARE multixact can form under it and locking it never waits;
both maintenance writers take it SKIP LOCKED first and skip a busy
scope, which also removes the inverted lock order (claim
generation→work vs Ack scope→work→generation) that closed the
40P01 cycle.

Deadlock review: every added lock is SKIP LOCKED on a childless
row, so neither statement gains a wait edge. Claim-vs-claim on one
scope serializes through the fence exactly like the #7115 candidate
fence. Ack-vs-claim on one scope: the second mover skips the
scope's maintenance; the candidate still excludes snapshot-
supersedable rows, so a skipped scope yields no claim and the next
claim or Ack retries the sweep. Plain UPDATE waits that remain
(the sweep's generation write, Ack's scope/work writes) are
acyclic: their holders never wait on the other party. Fail keeps
its order (scope, work, generation) and never takes the fence, as
before; a Fail-vs-sweep overlap waits briefly but cannot cycle
because the sweep never blocks. Residual: a non-fenced writer
(Fail's owned-generation update) can still, in principle,
contribute a committed-updater leg to a chain; it is exceptional,
brief, and documented here rather than silently assumed away.

## Fix implemented

- `locked_stale_scope_generations` joins each stale scope's
  `projector_scope_claim_fences` row and locks
  `OF sweep_fence SKIP LOCKED` ahead of
  `OF stale_generation SKIP LOCKED`. Predicates, the
  `marker_spared` lock-time flag, and the work-lock recheck are
  byte-identical otherwise.
- `supersedeProjectorObsoleteGenerationsQuery` moves to
  `projector_queue_marker_fence_sql.go` beside the fence queries
  (keeps `projector_queue_sql.go` under the 500-line cap without
  adding a dirgate-counted file) and locks
  `OF obsolete_fence SKIP LOCKED` ahead of
  `OF stale_generation SKIP LOCKED`. Uncontended behavior is
  unchanged; a fenced-busy scope contributes no rows and the Ack
  still activates.
- `projector_queue.go` lock-order comment (#7389) names the new
  fence-first prefix for the sweep, the obsolete maintenance, and
  the marker.
- `claimOnce` warns at `slowClaimStatementDuration` (1 s) with
  queue, lease owner, and elapsed time, so a future chain wait
  shows in operator logs joined to `pg_stat_activity`.
- Shape pins: `TestProjectorClaimSweepLocksSweepFenceFirst` (new)
  and the extended `TestAckObsoleteSupersedeLocksStaleGenerations`
  pin the fence joins and the fence-before-generation lock order;
  `TestSupersedeWriterGuardSeededViolation` follows the moved
  query to its new file. A test-only observability fix was
  required: the longer obsolete statement pushes
  `superseded_work` past `track_activity_query_size` (1kB), so the
  fidelity test's `pg_stat_activity` poll now matches the leading
  CTE name.

Note: migration 130's comment still says only the claim locks the
fence table; it is intentionally left untouched (migration file
checksums), and the live protocol invariant now lives in the
`claimProjectorWorkSweepSQL` header comment.

## Proof

All proof ran against PostgreSQL 18.6 on a disposable database.

- RED→GREEN: `TestProjectorClaimDoesNotWaitOnGenerationUpdateChain`
  fails on the base tree (claimer observed in
  `Lock/transactionid`) and passes with the fix (claims gen-z2
  while the Ack-shaped transaction is open, skips fenced scope-s
  without touching gen-s1, retires it on the follow-up claim, 0
  server deadlocks).
- Concurrency matrix GREEN with the fix (245 passed, 0 failed):
  the #7108 lock-skip and #7115 scope-chain proofs, the
  maintenance-semantics set, the #7469/#7473 EPQ set
  (holder-claimed-after-snapshot, fence-across-clocks,
  superseded-sweep lease recheck, marker-txn skip, full-guard
  set), the fence-lifecycle set, the Ack obsolete/superseded
  set, the heartbeat supersession set, the dead-letter set, the
  delta-baseline set, the reclaim set, and the prior-failure
  fold fidelity set that executes the changed Ack statement
  directly.
- Load: `TestProjectorClaimConcurrentLoadHasNoDeadlock`
  (16 workers, 40 scopes, 20 s): 233 claims,
  server_deadlocks=0, claim_deadlock_errors=0,
  overlapping_leases=0, superseded_refusals=5,
  retired_rows_claimed=0.

No-Regression Evidence: the matrix above is the before/after
contract proof (every listed suite passed on the base tree and
passes with the fix; the two failures seen mid-lane were a
writer-guard file map and a `pg_stat_activity` truncation
predicate, both test-only, both fixed and re-proven). The added
claim work per stale row is one PK fence lookup and one extra
locking clause; the added Ack work per obsolete statement is one
PK fence lookup. EXPLAIN on the shipped obsolete statement shows
the fence as an `Index Scan using
projector_scope_claim_fences_pkey` nested loop with the LockRows
and generation-order sort unchanged and no new scan types.
Same-machine relative comparison only, not a quiet-host profile.

Observability Evidence: new `slog.WarnContext` slow-claim log
(`projector claim statement slow; possible lock wait`, fields
queue/lease_owner/elapsed) fires at 1 s of claim-statement time;
existing `eshu_dp_queue_claim_conflict_retries_total` and the
server deadlock counter cover the conflict path. No new metric,
span, or status.
