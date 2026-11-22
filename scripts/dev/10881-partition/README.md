# #10881 fact_records HASH(scope_id) rehearsal scripts

Reproducible measurement for
[the #10881 design records](../../../docs/internal/design/10881-fact-records-hash-partition-measurements.md).
Nothing here touches a production primary. The replica is read through
`kubectl port-forward` with `default_transaction_read_only=on`; its DSN comes from
the `REPLICA_DSN` environment variable and is never stored. Work files (pulled
samples, logs) live under `$WORKDIR`, outside the repository.

| Step | Script | What it does |
| --- | --- | --- |
| 1 | `census_replica.sh` | per-scope row counts from the replica |
| 2 | `pick_sample.py` | seeded whole-scope sample lists (giant scope kept whole) |
| 3 | `pull_sample.py` | stream the scopes into zstd COPY files in keyset pages (retries a page on a recovery conflict) |
| 4 | `make_divided_schema.py`, `make_index_profile.py` | rewrite a `pg_dump --schema-only` of the replica into the divided shape and the retained-index profile |
| 5 | `load_pieces.sh` | load pieces into one rehearsal database (triggers and FK checks off for the load only) |
| 6 | `s2_pipeline.sh` | migration stages, cutover and way back, removal arms in place, then the five-load plans on both shapes |
| 7 | `removal_bench.sh`, `removal_procs.sql` | one removal arm per session: seconds and per-backend WAL bytes, then `VACUUM (VERBOSE)` |
| 8 | `explain_loads.sql`, `run_explain.sh` | the five busiest fact loads, `EXPLAIN (ANALYZE, BUFFERS)` |
| 9 | `migration_rehearsal.sh` | expand, build, backfill, verify, cutover, way back, mixed-version writer proof; samples lock modes while each stage runs |
| - | `skew_census.sql` | how hash(scope_id) spreads the real per-scope counts at modulus 32 and 64 |
| - | `fact_id_collision_proof.sql` | self-checking proof of the id-rule change (needs only a scratch database) |
| - | `census_code.sh` | derivable writer-site and reader-shape counts; run at the commit you cite |
| - | `s1_arms.sh` | clone-based removal arms for a pair of databases |

Known limits: timings depend on host load (record `loadavg` and I/O pressure with
each result); the rehearsal container runs with `fsync=off`; the primary's
statistics are out of bounds, so write-side "busiest" is ranked by code path.
