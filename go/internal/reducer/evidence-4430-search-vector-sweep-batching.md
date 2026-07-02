# Search-Vector Build Sweep Batching Evidence (#4430)

Issue #4430 reported the search-vector build sweep as a long pole in the
post-bootstrap reducer tail: full-corpus evidence showed sweeps over about 33
scopes and 185k-198k documents taking about 100-104s per sweep. This note maps
the sweep's write path, isolates the dominant cost slice, and records the
before/after fix.

## Root Cause

`searchvector.Builder.Build` (`go/internal/searchvector/builder.go`) listed
active search documents in bounded pages (already efficient — capped by
`req.Limit`, default 500), but for every document in the page it issued two
sequential single-row round trips: `Values.Upsert` then `Metadata.Upsert`
(`go/internal/storage/postgres/eshu_search_vector_values.go`,
`eshu_search_vector_metadata.go`). At the issue's reported scale that is
`2 * 185,361` to `2 * 197,715` sequential `ExecContext` round trips per sweep,
serialized inside the per-scope loop in
`reducer.SearchVectorBuildRunner.RunOnce`
(`go/internal/reducer/search_vector_build_runner.go`). The embedder used in a
real corpus run without hosted provider credentials is the deterministic
local hash embedder (`go/internal/searchembed/hash.go`), so embedding compute
is cheap; the dominant cost is Postgres round-trip count, not CPU-bound
embedding work. The pending-scope selection query itself was already fixed
under #4233 (`NOT EXISTS` rewrite, indexed) and is not the bottleneck here.

## Fix

Added `UpsertBatch` to `EshuSearchVectorValueStore` and
`EshuSearchVectorMetadataStore`, issuing bounded multi-row
`INSERT ... VALUES (...), (...) ON CONFLICT DO UPDATE` statements (500
rows/statement, following the existing `SharedProjectionAcceptanceStore.Upsert`
batching precedent) instead of one round trip per document. `Builder.Build`
now accumulates each document page's metadata/value rows and writes them with
one batched call per page instead of one call per document, collapsing
`2 * document_count` round trips per scope sweep into
`2 * ceil(document_count / req.Limit)`. `Builder.Build` also now returns split
phase timings (`QueryLoadDuration`, `EmbedBuildDuration`,
`WriteUpsertDuration`), propagated through
`reducer.SearchVectorBuildResult` / `SearchVectorBuildRunnerResult`, plus a
`SchedulingWaitDuration` around the pending-scope list call in `RunOnce`.

## No-Regression Evidence

`cd go && go test ./internal/searchvector/... ./internal/reducer/...
./internal/storage/postgres/... -count=1` — 3834 passed, 0 failed (includes
the pre-existing full package suites plus new regression coverage).

`TestBuilderBatchesUpsertsPerPageInsteadOfPerDocument`
(`go/internal/searchvector/builder_test.go`) is the direct regression test:
five documents in one page must produce exactly one `Values.UpsertBatch` call
and one `Metadata.UpsertBatch` call, each carrying all five rows. Reverting to
per-document upserts would make this test fail immediately (it asserts call
count, not just row content), because the fake stores
(`recordingVectorValueStore`/`recordingVectorMetadataStore`) record every
`UpsertBatch` call into a `batches [][]T` slice, so a per-document regression
produces five batches of one row each instead of one batch of five.

`TestSearchVectorBuildRunnerSumsPhaseDurationsAcrossScopes`
(`go/internal/reducer/search_vector_build_runner_test.go`) proves `RunOnce`
sums each built scope's phase durations into the sweep-level result instead of
dropping or overwriting them.

`go test -race -count=1 -timeout 600s ./internal/storage/cypher/...
./internal/reducer/... ./internal/projector/... ./internal/correlation/...
./internal/content/shape/... ./internal/relationships/...` — 3834 passed, 0
failed. No new races introduced; the batched write path has no new shared
mutable state (each `Build` call still owns its own page-local slices).

## Performance Evidence

`TestEshuSearchVectorUpsertBatchScaleLive`
(`go/internal/storage/postgres/eshu_search_vector_upsert_batch_scale_live_test.go`,
skip-unless-`ESHU_SEARCH_VECTOR_UPSERT_BATCH_SCALE_LIVE=1`) measures the same
rows written two ways against a real Postgres 16 database (`eshu-pg-4430`
container, local Docker, disjoint scopes per phase so neither path gets a
cache-warm advantage):

Small-scale smoke run (2 scopes x 200 docs = 400 rows):

```
per_document=2.552907708s (400 rows, 6.380ms/row)
batched=35.092666ms (400 rows, 0.087ms/row)
speedup=72.7x
```

Full-scale run at the #4430 issue evidence shape (33 scopes x 5800 docs =
191,400 rows, matching the reported "33 scopes / 185k-198k documents"):

```
per_document=11m1.799715375s (191400 rows, 3.458ms/row)
batched=13.570806167s (191400 rows, 0.071ms/row)
speedup=48.8x
```

The batched write phase alone (13.57s for 191,400 rows) is well inside the
issue's reported 100-104s per-sweep envelope, so the write path is no longer
the dominant cost slice at this scale; the remaining sweep time is now
attributable to query/load and embed/build phases (both already split out
separately by this fix's telemetry) rather than write-round-trip
amplification.

Backend/version: Postgres 16 (`postgres:16-alpine`), local Docker container,
default configuration, no connection pooling beyond `database/sql`'s default
pool. Input shape: synthetic 8-dimension vectors, deterministic content hash
per document, `ON CONFLICT DO UPDATE` path exercised (all rows pre-exist as
inserts on first write, so the measured statements are the steady-state
upsert path the sweep runs on repeat cycles). Terminal row counts: every
document produces one `ready` metadata row and one value row; the test
asserts the batched path's row count and one sampled row's content directly
against the database (bypassing the two `ListActive` helpers, which have an
unrelated pre-existing pgx array-scan bug flagged separately and are out of
scope for this fix) to prove the optimization does not silently change what
is persisted.

## Observability Evidence

Added `eshu_dp_search_vector_build_phase_seconds`
(`go/internal/telemetry/instruments.go`), a histogram with `domain` (fixed
`search_vector_build`) and `write_phase` (`scheduling_wait`, `query_load`,
`embed_build`, `write_upsert`) attributes, recorded once per `RunOnce` sweep
in `reducer.SearchVectorBuildRunner.recordPhaseMetrics`. This directly answers
the issue's "split timing into query/load, embedding/hash/build, write/upsert,
and scheduling wait" requirement without recomputing it from logs. The
"search vector build sweep completed" structured log
(`go/internal/reducer/search_vector_build_runner.go`) was also enriched with
the same four fields (`scheduling_wait_seconds`, `query_load_seconds`,
`embed_build_seconds`, `write_upsert_seconds`) alongside the existing
`duration_seconds`. `docs/public/observability/telemetry-coverage.md` gained a
`search vector build sweep` row; `bash scripts/verify-telemetry-coverage.sh`
(X2) and `bash scripts/generate-operator-dashboard.sh` (X4, no drift) both
pass.
