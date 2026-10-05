# #7585 Hard-Ceiling No-Regression Evidence

## Scope

Configurable `HardMaxSupersededAge` (default 90 days) capping the retained
count in the generation-retention candidate query and targeted re-lock.
No worker, lease, batch-size, lock-ordering, or retry change: the selection
still takes the same scope/generation row locks (`FOR UPDATE ... SKIP
LOCKED`), the same savepoint-narrowed over-limit path, and the same
row-limit/ledger counting.

## No-Regression Evidence: retention hard ceiling

- Baseline: `origin/main` `fdd3f71c7` candidate shape (soft age AND rank).
  The existing neighbor suites (lock-set, selects-eligible, evalplanqual,
  candidate-plan EXPLAIN, narrow, fairness, probe-seed) assert that shape and
  stay green on the new tree, proving behavior for all history younger than
  the ceiling is unchanged.
- After: `TestGenerationRetentionHardCeilingLive` PASS on local Docker
  Postgres `postgres:18-alpine` (compose project `eshu-7585-ceiling`, pgx
  stdlib, Go 1.27.1) in 2.14s wall including the full 178-migration schema
  setup: pass 1 with an explicit 90d ceiling pruned exactly 1 generation
  (100-day-old, rank 1 of 24) and retained the 10-day-old rank-1 generation;
  pass 2 with an unset (zero-value, normalized to 90d) ceiling pruned
  exactly 1 generation (fresh 100-day-old rank 1). Neighbor live batches
  green on the same backend: lock-set + selects-eligible + evalplanqual in
  6.479s; candidate-plan + narrow + fairness + probe-seed in 8.608s.
  Changed-package `go test` and `-race` green (pre-push floor).
- Input shape and row counts: 3 single-generation scopes per pass; pruned
  generations carried zero fact rows, so the row-limit skip path was not
  exercised here and is proven unchanged by
  `TestGenerationRetentionStoreCandidateQueryRunsOncePerPass` (still exactly
  one candidate query, still no exclusion parameter) and the narrow live
  tests.
- Why safe: the OR-branch reuses the same indexed `superseded_at` range
  predicate and keeps every non-age guard (status, active-generation id,
  NOT NULL, live-work anti-join) outside the branch, so the ceiling bypasses
  only the count preference — exactly the #7585 contract. The ops-qa census
  in the issue found no superseded history older than 90 days, so the
  eligible set on current data is identical with the default ceiling;
  eligibility only widens as data ages past it, through the existing bounded
  batch/lock/row-limit machinery.

## No-Observability-Change: existing retention signals cover the ceiling path

No new metrics, spans, logs, or status fields: the existing retention
signals already cover the ceiling path —
`eshu_dp_generation_retention_generations_pruned_total`,
`_rows_pruned_total`, `_failures_total`, `_skipped_total`,
`_duration_seconds`, `_batch_size`,
`_oldest_eligible_age_seconds` (buckets reach 7776000s = 90d),
`_phase_duration_seconds{phase}`, `_scope_lock_hold_seconds`, plus one
retention event per pruned generation carrying policy scope and revision.
Contradictory settings (hard ceiling below the soft age) fail closed at
reducer startup and in the storage prune, surfacing through the existing
startup-error and retention-failure channels.

## #7611: unset hard ceiling follows a longer soft window

Classification: correctness win. This section makes no latency or throughput
claim.

### No-Regression Evidence: derived hard ceiling default

- Change: an unset (or unparsable, or non-positive) hard ceiling resolves to
  `postgres.DefaultGenerationRetentionHardMaxAge(MaxSupersededAge)`, which is
  `max(2160h, MaxSupersededAge)`. Before, it was a flat 2160h, so a soft
  window above 90 days with the hard ceiling unset failed reducer startup
  and the storage prune. An explicit hard ceiling below the soft window
  still fails closed.
- Baseline: `origin/main` `0b3588b43`. RED on that tree:
  `TestLoadGenerationRetentionConfigDerivesHardCeilingFromLongSoftAge`
  (loader returned `2160h`, want `87600h`),
  `TestGenerationRetentionPolicyNormalizeDerivesHardCeilingFromLongSoftAge`
  (normalized `2160h`, want `87600h`), and
  `TestGenerationRetentionStoreAcceptsUnsetHardCeilingWithLongSoftAge`
  (the prune refused `2160h` below `87600h`).
- After: those tests pass. The default path is unchanged: with no env set
  the ceiling is still `2160h` (`TestLoadGenerationRetentionConfigDefaults`,
  `TestGenerationRetentionPolicyNormalizeHardCeilingDefaults`). The explicit
  contradiction is still rejected
  (`TestLoadGenerationRetentionConfigKeepsExplicitHardCeilingBelowSoftAgeFatal`,
  `TestGenerationRetentionStoreRejectsHardCeilingBelowSoftAge`). Unit proof
  ran on Go 1.27.1, darwin/arm64: `go test ./cmd/reducer/ -count=1` (294
  passed) and `go test ./internal/storage/postgres/ -count=1` (non-live).
  Reverting the derivation in `normalize()`, and separately in the loader,
  turned each layer's tests red.
- Why safe: the candidate and targeted SQL text, lock order, savepoint
  path, batch limits, and row-limit counting are unchanged. Only the bound
  value of the hard-cutoff parameter changes, and only when the ceiling is
  unset and the soft window is longer than 2160h. Then the hard cutoff
  equals the soft cutoff, so history older than the soft window becomes
  eligible regardless of rank. Every other guard (active generation,
  live work, dependency pins) still applies. The derivation is one `max`
  per config load and per prune call, not per row.

### Observability Evidence: hard ceiling lift startup log

When the derived default lifts the ceiling above 2160h, the reducer logs
one INFO record at startup, `generation retention hard ceiling raised to the
soft keep window`, with `hard_env`, `soft_env`,
`effective_hard_max_superseded_age`, `max_superseded_age`, and
`default_hard_max_superseded_age`. It is silent for the defaults, for an
explicit hard ceiling, and for a soft window at or below 2160h
(`TestLogGenerationRetentionHardCeilingLiftOnlyWhenDerivedAboveDefault`).
The existing retention metrics and events above cover the prune path.
