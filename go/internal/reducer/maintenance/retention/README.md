# Generation Retention Runner

## Purpose

Prunes superseded source-generation history (including the changed-since
ledger tables, #7127) in bounded transactions on its own poll interval,
so durable snapshot history stays available without reprocessing
obsolete generations. Moved here from the flat `maintenance` package
under #7648.

## Ownership boundary

Owns the drain loop, the skipped-pass backoff, and the per-cycle
accounting. Does not own candidate selection, locking, or the prune
transaction (the storage `Pruner` implementation), or the `Service`
side-runner startup loop in the reducer root.

## Exported surface

- `Runner`, `Config` — the drain loop and its bounds
- `Policy` — retained count/age bounds and batch limits
- `Result` — pruned counts, skips, ledger share, lock hold
- `Pruner` — storage port (`PruneSupersededGenerations`)
- `ErrKeyIndexUnavailable` — pruner refusal marker

See `doc.go` for the full contract.

## Dependencies

- `internal/telemetry` — counters, histograms, log attributes
- `pkg/log` — structured error logging

Never `internal/reducer`.

## Telemetry

- Metrics: `eshu_dp_generation_retention_generations_pruned_total`,
  `eshu_dp_generation_retention_rows_pruned_total{table}`,
  `eshu_dp_generation_retention_failures_total{reason}`,
  `eshu_dp_generation_retention_skipped_total{reason}`,
  `eshu_dp_generation_retention_duration_seconds`,
  `eshu_dp_generation_retention_batch_size`,
  `eshu_dp_generation_retention_oldest_eligible_age_seconds`,
  `eshu_dp_generation_retention_phase_duration_seconds{phase}`,
  `eshu_dp_generation_retention_scope_lock_hold_seconds`,
  `eshu_dp_generation_retention_over_limit_batches_total`
- Spans: none; the drain runs inline in the side-runner goroutine
- Logs: `generation retention cycle completed` (pruned/skipped totals,
  ledger share, lock hold); WARN when the ledger delete differs from
  its pre-count

## Gotchas / invariants

- A skipped-only pass retries soon (1m doubling per consecutive
  skipped-only pass, capped at the poll interval) instead of sleeping
  the full interval (#7398). Lock-held candidates stay invisible by
  design (the candidate SELECT uses SKIP LOCKED), so a lock-starved
  pass keeps the full sleep.

  No-Regression Evidence (#7398): baseline, a pass that found candidates but
  pruned none slept the full poll interval (default 1h), so a skipped-only
  streak ran 2 passes in its first 63 minutes (t=0, t=60m). After, the waits are
  1, 2, 4, 8, 16, 32 minutes, then the poll interval, so the same streak runs 7
  passes (t=0, 1, 3, 7, 15, 31, 63m) and then returns to one pass per poll
  interval. The extra cost is bounded at 6 passes per streak. Each pass is the
  existing candidate selection (SKIP LOCKED) and prune path: no new query, no
  Cypher, no new concurrency, still one goroutine, context cancellation
  unchanged. Measured with the unit harness (fake pruner, Go 1.27.1,
  darwin/arm64): `TestGenerationRetentionRunnerSkippedRetryBacksOffToPollInterval`
  asserts the 8-wait sequence above with a 1h poll interval, and
  `TestGenerationRetentionRunnerSkippedBackoffResetsAfterPrune` asserts the
  streak resets after a pruning pass. Not measured against live Postgres: the
  per-pass query cost is unchanged, only how often a blocked backlog is
  retried.

  Observability Evidence (#7398): no new signal is needed. Every pass already
  emits `eshu_dp_generation_retention_skipped_total{reason}` and the
  `generation retention cycle completed` log line with `skipped_total`,
  `skipped_by_reason` and `rows_over_batch_row_limit`. An operator sees the new
  cadence as that log line's interval shrinking from the poll interval to 1, 2,
  4 … minutes while skips persist, and returning to the poll interval once the
  streak is capped or a pass prunes.
- `ScopeLockHold` is the window a concurrent fact insert into a
  pruned scope waits out (#7279); keep the transaction bounded.
- The unexported `contextDone` helper is duplicated in the `infra`
  leaf; the two shared one copy before the #7648 split.

## Related docs

- `docs/public/reference/reducer-guarantees.md`
- `docs/public/observability/telemetry-coverage.md`
