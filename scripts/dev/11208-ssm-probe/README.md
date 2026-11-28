# 11208 SSM host read probe shims

Measurement shims for [#11208](https://github.com/eshu-hq/eshu/issues/11208),
not gates and not migrations. They build a synthetic fixture in a disposable
`postgres:18` container you start yourself and run `EXPLAIN (ANALYZE, BUFFERS)`
of the SSM host fact read. The results and their limits are in
#11208 (retired evidence record).

- `seed.sql`: hand-written tables and the indexes the read can use, plus
  non-SSM facts. Not the production schema.
- `ssm_facts.sql`: adds SSM host facts for the non-empty case.
- `partial_index.sql`: the candidate partial index, for measuring only.
- `probe_base.sql`: `buildSSMHostActiveFactsSQL` output at `9f22ee51e4`, with the
  two bind values substituted. Re-dump it if the builder changes.
- `probe_c1.sql`: the rejected collector-kind prefilter.
- `measure.sh`: `seed`, `ssm`, `index` and `explain` actions against a named
  container. It starts and removes nothing.
- `test-measure-guard.sh`: hermetic self-test (a stub `docker`) that
  `measure.sh` refuses any container name not matching `^perf11208-`.

`seed.sql` drops tables, so it also aborts when `ingestion_scopes` already has
rows and is not a fixture it created.
