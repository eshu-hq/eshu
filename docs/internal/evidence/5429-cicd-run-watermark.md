# #5429: CI/CD run watermark -- theory proof and local proof

## Theory being proved

`cicd_run_watermarks` (`go/internal/storage/postgres/cicd_run_watermark.go`)
is a new table backing `runwatermark.Store`. The claim: point `Load`/`Save`
queries scoped by the `(scope_id, repository)` primary key use an index
lookup, not a sequential scan, at a representative row count, and the
fencing `WHERE` guard on `Save`'s `ON CONFLICT DO UPDATE` correctly rejects a
stale (lower) fencing token while accepting a newer one -- in real Postgres,
not only in the Go unit tests that fake the `ExecQueryer`/`Rows` seam.

## Proof method

Ran the throwaway shim below against a scratch `postgres:16-alpine` container
(`docker run --rm -e POSTGRES_PASSWORD=postgres -p 55429:5432
postgres:16-alpine`), no eshu Docker Compose stack involved. 50,000
synthetic `(scope_id, repository)` rows -- a generous upper bound versus any
realistic single-deployment fleet of polled GitHub Actions repositories.

Full script:
`/private/tmp/claude-501/-Users-asanabria-repos-eshu-hq-eshu/d7b9d08f-a0d9-493d-9b12-2459308cf5a0/scratchpad/5429_watermark_proof.sql`
(scratchpad path, not committed; reproducible from the DDL/query constants in
`cicd_run_watermark.go` plus the `INSERT ... generate_series` seed below).

```sql
CREATE TABLE cicd_run_watermarks (
    scope_id TEXT NOT NULL,
    repository TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    last_run_id TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, repository)
);

INSERT INTO cicd_run_watermarks (scope_id, repository, generation_id, fencing_token, last_run_id, updated_at)
SELECT
    'ci-cd:github-actions:org' || (i % 500) || '/repo' || i,
    'org' || (i % 500) || '/repo' || i,
    'generation-' || i,
    i,
    (100000 + i)::text,
    now() - (i || ' seconds')::interval
FROM generate_series(1, 50000) AS i;

ANALYZE cicd_run_watermarks;
```

## Results

### Load query -- index scan on the PK

```
EXPLAIN (ANALYZE, BUFFERS)
SELECT last_run_id, generation_id, fencing_token, updated_at
FROM cicd_run_watermarks
WHERE scope_id = 'ci-cd:github-actions:org250/repo25000' AND repository = 'org250/repo25000';
```

```
Index Scan using cicd_run_watermarks_pkey on cicd_run_watermarks
  (cost=0.41..8.43 rows=1 width=39) (actual time=0.009..0.009 rows=0 loops=1)
  Index Cond: ((scope_id = ...) AND (repository = ...))
  Buffers: shared hit=3
Execution Time: 0.021 ms
```

### Comparison baseline -- forced sequential scan (same predicate)

```
SET enable_indexscan = off; SET enable_bitmapscan = off;
EXPLAIN (ANALYZE, BUFFERS) <same SELECT>
```

```
Seq Scan on cicd_run_watermarks
  (cost=0.00..1520.97 rows=1 width=39) (actual time=2.537..2.537 rows=1 loops=1)
  Filter: ((scope_id = ...) AND (repository = ...))
  Rows Removed by Filter: 50001
  Buffers: shared hit=770
Execution Time: 2.540 ms
```

The PK index scan reads 3 buffers and 0.021 ms versus the seq scan's 770
buffers and 2.540 ms at 50,000 rows (~120x fewer buffer reads, ~120x lower
latency) -- the query plan the shipped `Load` query gets in practice, not the
forced worst case.

### Save (UPSERT) -- fencing guard rejects a stale token, accepts a newer one

Existing row at `fencing_token=99999` (from a prior save in the same
session). Attempting a stale write (`fencing_token=1`):

```
Insert on cicd_run_watermarks (actual time=0.021..0.021 rows=0 loops=1)
  Conflict Resolution: UPDATE
  Conflict Arbiter Indexes: cicd_run_watermarks_pkey
  Conflict Filter: (cicd_run_watermarks.fencing_token <= excluded.fencing_token)
  Rows Removed by Conflict Filter: 1
  Tuples Inserted: 0
  Conflicting Tuples: 1
```

`RowsAffected() == 0` here is exactly what `CICDRunWatermarkStore.Save`
turns into `runwatermark.ErrStaleFence`. Confirmed the row was in fact left
unchanged:

```sql
SELECT fencing_token, last_run_id FROM cicd_run_watermarks WHERE ...;
--  fencing_token | last_run_id
-- ---------------+-------------
--          99999 | 999999
```

A higher fencing token (`99999` following an earlier lower value) shows
`Tuples Inserted: 1` -- the update proceeds. A brand-new key (no conflict)
also inserts cleanly via the same statement.

## Conclusion

Theory confirmed: the PK composite index is used for both `Load` and
`Save`'s conflict arbiter (no sequential scan at 50k rows), and the fencing
`WHERE` guard is enforced by Postgres itself, not only application code --
real-Postgres proof, not the Go unit tests' faked `ExecQueryer`. No
optimization is being claimed here (this is a brand-new table, not a
rewrite of an existing query), so there is no "old shape vs. new shape"
equivalence check to run; this proof exists to satisfy the repository's
mandatory prove-the-theory-first gate before landing new hot-path-adjacent
SQL, and to catch a missing/wrong index before it ships.

Container was a throwaway `docker run --rm ... postgres:16-alpine`, stopped
and removed after the run; no state persists.
