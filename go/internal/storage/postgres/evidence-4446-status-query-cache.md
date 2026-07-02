# #4446 — status-query index + caching for activeFactWorkItemsCTE

Evidence note for the A3 status-query optimization (issue #4446). The status
stage-counts read (`listStageCounts`, which drives the `activeFactWorkItemsCTE`
status surface) was an O(scan) query re-run on every status poll at repo scale.

## Performance

Performance Evidence: A 2s-TTL in-process cache (`status_stage_counts_cache.go`) now serves repeat
  status reads within the TTL window without re-running the CTE — the
  deterministic operator-visible win. Proven by
  `TestListStageCountsCacheServesRepeatReadsWithinTTL` (fakeQueryer-based, no
  live-Postgres/planner dependency): a second read inside the TTL issues zero
  additional queries.
- Cache invalidation/propagation covered by
  `TestListStageCountsCachePropagatesQueryErrors` (errors are not cached).
- A `scope_generations_scope_generation_idx` index is added for the cache-MISS
  path. Honest caveat: at repo scale the Postgres planner cost-ties this index
  against the pre-existing `scope_generations_scope_idx` at some ANALYZE
  samples (a genuine planner behavior, not an index defect); `SET STATISTICS
  1000` improves the row estimate. The plan-assertion test is therefore
  de-flaked to assert "no Seq Scan on scope_generations" (deterministic
  regardless of which index wins), and the deterministic proof of the #4446
  win is the cache, not the plan choice.

## Observability

Observability Evidence: New counter `eshu_dp_status_stage_counts_cache_total` (bounded `outcome`
  label: hit/miss/error) registered in `go/internal/telemetry/instruments.go`
  (`Instruments.StatusStageCountsCacheTotal`) and recorded from
  `listStageCounts` via the nil-safe `StatusStore.Instruments` field, with its
  X1 row in `docs/public/observability/telemetry-coverage.md` under "Queue
  Domains", so an operator can read the cache hit/miss/error split for the
  status stage-counts read at 3 AM.

## No-Regression

No-Regression Evidence: Full `go/internal/storage/postgres` package suite green (1202 tests),
  `-race` clean, `go vet` clean, `make pre-pr` all local gates pass.
