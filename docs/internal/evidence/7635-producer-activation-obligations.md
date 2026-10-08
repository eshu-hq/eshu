# Producer activation obligations (#7635)

When a scope that produces evidence for the correlation domains activates a
new generation, the consumer domains' work items replayed only if an
ingestion commit followed. After a quiet Ack nothing replayed them: the
#7584 activation obligation wakes only `deployment_mapping` rows, on
`backward_evidence_committed`.

The fix owes a second obligation family, `producer_activation_obligations`
(migration 163), inside the Ack transaction whenever the activated
generation carries evidence a correlation consumer reads (OCI identity
facts, cloud image references, git docker/pod references, or Terraform
state carrying an ARN). A resolution-engine consumer
(`ProducerActivationRunner`, default-off behind
`ESHU_PRODUCER_ACTIVATION_CONSUMER_ENABLED`) claims each obligation and
settles it: it reads the owed generation's linkage keys once, reopens the
floored succeeded consumer items whose keys intersect and that completed
before the obligation was owed, and completes under a token-fenced claim.
There is deliberately no producer catch-up: a catch-up would re-owe every
pruned generation (no persistent "consumers replayed" marker exists), while
the Ack insert commits atomically with the activation, leaving no crash gap
a catch-up would close; generations activated before this ships replay
through the epoch whole pass exactly as before.

## Design verdicts (arbiter, Muse Spark)

- R1: a separate `producer_activation_obligations` table, not a kind column
  on the #7584 table and not a direct reopen from the Ack path (lease plus
  fence by construction, small Ack transaction, no primary-key migration).
- R2: (A) the dependent set is key-intersecting succeeded items plus an
  `updated_at < obligation.created_at` staleness conjunct; digest linkage is
  content-addressed, tag linkage is `(repo_key, tag)` qualified, cloud
  digests only; (B) the linkage sits beside
  `crossScopeCorrelationReopenDomains`, reusing the reopen helpers;
  (C) drift is owed with ARN overlap plus staleness, so readiness never pays
  twice; (D) deployable-unit needs no separate reopen (the #7584 closure
  covers the symmetric repo endpoints).

## Proof matrix

Live tests (`internal/storage/postgres/activation`,
`internal/storage/postgres`, `internal/reducer/maintenance`, `cmd/reducer`):

- `TestProducerActivationQuietAckLeavesConsumerUnreplayedLive` (RED on clean
  main: consumer stayed `succeeded` after a quiet Ack; GREEN: the settle
  replays exactly the linked consumer).
- `TestProducerActivationSettleIsExactlyOnceLive` (second settle claims
  nothing, writes nothing, obligation stays `completed`).
- `TestProducerActivationLeaseFencesStaleSettleLive` (expired owner's settle
  writes nothing and reports `not_owner`; the reclaiming owner replays and
  completes).
- `TestProducerActivationUnrelatedScopeIsNotReopenedLive` (disjoint keys stay
  `succeeded`; no reopen storm).
- `TestProducerActivationDriftReopenLive` (stale drift item replays; an item
  completed after the obligation stays `succeeded`).
- `TestProducerActivationSettleMatchesEpochPassLive` (output differential:
  quiet+settle replays exactly the stale linked consumer; the epoch whole
  pass replays it plus the disjoint consumer as blanket churn — the settle's
  set is a subset of the epoch pass's, so quiet+settle converges with
  commit-follows+epoch and replays nothing the epoch pass would not).
- `TestProducerListingsDeriveFromShipped` (both listings are the shipped
  correlation listing plus exactly the linkage conjunct, byte-pinned).
- `TestProducerImageRefParseParity` (the SQL reference mirror agrees with
  `ParseContainerImageRef` over 15 references; it caught a real divergence
  where digest-form refs leaked a spurious tag, fixed before merge).
- `TestProducerDependentListingCostLive` (selectivity bound below).
- `TestProducerActivationPruneAndStatsLive` (census plus prune keeps only
  unfinished and inapplicable rows).
- `TestProducerActivationRunnerStoreSettlesThroughThePort` plus
  `TestProducerSettleErrorMapsPortSentinels` (port outcome and error mapping).
- `TestProducerActivationRunner*` (4 runner tests on a fake store: outcome
  and per-domain reopened counts, lease-loss/lock-timeout/error
  classification, cycle bound plus housekeeping, config validation).
- `TestProducerActivationConsumer*` plus
  `TestBuildReducerServiceStartsTheProducerActivationConsumerOnlyWhenEnabled`
  (default-off wiring, Postgres store, process-unique owner, composition).

## Listing cost: one owed generation against 301 floored consumers

`EXPLAIN (ANALYZE, BUFFERS)` on the OCI dependent listing, 300 disjoint
plus 1 linked consumer, Postgres 18, same host as the settle (raw plan in
the `TestProducerDependentListingCostLive` log):

- 301 floored candidates in, exactly 1 row out (the linked item).
- 3,644 shared-buffer hits (~12 per candidate), execution 10.7 ms,
  planning 10.2 ms (wall figures are same-host runs on a shared box and
  carry its noise; the buffer count is the deterministic claim).
- Plan shape: the correlated EXISTS probes each candidate scope's active
  facts through `fact_records_scope_generation_keyset_idx` (301 index
  probes, one per candidate) plus primary-key scope/generation probes. No
  corpus scan: the owed keys are fetched once by
  `producerOwedOCIKeysQuery` and passed as arrays. An earlier single-query
  shape rescanned the owed generation's facts per candidate (23,791
  buffers); hoisting the owed keys cut that 6.5x to 3,644.
- Full settle wall on the same corpus: 68 ms (host-bound, informational);
  second settle reopens nothing.

Performance Evidence: the dependent OCI listing returns exactly 1 row from
301 floored candidates at 3,644 shared hits with per-candidate indexed
probes and no corpus scan; the production settle reopens exactly that row
and a second settle reopens nothing (TestProducerDependentListingCostLive).

Observability Evidence: the consumer emits `eshu_dp_producer_activation_settle_total`
(by outcome), `eshu_dp_producer_activation_reopened_total` (by consumer
domain), `eshu_dp_producer_activations` and
`eshu_dp_producer_activation_oldest_open_age_seconds` (per-cycle census),
`eshu_dp_producer_activation_claim_age_seconds`,
`eshu_dp_producer_activation_pruned_total`, and
`eshu_dp_producer_activation_failures_total` (by reason, with
`settle_lock_timeout` at Warn), the `reducer.producer_activation_settle`
span, and the `producer activation settled` / `producer activation step
failed` logs (see the telemetry-coverage row).
