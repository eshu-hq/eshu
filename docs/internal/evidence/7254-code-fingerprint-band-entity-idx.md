# code_fingerprint_band Entity Index Evidence (#7254)

`code_fingerprint_band` carries `entity_id` only as the fourth column of its
primary key `(repo_id, band_no, band_hash, entity_id)`. Two statements filter
on `(repo_id, entity_id)` and so cannot seek:

- `deleteFingerprintBandsForEntitiesSQL` (`repo_id = $1 AND entity_id =
  ANY($2)`), run in 500-id chunks on every `Write` for every re-upserted
  fingerprinted entity, and
- `staleFingerprintBandEntityIDsSQL`, the stale-id read of the #7230 reap's
  second pass (`repo_id = $1` set-difference against `content_entities`).

Migration `151_code_fingerprint_band_entity_idx.sql` adds
`code_fingerprint_band_entity_idx ON code_fingerprint_band (repo_id,
entity_id)` as a sole `CREATE INDEX CONCURRENTLY IF NOT EXISTS` statement.

## Fixture (local only, no ops-qa)

`postgres:16.15` in a throwaway container, loaded with the migration 111
table and lookup index plus a minimal `content_entities`
(`(repo_id, entity_id)` index, as migration 035).

| Table | Rows | Note |
| --- | --- | --- |
| `repo-5x` bands | 799,370 | 229,040 entities, ~3.49 bands each; the #7254 shim reports 799,200 |
| `repo-5x` `content_entities` | 229,040, then 10% removed (206,136 live) | churn so the reap has stale ids (22,904) |
| 20 filler repos | 3,197,480 | 45,808 entities each; the shim's whole table held 10.5M rows, this one holds ~4.0M |

Sizes: table 355 MB, primary key 430 MB, new index 81 MB. The concurrent build
took 11.5 s on this table. Band hashes are `md5` of a clustered integer, so the
fixture has LSH-like shared bands; it is not the shim's data.

Statements are measured with `EXPLAIN (ANALYZE, BUFFERS)` inside `BEGIN ...
ROLLBACK` on this fixture database. `plan_cache_mode = force_generic_plan` is
the generic arm, `force_custom_plan` the reference. One 500-id chunk is spread
across the repository (`repo-5x:e<(g*457)%229040+1>`).

## Before / after

Warm executions (a warm-up run first), three each, in milliseconds.

| Statement | Plan mode | Before (no index) | After (index) |
| --- | --- | --- | --- |
| upsert delete, one 500-id chunk | generic | 4,647 / 4,413 / 4,695 | 668 (first, cold pages) / 5.0 / 4.4 |
| upsert delete, one 500-id chunk | custom | 127 / 239 / 157 | 9.2 / 4.7 / 4.3 |
| reap stale-id read (`EXCEPT`) | generic | 3,066 / 3,310 / 3,436 | 749 / 911 / 822 (534 in a later single run) |
| reap delete of a 500-id stale chunk | generic | 134 / 137 / 396 | 102 / 88 / 89 (73 later) |

Plan shape, generic upsert delete, before (cold single run 3,409 ms; warm above):

```text
Delete on code_fingerprint_band
  -> Bitmap Heap Scan on code_fingerprint_band  (rows=1745)
       Recheck Cond: (repo_id = $1)
       Filter: (entity_id = ANY ($2))
       Rows Removed by Filter: 797625
       -> Bitmap Index Scan on code_fingerprint_band_lookup_idx  (rows=799370)
```

After (3.7 ms):

```text
Delete on code_fingerprint_band
  -> Index Scan using code_fingerprint_band_entity_idx  (rows=1745)
       Index Cond: ((repo_id = $1) AND (entity_id = ANY ($2)))
```

The stale-id read changes from an index-only scan of the 430 MB primary key to
an index-only scan of the 81 MB entity index; the remaining cost is the sort
and set difference, which the index does not remove.

### Auto mode (acceptance 1)

`plan_cache_mode = auto`, six executions of the delete in one session, EXPLAIN
of the sixth. Without the index the sixth execution is the generic bitmap plan
above (3,229 ms, 797,625 rows filtered). With the index it is
`Index Scan using code_fingerprint_band_entity_idx` (3.8 ms). Auto mode does
select the slow generic plan on this fixture.

### Write amplification (acceptance 3, local, 1x only)

Inserting one 1x repository's bands (45,808 entities, ~159.8k rows) through
`INSERT ... SELECT`, three runs each, rolled back: 1,216 / 1,196 / 1,240 ms
without the index, 1,640 / 1,825 / 1,743 ms with it. That is about +0.5 s
(about 40%) for the statement. The statement also generates the entity rows,
which cost the same in both arms, so the ratio for the band rows alone is
higher than 40%; the absolute delta is the figure to use. It was not measured
at 5x or through the production writer's batched `INSERT`. The reference corpus's `upsert_fingerprints` sums to
364-414 s per bootstrap (about 12% of `content_write`, per the issue); the
index cost on that corpus is NOT_CHECKED here because reference-corpus timing
is a remote run this lane does not do.

## Differences from the issue's shim, stated plainly

- The issue's generic plan is an index scan of the primary key with the `ANY`
  on the fourth column, 18,853 ms per chunk. This fixture's generic plan is a
  bitmap scan of the repository range with a filter, 4.4-4.7 s. Both read the
  whole repository range and ignore `entity_id`; the magnitude differs because
  the data and planner statistics differ. The 18.9 s figure was not reproduced.
- The fixture holds ~4.0M rows, not 10.5M.
- Acceptance 2 (search the #7206 round-3 `auto_explain` logs) and the shim's
  churn cell were not run: they need ops-qa artifacts.

## Migration build strategy (acceptance 4)

A concurrent build takes `ShareUpdateExclusiveLock`, which does not block
`INSERT`/`UPDATE`/`DELETE`, so writers keep running during the build. The file
holds exactly one statement so the migration coordinator runs it in autocommit
without the bootstrap `lock_timeout` (#7004) and drops an invalid same-name
index before a retry. `IF NOT EXISTS` makes a re-run a no-op (verified on the
fixture). The build reads the table once; 11.5 s for 4.0M rows here.

## No-Regression Evidence

No-Regression Evidence (#7254): the change is one new index and no query text
changes, so no reader gets a different result. The upsert delete goes from a
repository-range read to an index seek. The reap's stale-id read is still a
repository-range read, now an index-only scan of the 81 MB index instead of
the 430 MB primary key (table above). Not re-measured: the drifted-pairs read
(`listCodeDriftedPairsQuery`, filters on `repo_id`), which could now choose the
new index; it still needs `code_fingerprint_band_lookup_idx` for its join. The cost is on the
write side: about +0.5 s to insert a 1x repository's ~160k band rows in this
fixture (see the write-amplification note for the caveats), and 81 MB of storage per ~800k band rows. That is a deliberate trade
against the per-chunk delete, which repeats once per 500 ids and scaled with the
repository's band rows. No lease, claim, queue, or transaction path changes.

## Observability Evidence

No-Observability-Change: no metric, span, log, or status signal is added. The
index is observable through `pg_stat_user_indexes`
(`idx_scan` on `code_fingerprint_band_entity_idx`) and the existing
`upsert_fingerprints` stage timing.
