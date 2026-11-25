# #11430 conflict-key-partitioned handler parallelism: design (no implementation)

Date: 2026-11-25. Lane: `drive/11430-drain-bottleneck` (leaf child).
Status: DESIGN ONLY. No production code is changed or added by this doc; the
companion proof is a hermetic test that guards the invariants a future
implementation must keep. Implementation is a separate reviewed step.

Context: the [saturation shim](../evidence/11430-saturation-shim.md) convicted
the handler drain (sci p50 39.4s, max 551s; 8/8 workers busy; 6-slot graph-write
semaphore) and exonerated the claim loop and the per-key conflict fence. A bare
`Workers` increase is refused. This doc designs the approved primary lane:
conflict-key-partitioned parallelism, architecture-first.

Skills active: `golang-engineering`, `concurrency-deadlock-rigor`.
Vendor sources: PostgreSQL 19 docs (13.2.1 Read Committed; SELECT locking
clause), Neo4j concurrent-data-access + deadlock docs, each cited inline.

## 1. Current lock / transaction map (what serializes today)

One reducer cycle is four separate transactions (claim, execute, ack/fail;
heartbeat interleaves). Nothing spans more than one statement except the
handler's own graph transaction.

### 1.1 Claim (single statement, one implicit transaction)

Entry: `ReducerQueue.ClaimBatch` (`go/internal/storage/postgres/reducer_queue_batch.go`)
renders `claimReducerWorkBatchQuery`
(`go/internal/storage/postgres/reducer_queue_batch_query.go`), executed
once per claimer poll. Shape, in order:

1. Snapshot CTEs (`reducer_source_inflight`, `reducer_domain_inflight`, gate
   precomputes, `base`, `reps_ranked`, `reps`, `live_fence_keys`,
   `source_heads`, `candidate`): pure reads under READ COMMITTED, no locks.
   Window functions (`row_number() OVER w_same / w_fairsame`, running-sum
   fair rank) run here, inside inner CTEs — never at the locking level.
   Vendor rule (PostgreSQL SELECT reference): locking clauses cannot appear
   with `DISTINCT`/`GROUP BY` at the same query level, and a locking clause
   "affects how SELECT locks rows as they are obtained"; the batch claim
   satisfies this by locking one level out, in the arms below.
2. Lock arms (`locked_search_doc_reserved`, `locked_rest` in
   `reducer_queue_batch_locked_arms.go`): per-candidate
   `JOIN LATERAL` primary-key probe with
   `FOR UPDATE OF lock_target SKIP LOCKED`. Row locks are taken in claim
   order (`reclaim_rank, gate_boost, ... updated_at, work_item_id`), held to
   statement end (milliseconds), never across the handler. SKIP LOCKED never
   blocks: "any selected rows that cannot be immediately locked are skipped"
   (SELECT reference), so concurrent claimers never wait on each other and
   there is no claim-side deadlock edge.
3. `claimed`: `UPDATE fact_work_items ... FROM locked`, plus the lease-mirror
   CTE in the same statement. One statement, one atomic hand-out.

EvalPlanQual: under READ COMMITTED, when `FOR UPDATE` meets a row updated
concurrently, the locker waits, then "the updated version of the row" is
re-checked against "the search condition" (13.2.1) — but only the locking
level's quals are re-run. The pre-rewrite query locked the
predicate-bearing SELECT directly; the rank-once rewrite moved predicates
off that level, so both arms re-apply the row-self lease/visibility/status
predicates on `lock_target`. Dropping one arm's copy reintroduces lease
theft on that arm. Guard: `TestClaimBatchLockRecheckPredicatesPinnedByCount`
(exact count 2 per fragment) plus the proof test in section 4.

Table locks: `FOR UPDATE OF` takes the ordinary `ROW SHARE` table lock on
`fact_work_items` (SELECT reference: SKIP LOCKED "apply[s] only to the
row-level lock(s)"). Short-lived; not a scaling factor.

Lease safety: a claimed row carries `(lease_owner, claim_until,
last_attempt_at)`; every later mutation (heartbeat, ack, fail) re-fences on
`(work_item_id, last_attempt_at = claimed_at, lease_owner, claim_until >
clock_timestamp())` and reports zero matched rows as `ErrReducerClaimRejected`
/ `BatchAckSupersededError` instead of stealing the lease. Heartbeat is a
single-row `UPDATE` (`heartbeatReducerWorkQuery`, via
`ReducerQueue.Heartbeat` at `reducer_queue.go:424`); ack is
`FOR NO KEY UPDATE` on the joined `locked_work` set, then `UPDATE ...
RETURNING`, then lease-mirror delete — all in one statement
(`ackReducerWorkBatchQuery`). Fail fences the same way (lease-lost maps to
dropped item, never to a second terminal transition).

Net: the claim path holds no lock longer than one statement, blocks on
nothing, and every post-claim write is epoch-fenced. The claim loop is
correct under concurrency today; that is why the shim exonerated it.

### 1.2 Handler (the convicted serialization)

Entry: `executeAndReport` (`service_batch.go:409`) under a per-worker
heartbeat. For `supply_chain_impact` the handler spends ~39s median in:

- Postgres holder key-range reads (~2.2–2.6s each, ~16 per pass — the p50),
  plain `SELECT`s, no locks held across them;
- Neo4j graph writes through one shared `BackpressureGate`
  (`go/internal/storage/cypher/backpressure_executor.go`,
  `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=6` on the vuln lane). The gate is an
  in-process semaphore: at most 6 graph transactions in flight across all 8
  workers. Two workers always park at the gate while six hold graph
  transactions open.

Neo4j vendor semantics (concurrent-data-access; deadlock docs): a
transaction takes exclusive write locks on every node/relationship it
creates, updates, or deletes; "locks are added to the transaction and
released when the transaction finishes"; reads take no locks. Concurrent
transactions touching the same nodes contend (one waits); a lock cycle
raises a deadlock error, marks the transaction for rollback, and the
caller must retry. So per-handler graph transactions are the long-held
lock scope in this system (tens of seconds), and their conflict domain is
the node set the handler writes — today one scope's winners chunk per
`s supply_chain_impact` intent.

### 1.3 Commit (ack) and wake

Ack is one statement per batch (section 1.1). The winners watermark advance
is a singleton-row upsert stamped `GREATEST(previous, LEAST(S, residual
min - 1us))` (#11117: "the watermark never regresses"), followed in-tx by
`wakeWinnersWatermarkQuery`, which reopens `reducer_waiting_items` rows
whose covering stamp arrived (`FOR UPDATE` on the wait rows, 2s
`lock_timeout`, 55P03/40P01 mapped to retryable `WinnersWakeContentionError`).
The advisory `advisory_group_projection` gate reads this watermark
(`AdvisoryGroupWinnersWatermark`); while it is hours stale every advisory
claim defers in ~0s and no worker count helps — the shim's second
serialization, upstream of any parallelism.

## 2. Partitioned design

### 2.1 Canonical key and lane assignment (disjoint, covering, stable)

Partition key (already computed verbatim in `base`):
`ckey = COALESCE(conflict_key, scope_id)` plus `conflict_domain`.
Lane assignment must be a pure function of that pair, identical in SQL
and in any operator tooling:

```sql
SUBSTRING(md5(conflict_domain || '|' || ckey) FROM 1 FOR 1) IN (<lane chars>)
```

One hex char = 16 lanes max; a lane owns a set of chars. `md5(text)` is a
stable byte function (unlike `hashtext`, whose output must not be relied on
across versions), and hex-prefix slicing tiles the space uniformly without
calibration. UTF-8 caveat: the digest runs on database-encoded bytes; Go
`md5.Sum` over the same UTF-8 string agrees, which the proof test checks
on a corpus including multibyte keys. NULL `conflict_key` falls back to
`scope_id` exactly as `base.ckey` does, so no key is unassigned.

Placement rule (hard): the lane predicate goes in the **snapshot** CTEs
(`base`/`reps`, beside the existing `$2` domain filter), never in the lock
arms and never by moving `FOR UPDATE`. The arms join `lock_target` by
primary key to `ordered_candidate`, so a snapshot-level predicate
partitions the lock loop automatically with the EvalPlanQual recheck
intact. Any future claim-SQL edit that moves `FOR UPDATE` off the
predicate-bearing lock level must add the row-dropped live test from the
concurrency skill matrix; this design needs none because it does not move
it.

### 2.2 Per-lane claimers, shared nothing except the table

Each lane runs today's `runBatchConcurrent` claimer + workers + acker with
an added lane predicate. Consequences, all by construction:

- Disjoint admission: representative election (`reps.same_rn = 1`) is
  per `(conflict_domain, ckey)` and every key hashes to exactly one lane,
  so partitioned election unioned over lanes equals global election
  (proof test, section 4). `keyed-equals-full`: for the shim's measured
  shape (max 1–2 rows/key) nothing changes; for a multi-row key the holder
  still wins in its lane and siblings stay fenced there.
- Watermark monotonicity: lanes never write watermarks. The singleton
  maintainer still advances with `GREATEST`; `wakeWinnersWatermarkQuery`
  reopens by covering stamp, keyed on `waits_for_key`, not on lanes — a
  wake covers every lane. A late-committing lane cannot regress a
  watermark (the upsert takes the max) and a woken row re-parks by the
  same bound it was woken with (#10876), so cross-lane timing cannot wedge
  it.
- Late-commit coverage: `gate_waited_generations`, `reclaim_rank`
  (expired holds sort first per lane — a lane never starves its own
  reclaim), and the ack epoch fence are all lane-local or lane-agnostic;
  no cross-lane ordering is assumed anywhere.
- Idempotency under concurrent writers: claim/ack/fail stay keyed on
  `(work_item_id, claimed_at epoch, lease_owner)`; two lanes cannot admit
  the same key (disjoint partitions), and the live-lease unique index plus
  the whole-statement rollback on 23505 (`claim_mode=batch`) remain the
  backstop for a single-claim vs batch-claim race, unchanged.

### 2.3 What partitioning does and does not buy (capacity honesty)

Partitioning removes the single-claimer coupling
(`claimLimit = min(batchSize, idleWorkers)` on one poll loop) and isolates
lanes from each other's tails (the 551s sci tail, the advisory 0s-defer
recycle). It does **not** create write-path headroom: total graph-write
pressure is still bounded by the `BackpressureGate` budget and Postgres
primary headroom (shim: 7.7 cores vs 8-core request — the tighter bound;
Neo4j at ~1.4% — vast). Scaling lanes without scaling those budgets just
moves the queue to the gate. The approved scaling rule: add sci lanes only
with proportional PG headroom watched, keep the per-lane gate budget
constant or split the existing budget (never sum above what PG/Neo4j
proved), and keep the advisory lane at its current worker count until the
sci winners watermark is current — its serialization is the watermark, not
workers.

### 2.4 Neo4j contention under partitioning

Disjoint conflict keys imply disjoint scope subgraphs for the measured
1-row-per-key shape, so partitioned lanes do not contend on graph locks in
the common case. Shared nodes (package nodes referenced by many scopes,
the singleton watermark-adjacent rows in Postgres) can still serialize
briefly: short row locks, fine. A true cross-lane graph deadlock surfaces
as today's deadlock error → rollback → handler retry path; the design
adds no new deadlock edge because no lane ever waits on another lane
(SKIP LOCKED preserved, gate waits are bounded permits, not row locks).

## 3. Open risks (for the implementation step)

1. Hot-key skew: md5 tiling is uniform over keys, not over cost. One
   551s-tail scope still occupies its lane worker; mitigate with the
   existing per-domain inflight fairness keys, not with rebalancing.
2. PG primary headroom (96% of request) is the binding write-path budget;
   the implementation PR must carry before/after `top pods` + claim
   latency + gate-wait histograms, or it repeats the refused bare-`Workers`
   mistake in partitioned form.
3. Advisory lane stays watermark-bound; partitioning must not be read as
   fixing advisory drain. The watermark-staleness root cause is upstream
   (separate issue).
4. `md5` deprecation posture: if a future policy bans md5 even as a
   non-cryptographic partition function, replace with `sha256` prefix —
   same tiling proof, re-run the proof test.

## 4. Hermetic proof (committed with this doc)

`go/internal/storage/postgres/reducer_claim_partition_proof_test.go` —
no database, no build tags, runs in the default `go test`:

- `TestPartitionRangesTileKeySpace`: md5-hex lane tiling is disjoint and
  covering over a corpus with empty, multibyte, `\xff`, and long keys;
  every key lands in exactly one lane.
- `TestPartitionedRepresentativeElectionEqualsGlobal`: per-lane
  `same_rn = 1` election unioned over lanes equals the global election on
  a modeled set with a multi-row key, an expired-hold reclaim row, and a
  live-lease-fenced key — i.e. keyed-equals-full under partitioning.
- `TestWatermarkAdvanceIsMonotonicUnderPartitionedWriters`: concurrent
  lane writers advancing through a `GREATEST` upsert model never regress
  the watermark, including a late commit with an older stamp (`-race`
  clean).
- `TestPartitionPredicateBelongsOnSnapshotSide`: structural guard on the
  shipped `claimReducerWorkBatchQuery` — both lock arms join by primary
  key to `ordered_candidate`, `FOR UPDATE` appears only as
  `FOR UPDATE OF lock_target SKIP LOCKED`, and the representative rule
  `reps.same_rn = 1` is intact — so a snapshot-level lane predicate
  partitions the lock loop without moving `FOR UPDATE`.

## Commands and observed values

- `git symbolic-ref -q HEAD` → `refs/heads/drive/11430-drain-bottleneck`
  (verified before editing; all reads/writes inside that worktree).
- Saturation inputs reused from
  `docs/internal/evidence/11430-saturation-shim.md` (8/8 workers busy, sci
  p50 39.4s / max 551s, `GRAPH_WRITE_MAX_IN_FLIGHT=6`, ~10k claimable,
  23505 ×1 benign, advisory 411/411 instant defer on stale watermarks).
- Vendor reads: PostgreSQL 19 `transaction-iso` 13.2.1 (updated-row
  version re-checked against the search condition); PostgreSQL 19
  `sql-select` locking clause (SKIP LOCKED skips un-lockable rows;
  locking clauses incompatible with `DISTINCT`/`GROUP BY` at the same
  level; table-level `ROW SHARE` still taken); Neo4j concurrent-data-access
  (exclusive write locks held until the transaction finishes; reads take
  none) and deadlock docs (cycle → exception, rollback mark, caller
  retries).
- Code reads (all in-worktree): `reducer_queue_claim_query.go` (single
  claim), `reducer_queue_batch_query.go` + `reducer_queue_batch_locked_arms.go`
  (batch claim, both arms), `reducer_queue_batch.go` (ClaimBatch/AckBatch),
  `service_batch.go:59` (`runBatchConcurrent`), `reducer_queue.go:424`
  (Heartbeat), `backpressure_executor.go:64` (gate),
  `supply_chain_impact_canonical_winners_rebuild.go:85` (GREATEST
  watermark), `reducer_waiting_winners_wake.go` (wake query, lock_timeout,
  55P03/40P01 mapping), `contract/intent.go:118` (Intent — conflict
  identity lives in queue columns, not the intent).
- Verification of the proof test: `gofumpt`, `go vet`, focused `go test`
  on the package — see the handoff notes for exact commands and exits.
