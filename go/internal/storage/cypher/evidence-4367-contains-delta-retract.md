# CONTAINS Delta Retract Evidence (#4367)

This note records the local proof for the C-14 `retractable_edge:CONTAINS`
delta tombstone backfill. The change teaches the offline replay delta fixture to
move a surviving `Directory` child from `edge-parent-a` to `edge-parent-b`, then
uses the canonical writer's delta retract phase to delete the stale parent
`CONTAINS` edge before the current generation writes the new edge.

Performance Evidence: not a throughput optimization. The new runtime work is
bounded by delta inputs:

- Explicit deleted-directory cleanup is path-seeded from
  `CanonicalMaterialization.DeltaDeletedDirectoryPaths` and batched with
  `canonicalNodeRefreshFilePathBatchSize`.
- Current directory parent refresh is row-seeded from the current generation's
  non-root `DirectoryRow` values and batched with the same cap.
- The backend seed properties are `Directory.path` plus `repo_id` /
  `evidence_source`; Eshu pins `directory_path` uniqueness in the graph schema.

No-Regression Evidence: baseline main before this branch was
`28a73878810c6e3e1fe9ec351f4de6c63a4367cc`, which includes PR #4700's guard
that kept `retractable_edge:CONTAINS` uncovered. A backend replay proof before
the final row-bounded parent refresh failed with the old parent edge still
present:

```text
/tmp/eshu-4367-green19-replay-tier.log:
old edge-parent-a CONTAINS edge count = 1, want 0
```

The final proof ran against the local NornicDB replay-tier container using the
pinned Eshu NornicDB image family documented in
`docs/public/run-locally/docker-compose.md`:

```text
ESHU_REPLAY_TIER_HTTP_PORT=17475 ESHU_REPLAY_TIER_BOLT_PORT=17688 \
  bash scripts/verify-replay-tier.sh
```

`/tmp/eshu-4367-green22-replay-tier.log` showed:

- old `edge-parent-a -> edge-child` `CONTAINS` edge count `0`;
- new `edge-parent-b -> edge-child` `CONTAINS` edge count `1`;
- anonymous `Directory` shell count `0`;
- idempotent second gen2 write with the same readback;
- `offline replay tier PASSED against real NornicDB`.

Focused and local pre-PR verification also passed:

```text
go test ./internal/replaycoverage ./cmd/replay-coverage-gate ./internal/replay/offlinetier ./internal/storage/cypher -count=1
bash scripts/test-verify-replay-coverage-gate.sh
bash scripts/verify-replay-coverage-gate.sh --blocking
make pre-pr
```

The blocking replay coverage gate reported `216 pass, 0 required-fail, 173
advisory-warn`, with `retractable_edge_type 1/52`.

Observability Evidence: no new metric, span, or log name is introduced. The
changed Cypher still flows through the existing canonical writer executors, so
operators retain `neo4j.execute` / `neo4j.execute_group` spans,
`eshu_dp_neo4j_query_duration_seconds`, `eshu_dp_neo4j_batch_size`,
`eshu_dp_neo4j_batches_executed_total`, and retry counters for graph-write
diagnosis.

No-Observability-Change: the replay-tier proof and local pre-PR run exercise
the changed writer statements through the existing instrumented executor path;
the feature adds no new async worker, queue, or endpoint surface requiring a new
operator signal.
