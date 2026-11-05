# 10020: Generation Outcome Counter Evidence

Refs #10020. The KEV scope held 614 generations for 8 distinct catalog
versions because the collector minted a snapshot on every 30-minute poll until
the freshness-hint skip took effect. The only signal for the skip was a log
line, and nothing counted mints. This change adds the operator signal. It does
not change collector behavior, retention, the freshness hint, or the reader.

## Performance Evidence

Performance Evidence: the change adds one OTEL counter increment per commit
outcome and no SQL, lock, transaction, or queue work. The increment runs after
the ingestion transaction commits (mint) or after the skip decision is final
(skip). One skip path is the exception: the snapshot-less re-projection of a
superseded generation records its skip while its read-only scope-lock
transaction is still open and rolls back on return. The add is in-memory and
does no I/O, so the lock is held for no longer.

- Baseline: no counter. The commit path issues several statements per
  generation (scope upsert, generation upsert, batched fact upserts, projector
  enqueue, commit), each in the millisecond range against Postgres.
- After: `BenchmarkRecordGenerationOutcome` (go test, Apple M5 Max, host load
  average about 41 during the run, so the figures are a loaded upper bound):
  507 to 592 ns/op, 456 B/op, 6 allocs/op over five runs (591.5, 539.1, 507.4,
  529.2, 506.5 ns/op). The 6 allocations are the OTEL attribute set. The
  collector-kind check is a map lookup on a set built once at init, with no
  per-call allocation.
- Input shape: one call per commit outcome, so the added cost is under a
  microsecond per commit (measured above) and independent of the fact count.
- Backend: no Postgres or Neo4j access is added. The unit tests use the
  in-memory fakes of the ingestion store, so there are no terminal queue or row
  counts to report for this change.

No-Regression Evidence: the existing ingestion, successor-guard, backfill, and
telemetry package tests pass unchanged after the change
(`cd go && go test ./internal/telemetry ./internal/storage/postgres/... -count=1`;
the only failure is `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline`,
which needs `ESHU_DEPLOY_CHECKOUT` and is unrelated to this change). The new counter runs after the
transaction closes, so it holds no transaction, row lock, or advisory lock.

## Observability Evidence

Observability Evidence: `eshu_dp_ingestion_generation_outcomes_total` has the
labels `collector_kind` (a value of `scope.AllCollectorKinds` or `other`),
`outcome` (`minted`, `skipped`), and `reason` (`none` for a mint; the closed
`telemetry.RefreshSkipReasons` set for a skip). It carries no scope id: OSV
alone owns more than 1,600 scopes.

- `telemetry.Instruments.RecordGenerationOutcome` folds every label onto its
  closed set, so a hostile or malformed value cannot widen the series count.
  `TestRecordGenerationOutcomeLabelSetIsBounded` feeds 2,000 distinct kinds,
  outcomes, and reasons and asserts the series stay inside the closed bound and
  fold into the `other` bucket.
- `TestCommitMintedGenerationCountsMintedOutcome`,
  `TestCommitUnchangedGenerationCountsSkippedOutcome`, and
  `TestCommitRefusedSuccessorCountsSkippedNotMinted` drive the real ingestion
  commit path. Before the emission was wired, all three failed with an empty
  series set (`generation outcomes = map[], want ...`). After it, a stored
  generation moves `{git, minted, none}`, an unchanged snapshot moves
  `{git, skipped, unchanged}`, and a refused successor moves
  `{git, skipped, destructive_successor_refused}` and does not mint.
- Per-scope evidence stays in logs and spans. The `commit_transaction` stage
  log now carries `collector_kind` and `refresh_outcome=minted`. A mint adds an
  `ingestion.generation_minted` span event next to the existing
  `ingestion.refresh_skipped` event.
- An operator reads the mint-to-skip ratio per collector kind with the queries
  in [Generation Outcome Telemetry](../../public/reference/telemetry/generation-outcomes.md).
  For the 24-hour recount in #10020, `outcome="minted"` for
  `collector_kind="vulnerability_intelligence"` should track upstream catalog
  releases, not the 48-per-day poll cadence.

The counter is per process. Several replicas sum, and a restart resets a
series, the same as the other process-local runtime counters.

## Why it is safe

The recorder is nil-safe (a store built without instruments records nothing),
runs after the commit decision, and cannot fail the commit. A skip is counted
in `backfill.Store.RecordSkippedRefresh`, which every skip path already calls,
so no new skip path can miss the counter. The reconcile re-projection path
supplies no collector kind and lands in the `other` bucket, which the page
documents.
