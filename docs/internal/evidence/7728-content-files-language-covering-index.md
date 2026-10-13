# 7728 repository-by-language covering index

## Context

Issue eshu-hq/eshu#7728: the repository-by-language listing
(`ReadRepositoriesByLanguage` in
`go/internal/query/content_reader_language_inventory.go`) aggregates
`COUNT(*)` and `MAX(indexed_at)` per `(repository, language)` out of
`content_files`, but the `(language, repo_id)` index did not carry
`indexed_at`. Every matched file row needed a heap fetch: about one heap
block per file row on a production-shaped heap (22k matching files over
20.8k heap blocks, 1.32 s first call against 51 ms warm, 26% all-visible).
Migrations 171-173 rebuild the index as `(language, repo_id) INCLUDE
(indexed_at)` behind a guarded same-name swap, plus vacuum. No query text
changes, so read semantics are identical by construction.

## Performance and observability summary

Performance Evidence (#7728): baseline is the legacy
`content_files_language_repo_idx` on `(language, repo_id)` without the
covering column; after is the covering definition plus `VACUUM`. Backend:
Postgres 18 with `track_io_timing=on`, measured on a production-shaped
scratch copy (27k javascript rows at about 1 live row per heap block,
visibility map fully clear). Input shape is the full
repository-by-language listing over `content_files`. Before: 1.32 s first
call, 51 ms warm; the covering definition alone still reads 27,000 heap
blocks for 27,000 rows (planner stays on a Bitmap Heap Scan at 0%
all-visible). After: Index Only Scan with Heap Fetches 0 over 766 buffers
in 4.7 ms. The rejected rollup-table alternative read a 462-row rollup in
0.7 ms but adds a table, write amplification on the hot content-writer
path, and a staleness bound. Hermetic replay guards
(`go/internal/storage/postgres`, `CC=clang`) prove fresh and upgrade
installs converge and tracked plus untracked replays leave the index
untouched; a live replay proof is enrolled in the live-postgres-readiness
runner. It is safe because the covering index strictly dominates the old
definition for this read (same key prefix, one added `INCLUDE` column),
the writer path is untouched, and the worst case of planner drift is
status-quo speed, not a regression.

No-Observability-Change: the statement text is unchanged and no metric,
span, log, status, or pprof signal is added or renamed. The plan change
(Index Only Scan, Heap Fetches 0) is visible only through `EXPLAIN
(ANALYZE, BUFFERS)`; existing Postgres telemetry keeps its shape.
