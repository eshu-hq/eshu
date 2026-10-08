# Producer activation obligations (#7635)

When a scope that produces evidence for the correlation domains activates a
new generation, the consumer domains' work items replayed only if an
ingestion commit followed. After a quiet Ack nothing replayed them: the
#7584 activation obligation wakes only `deployment_mapping` rows, on
`backward_evidence_committed`.

The fix owes a second obligation family, `producer_activation_obligations`
(migration 163), inside the Ack transaction for every activated
generation. A resolution-engine consumer (`ProducerActivationRunner`,
default-off behind `ESHU_PRODUCER_ACTIVATION_CONSUMER_ENABLED`) claims
each obligation and settles it: it first probes whether the owed
generation carries evidence a correlation consumer reads (OCI identity
facts, cloud image references, git docker/pod references, or Terraform
state carrying an ARN), retiring the rest as inapplicable; then it reads
the owed generation's linkage keys once, reopens the floored succeeded
consumer items whose keys intersect and that completed before the
obligation was owed, and completes under a token-fenced claim. Ack owes
unconditionally because the producer probe plans in ~8.4 ms per call and
belongs on the background runner, not in Ack (F1 below). There is
deliberately no producer catch-up: a catch-up would re-owe every pruned
generation (no persistent "consumers replayed" marker exists), while the
Ack insert commits atomically with the activation, leaving no crash gap a
catch-up would close; generations activated before this ships replay
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
- `TestProducerActivationRetentionCascadeLive` (generation delete cascades
  to the obligation row; pins the default-off retention story).
- `TestProducerActivationInsertConflictBranchesLive` (re-owe revives an
  `obsolete` row to `pending`; `completed` and `inapplicable` stay terminal
  with `finished_at` kept).
- `TestProducerEvidenceProbeCostLive` (settle-probe plans below).
- `TestProducerEvidenceKindPrefilterDifferential` plus
  `TestProducerEvidenceKindDerivation` (the derived kind prefilter agrees
  with the unprefiltered probe on every corpus generation including one
  carrying every arm kind; the derived list is pinned).

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

## F1: the probe lives in the settle, not in Ack

An earlier shape probed in Ack (`GenerationCarriesProducerEvidence` inside
`oweProducerActivationObligation`) and owed only for producer generations.
Review finding F1 asked for the Ack-path cost evidence, and the measurement
killed that shape: through the Go driver the probe plans in 8.43 ms mean
per call (`pg_stat_statements`, 200 mixed-generation calls,
`track_planning = on`, 0.21 ms mean exec, 74.8 shared hits per call), which
would multiply Ack server time (~1.3 ms) and the scope-row hold on every
activation. The shipped shape owes unconditionally — one insert at 9.17
shared hits and 0.097 ms mean server time per call over 100 calls — and the
background settle absorbs the probe, retiring non-producers as
inapplicable.

The settle probe is generation-anchored
(`TestProducerEvidenceProbeCostLive`, 100k-row bulk corpus, raw plans in
the test log): an Index Scan on `fact_records_scope_generation_idx` at
`(scope_id, generation_id, fact_kind)` costs 290 buffers for a 2000-file
generation, 4 for a 5000-row non-producer generation, and 3 to 4 for
producer, drift, empty and all-arms generations. The kind prefilter is
derived from the identity-filter text (never hand-copied) and is implied
by the predicate: a 5000-row non-producer generation costs 882 buffers
without it and 4 with it, and
`TestProducerEvidenceKindPrefilterDifferential` proves identical outcomes
on every corpus generation. Bulk OCI rows in other generations are never
touched. Wall figures are same-host runs on a shared box and carry its
noise; buffer counts and plan shapes are the deterministic claims.

## F5 limitation: removal-only OCI generations (deferred follow-up)

Removal-only OCI generations owe nothing and removal-affected consumers
can be missed: `producerEvidenceExistsQuery` requires non-tombstoned
identity facts, so a generation that only tombstones manifests creates no
obligation, and partial-removal generations owe on their remaining
evidence while consumers embedding the removed keys lack intersection.
Those consumers stay stale until the next commit-driven epoch pass. (Drift
is not affected: its arms carry no tombstone filter, so tombstoned ARNs
still owe and link.) This sits inside arbiter-blessed R2-A and needs a
design decision (tombstone-aware owe plus previous-generation linkage),
so it is deferred to #7705 rather than fixed here; see that issue for the
acceptance criteria.

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
