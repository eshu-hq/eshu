# Evidence #7797 — delta-base union read

Delta-active scopes projected delta-only facts, so their graph stayed
incomplete until a full generation arrived. The fix overlays each delta
generation on its base at read time (`ListFacts`/`LoadFacts` in
`go/internal/storage/postgres/delta_union.go` with the pure overlay in
`go/internal/facts/delta_union.go`): delta wins per stable_fact_key,
tombstones and deleted paths remove base rows. The base is the newest
projected full generation at `DeltaBaselineCommitSHA`; a missing base
refuses with `refused_base_missing` through the fence plus a reindex
watermark to the forced-full path. Operator record:
`docs/public/operate/graph-delta-union.md`.

## Performance Evidence:

Base-lookup `EXPLAIN (ANALYZE, BUFFERS)` on a scratch PostgreSQL
instance, 60,000 `scope_generations` rows (40 scopes x 1,500
generations, mixed full/delta and five statuses): without the index
0.328 ms and 56 buffers with 898 rows removed by filter; with the
migration-178 partial index one seek, 0.132 ms and 19 buffers, a
byte-identical row. A plain
`(scope_id, is_delta, source_commit_sha)` index is never chosen by the
planner and was not shipped.

- Backend: local PostgreSQL scratch instance for the plan proof; live
  union-read proof below ran against local PostgreSQL 15433.
- Input shape: the 60k-row generations table above for the plan; live
  reads seed one base-full plus one delta generation per scope.
- Per-delta-load cost: one target-row read, one base seek, one
  prune-event probe only for superseded bases, plus the base fact pages
  the projection would otherwise skip. `CountFacts` reports the
  base+delta upper bound so the large-generation semaphore classifies
  on the union.
- Live timings:
  `ESHU_POSTGRES_TEST_DSN=postgres://eshu:change-me@localhost:15433/eshu?sslmode=disable
  go test ./internal/storage/postgres/ -run
  'UnionLive|BaseMissingLive|PrunedBase|ReadDeltaUnionActiveLive'
  -count=1` =>
  `TestFactStoreListFactsDeltaUnionLive` 0.08s,
  `TestFactStoreListFactsDeltaBaseMissingLive` 0.06s,
  `TestFactStoreReadDeltaUnionActiveLive` 0.05s,
  `TestFactStoreListFactsPrunedBaseRefusesLive` 0.07s, package 0.324s.
- Why safe: the base lookup is one partial-index seek per delta load;
  the overlay is in-memory over rows already paged; migration 178
  indexes only `is_delta = false` projected generations, so delta
  inserts pay nothing and full inserts pay one small partial-index
  write. No new hot write path, no worker or batch change. The
  161-scope production repair-load plan (per-scope base/delta/union
  counts and stage durations against each scope's full band) is an
  owner act recorded in the operate note.

## Observability Evidence:

New bounded telemetry, all covered in
`docs/public/observability/telemetry-coverage.md` (gate and mirror
pass):

- Counter `eshu_dp_projector_delta_union_total` by closed `outcome`
  (`union_base_found`/`union_base_missing_refused`); no SHAs, scope
  ids, or generation ids in labels.
- Histogram `eshu_dp_projector_delta_union_facts` carrying
  base/delta/union fact counts as values by `outcome` and `kind`.
- One `projector.delta_union` span event and one INFO
  `projector delta union fact load` log per union decision.
- Refusals reuse the fence counter outcome `refused_base_missing`
  (`projector_delta_base_missing` failure class), one WARN log, and
  the reindex watermark write (WARN on watermark failure).
