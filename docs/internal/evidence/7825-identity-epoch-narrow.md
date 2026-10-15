# Narrow the identity epoch fingerprint to identity-relevant activations (#7825)

## Problem

The identity-fact cache (#7805) treated any move of its epoch as a torn load.
The epoch fingerprint covered every scope in `ingestion_scopes`, so a
generation activation in a scope with no identity facts, during an identity
load, still moved the epoch and could tear that load. After two torn attempts
the flight discarded its set, waiters re-probed, and the leader's item failed
with the retryable class `identity_epoch_unstable`; with
`ESHU_REDUCER_MAX_ATTEMPTS=3`, three consecutive torn attempts dead-lettered
the item.

## What changed

`probeIdentityEpochQuery`
(`go/internal/storage/postgres/facts_active_container_image_identity.go`) no
longer digests the `ingestion_scopes` mapping. Count, max(`observed_at`) and
fingerprint come from one single scan of the same filtered identity set, and
the fingerprint is the SHA-256 digest of the DISTINCT active
`(scope_id, generation_id)` pairs in that set, ordered and joined with `|`.
An activation in a scope with no identity facts leaves the pair set
unchanged, so it no longer moves the epoch or tears an in-flight load. The
torn test (`judgeLoad` in `go/internal/storage/postgres/identity_epoch_cache.go`)
compares whole epochs, so narrowing the fingerprint narrows the torn test
with no second change. An empty identity set digests the empty string, so the
fingerprint stays 64 hex characters (the FIPS length pin still holds).

Design alternative rejected: restrict the `ingestion_scopes` aggregate with an
`EXISTS` probe per scope into `fact_records`. That keeps a second table scan
(about 1,400 index probes) and duplicates the identity filter. The shipped
shape aggregates pairs the count/max scan already fetches, so it removes a
scan instead of adding one.

## Acceptance

- A flip of `active_generation_id` in a scope whose generations hold identity
  facts moves the epoch even when count and max(`observed_at`) are unchanged
  (`TestIdentityEpochProbeDetectsSupersessionLive`, now seeding identity facts
  in both generations at the same `observed_at`).
- A flip in a scope with no identity facts leaves the whole epoch unchanged,
  and a later identity insert into the newly active generation moves it again
  (`TestIdentityEpochProbeIgnoresIdentityEmptySupersessionLive`).
- Superseded-generation deletes still leave the epoch unchanged and
  active-generation inserts/deletes still move it
  (`TestIdentityEpochIgnoresSupersededGenerationRowsLive`, unchanged).
- The shared-flight, torn-set and waiter-bound proofs from #7805 run
  unmodified against the narrower definition (same epoch comparison, narrower
  input).

## Measurement

Plan-shape `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)` of old vs new
`probeIdentityEpochQuery` on scratch PostgreSQL 18-alpine (near-empty fixture
DB — plan shapes are structural; toy-DB timings are not comparable and no
production timing is claimed):

- Old: `Nested Loop` over two Aggregates — the fact_records count/max scan
  plus a second `Seq Scan on ingestion_scopes` with Sort for the fingerprint.
- New: single Aggregate over one fact_records scan; the only
  ingestion_scopes read is the hashed SubPlan active-generation filter,
  riding the same `scope_generations_active_scope_idx` and
  `ingestion_scopes_active_generation_idx` index searches as the old plan.
- Predicate coverage is unchanged:
  `TestIdentityEpochIndexPredicateMatchesIdentityFactFilter` passes, so the
  partial index `fact_records_identity_epoch_idx_v2` still covers the probe
  (no full-scan fallback). The #7805 identity-page query and its 546k-row
  4.7s timing are untouched — the page query is not in this diff.

Conclusion: the narrowing removes a scan instead of adding one; no
performance regression expected.

## No-Regression Evidence:

Baseline: old probe plan-shape with the second `ingestion_scopes` scan (see
Measurement above). After: single-scan plan-shape, same index searches,
predicate coverage pinned by test. Backend: scratch PostgreSQL 18-alpine
(container `eshu-7825-pg`). Input shape: near-empty fixture DB; structural
plan comparison only, no wall-time claim. Queue/row counts: terminal live
proofs green (`TestIdentityEpochProbeIgnoresIdentityEmptySupersessionLive`
PASS: empty flip ignored with count=3; `DetectsSupersessionLive` PASS with
count=2 unchanged and fingerprint flipped).

## No-Observability-Change:

No-observability-change. No new metrics, spans, or logs: the narrowing flows
through the existing #7805 signals (`IdentityCacheReloadTotal`,
`IdentityCacheReloadDuration`, `IdentityCacheLoadRetryTotal`,
`IdentityCacheProbeDuration`, `IdentityCachePassthroughTotal`, and the
`torn_set` waiter outcome). Fewer identity-empty activations move the epoch,
so torn-flight retries should decrease — observable on the existing retry
counter with no new signal required.
