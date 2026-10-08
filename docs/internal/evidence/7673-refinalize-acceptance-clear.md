# 7673: refinalize clears shared projection acceptance — performance note

The #7673 fix adds one DELETE to the refinalize transaction
(`clearSharedProjectionAcceptanceTemplate` in
`go/internal/storage/postgres/rebuild/reset/reset.go`): it removes the
`shared_projection_acceptance` rows pointing at the refinalized
`(scope_id, generation_id)` pairs so the repo_dependency lane cannot project
edges for a generation whose supporting state was wiped. The reset steps run
sequentially in one recovery-only transaction, so the whole change's added
cost is this statement's cost, measured below.

## No-Regression Evidence:

Baseline: the 4-statement refinalize at origin/main b1f8cdc52 (no acceptance
cost; acceptance rows leak and the lane keeps stale authority).
After: the same refinalize plus the acceptance DELETE. Measured with
`EXPLAIN (ANALYZE, BUFFERS)` on seeded rows (throwaway probe, since
removed): single-pair DELETE over 200 of 1000 rows executes in 0.097 ms
(planning 0.096 ms) via an index scan on
`shared_projection_acceptance_scope_idx(scope_id, generation_id)`, 205
shared buffers hit, 0 read; five-pair DELETE over 800 remaining rows
executes in 0.323 ms (planning 0.046 ms) via a BitmapOr over the same
index, 823 shared buffers hit, 0 read. Terminal counts: 200 then 800 rows
deleted, 0 left; probe rows cleaned up afterwards.
Backend/version: PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2) in Docker,
isolated scratch schema with the repo migrations applied.
Input shape: 5 scopes x 200 acceptance rows (1000 total), mixed
code-import and resolver source runs — the same shape as the regression
fixtures, scaled down (production counts are symptoms, not reproduced).
Why safe: the predicate matches the existing `(scope_id, generation_id)`
index exactly (no seq scan at either arity); the added cost is sub-ms
against a refinalize dominated by the drain fence, projector re-enqueue,
and the other four resets; and refinalize is operator-triggered recovery
work, not a hot loop — it runs per rebuild, not per batch.

## Observability Evidence:

The cleared count is operator-visible as `shared_projection_acceptance_cleared`
in all three refinalize responses (admin `recover-generations`, admin
`refinalize`, runtime `/admin/refinalize`), alongside the other four reset
counts, and is covered by the OpenAPI contract test. A rebuild that clears
zero rows on a populated stack is as visible as a zero anywhere else in
that row. No new metric or log line: the pre-existing convention keeps the
reset counts in the response body (only `enqueued` and `generations_retired`
also ride the `recover-generations completed` / `refinalize completed` log
lines), and this change follows it.
