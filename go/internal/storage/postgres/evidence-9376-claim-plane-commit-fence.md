# Evidence: claim-plane commit fence (#9376 P2)

The ingestion commit gains a second fence mode for claims held by the
extracted claim plane (`IngestionStore.ClaimPlane`). The in-tree mode is
unchanged. Ruling and design: `docs/internal/design/9376-claim-plane-commit-fence.md`.

## Conflict domain and settings

- Conflict domain: the `generation_commit_fences` row keyed
  `(generation_id, work_item_id)`, written only while the transaction
  already holds the scope's `ingestion_scopes` row lock (the same lock
  that orders projector Ack). Different work items on one generation
  are different rows and never refuse each other.
- Claim lease 60 s (`workflow/defaults.go`), re-pin every lease/3 = 20 s,
  refusal margin 5 s (`DefaultClaimFenceMargin`, refused at begin unless
  below lease - lease/3), pin attempts 3 with 200 ms linear backoff, each
  pin and re-pin call bounded by `DefaultClaimFencePinAttemptTimeout`
  (10 s, capped at lease/3). The keeper stops when COMMIT returns and
  cancels a re-pin in flight when it stops.
- No new lock-order edge: the fence row is touched after the scope row
  lock; its FK takes `FOR KEY SHARE` on the generation row this same
  transaction just upserted; retention deletes only `superseded` rows
  through a `SKIP LOCKED` leg (`generation_retention_sql.go`).

## Performance

Performance Evidence: prove-the-theory shim, then the work, on a disposable `postgres:18`
(18.6, linux/arm64, docker, localhost only) bootstrapped with the full
core schema (268 tables) and seeded with 50,000 scopes x 20 generations =
1,000,000 `scope_generations` rows and 1,000,000 fence rows, `ANALYZE`d.

- Fence upsert `EXPLAIN (ANALYZE, BUFFERS)`: `Conflict Arbiter Indexes:
  generation_commit_fences_pkey`, 5-12 shared buffers, 0.016-0.066 ms for
  the six outcomes (first write, lower token refused, equal token same
  claim accepted, equal token other claim refused, higher token accepted,
  second work item on the same generation accepted). Cascade delete of a
  generation adds 0.043 ms (the FK trigger) to 122.9 ms of existing
  cascades.
- Fence upsert transaction under pgbench, 20 s per run, random keys over
  the 1M rows, random tokens 1-6 (mix of accepts and refusals), 0 failed:
  1 client p50 81 us / p95 90 us / p99 102 us at 12,115 tps; 8 clients
  p50 85 us / p95 229 us / p99 355 us at 71,844 tps.
- The plane path adds one pin round trip before BEGIN and one PK upsert
  inside the transaction; the plane's own pin latency is measured by the
  satellite (P3 histogram), not here.

## No regression

No-Regression Evidence: in-tree commit path (ClaimPlane nil): the only change on that path is two
nil checks, and `TestIngestionStoreCommitClaimedScopeGenerationFencesClaimInTransaction`
(statement sequence) plus `TestInTreeClaimedCommitWritesNoFenceRow` pin
the unchanged statements. Measured: `BenchmarkIngestionStoreCatalogLoadsPerCommit/cached`
(200 commits per op, fake DB), 6 runs each on the same shared host,
before (`c98bb3d7e7`) median 84.6 ms/op (71.5-89.3), after (`653445387a`)
median 87.1 ms/op (83.9-92.1), 110.04 MB/op on both. The 3% median gap
is inside the before run's own 25% spread on a loaded host; allocation
bytes are identical, so no regression is claimed or shown.

Correctness and concurrency, all against the disposable Postgres:
`TestClaimPlaneFenceRefusesReclaimedStaleWriterLive` (red with the fence
row write removed: the stale writer's commit returned nil; green with
it), `TestClaimPlaneFenceDeadlineRollsBackLive`,
`TestClaimPlaneFenceStaleRepinCancelsCommitLive`,
`TestClaimPlaneFenceReplayAndCrossItemLive`,
`TestClaimPlaneFenceConcurrentWritersAckAndMaintenanceLive` (`-race`,
5 runs: 8 writers per race, race 1 committed 1-3 and refused the rest,
race 2 with a concurrent projector Ack mixed commits, refusals, and
finalized skips; stored fence token always equal to the highest committed
fact token; maintenance exclusive holder took the lock 5-9 times per run;
no deadlock inside 30 s), and
`TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks` with odd
slots on the plane path (60 fence rows asserted). Parity:
`TestParityStaleWriterCommitFence` on both legs against `claim-plane`
built from `eshu-workflow` `v0.1.1` (`8311ed6`). Migration 270:
bootstrap at the pre-270 tree, apply 270, re-apply; schema dumps identical
apart from pg_dump's per-run `\restrict` nonce. Terminal counts: the
harness dropped its per-test databases; only the shim databases remain.

## Observability

Observability Evidence: the signals below, asserted where noted.

- `eshu_dp_ingestion_claim_fence_rejections_total{reason}` (closed set:
  `stale_fence`, `not_found`, `pin_unavailable`, `deadline`,
  `fence_token`, `fence_claim_mismatch`); asserted in the live tests
  (`fence_token` = 1 in T1, `deadline` = 1 in T2).
- `eshu_dp_ingestion_commit_repins_total{outcome}` (`ok`, `stale`, `error`).
- `eshu_dp_ingestion_commit_pin_slack_seconds` (unlabeled, 0-120 s).
- Span `ingestion.commit.claim_fence` (only `fence_mode=plane` is emitted;
  the in-tree path emits no fence span) and one log per refusal (WARN;
  ERROR for `fence_claim_mismatch`, an integrity alarm) with
  scope, generation, work item, claim, reason, requested and stored
  tokens, and pin slack. Operator page:
  `docs/public/reference/telemetry/claim-plane-commit-fence.md`.
