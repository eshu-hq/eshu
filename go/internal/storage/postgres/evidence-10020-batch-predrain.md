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
