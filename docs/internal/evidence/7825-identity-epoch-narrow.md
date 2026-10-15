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

MEASUREMENT PENDING: throwaway EXPLAIN ANALYZE shim, old vs new probe.
