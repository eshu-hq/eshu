# #11129 staging-reads rulings (arbiter-eshu, binding), 2026-11-21

Four binding rulings govern #11129. All are recorded verbatim below from the
coordinator's copies; the arbiter role is read-only. Paths inside them are
repository paths at the commit each ruling names. The evidence they call for is
in [11129 staging reads](../evidence/11129-staging-reads.md).

- Ruling 1 (arbiter-11103-staging): the #11103 P2 failure is a design defect
  in the staging reads; it names the four-part fix and the theory proofs T1-T4.
- Ruling 2 (arbiter-11129-theory): the verdict on T1-T4; it keeps the
  captured_at btree judged on the real writer transaction, bounds the dead-head
  cost to three in-lock walks plus an intra-tick seq range on the collapse
  arms, adds the DELETE-arm range fix, and adds the T5 theory round.
- Ruling 3 (arbiter-11129c): T5-A neither passed nor failed; it restates the
  writer-cost bar on measures that resolve (R1 at 8 writers, R2 at 16 over 1
  writer), fixes the decision rule before the data, and orders one bounded
  round T5-A2.
- Ruling 4 (arbiter-11129d): T5-A passes on the restated bar; the R2 CPU
  ratio is withdrawn as a defective clause and replaced by a post-data
  absolute bound; migration 330 ships with the non-index work.

## Ruling 1

Recorded verbatim by the coordinator from arbiter-11103-staging (the arbiter role is read-only).
Verdict line: SHIP-#11103-AFTER a separate staging-reads fix (new issue, text below) merges and the P2 rerun on the built binary is green; R2 is not held; no pacing, no smaller batch, no writer-side counter.

Verified at origin/main 3d9ab0b5cf. Paths under go/internal; pg = storage/postgres. Skills loaded: postgres-rigor, concurrency-deadlock-rigor. The arbiter ran no SQL; every plan figure is the lane's from p2_plans.txt.

### 1. Findings from source

1. The tick runs three whole-table staging reads, not two. Each is `count(*)` next to another aggregate, with no WHERE:
   - collapse stats, `count(*), max(seq)` (pg/supply_chain_impact_winners_staging_collapse.go:55-58, called at :183, from pg/supply_chain_impact_winners_keyed.go:98), under the resweep lock;
   - residual, `count(*), min(captured_at)` (staging_collapse.go:157-159; callers keyed.go:143, keyed_chunk.go:313, and the full path at pg/supply_chain_impact_canonical_winners_rebuild.go:361), under the lock;
   - the gauge sample, `count(*), min(captured_at)` over staging (pg/supply_chain_impact_winners_keyed_sql.go:419-426), which runs before the lease claim on every tick of every reducer instance (reducer/supplychain/core/winners_keyed_maintainer.go:187-204), so it is outside the lock and multiplied by instance count. The lane's P2 did not name this one.
   The full path also reads `min(captured_at)` over staging inside the rebuild stamp (canonical_winners_rebuild.go:118-124).
2. The only staging index is the non-unique btree on `seq` (pg/migrations/309_supply_chain_impact_winners_dirty_staging.sql:56-57). Nothing supports `min(captured_at)`. `count(*)` beside `max(seq)` cannot use the index endpoint, which matches the measured Parallel Seq Scan (p2_plans.txt:2-19, 21-38).
3. The cost is proportional to the staged backlog on every tick, so a wave of N marks costs about N/50,000 ticks times three scans of N rows. That is quadratic in the wave. It is a design defect in the reads, independent of #11103.
4. The bar's time limit is not actually measured. The lane timed whole ticks. Two warm scans at 831 ms each explain about 1.7 s of the 3.21 s p50; the 8.35 s p95 is not attributed. The plan shape fails the bar on its own (full scan per tick, 25.6 s cold), so the verdict does not depend on the split.
5. The temp file is bounded and small: a 50,000-row sort, 5,976 kB, 115 ms total (p2_plans.txt:43-58). It is a bar miss, not the cause of the tick cost.

### 2. Decisions

1. Q1, scope of the defect. It is not only a #11103 blocker. Any staged burst pays it, in proportion to its size.
   - Normal tick on QA today: cheap while the staging heap is small. NOT_CHECKED whether the heap is still at a past high-water size; a seq scan reads every page, live or not. Probe 1 below settles it.
   - R2: the #11113 wave is bounded at about 2.8M re-marks (arbiter-11113-deploy item 4), 23 percent of the P2 load. By linear scaling from P2 that is about 0.2 s per warm scan, three per tick, 56 collapse ticks, and about 6 s for a fully cold scan. That is inside the 15 s tick bar. It is an extrapolation, not a measurement. R2 is not held.
   - After R2 the keyed and full paths both still call the residual read, so the exposure is unchanged by #11117.
2. Q2, the fix. Replace the three reads so no per-tick statement is proportional to the staged backlog. Four parts, one PR:
   a. Collapse stats: read the bound from the seq index alone, at most maxRows+1 entries (shape: count and max over `SELECT seq ... ORDER BY seq LIMIT maxRows+1`). Zero rows means nothing staged; maxRows+1 rows means split, with the bound at the maxRows-th seq; otherwise the bound is the newest seq. The measured analog is the existing OFFSET read: Index Only Scan, 76.7 ms cold. The `seq <= bound` collapse predicate, the shared `staged` CTE for merge and delete, and the key-ordered merge do not change.
   b. Residual minimum: add a non-unique btree on `captured_at` and read `min(captured_at)` as its own statement, so the planner can take the index endpoint. The value must stay exact. It feeds the stamp `GREATEST(prev, LEAST(S, residual min - 1us))`, and a value above the true minimum would stamp over an unapplied mark.
   c. Residual count and the depth gauge: exact up to the cap by the same bounded seq read; above the cap, report the visible seq span (`max(seq) - min(seq) + 1`, two index endpoint probes) and mark the reading as an upper bound in the span, the log, and the telemetry reference. `Bounded` needs only "remaining > 0" (keyed.go:150, :278, :343), which the bounded read gives exactly. The gauge sample (keyed_sql.go:419-426) takes the same two reads.
   d. Temp file: scope `work_mem` to the two collapse arms with `set_config('work_mem', <value>, true)` and restore the saved value before the tick-clock read, so admission and apply plan under the session setting. A rollback discards it. Pick the value from the measured in-memory sort size with at least 2x headroom. The resweep lock means one backend at a time holds it.
   Constraints every part keeps:
   - Seq order is not commit order. A writer can commit a lower seq after the collapse snapshot. No read may assume "everything below the last bound is gone", and no residual read may filter on `seq > bound`.
   - `captured_at` is `clock_timestamp()` per row (migration 309 line 34), so it is not monotone in seq across sessions. The first row by seq does not carry the minimum.
   - Writers stay plain INSERT with no unique index and no shared row.
   - Conservation holds: marks == drained version sums + residual log versions + staging residual (staging_collapse.go:29-36).
3. Options ruled out.
   - Smaller batch bound: fitting the sort in 4 MB needs roughly 15,000 rows, about 811 ticks, which fails the 250-tick bar. It is also pacing.
   - A counter row or summary row maintained by writers: every writer would update one row. That is the arbiter the staging table exists to remove (migration 309 lines 11-17). Serialization.
   - Head-of-seq `captured_at` as the minimum, or any margin on it: not exact (constraint above). Accuracy.
   - Holding the stamp at `prev` while a wave is split: safe but hides the horizon the watermark readings depend on. Reliability by hiding.
   - `pg_class.reltuples` for the count: stale by design.
   - BRIN on `captured_at`: the planner does not answer `min()` from BRIN.
   - Session-wide or role-wide `work_mem`: changes the apply plans. Only the scoped form is allowed.
   - Throttling holders, cutting workers, staging the wave: stands as ruled in arbiter-11103c section 6.
4. Q3. This is a new issue. #11120 covers the apply phase (`build_active_facts`, payload detoast) with its own acceptance criteria; these are different statements with a different cost driver. Text in section 4.
5. Q4. The fix gates #11103's merge and its R3 release. It does not gate R2. It also gates any later release whose expected staged burst exceeds the R2 bound of 2.8M marks. #11103's PR may open for review now, with the P2 section stating the failure and naming the new issue as its merge gate. #11103 merges after the fix merges, so no main pin can carry #11103 without it. The fix may ship in the R3 image with #11103 or in an earlier roll; it must not delay R2. The rest of arbiter-11103c stands (R3 gate, rollback triggers, M1-M4, will-not-do).

### 3. Required proof

Theory first, before any production code (throwaway SQL on the P2 database, 12.16M rows):
- T1: EXPLAIN (ANALYZE, BUFFERS) of the three replacement reads. Bar: no Seq Scan on staging, each under 250 ms cold, buffers bounded by the batch and not by table size.
- T2: the fact-arm collapse SELECT under the scoped `work_mem`. Bar: `Sort Method: quicksort`, no temp buffers, same GroupAggregate and key order as today.
- T3: staging insert throughput with and without the new index, at the writer concurrency the holder passes use. Baseline is the P2 stage of 12.16M rows in 3m02s. Bar: at most 10 percent slower. Over that, return to the arbiter with the numbers.
- T4: dead-entry head. Hold a REPEATABLE READ snapshot open, run 50 collapse ticks, then EXPLAIN the bound read and the minimum read. Both walk index entries of deleted rows that vacuum cannot remove yet. Report the cost. Over 1 s, return to the arbiter; do not add a seq floor (constraint above).
Then the work proof, on the built binary:
- P2 rerun with phases timed separately. Bar: collapse plus all staging reads at most 5 s per tick including the first cold tick, at most 250 ticks, no temp file, with writers inserting during the drain.
- Concurrency: a writer that commits mid-tick, and one that commits a lower seq after the collapse snapshot. Both marks survive, and the watermark never exceeds an unapplied mark's `captured_at`. Conservation identity checked at the end.
- A failing regression test first: a per-tick staging statement whose plan is a Seq Scan at a seeded backlog fails, then passes.
- Telemetry: the phase histogram already splits collapse and stamp; add the upper-bound marker from 2c and update the telemetry reference in the same PR.
Read-only QA probes (replica, statement_timeout set), before R2:
- Probe 1: `SELECT pg_relation_size('supply_chain_impact_winners_dirty_staging'), n_live_tup, n_dead_tup, seq_scan, seq_tup_read, last_autovacuum FROM pg_stat_user_tables WHERE relname = 'supply_chain_impact_winners_dirty_staging';`
- Probe 2: `SHOW work_mem;` and the collapse and stamp phase histograms as the pre-roll band.
During the R2 #11113 wave: record the same histograms and Probe 1 at peak staging depth. This is the first live measurement of the defect.

### 4. New issue

Title: Winners tick scans the whole staging table three times per tick, so a staged burst costs quadratic work
Labels: backend, capability:reducer-scale, performance, priority:high
Epic: #11120 sits under #10860; link the same epic if it is still the keyed-tick epic (NOT_CHECKED).
Refs: #11103, #10837, #11117.
Acceptance criteria:
- [ ] T1-T4 recorded before any production code, with plans.
- [ ] No per-tick statement on the staging table is a Seq Scan at a 12.16M-row backlog; a regression test pins it.
- [ ] Collapse plus staging reads at most 5 s per tick, at most 250 ticks for 12.16M marks over 121.6k keys, no temp file, measured by phase on the built binary with concurrent writers.
- [ ] The stamp's residual minimum is exact; the late-commit and mid-tick writer tests pass; the conservation identity holds.
- [ ] Staging insert throughput within 10 percent of the baseline with the new index.
- [ ] `Remaining` and the staging depth gauge state when they report an upper bound; the telemetry reference is updated.
- [ ] Live QA reading after deploy: collapse and stamp phase time and staging relation size, before and after.

### 5. Will not do

Lower the 50,000-row batch, pace or pause holders, add a writer-maintained counter, approximate the horizon, freeze the stamp during a wave, raise `work_mem` beyond the collapse statements, hold R2, fold this into #11120, or waive the no-temp-file bar. Each one either serializes, trades accuracy, or hides the cost.

### 6. Unverified

- No statement was executed by the arbiter. That `count(*)` beside `min` or `max` blocks the index-endpoint plan, and that a lone `min(captured_at)` takes it, is from Postgres planner behavior as the arbiter knows it; T1 is the proof.
- The R2 figures (0.2 s warm, 6 s cold) are linear scaling from a shared-host P2 run.
- Whether QA's staging heap is bloated today, and QA's `work_mem`: Probes 1 and 2.
- The unattributed part of the 8.35 s p95 tick.
- P2 reports dirty-log depth 121,000 after 244 ticks at 200 identifiers per tick, which would be expected to drain up to 48,800 keys. The lane must explain this in the evidence note before the rerun is read.
- The size of the dead-entry head cost under a held snapshot (T4). The current bound read and collapse arm already carry this exposure.
- Whether the staging reads sit inside the #11117 contract hash: the lane confirms with A's hermetic hash test.
- #11127's diff was read only as merged on origin/main; the lane's worktree at 72698052b7 and its evidence note were not reviewed beyond the P2 pointer at line 154.

## Ruling 2

Recorded verbatim by the coordinator from arbiter-11129-theory (the arbiter role is read-only).
Verdict line: PROCEED-AFTER-ONE-SHORT-THEORY-ROUND (T5, four measurements on the existing p11129 database). Keep the captured_at btree, judged against the real writer transaction and not the pure-insert microbench; accept the dead-head cost with at most three left-edge walks per tick and an intra-tick lower seq bound on the collapse arms; add the DELETE-arm range fix to #11129. No pacing, no smaller batch, no approximate horizon, no cross-tick floor.

Verified at origin/main 0e27dfb2e0. Paths under go/internal; pg = storage/postgres. Skills loaded: postgres-rigor, concurrency-deadlock-rigor, performance-rigor. The arbiter ran no SQL. Plan figures are the lane's, read from its files t_arm_explain.txt, t4_explain_held.txt, t4_ticks.txt, t1_after_warm.txt and the t3 files.

### 1. What I verified

1. The DELETE arm has no range predicate (pg/supply_chain_impact_winners_staging_collapse.go:107-111 and :144-148). The lane's plan shows Hash Join over a Seq Scan of 9.51M rows, 16 batches, 39,542 temp blocks, 7.16 s for the arm (t_arm_explain.txt). The finding stands. I did not find the plan for the fixed form (498.8 ms) in the directory, so that figure is the lane's claim until T5 records it.
2. The T4 tick loop (t4_proc.sql) ran the unfixed DELETE. Its tick times (3.8 s rising to 5-15 s) therefore mix the Seq Scan delete, the dead head, and the dirty-log merge. They do not measure the proposed design.
3. Every left-edge index read under the held snapshot pays the same walk: 2,500,001 to 2,550,001 heap fetches, 0.50 to 0.87 s each across the two runs in t4_explain_held.txt. This is true of both indexes. The cost is per walk, so the count of walks is the lever.
4. The collapse arms also start at the left edge (`Index Cond: seq <= bound`, t_arm_explain.txt), so the fact SELECT, the scope SELECT, and both DELETEs each pay the walk too once the DELETE takes the range. The predecessor's ruling did not count these.
5. The real fact_upsert writer stages marks with one statement per pass, after the fact upsert, in the same transaction (reducer/supplychain/core/writer_retract.go:166-177; pg/supply_chain_impact_winners_capture.go:77-98). The staging insert is one part of a larger transaction. The other writers are the triggers of migrations 310 and 311 and the audit insert (pg/supply_chain_impact_winners_audit.go:99).
6. Writer concurrency is `ESHU_REDUCER_WORKERS` times the reducer replica count (go/cmd/reducer/OPERATIONS.md:62). The default is `min(NumCPU, 4)` on Neo4j (docs/public/reference/environment-ingestion-queues.md:104).
7. The full resweep drain deletes the whole staging table by design (pg/supply_chain_impact_winners_keyed_settle_sql.go:227-233). It is not a keyed per-tick statement.

### 2. Decisions

1. Q1, the captured_at index. Keep the non-unique btree on `captured_at`. It is the only option on the list that gives an exact minimum from an additive, reversible change.
   - The 10 percent bar was a proxy for the writer path. The pure-insert run isolates the staging insert, which the real writer never does alone. The bar is restated against the real writer transaction (T5-A). The +22 to 25 percent microbench figure is recorded in the evidence note as the component cost, not hidden.
   - Restated bar: writer-transaction throughput at the real concurrency within 5 percent with the index. The 5 percent is tighter than 10 because the denominator is larger.
   - Second bar, concurrency: the index must not add a contention point. If the relative insert overhead at the highest writer count is more than twice the overhead at one writer, treat it as a concurrency regression.
   - If either bar fails, the fallback is pre-authorized, no return needed to choose it: replace the seq index with one btree on `(captured_at, seq)`, order the collapse by that pair, and read count, bound, and exact minimum from one bounded walk. That design needs its own theory round and a two-release index swap, so it is the fallback and not the first choice. Return to the arbiter only with its plans.
   - Ruled out: fillfactor (does not change right-edge insert work); a partial index (no predicate separates the minimum); BRIN probing (ranges only widen once freed heap pages are reused, so the read degrades to a full scan); a drain-maintained minimum or side row (the drain cannot know the `captured_at` of a row that commits late with a low seq without reading it); time partitions (a larger change than the fallback, with the same exposure); any head-of-seq value or margin (not exact, as ruled before).
2. Q2, the dead head. Accept the exposure. It is inherent in reading the oldest live entry of a queue table while an older snapshot exists, and the shipped bound read and collapse arm already carry it. Two changes bound it:
   a. At most three left-edge walks inside the lock per keyed tick: the collapse bound read (seq index), the residual bounded read (seq index), and the residual minimum (captured_at index). The residual statement takes `min(seq)` from the first row of its bounded read, not from a separate endpoint probe. That removes one of the three walks in the lane's R2.
   b. Intra-tick lower bound on the collapse arms. The bound read also returns the lowest visible seq, `lo`. Both arm SELECTs and both DELETEs use `seq >= lo AND seq <= bound`.
   Correctness of 2b. This is not the floor the predecessor forbade. `lo` comes from an unfloored read in the same tick and is never stored. A row that commits below `lo` after the bound read is not folded this tick; it stays staged, exactly as if it had committed a moment later. The merge and the delete still share one `staged` CTE, so nothing is deleted unmerged. The next tick's bound read is unfloored and meets that row first, so it cannot strand. The residual reads, the gauge, and the rebuild stamp stay unfloored, so the minimum stays exact.
   - Bars at the T4 condition (2.5M dead entries under a held snapshot): the four arm statements show no dead-head heap fetches; at most three in-lock walks; each walk at most 1 s; all recorded with plans. The 1 s limit is now per walk, not for the sum.
   - Not required: a cross-tick floor, a stored floor, or any floor on a residual read.
   - Operator note: the telemetry reference must say that collapse and stamp phase time rising while staging depth is flat points at a held snapshot, and name the `pg_stat_activity.backend_xmin` check.
3. Q3. Yes. The DELETE-arm range joins #11129's scope as the fourth statement, in the 2b form. It is semantically redundant: every seq in `staged.seqs` already satisfies the range, and the statement has one snapshot.
4. Plan stability. The lane saw the fact-arm SELECT plan a Parallel Seq Scan under stale stats, and the DELETE misplan comes from row estimates. A wave starts with stats from an empty or small table. So every per-tick statement must hold its index plan in two states: after a fresh ANALYZE, and with stats taken on an empty table followed by the 12.16M load with no ANALYZE. If a plan flips, fix it by statement structure (for example a bounded `ORDER BY seq LIMIT` subquery feeding the aggregate). A scoped `enable_*` toggle needs a return to the arbiter.
5. The `work_mem` decision stands as T2 proved it: 32MB, transaction-scoped, restored before the tick-clock read.
6. Scope line for the no-Seq-Scan criterion: the keyed tick's staging statements, the pre-lease gauge sample, and the rebuild stamp's minimum. The full resweep drain is exempt because it deletes every row.

### 3. Restated design

- Bound read: one statement over the seq index, at most maxRows+1 entries, returning count, `lo`, and the bound.
- Collapse arms: unchanged merge, key order, and shared `staged` CTE; SELECT and DELETE both ranged on `seq >= lo AND seq <= bound`; scoped `work_mem`.
- Residual: bounded count and `min(seq)` from one seq-index read, `max(seq)` from the right edge, `min(captured_at)` from the new index. Exact up to the cap; above it, the seq span marked as an upper bound.
- Gauge sample and rebuild stamp: the same reads, unfloored.
- New index: non-unique btree on `captured_at`, added by migration.

### 4. Required proof

Theory round T5, before production code, on p11129:
- T5-A, writer path. Run the shipped fact upsert plus `captureWinnersDirtyFactsSQL` in one transaction per pass, with and without the index, three alternating pairs, at 1 writer, at the real writer count, and at twice that. Also rerun the pure-insert pair at 1 writer. Report throughput and the overhead ratio. To find the real count: `ESHU_REDUCER_WORKERS` and the reducer replica count from the QA deployment config or the running pods; the coordinator supplies them. If they are not available, use 4, 8, and 16 and say so.
- T5-B, the ranged arms. EXPLAIN (ANALYZE, BUFFERS) of both arms in the 2b form: no Seq Scan, no temp file from the delete beyond what the lane reports and explains, time recorded.
- T5-C, the dead head again. Hold the snapshot, run 50 ticks with the T5-B statements, then EXPLAIN the three in-lock reads and the four arm statements. Bars in decision 2.
- T5-D, stale stats. Every per-tick statement in both stats states of decision 4.
Then the work proof from arbiter-11103d section 3, unchanged, with these edits: the regression test covers the DELETE arms and both stats states; the migration states how the index is built on a populated table and how long writers wait, measured at 12.16M rows.

### 5. Acceptance criteria for #11129, restated

- [ ] T1-T5 recorded before production code, with plans.
- [ ] No keyed per-tick staging statement, gauge sample, or rebuild-stamp minimum is a Seq Scan at a 12.16M-row backlog, with fresh and with pre-burst stats. This includes both collapse DELETE arms. A regression test pins it.
- [ ] Collapse plus staging reads at most 5 s per tick, at most 250 ticks for 12.16M marks over 121.6k keys, no temp file from the collapse sort, measured per phase on the built binary with writers inserting during the drain.
- [ ] Under a held snapshot with 2.5M deleted entries: at most three left-edge walks in the lock, each at most 1 s, and no dead-head fetches in the arm statements.
- [ ] The residual minimum is exact. A mark that commits mid-tick, one that commits a lower seq after the collapse snapshot, and one that commits below `lo` after the bound read all survive; the watermark never exceeds an unapplied mark's `captured_at`; the conservation identity holds.
- [ ] Writer-transaction throughput within 5 percent with the new index at the real writer concurrency, and the insert overhead ratio from 1 writer to the highest count at most 2x. The pure-insert component cost is stated.
- [ ] `Remaining` and the depth gauge say when they report an upper bound; the telemetry reference is updated, including the held-snapshot note.
- [ ] Live QA reading after deploy: collapse and stamp phase time and staging relation size, before and after.

### 6. Will not do

- Waive the insert cost by argument. The restated bar needs the T5-A measurement.
- Add a cross-tick or stored seq floor, or floor any residual read. A late low-seq commit would be missed and the stamp could pass an unapplied mark.
- Approximate the minimum, freeze the stamp, lower the batch, pace writers, or add a writer-maintained row. Each trades accuracy or serializes.
- Adopt the composite-index design now. It is unmeasured and has a larger rollout risk; it is the named fallback only.
- Treat the T4 tick times as evidence for or against the design. They ran the unfixed DELETE.

### 7. Unverified

- The fixed DELETE figure (498.8 ms) and the cause of the +22 to 25 percent insert cost. T5-A and T5-B settle both.
- QA's `ESHU_REDUCER_WORKERS` and replica count.
- Whether the DELETE Seq Scan explains lane-11103's 8.3 s p95 and 46.8 s first tick.
- The dirty-log merge took 3.07 s with 2.53M dead tuples under the held snapshot. That is the P2 re-mark pattern meeting the same snapshot, outside #11129. It means the 5 s tick bar cannot be read under a held snapshot; the held-snapshot criterion is therefore stated on staging statements only.
- The delete joins on `seq` alone and no constraint makes seq unique. An explicit duplicate seq would let the delete remove an unmerged row. Existing behavior, not introduced here; the lane should say whether any writer sets seq explicitly.
- keyed.go's comment says a row that commits after the tick-clock read carries a later `captured_at`. `captured_at` is taken at insert, not at commit, so a writer that inserted before the read and commits after it is not covered by that sentence. I did not trace how #10706 handles it. The coordinator should have it checked; it does not change this ruling.
- Whether the migration runner allows `CREATE INDEX CONCURRENTLY`.

### Coordinator note: real writer concurrency on QA

Read from the QA deployments on 2026-11-21: the supply_chain_impact writer runs only in the vulnerability reducer deployment (claim domains supply_chain_impact and advisory_group_projection): ESHU_REDUCER_WORKERS=8, 1 replica, so 8 writers (the general deployment has ESHU_REDUCER_WORKERS=4 and does not claim supply_chain_impact). Use 1, 8 and 16 writers in T5-A.

## Ruling 3

RULING: T5-A is neither passed nor failed. Run one bounded round (T5-A2, about 20 minutes, time box 45 minutes wall, one round, a failed attempt does not restart the clock) with the decision rule below. Hold migration 330 and the push until it reports. The non-index work proceeds now (no push, no PR update). #11136 stays separate.

### Verified by the arbiter
1. The ruling's primary measure is throughput (arbiter-11129b.md:22: "writer-transaction throughput at the real concurrency within 5 percent"). CPU is named nowhere in it. Accepting on CPU misstates the ruling.
2. The second bar (arbiter-11129b.md:23, :62) says "relative insert overhead" at the highest writer count at most twice the 1-writer overhead and names no measure. WAL bytes were the lane's proxy, not the ruling's text.
3. Recomputed pair statistics (idx over noidx, mean of pair deltas, SE across pairs): 1 writer, 2 pairs: throughput -0.95% (SE 0.12), CPU +1.81% (SE 0.47), WAL +1.24% (SE 0.01, +28,260 B). 8 writers, 6 pairs: throughput +0.51% (SE 2.98), CPU +1.88% (SE 0.59), WAL +2.08% (SE 0.54, +59,233 B). 16 writers, 5 pairs: throughput -2.09% (SE 2.26), CPU +2.11% (SE 0.81), WAL +3.20% (SE 1.77, +88,177 B). The no-index WAL spread (CV) is 0.61%, 0.31%, 2.18%.
4. Throughput at 8 writers does not resolve the 5 percent bar: pair deltas run from -6.48 to +13.81 percent; the 95 percent interval is about -7 to +8 percent. CPU and WAL bound the added work at 8 writers below 5 percent (upper 95 percent bounds about +3.4 and +3.5 percent). Added waiting is not measured.
5. The 16-writer WAL ratio (2.58x by pair means, 2.52x by ratio of means) is not a finding: the no-index cells alone vary by 2.18 percent at 16 writers against a 3.2 percent effect, and one pair is negative. The 8-writer ratio is 1.68x.
6. Index footprint per staged row is flat across writer counts (db_bytes deltas 22.8, 24.7, 25.1 B/row at 1, 8, 16 writers). That argues against a split-pattern change.
7. The writer's advisory lock is keyed per (scope, generation); other keys never wait (writer_retract.go:120-128). captured_at defaults to clock_timestamp() per row (migration 309:34), so all writers insert at the right edge of the new index, the same pattern the seq index already has.
8. Pure insert at 1 writer: three runs at 103.4 to 103.6 tps and one no-index run at 123.4; that outlier is the whole -16.2 percent. The ruling treats pure insert as a recorded component cost, not a bar (:21).

### Decisions
1. Not "accept on CPU": the primary bar is throughput and it is unresolved; CPU and WAL are supporting evidence only.
2. Not "fail on WAL": a 1.1 SE difference with a negative pair is noise; firing a two-release index swap on it would invent a failure.
3. One bounded round T5-A2. The earlier ratio bar had a defect (it named no measure, and a ratio of two overheads near the noise floor is ill-conditioned). It is restated on measures that resolve, fixed before the data.
   - Design: same harness, private container, same settings, same pass transaction, fresh clone and CHECKPOINT per cell. 4 pairs at 1 writer, 4 at 8, 6 at 16. Flip the order inside alternate pairs (index first on even pairs).
   - Add per cell: start and end pg_current_wal_insert_lsn(); pg_stat_wal deltas of wal_records, wal_fpi, wal_bytes; pg_waldump --stats=record over that LSN range, to split record bytes from full-page-image bytes.
   - Add a sampler: one connection polls pg_stat_activity every 10 ms for the writer backends and counts active samples by wait_event_type and wait_event.
   - The lane states the advisory key each writer uses. Keys must be distinct per writer, as in production. If writers shared a key, every 8- and 16-writer cell is invalid for the concurrency bar and must be rerun.
4. Decision rule, fixed in advance.
   - R1, primary, at 8 writers. Pass needs all four: throughput delta at or above -5 percent on the mean of old and new pairs pooled; CPU per pass at most +5 percent; total WAL bytes per pass at most +5 percent; the share of active writer samples waiting on LWLock or Lock rises by at most 2 percentage points with the index.
   - R2, concurrency, 16 writers over 1 writer. Pass needs: non-FPI WAL record bytes added per mark at most 2x; CPU overhead at most 2x; the same 2-point wait rule at 16 writers.
   - Full-page-image bytes are reported beside it, not in the ratio. If the FPI count added per mark at 16 writers is over 2x the 1-writer value with the same sign in 5 of 6 pairs, return to the arbiter with the rows.
   - The throughput ratio is recorded and is not decisive (resolving a 1-point effect against a 5-point pair spread needs over 100 pairs).
   - All pass: T5-A passes and migration 330 ships in the PR. R1 or R2 fails on record bytes, CPU or waits: the (captured_at, seq) fallback fires as pre-authorized, with its own theory round. Anything else: return with raw rows.
5. Reconcile two figures in the same round. The writer transaction adds about 40 B of WAL per mark at 1 writer; pure insert adds 66 B per row; a plain btree insert record is about 64 B. The pg_waldump Btree record count must show about 714 more records per pass with the index. If it does not, the harness is not staging what it reports.
6. Name the limiter. Throughput rises only 1.31x from 1 to 8 writers on a 16-core host. The wait profile must say what the writers wait on without the index.
7. Non-index parts: the lane proceeds now in its worktree with the ranged arms, bounded reads, comment fix and tests. No push and no PR update until T5-A2 reports (without the index the min(captured_at) read is a Seq Scan, which fails acceptance criterion 2; and the fallback reorders the collapse by (captured_at, seq), so the arms are at risk of rework). Migration 330 stays uncommitted.
8. #11136 stays a separate issue. Criterion 3 bounds "collapse plus staging reads", measured per phase. The 2.9 s reading was taken under a held snapshot, and arbiter-11129b.md:79 already says the 5 s bar cannot be read in that state. The P2 rerun must report the dirty-log read phase on its own line. If that time cannot be separated from the collapse or staging phase, return to the arbiter.
9. #11136's text says the dirty-log index is judged against "the same 10 percent bar #11129 uses". That is stale: the bar is 5 percent on the real writer transaction plus the restated R2. The coordinator corrects it in a comment.

### Required proof
T5-A2 output files with the per-cell rows, the pg_waldump stats, the wait-sample table, and the per-writer advisory keys. The evidence note records both rounds, the pure-insert component cost (+22 to 25 percent at 8 writers, +66 B/row), and the restated bar with this ruling as its source. The harness source is kept beside the data files.

### Will not do
Pass T5-A on CPU. Fail T5-A on the 16-writer WAL ratio. Drop or loosen the 2x bar. Let the index ride in on argument, or let a dirty-log index ride into this PR unmeasured. Accept fewer writers, pacing or a smaller batch as an answer to any contention T5-A2 finds.

### Unverified (arbiter)
The harness source (zz_t5_writer_11129_live_test.go was not on disk): advisory keys, how CPU per pass is sampled, what the WAL counter covers. The 324 to 390 B/row pure-insert WAL figure. The 1-writer CPU baseline moved from 53.2 to 64.7 ms between two no-index cells (cause unknown; pair deltas are stable). Whether the 2-point wait threshold is the right size: if the no-index wait share varies by more than 2 points between cells, return to the arbiter before applying the rule. #11136's three reads and their plans (issue text only read).

## Ruling 4

RULING: T5-A passes; migration 330 ships in the #11129 PR. R1 passes all four checks, R2 passes on record bytes and waits, and the R2 CPU ratio is withdrawn as a defective clause of 11129c and replaced by an absolute CPU bound that passes. No further measurement is ordered. The (captured_at, seq) fallback does not fire.

### Verified by the arbiter
1. All 28 cells pass; each cell asserts staged rows == marks (harness:171). 4 pairs at 1 writer, 4 at 8, 6 at 16, order flipped in alternate pairs.
2. Advisory keys are distinct per writer in every cell (8 of 8, 1 of 1, 16 of 16).
3. The harness stages what it reports: 1-writer cells add Btree INSERT_LEAF +106,808 and SPLIT_R +292 = 107,100 = 714 x 150 exactly. Added Btree records per pass +715.8, +716.4, +718.0 at 1, 8, 16 writers. The harness index (harness:64) has the same name and column as migration 330.
4. Recomputed means match analysis.txt. Old 8-writer pairs: -6.48, +2.57, +0.55, -3.20, -4.19, +13.81; pooled with the new four the mean is -0.547 percent.
5. Reconciliation (11129c decision 5) holds at 1 writer on three counters: LSN diff +63.2 B/mark, pg_stat_wal +64.7, waldump record bytes +64.55. The "+64.5/+63.9/+65.5" figures are waldump record bytes per mark; the LSN diffs are +63.2, +66.1 (SE 24), +158 (SE 61, includes full-page images).

### Decisions
1. R1 at 8 writers passes.
   - Throughput pooled mean -0.55 percent (10 pairs, 95 percent interval about -5.9 to +4.8): passes the fixed rule on the mean. Resolved neighbours: 1 writer -0.99 percent (interval -1.66 to -0.32), 16 writers -3.13 percent (interval -4.78 to -1.49).
   - CPU per pass -0.47 percent (interval -2.76 to +1.82). WAL bytes per pass +1.58 percent (interval -0.30 to +3.45).
   - Wait share mean -0.29 points passes. The precondition tripped (no-index spread 6.44 points); the lane's reading that the host effect cancels inside a pair is NOT supported (pair deltas +3.69, -2.32, -1.89, -0.64, SD 2.75; drift runs over blocks of cells; lock share correlates with passes/s at -0.84). The precondition's purpose is met by splitting the samples by wait event: lock classes other than LWLock/WALWrite rise -1.12 points (interval -3.52 to +1.28, upper bound under 2; WALInsert -0.70, BufferContent -0.32): where an index contention point would show. LWLock/WALWrite alone +0.83 points (interval -1.20 to +2.85), the commit flush queue; at 16 writers the same event is +0.14 (interval -1.09 to +1.36).
2. R2 record bytes and waits pass: non-FPI record bytes per mark 65.47 / 64.55 = 1.01x; wait rise at 16 writers +1.23 points (interval +0.76 to +1.71, all six pairs positive); the rise sits in WALInsert (+0.73) and BufferContent (+0.49); no new wait event in the top eight.
3. R2 CPU ratio: withdrawn, neither pass nor fail (defect of 11129c decision 3). CPU is the container cgroup counter (harness:281) and includes background work: cells 10, 11, 12, 15 contain Heap/INPLACE, Gin/CREATE_PTREE, Standby/INVALIDATIONS and extra commits; cells 8, 9, 13, 14 do not (64.96/64.86/59.87/61.17 ms versus 53.64/54.03/53.98/53.81). The 16w/1w ratio is anywhere from 0.9x to 10x by choice of denominator; defining it on pairs picked after the data is not allowed. Replacement measure, chosen after the data (the note must say so): added CPU per pass at 16 writers as a percent of no-index CPU per pass, 95 percent upper bound at most +5 percent. Result +1.59 percent, upper bound +2.72 percent, +1.27 ms per pass, 1.8 microseconds per mark: pass. The bar's purpose (11129b.md:23, the index must not add a contention point) is tested directly by the wait rule and by record bytes, where 2x stays in force.
4. Full-page images: no return. The ratio to the 1-writer value is degenerate (no checkpoint inside a 9-second 1-writer cell). By sign 4 of 6 pairs at 16 writers are positive, under the 5 of 6 trigger; the lane's "0 of 6" is an artifact of a near-zero negative denominator. Added Btree FPI bytes (mean +51 MB per cell, up to +141 MB) exceed the whole new index (~14 MB at 25 B/row): images of other indexes' pages. Report as unexplained.
5. No more data: more 8-writer pairs could only fail the wait check if new pairs averaged above +2.8 points in WALWrite while WAL bytes rise 1.6 percent and 16 writers shows +0.14; more 1-writer pairs would leave the CPU ratio inside its own error.
6. Limiter, named: without the index one writer is 89 percent on CPU; at 8 writers 67 to 72 percent of active samples wait on LWLock/WALWrite; at 16 writers 44 percent on WALWrite and 35 percent on WALInsert. This round 8 writers ran 1.05x the 1-writer rate (17.72 over 16.92 passes/s; cells 0.96x to 1.16x), with and without the index; a reading on this host and container, not a production claim.
7. Migration 330 ships in the #11129 PR with the non-index work.

### Required proof (evidence note)
- Both rounds with per-pair rows, and paths to run.txt, the waldump files, analyze.py and the harness source.
- The bar as restated in 11129c, this ruling as the source of the pass, and the CPU-ratio withdrawal with its reason and the post-data replacement bound.
- The costs stated plainly: throughput -0.99 percent at 1 writer, -0.55 percent pooled at 8, -3.13 percent at 16 (interval -4.78 to -1.49; pooled with the earlier round -2.66); +65 B of WAL records and 714 Btree inserts per pass; CPU +1.59 percent and lock wait +1.23 points at 16 writers; the pure-insert component cost (+22 to 25 percent at 8 writers, +66 B/row).
- The 8-writer wait-share spread, the wait-event split, and the 2.85-point upper bound on WALWrite.
- The FPI figures (+92 B/mark at 16 writers, SE 61, 4 of 6 positive) as unexplained.
- The limiter, and host load during 8-writer cells 4 to 6 (load 9.5 to 11.4).

### Will not do
Fail T5-A on a CPU ratio whose denominator is 0.15 to 1.8 percent. Define that ratio on pairs picked after the data. Pass the wait check on the claim that pairing cancels the host effect. Order pairs that cannot change the decision. Treat the 16-writer throughput cost as zero (about 3 percent and resolved). Accept fewer writers or pacing as an answer to the WAL flush limiter.

### Unverified (arbiter)
What ran in the background of the four 1-writer cells (autovacuum presumed); why FPI bytes rise at 16 writers in 4 of 6 pairs (checkpoint timing a hypothesis); the cause of the earlier round's +40 B/mark (pg_stat_wal lag is the lane's reading); what raised host load to 11.4 in the quiet window; byte equality of the harness lock key with the production writer's key (11129c item 7 checked the production side only); whether the WAL flush limiter appears on production storage.
