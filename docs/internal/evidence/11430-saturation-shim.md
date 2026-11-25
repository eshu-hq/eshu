# #11430 saturation shim + capacity arithmetic: handler-bound, not claim-bound — STOP, no bare-Worker increase

Date: 2026-11-25. Lane: `drive/11430-drain-bottleneck` (leaf child, read-only).
Tree: branch `drive/11430-drain-bottleneck`; no production code touched.
Prior: the [pick-order shim](11430-pick-order-shim.md) convicted handler drain
and exonerated pick/limit; the [reopen census](11430-reopen-census-shim.md)
convicted the two wake sites; the
[suppression-count shim](11430-suppression-count-shim.md) ruled out an
exact-equality gate. This shim names, per domain, what serializes today plus
the conflict domain, and does the capacity arithmetic. All clocks UTC; QA reads
via `sh /tmp/qa-read.sh <file>` (15s statement timeout, read-only txn);
kubectl reads via the QA cluster context and namespace. SQL files lived in /tmp only,
never in the tree. Observation window ~14:13–14:55.

## Verdict

SATURATED on the vulnerability lane, and the binding constraint is handler
service time plus a cross-domain watermark gate — NOT the claim loop and NOT
the per-key conflict fence. A bare `Workers` increase is refused: it scales
the saturated resource (handler work / graph writes / Postgres CPU) without
touching either named serialization. Conflict-key-partitioned handler
parallelism is the primary lane ONLY with this per-domain proof attached:

- `advisory_group_projection` (conflict_domain `advisory_group`): serialized
  by the **winners-watermark readiness gate on the sci winners rebuild**, not
  by any claim key. 411/411 executions in the 37-min window defer/fail
  instantly (durations 0.0–1.5s, 0 successes); watermarks named in the errors
  are hours stale (`...rebuilt at 2026-11-25T01:10–01:15Z`). 0 net
  completions/hr vs 2/hr inflow; waiting 1190, inflight 5.
- `supply_chain_impact` (conflict_domain `scope`): serialized by **handler
  service time** (p50 39.4s, max 551s; 16 holder key-range statements at
  ~2.2–2.6s each ≈ the p50) under a **`GRAPH_WRITE_MAX_IN_FLIGHT=6`
  semaphore** with 8 workers. All 8 workers productive (10–33 distinct
  completions each), 16/16 claim slots full (8 executing + 8 prefetch),
  ~10k claimable rows waiting. Gross ~297/hr vs demand ~271/hr → saturated,
  backlog flat-to-growing.
- Claim loop (single claimer, poll 1s, `claimLimit=min(batchSize,idleWorkers)`,
  `batchClaimSize=Workers*4` cap [4,64] → 32 vuln / 16 general,
  `ORDER BY reclaim_rank, gate_boost, priority, source_inflight,
  source_fair_rank, domain_fair_rank, domain_inflight, updated_at,
  work_item_id`): **exonerated**. Keys are unique per row (max 1–2 rows/key),
  so the representative/live-lease fence admits everything; 1 benign
  23505-deferral in 37 min (the designed #4137 reclaim path, not attrition);
  no `ack_claim_rejected` lines; lock-wait is noise next to 39s service time.

## Topology (what runs where in QA, read from the API server)

- `eshu-resolution-engine-vulnerability` (`/usr/local/bin/eshu-reducer`):
  `ESHU_REDUCER_WORKERS=8`, claims ONLY
  `supply_chain_impact,advisory_group_projection`;
  `GRAPH_WRITE_MAX_IN_FLIGHT=6`; handler timeouts `sci=30m, advisory=5m`
  (default 1h).
- `eshu-resolution-engine-general`: `ESHU_REDUCER_WORKERS=4`, claims all other
  domains; `GRAPH_WRITE_MAX_IN_FLIGHT=4`.
- No `ESHU_REDUCER_WORKERS` override would apply defaults
  (`min(UsableCPUs,4)` on neo4j), but both engines set it explicitly, and
  `ESHU_GRAPH_BACKEND=neo4j` on both. Claim poll default 1s
  (`defaultPollInterval`, `service.go:43`); batch size default `Workers*4`
  clamp [4,64] (`config.go:147-164`); `claimLimit=min(batchSize,idleWorkers)`
  (`service_batch.go:134-137`).

## Saturation shim (observed values)

- s1 census (reducer stage): sci pending 8369 + retrying 487 + claimed 7;
  advisory pending 231 + retrying 964; workload retrying 32; everything else
  ≤3 waiting. In-flight at s3: 20 claimed, single `lease_owner='reducer'`,
  oldest claim ~30 min, horizons current (heartbeats live).
- s5 hot-domain 1h boundaries: advisory inflow 2/hr, completed(net) 0/hr,
  waiting 1190, inflight 5; sci inflow 41/hr, completed(net) 35/hr, waiting
  8852, inflight 11; workload inflow 20/hr, completed 740/hr, waiting 32.
  Overall: completed(net) 990/hr vs inflow 324/hr — the general lane drains;
  the vuln lane does not.
- Worker-busy: vuln 8/8 workers with 10–33 distinct sci completions each in
  37 min (100% busy, prefetch buffer full at 16/16 inflight). General 3/4
  workers with successes in a 2000-line sample, lane net-draining.
- Backend headroom at 2–3x in-flight: Postgres primary 7.7 cores vs req 8 /
  lim 12 (at 96% of request, 56% headroom to limit — the tighter bound);
  read replica 91m; Neo4j 464m vs 32-core limit (~1.4%, vast headroom).
  Tripling sci handler parallelism triples holder-range read pressure (~2.3s
  × 16 per pass) and graph-write pressure against a 6-slot semaphore and a
  Postgres already near its request — that is the 2–3x risk, measured, not
  assumed.

## Falsifiers (all three tested, all fail to exonerate idleness)

1. Workers idle while claimable backlog waits: REFUTED on the vuln lane
   (8/8 busy, buffer full, ~10k unique-key rows admissible and waiting;
   advisory claims recycle in ~0s, so no worker is ever parked).
2. Lock-wait rivaling handler service time: REFUTED (1 live-lease conflict
   deferral in ~37 min / 5000 lines; 0 `ack_claim_rejected`; 39s–551s
   service times dominate by 1–2 orders of magnitude).
3. 23505/ack attrition growing with concurrency: REFUTED (the single 23505
   at 14:27:48Z is the designed batch-rollback-and-reclaim path,
   `claim_mode=batch, constraint=main`; attempt-count spread on hot rows is
   970/294/183/1 at attempts 1/2/3/4 — mild, no storm).

## Capacity arithmetic (same 1h boundaries)

- sci: demand ≈ 41/hr new + ~230/hr reopens (census rate) ≈ 271/hr; gross
  service ≈ 297/hr at 8 workers (≈37/hr/worker from 183 distinct intents in
  37 min); net completed 35/hr → backlog +6/hr (8852 and growing). +4
  workers (12) → gross ~444/hr → net drain ~170/hr → ~52 hr to clear;
  +8 (16) → gross ~590/hr → ~28 hr. Linear scaling assumed AND at risk from
  the 6-slot graph-write semaphore and Postgres headroom above — the
  parallelism must be conflict-key-partitioned (per-key rows are unique, so
  partitioning is free on the claim side) AND paired with write-path proof.
- advisory: demand 2/hr, service 0/hr net — unbounded until the sci winners
  watermark advances. Any worker count recycles instant defers; capacity is
  not a worker function here at all.
- workload_materialization: 20/hr in, 740/hr out — drains 37x over; needs
  nothing.

## Decision

STOP after the arithmetic. No implementation, no design doc, no concurrency
proof in this lane. The arbiter gate is met: conflict-key-partitioned handler
parallelism may proceed as the primary lane, with per-domain serializations
(watermark gate for advisory; 39s service + 6-slot write semaphore for sci)
and conflict domains (`advisory_group`, `scope`) named above. A bare `Workers`
increase without the partition + write-path proof stays refused.

## Commands and observed values

1. `kubectl get sts eshu / deploy eshu-core` worker env: `ESHU_COMMIT_WORKERS
   =2, ESHU_PARSE_WORKERS=4, ESHU_SHARED_PROJECTION_WORKERS=8` (ingester);
   `ESHU_GRAPH_BACKEND=neo4j` both lanes.
2. Resolution-engine env (the reducer): vuln `WORKERS=8`,
   `CLAIM_DOMAINS=supply_chain_impact,advisory_group_projection`,
   `GRAPH_WRITE_MAX_IN_FLIGHT=6`; general `WORKERS=4`,
   `GRAPH_WRITE_MAX_IN_FLIGHT=4`; overrides
   `sci=30m, workload=5m, deployment_mapping=5m, advisory=5m,
   code_import_repo_edge=5m`.
3. s1 census: table above (37 rows; hot rows quoted).
4. s2 rates: completed(net) 990/hr total, inflow 324/hr total (97 rows).
5. s3 inflight: 20 claimed, owner `reducer`, oldest 14:20:40Z, horizon
   14:49:19Z.
6. s4 conflict: sci 8863 keys / 8854 waiting / max 1 per key; advisory 1193
   keys / 1188 waiting / max 1 per key; workload 19 keys / 32 waiting /
   max 2 per key. Fence admits everything.
7. s5 hot 1h: advisory 2/0/1190/5; sci 41/35/8852/11; workload 20/740/32/0
   (inflow/completed/waiting/inflight).
8. s6 attempts (hot domains): 970/294/183/1 at 1/2/3/4.
9. `top pods`: pg-primary 7718m (req 8/lim 12), pg-read 91m, neo4j 464m
   (lim 32 cores), api 3m, core 1m.
10. Vuln log census (5000 lines, 14:13:56–14:51:16Z): 1663 holder key-range
    slow-ok (~2.2–2.6s each), 623 execution-started, 540 shared-projection
    backoff, 212 deferred, 210 failed, 189 sci succeeded.
11. Durations: advisory defer/fail p50 0.0s (411 outcomes, 0 success); sci
    succeeded p50 39.4s max 551.2s; sci 183 distinct intents 10–33 per
    worker across all 8 workers.
12. Advisory reasons: `advisory_group_winners_watermark_stale/not_ready`
    (watermarks 01:10–01:15Z, hours stale).
13. 23505: exactly 1 line (14:27:48Z, live-lease-conflict deferral,
    `claim_mode=batch`); 0 `ack_claim_rejected`.
14. General lane (2000 lines): defers dominate per-domain but lane drains
    (workload 12 succeeded + small domains); 3/4 workers with successes.

## Not covered

- Whether the sci 551s-max tail is one slow scope kind or holder fan-out
  shape (needs per-intent sub-duration cut, not done here).
- Why the sci winners watermark is hours stale (upstream of this lane).
- Postgres active-session / lock-wait percentiles at 2–3x (needs a loaded
  probe, explicitly out of scope for a read-only shim).
- Reopen-vs-first-delivery split for the ~230/hr sci refill (prior shims
  cover the mechanism; the arithmetic uses the gross census rate).
