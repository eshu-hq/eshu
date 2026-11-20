# #10879 queue-hot cheapest shim: lease split + fillfactor (mechanism proof)

Status: mechanism PROVEN on a local shim; proportion NOT proven; NO production
code changed; NO migration run. This note is the prove-the-theory-first shim
for #10879 per the arbiter design (prior result 1, decisions 1/3/6/7c).

## Verdict

- RED: today's shape reproduces the QA symptom exactly — 50,000 heartbeat
  renewals, **0 HOT (0.0000)**, matching QA's 0.7% HOT (20.7M updates, 146k HOT).
- GREEN: lease-table shape — 50,000 renewals, **50,000 HOT (1.0000)**, with
  **0 new main-table updates** in the heartbeat window.
- The win is index-write elimination + heap-growth avoidance, NOT WAL bytes:
  renewal WAL is ~parity (443 B vs 470 B/renewal, local box). Reported honestly
  below; anyone expecting WAL savings must look elsewhere.
- What this does NOT prove (still owed per the arbiter ladder): the
  census-by-cause on the #10862 replay (the shim *assumes* a
  renewal-dominated mix, 25:1); claim-latency regression (<10% gate) on the
  real claim query; concurrency proofs; migration locking/duration.

## Setup (disposable local DB `eshu_10879_shim`, PG 18.6, never shared infra)

- BEFORE `q_before`: today's shape on a subset of the real indexes
  (`migrations/005`): PK + `(status, updated_at)` + partial `(claim_until)`,
  fillfactor 100 (= production, which sets no fillfactor). Heartbeat mirrors
  `reducer_queue.go:40-50`: `SET claim_until, updated_at` (indexed col → never HOT).
- AFTER `q_main` + `q_lease`: main keeps `(status, attempt_count, updated_at)`
  + its status index; lease holds the fencing triple
  `(lease_owner, claim_until, last_attempt_at)`, fillfactor 70, PK only, no
  secondary index (arbiter decision 3). Heartbeat writes `claim_until` only.
  The conflict unique index (decision 2) is deliberately omitted: conflict cols
  are immutable after claim, so it costs an insert-time write only, never a
  renewal write.
- Same data both sides: N=2000 rows seeded identically, one set-based claim
  each, then H=25 round-robin renewals, one row per autocommitted transaction
  (production heartbeat is one row per transaction; a single-transaction sweep
  defeats heap pruning — first attempt measured only 31% HOT for exactly this
  reason, see §4).

## Results (same data, same workload)

| Measure | BEFORE (today) | AFTER (lease split) |
| --- | --- | --- |
| Heartbeat updates | 50,000 | 50,000 (lease) + 0 (main) |
| HOT share, heartbeat window | 0/50000 = **0.0000** | 50000/50000 = **1.0000** |
| Main-table updates in hb window | 50,000 (every renewal) | **0** |
| Heap after claim+25 renewals | 1,236,992 B (10x seed) | main 245,760 + lease 204,800 B |
| Renewal indexes after window | status 1,490,944 + claim 1,179,648 B | main status 147,456 (claim only) + lease pkey 81,920 (flat) |
| Renewal WAL (50k renewals) | 22,179,504 B (443 B/row) | 23,490,248 B (470 B/row) ~parity |
| Single-row renewal (EXPLAIN ANALYZE) | 0.024 ms, 15 buffers | 0.027 ms, 6 buffers |
| Total wall (100k renewals + setup) | 100 s shared box (~1 ms/renewal; NOT a QA claim) | same run |

Plans (both PK-lookup writes; BEFORE maintains 2 extra indexes → 15 vs 6 buffers):

- BEFORE: `Update on q_before`, `Index Scan using q_before_pkey`,
  `Buffers: shared hit=15`, `Execution Time: 0.024 ms`.
- AFTER: `Update on q_lease`, `Index Scan using q_lease_pkey`,
  `Buffers: shared hit=6`, `Execution Time: 0.027 ms`.

## Reproduce

`./scripts/dev/10879-queue-hot-shim.sh` (needs local `eshu_10879_shim` DB;
`DB`/`N`/`H`/`OUT_DIR` overridable). Raw log: `/tmp/10879-shim/*.log`.
Exits nonzero unless RED (BEFORE HOT < 10%) and GREEN (lease HOT > 50%, zero
new main updates) both hold. Shim SQL: `scripts/dev/10879-queue-hot-shim.sql`.

## Limitations (why this is a shim, not the proof)

1. Narrow synthetic rows, 2,000 rows (QA: 264,567 wide rows with payload
   JSONB). Ratios transfer; absolute bytes/timings do not.
2. No concurrent readers/writers, no autovacuum pressure, no claim-join
   regression measurement — the <10% claim-latency gate needs the real
   rank-once claim on replay hardware.
3. 100% HOT is an ideal-condition upper bound (fresh fillfactor-70 pages,
   prune-friendly spacing). Production will be lower; the floor this sets is
   "renewals stop touching main + its indexes entirely" (0 new main updates),
   which holds regardless of HOT rate.
4. pgstat reads need `pg_stat_force_next_flush()` first on PG18 (in-session
   reads raced pending counters and showed 0/0 mid-run; final numbers above
   are post-flush and authoritative).
5. Census-by-cause is the binding next step: if heartbeat/lease-only writes
   are <10% of the 20.7M updates, this theory is DEAD per decision 6 and the
   cheaper alternatives (index drop, heartbeat `updated_at` removal) win.

## Next (coordinator call)

(a) census shim on the #10862 replay; (b) cheaper-alternative shims first;
(c) full lease-split SQL shim with claim EXPLAIN + candidate EXCEPT diff;
(d–f) concurrency, migration, QA proofs. No ADR, no implementation from this lane.
