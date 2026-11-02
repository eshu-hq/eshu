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
(200 commits per op, fake DB), 6 runs each, interleaved before/after on
the same shared host (load average about 18 on 16 cores): before
(`578d2d38e6`, the PR-A tip PR-B stacks on) median 80.6 ms/op
(67.6-182.3, one outlier run), after (`aaf37e8d2d`) median 70.2 ms/op
(69.4-86.9), 110.04-110.05 MB/op and about 647,400 allocs/op on both.
The time gap is inside the host's run-to-run noise and allocation bytes
are identical, so neither a regression nor a speedup is claimed.

Correctness and concurrency, all against the disposable Postgres, run
on a freshly created, empty database (nothing in `public`), `-race`:
`TestClaimPlaneFenceRefusesReclaimedStaleWriterLive` (red with the fence
row write removed: the stale writer's commit returned nil; green with
it), `TestClaimPlaneFenceDeadlineRollsBackLive`,
`TestClaimPlaneFenceStaleRepinCancelsCommitLive`,
`TestClaimPlaneFenceHungRepinDoesNotHoldADurableCommitLive` (red before
the keeper fix: the commit never returned while a re-pin hung),
`TestClaimPlaneFenceReplayAndCrossItemLive`,
`TestClaimPlaneFenceConcurrentWritersAckAndMaintenanceLive` (5 runs:
8 writers per race, race 1 committed 1-5 and refused the rest, race 2
with a concurrent projector Ack mixed commits, refusals, and finalized
skips plus one guaranteed late-writer skip; stored fence token always
equal to the highest committed fact token; maintenance exclusive holder
took the lock 7-8 times per run; no deadlock inside 30 s), the three
`ingestion_tx_lock_split_integration_test.go` proofs (10 of 10 passed in one run, counting the lock-order proof below).

An earlier T3 pass was a false green: a stale re-pin cancels the commit
context, database/sql discards that connection, and the replacement lost
the session `SET search_path`, so the verification counted rows in
`public` of a shared database. On a clean database it failed 3 of 3. The
helper now sets `search_path` as a connection startup parameter, and the
fence proofs refuse any verification connection whose `current_schema()`
is not the proof schema (seeded: the old helper against a polluted
`public` fails with that message).

Lock order: `TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks`
with odd
slots on the plane path (60 fence rows asserted). Parity:
`TestParityStaleWriterCommitFence` on both legs against `claim-plane`
built from `eshu-workflow` `v0.1.1` (`8311ed6`). Migration 270:
bootstrap at the pre-270 tree, apply 270, re-apply; schema dumps identical
apart from pg_dump's per-run `\restrict` nonce. The later edit to 270
adds header comments only (the lock note); the statement is unchanged,
and every live run above applied it to a fresh schema. Terminal counts: the
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
