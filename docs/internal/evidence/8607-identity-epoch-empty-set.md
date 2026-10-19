# Identity epoch probe on a globally-empty set (#8607)

## Problem

`probeIdentityEpochQuery`
(`go/internal/storage/postgres/facts_active_container_image_identity.go`)
returned `COALESCE(max(observed_at), '-infinity')` and
`probeIdentityEpoch` scanned that column into a `time.Time`. On a
globally-empty image-identity set the aggregate input is empty, so the column
held `-infinity`, which pgx cannot scan into `*time.Time`
(`pgtype/builtin_wrappers.go`: "cannot scan -Infinity into *time.Time";
go.mod pins pgx v5.9.2). Every probe failed with `probe identity epoch scan`,
so the loader could never serve the empty set: the pre-load probe error
returns directly from `IdentityEpochCache.get`, and a post-load probe error
lands a `probe_error` verdict in `judgeLoad`, which discards the set and fails
the leader's item with the retryable `identity_epoch_unstable` class — a
permanent fail/retry loop on identity-empty fleets. No committed corpus
triggers it (all fixtures seed identity facts), so the defect was latent.

## What changed

The probe selects the bare nullable `max(fact.observed_at)` and scans it into
a `sql.NullTime`, mapping NULL to the zero `time.Time`. The zero time is
stable across probes, so the empty-set epoch (count 0, zero max, SHA-256 of
the empty string) compares equal and stays cacheable. Non-empty sets are
unaffected: a present max still scans to its timestamp.

Design alternative rejected: keep the COALESCE with a finite sentinel
(e.g. epoch). Any sentinel collides with real `observed_at` values and changes
epoch equality semantics; NULL-to-zero is exact and keeps the column
nullable-honest.

## Acceptance

- `TestProbeIdentityEpochEmptySet` (red-first: failed on the old `*time.Time`
  scan with `probe identity epoch scan`, passes after): count 0, zero max,
  empty-digest fingerprint pass-through, and epoch equality across two probes.
- All existing epoch/cache/flight/concurrency/unit proofs run unmodified
  against the nullable contract (their staged probe rows now carry
  `sql.NullTime{Valid: true}`); affected filter: 84 PASS, 0 FAIL.
- Filter-drift and fingerprint-shape contracts unchanged
  (`TestIdentityEpochProbeFilterDrift`,
  `identity_epoch_cache_contract_test.go`): the filter text, the ordered
  DISTINCT pair digest, and the single-`FROM ingestion_scopes` shape are
  untouched.

## Measurement

No wall-time claim is made: the removed COALESCE applied once to the
aggregate output row, not per input row, and the filter, scanned row set, and
covering partial index (`fact_records_identity_epoch_idx_v2`) are identical,
so the probe plan shape is unchanged by construction. There was no live
backend in the leaf environment (live tests SKIP without a DSN); the plan-shape
live tests (`TestIdentityPageQueryPlanRidesOrderedIndexLive`) are untouched
and owned by the postgres_ci lane.

## No-Regression Evidence:

Baseline: probe text with `COALESCE(max(fact.observed_at),
'-infinity'::timestamptz)` scanned into `time.Time`. After: bare
`max(fact.observed_at)` scanned into `sql.NullTime`, NULL mapping to the zero
time. Backend/version: no live backend locally (unit/fake proof only);
PostgreSQL plan ownership stays with the postgres_ci lane. Input shape:
globally-empty identity set (count 0, NULL max, fingerprint `e3b0c44...b855`)
plus unchanged non-empty probes. Terminal counts: `go build ./...` exit 0;
affected unit filter 84 PASS, 0 FAIL; `go vet` clean; `git diff --check`
clean. Why safe: the diff removes a scalar default on the aggregate output
only — same filter, same index, same rows — and the formerly-crashing input
(empty set) is now pinned by a regression test.

## No-Observability-Change:

No-observability-change. No new metrics, spans, or logs: the probe flows
through the existing signals (`IdentityCacheProbeDuration`,
`IdentityCachePassthroughTotal` with the `probe_error` reason,
`IdentityCacheLoadRetryTotal`). On identity-empty fleets the `probe_error`
passthrough and `identity_epoch_unstable` retries should drop to zero —
observable on the existing counters with no new signal required.

## Live-backend supplement (PR #8620)

The unit/fake proof above stands on its own; this branch additionally
measured the probe on a disposable backend and pins the empty-set
contract with a live regression test
(`go/internal/storage/postgres/identity_epoch_empty_set_live_test.go`,
`TestProbeIdentityEpochEmptySetLive`, enrolled in the
live-postgres-readiness inventory). Terminal live proof: PASS with
count 0 and digest `e3b0c44...b855` on disposable PostgreSQL 18
(`pg8607-epoch`); seeded comparison count 1,000 both spellings.

`EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)` of the old spelling (derived from
the shipped constant) vs the new spelling, same disposable PostgreSQL 18
container, same 1,000-row seeded set (20 scopes, one active generation each
with 50 identity facts): identical plan shape — one Aggregate over one
`Index Scan using fact_records_identity_epoch_idx_v2` (1,054 shared hits)
with the same hashed active-generation SubPlan (44 + 42 shared hits).
Execution 1.458 ms old vs 1.504 ms new; wall average over 5 runs 2.23 ms
old vs 2.27 ms new. Same count (1,000) and fingerprint both ways. The
difference is a projection-only simplification, so no plan or cardinality
change exists to regress.
