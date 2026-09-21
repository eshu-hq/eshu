# Code-Divergence Signals

Two stages feed the divergence report, and each has a 3 AM signal. Metric
row detail lives in the [telemetry coverage contract](../../observability/telemetry-coverage.md);
per-metric bucket and cardinality notes live in
[Ingestion And Collector Metrics](metrics-ingestion-collectors.md) and
[Reducer And Storage Metrics](metrics-reducer-storage.md).

## Fingerprint stage

`go/internal/parser/fingerprint` emits one counter and one histogram out of
the snapshot path (`snapshot_prescan_stats.go`):

- `eshu_dp_code_fingerprint_entities_total` — function entities fingerprinted
  or skipped, labeled by bounded `language` and `outcome` (`fingerprinted`,
  `skipped`); skipped rows carry bounded `reason` (`below_floor`,
  `has_error`, `no_body`). Zero counts emit nothing, so an absent series
  means zero, and absent fingerprint keys on an entity mean "not
  fingerprinted", never "unique".
- `eshu_dp_code_fingerprint_duration_seconds` — per-file fingerprinting time
  histogram, labeled by bounded `language`, observed only for files that
  produced any fingerprint outcome.

At 3 AM: a `has_error` spike on one language is a parser regression on the
ingest hot path — fingerprint cost was the #6834 prove-first gate, so treat
a p99 duration jump the same way. A `below_floor` jump is a corpus shape
change (many tiny functions), not a bug. A repo whose entities carry no
fingerprint keys after a full ingest means the stage never ran for it, not
that it has no clones.

## Drifted-pair reducer domain

`go/internal/reducer/codedivergence` verifies LSH-nominated candidates by
exact Jaccard and publishes admitted pairs as durable
`reducer_code_drifted_finding` facts, idempotent by finding identity so
retries never duplicate a row. It reports through the shared correlation
instruments with `pack="code_drifted"`:

- `eshu_dp_correlation_rule_matches_total` — match-phase activity:
  `admit_drifted` for verified pairs, `similarity_below_threshold` for
  nominated pairs that failed verification, `candidate_budget_exhausted`
  for pairs the budget left unevaluated.
- `eshu_dp_correlation_drift_detected_total` (`rule="admit_drifted"`,
  `drift_kind="drifted"`) — admitted pairs actually published.

At 3 AM: zero admits with nonzero nominations localizes by comparing the two
counters — matches without detects means verification is rejecting (check
the threshold the finding rows carry); neither means the candidate pipeline
stalled (check the loader filters the read surface reports: `no_shingles`
means pre-shingle payloads still in the store, `equality_duplicate` means
the exact surface already claimed the pair). Climbing
`candidate_budget_exhausted` means pairs are waiting on budget, not failing.
Drain, lease, retry, and dead-letter behavior ride the shared reducer
signals (see [Telemetry Overview](index.md)); the domain's own proof is
contention, retry, idempotency, and dead-letter evidence under
`docs/internal/evidence/`.
