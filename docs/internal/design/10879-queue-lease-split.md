# Queue lease split: stop rewriting the queue entry per progress update (#10879)

Status: **Accepted (phase 1 only, arbiter ruling for #10879 phase-1
promotion)**. The arbiter accepts this record as the build authority for
phase 1 only: create `fact_work_item_leases`, idempotent live-lease
backfill, in-statement dual-write, main columns authoritative, no read
switch. Phase 1b/2 remain **Proposed** and need a fresh ruling after their
gates (production H measurement, both cheaper-alternative shims). The
pre-promotion content satisfied the fact_work_items schema-gate
(`go/internal/storage/postgres/AGENTS.md`, "What NOT to change without an
ADR"); the phase-1 change adds a sidecar table without altering any
`fact_work_items` column, index, or conflict key, and the lockstep
statement updates are the coordination the gate exists to force, so the
gate is satisfied by this scoped acceptance. Merging before this flip
would have been out of order; phase 2 (read switch and heartbeat
narrowing) is staged but not built.

Refs #10879. Part of #10860 (phase 2, step 5). Blocked-by #10861 is lifted
for this content: #10861 stays Proposed and decides nothing about lease
placement (see its header); this record decides it.

## Problem

On QA, `fact_work_items` holds 264,567 rows and took 20,726,615 updates in
the measured period with only 146,469 HOT (0.7%). Every claim renews
(`claim_until`, `updated_at`), retries, finishes, or reopens a row, and all
of those columns are indexed, so every progress update rewrites the row and
its index entries. The #10862-replay census (prior proof) attributes the
updates by cause on the real `ReducerQueue` methods: claims, heartbeats,
finishes, retries, dead-letters, reopens — heartbeats are the only cause
that never changes availability, yet each one rewrites six indexes.

## Decision 1: lease shape

New table `fact_work_item_leases` (migrations 322/323/324 — 322 holds the
table and its indexes, 323 the idempotent backfill, 324 the locking
reconcile of backfill-raced rows, split per #7961 so no file holds a table
lock through bulk DML):

- `work_item_id TEXT PRIMARY KEY REFERENCES fact_work_items(work_item_id)
  ON DELETE CASCADE` — the mirror is exact; a deleted queue row cannot
  orphan a lease row.
- `stage TEXT NOT NULL` — scopes the one-per-group unique per stage so the
  projector lane (phase 1b) can share the table without cross-stage key
  collisions.
- `scope_id`, `conflict_domain`, `conflict_key` — denormalized at claim so
  the one-per-group unique needs no join (decision 2).
- `lease_owner`, `claim_until`, `last_attempt_at` — the fencing triple,
  moved as a unit because heartbeat, ack, and fail fence on all three
  (`reducer_queue.go:40-119`).
- `WITH (fillfactor = 70)` and no index on `claim_until`/`last_attempt_at`
  beyond PK and the conflict unique — heartbeat updates stay HOT (proven by
  the cheapest-shim proof: 0% -> 100% HOT on the lease shape).
- `attempt_count`, `status`, `visible_at`, `failure_*`, `payload`, and the
  `container_image_identity_*` columns stay on the main table: they change
  availability, and their index writes are accepted.

Lock order everywhere is main row first, then lease row: the claim UPDATEs
the main row before the chained lease INSERT; heartbeat, ack, fail, and
retry fence the main row in a leading CTE before touching the mirror.
Reverse order never occurs, so the split introduces no new deadlock edge.

## Decision 2: one-per-group moves with the lease

The partial unique index `fact_work_items_reducer_live_lease_uniq`
(migrations/005:89-91) is replicated as `fact_work_item_leases_live_lease_uniq`
over `(stage, conflict_domain, COALESCE(conflict_key, scope_id))`. The claim
path maps violations of either name to the same deferred no-op
(`isReducerLiveLeaseConflict`), and the batch path keeps its same_rn=1
representative fence plus the live-fence semantics rewritten against the
mirror in phase 2. Phase 1 keeps both indexes; phase 2 drops the main-table
one only after the mirror proves sole enforcement on a replay (candidate
EXCEPT 0/0 plus a true concurrent-claim race).

## Decision 3: fillfactor

Main-table fillfactor is rejected: the remaining main writes touch indexed
`status`/`updated_at`/`visible_at` and can never be HOT, so reserving space
only grows the heap. Fillfactor 70 belongs to the lease table only.

## Decision 4: cutover, backfill, rollback

- Phase 1 (this lane): create + backfill + dual-write. The migration
  backfills only fully-stamped live reducer leases and is idempotent
  (`ON CONFLICT DO NOTHING`), so a retried bootstrap replays safely. Every
  reducer writer maintains both copies in one statement; every reader still
  reads the main table. Old columns are retained.
- Phase 1b (follow-up lane): projector writers join the mirror (heartbeat,
  claim pool, terminal statements in `projector_queue_sql.go`,
  `projector_queue_claim_sql.go`,
  `projector_queue_claim_pool_sql.go`), reusing the `stage` scope. Needs a
  projector lease-write census first; without it, phase 2 stays reducer-only
  and every reader is stage-aware.
- Phase 2 (follow-up lane): switch reducer-row lease reads to the mirror
  (claim query tallies/filters/lock rechecks, terminal fences, generation
  liveness, freshness reproject, reopen fingerprint, fanout fence reads,
  refinalize live-lease fence: `retireResolutionGenerationsTemplate` in
  `go/internal/storage/postgres/rebuild/reset/reset.go`, which fences
  retirement on `w.stage='reducer'` with live `status`/`claim_until`),
  narrow heartbeat to a lease-only UPDATE, then drop the main-table lease
  columns and the main-table live-lease unique. Each step needs an exactness
  diff (candidate EXCEPT 0/0) and the <10% claim-latency gate below.
- Rollback: phase 1 reverses with `DROP TABLE fact_work_item_leases`; the
  main table is untouched, so no backfill back is needed. Phase 2 reverses
  by re-pointing reads at the retained main columns (one release) before
  the drop.

## Decision 5: the two rarely-read hand-out indexes

Both stay in this lane. `fact_work_items_cross_scope_source_v2_idx` serves
the completion-fanout consumer; a zero scan count alone cannot distinguish
a dead index from a stats reset or a fanout running on another database.
`fact_work_items_reducer_conflict_claim_idx` is the plausible drop (the
rank-once claim may no longer use it) and dropping it also cuts per-claim
index writes — but it ships only as its own shim with since-reset
`pg_stat_user_indexes` plus claim `EXPLAIN (ANALYZE, BUFFERS)` on the
#10862 replay, never bundled with the split and never on scan counts alone.

## Decision 6: census method and thresholds

On the #10862 replay queue (>= 8,000 items): count updates by cause via the
real `ReducerQueue`/`ProjectorQueue` methods with per-phase
`pg_stat_user_tables` deltas; report HOT share, WAL bytes via
`pg_wal_lsn_diff`, and per-index writes. The split proceeds to phase 2 only
if lease-only (heartbeat) writes are >= 30% of updates; below 10% the theory
is dead and the lane ships the index drop plus the heartbeat
`updated_at`-bump removal instead. Between 30% and 10% the arbiter re-rules
on the numbers. The two cheaper alternatives are shimmed before any
read-switch work.

## Required proof, in order

(a) census shim — done (175 statements, 175 heap updates, 0 HOT, per-cause
split recorded); (b) cheaper-alternative shims — open; (c) lease-split SQL
shim on a replay copy with claim `EXPLAIN ANALYZE` before/after (stop if
claim latency regresses > 10%) plus candidate-set EXCEPT 0/0 — open;
(d) concurrency proofs on built code: stale-worker heartbeat/ack/fail after
reclaim = 0 rows (including container-image epoch variants), expired-hold
reclaim races, concurrent one-per-group claims, duplicate delivery,
dead-letter/reopen leaving no orphan lease row, fairness order unchanged
except the named heartbeat-`updated_at` delta — phase-1 mirror subset done
in `reducer_queue_10879_lease_live_test.go`, epoch variants and true races open;
(e) migration proof on a 264k-row copy plus 10x synthetic with
`CONCURRENTLY` builds, batched backfill, every `ACCESS EXCLUSIVE` window
timed, rollback rehearsed — done for phase 1, recorded in the phase-1
evidence note; (f) before/after HOT share, index reads/writes, and DB time
on the replay, then QA — phase-1 overhead recorded, phase-2 win projected.

## What this record does not do

It does not accept worker/batch serialization as a workaround, does not
approve main-table fillfactor or scan-count index drops, and does not move
projector writes until the projector census exists.
