# Batch predrain moves normal fact volume pre-Begin (#10020)

Whole-batch generation deletes held the scope lock for the fact cascade:
QA pg_stat_statements shows `DELETE FROM scope_generations WHERE
generation_id = ANY(...)` averaging 3.7 s per call over 1,035 executions
against 0.3 ms for the ordered work-item lock, with every big child table
on an index seek (per-child EXPLAIN ANALYZE + ROLLBACK at breaching
shapes: facts 1850 ms, search docs 362 ms, work items 39 ms, rest under
12 ms each), so the hold is volume, not a plan defect. The per-child
split and the cheapest-shim proof live in the roll-3 delete-path
attribution note.

The predrain now drains every eligible generation with facts oldest-first
in committed batches under the existing row cap/deadline, and the prune
admits only drained or untouched generations; touched-but-unfinished
batches wait for the next pass.

No-Regression Evidence: baseline at 820f9b690e (chunk predrain only) on a
disposable PostgreSQL 18.6 backend — 3 superseded generations x 40,000
file facts, BatchRowLimit 200,000 — prunes whole with
ChunkPredeleteBatches 0 and ScopeLockHold ~1.0 s; after the change the
same shape predrains in 3 committed batches, prunes whole, reports
120,000 fact rows exactly once, and ScopeLockHold is 26 ms with
locked_recount under 2 s
(`TestGenerationRetentionBatchPredrainKeepsScopeLockBoundedLive`, plus
hermetic partial-deferral, crash-resume, delta-flip, and concurrent-pass
coverage in the same package). QA-scale proof rides the rollout
(first-cycle and Gate-3 STOP watches), not this gate.

Observability Evidence: one new histogram plus its cycle log field (no
new span, worker, queue, lease, or runtime knob).
`eshu_dp_generation_retention_predrained_over_limit_generations` and the
cycle log's `predrained_over_limit_generations` carry the former
`PredrainedOverLimitGenerations` result field end to end (retention.Result
-> reducer wiring -> recordResult metric and log, pinned by
`TestGenerationRetentionRunnerRecordsChunkPredeleteBatches` with a mutant
kill), alongside the existing `predrain_chunk` phase,
`ChunkPredeleteBatches` counter, and `row_limit_chunked` skip: Gate-3
tells a predrained-normal cycle (batches > 0, over-limit count 0) from a
chunk cycle (over-limit count nonzero) from these fields, and the
per-generation "left for a later pass" log line still marks every deferred
drain. Supersession (arb-10020-batch-predrain-packing): the packer counts
post-drain rows, so drainable combined facts-plus-ledger over-limit packs
within the limit and `RowsOverLimit` stays quiet; only ledger-alone
overage on a lone batch reports, and the PB5 batches-of-one letter no
longer applies to drainable generations.

## Gate-3 closeout (QA, image eb583d14, 2026-11-14)

Gate-3 closed on the normalized bars with cycle-4 as the closing cycle
(arbiter ruling 2026-11-14, `docs/internal/arbiter/10020-gate3-close-ruling-2026-11-14.md`).
Cycle-4 is drain-shaped, not steady-state; the close rests on the safety
invariants holding on all four observed cycles, whose shapes exceed anything
steady state produces. The absolute `count_rows` bar stays voided for backlog
cycles (planning-shape future work). The pending plateau (474-510, arrivals ~=
drain) is the expected live-system equilibrium while sync runs; pending = 0 is
not an observable state. Predrain health held throughout: over-limit 0,
chunk batches > 0, every skip reason-tallied, no errors or deadlocks.

| Cycle (UTC) | Gens | Rows | Lock hold | Per-gen | Recount | count_rows | Over-limit |
|---|---|---|---|---|---|---|---|
| c1 | 1 | 580,000 | 4.88 s | 4.88 s | 0.25 s | 73 s | 0 |
| c2 | 1 | 647,000 | 5.11 s | 5.11 s | 0.15 s | 238 s | 0 |
| c3 | 28 | 487,000 | 20.38 s | 0.73 s | 0.21 s | 575 s | 0 |
| c4 11:32:49 | 20 | 1,848,925 | 16.67 s | 0.83 s | 0.29 s | 636.9 s | 0 |

c4 skips: 182, all tallied (delta_base 75, row_limit 79, row_limit_chunked 27,
same_sha_newest 1). c4 phases: predrain_chunk 174.2 s / 32 batches,
predelete_search_index 21.8 s / 308 batches, delete_scope_generations 13.4 s,
commit 0.005 s, rows_over_batch_row_limit 0, locked_scope_rows 16.

Cycle-5 bonus (11:41:43Z): 0 gens admitted, 519,514 rows via pre-Begin
pre-deletes, scope lock hold 0 s, 107 skips tallied (first-seen row_limit_ledger
1 and search_index_undrained 1 among them), over-limit 0, duration 95.4 s.
A zero-lock cycle: background drain progresses without holding the lock.

Confirmation notes: the 11:18:37 event batch (20 events) is cycle-4's batch
(`now` is captured pre-predrain at `generation_retention.go:97`, so events
stamp cycle start; 11:18:37 + 174.2 s predrain + 654.9 s prune ~= 11:32:49).
Its events sum 106,311 rows vs `rows_pruned_total` 1,848,925: the selection
runs post-predrain, so event counts are post-predrain residuals, and
`finish()` merges predrained rows into `RowsPruned` only for pruned gens while
predrained rows of skipped gens merge with the `row_limit_chunked` skip and
are recorded nowhere. No missing writes (same-tx atomicity, 20/20 count
match); the accounting gap is filed as P3 #10677 alongside the timestamp lag.
