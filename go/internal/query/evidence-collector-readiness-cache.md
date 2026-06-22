# Evidence: collector-readiness TTL cache — issue #3466

## Endpoint

`GET /api/v0/status/collector-readiness`

## Root cause

`collectorFactEvidenceQuery` runs a per-scope `COUNT(*) + MAX(observed_at) +
GROUP BY evidence_source, source_system` lateral aggregation over
`fact_records WHERE active = true`. At 982 scopes × ~6 740 facts/scope =
6.6 M rows scanned per request. The status page polls this endpoint on every
render, so every page load pays the full scan.

14 SQL variants investigated (DISTINCT ON inside LATERAL, MATERIALIZED CTEs,
composite indexes on `(scope_id, generation_id, source_system, observed_at DESC)`)
— Postgres correlated LATERAL subqueries do not support index-native skip-scan;
the structural O(total-active-facts) floor cannot be removed without changing
query semantics.

## Fix

Short-TTL (30 s) in-memory response cache on `StatusHandler.getCollectorReadiness`
(`collectorReadinessTTL` const, `collectorReadinessCache` struct).

The cache maintains **two separate slots** keyed by auth scope (unscoped/admin
vs scoped/tenant). Scoped callers receive family-level readiness with instance
IDs redacted; unscoped callers receive full per-instance detail. A single shared
slot would allow an admin-warmed entry to be served to a scoped caller within
the same TTL window, leaking per-instance identity across auth boundaries. The
two-slot design prevents this.

- Cache is `sync.Mutex`-protected; zero value is a valid empty/both-expired cache.
- On cache miss: full query runs, result stored in the caller's scope slot.
- On cache hit (same scope, within TTL): stored payload returned immediately.
- Data semantics unchanged: the query still computes accurate `MAX(observed_at)`
  and evidence staleness on each miss. `ObservationCount` is display-only (not
  used by `derivePromotionState`), so caching does not affect any promotion verdict.

## Performance Evidence

Performance Evidence: Postgres, live ~900-repo / 982-scope stack.
Input shape: `fact_records WHERE active = true`, 6.6 M rows at time of measurement.

| Case | Before | After |
|---|---|---|
| Cache hit (common: status page polling within TTL) | 9.2 s | < 50 ms |
| Cache miss (first request per 30 s window) | 9.2 s | ~9 s (unchanged) |
| DB scan load at 1 req/s sustained | ~60 full scans/min | ~2 full scans/min |

Baseline: 9.2 s p50 at ~900-repo / 982-scope scale, regressed from ~50–70 ms
after the #3375 LATERAL fix landed on a smaller dataset.

The underlying query cost will also drop once the #3451 reducer/projection
backlog drains the active-fact count. The cache ensures the SLA is met
regardless of that backlog state.

## Observability

No-Observability-Change: The handler's span, truth envelope (`TruthLevelExact`, `TruthBasisRuntimeState`,
`FreshnessFresh`), and response shape are identical on cache hits and misses.
The `generated_at` field reflects the time of the originating cache-miss, which
is accurate to within the TTL window — consistent with how the status page
already displays a point-in-time snapshot. No new metrics, spans, or log lines
are added; the cache is intentionally transparent to operators.

## Focused proof

```
go test ./internal/query -run "TestCollectorReadinessCache" -count=1 -v
```

Three tests pin the cache semantics:
- `TestCollectorReadinessCacheHitReturnsCached` — warm hit does not reach StatusReader.
- `TestCollectorReadinessCacheExpiryRefetches` — expired entry triggers fresh read.
- `TestCollectorReadinessCacheDoesNotCrossScopes` — admin-warmed unscoped slot must
  not serve a scoped request (security regression guard).
