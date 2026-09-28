# #7242 workload-name scope index theory

## Observed read

A read-only ops-qa plan on the 12,403-file repository used
`fact_records_workload_identity_workload_idx` to visit 6,291 retained
`reducer_workload_identity` facts, then filtered approximately 6,283 facts
from other scopes to return eight target rows. The first observed statement
took 687.792 ms, with 4,620 shared reads. A later execution took 30.707 ms
with 4,719 shared reads. These are separate SQL probes, not synchronous
samples of the context request. The deployed context API's first observed call
on the same repository took 1.832 s; its warm ten-call p95 was 0.177 s. The
SQL probe is a plausible contributor to cold latency, not a per-request trace
attribution.

`ContentReader.repositoryWorkloadNames` filters by `scope_id`, fact kind, and
non-tombstone status, expands `payload.entity_keys`, and returns distinct
names. It intentionally reads retained generations. Restricting this query to
the active generation would change its result contract. The proposed index
changes no SQL or result filtering:

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_workload_names_scope_idx
    ON fact_records (scope_id)
    WHERE fact_kind = 'reducer_workload_identity'
      AND is_tombstone = FALSE;
```

## Performance Evidence:

A disposable PostgreSQL 18 fixture had 62,910 interleaved fact rows, including
6,291 workload identities and eight matching target-scope workload rows. The
fixture removed its generic scope index to reproduce the *observed* ops-qa
baseline plan: a global workload-index bitmap scan followed by scope filtering.
The real database retains `fact_records_scope_generation_idx` (scope, then
generation and kind) and the migration 099 scope/generation keyset index. The
new smaller partial index matches the query's kind and tombstone predicates
and leads on scope without requiring a generation filter; the production
planner choice among them is still unmeasured. The fixture therefore does not
reproduce the full production index inventory. The query text and
returned rows stayed the same across the two fixture plans.

| Fixture plan | SQL time | Shared buffers | Workload rows visited |
| --- | ---: | ---: | ---: |
| Existing global workload index | 12.994 ms | 4,600 hits | 6,291 |
| Added scope-leading partial index | 0.458 ms | 11 hits, 2 reads | 8 |

The baseline bitmap heap scan touched 4,494 heap blocks. The candidate index
was 73,728 bytes on this fixture; the existing workload index was 417,792
bytes. A single rollback-only 1,000-row matching insert took
4.477 ms and generated 831,999 WAL bytes without the new index, versus
5.054 ms and 887,219 WAL bytes with it. The added index cost 0.577 ms
(12.9%) and 55,220 WAL bytes (6.6%) in this one sample. These values are
fixture measurements and do not establish production write throughput.

The final migration file was applied outside a transaction to the same
disposable PostgreSQL 18 cluster with `psql -f` (exit 0). The resulting index
was valid and ready. A subsequent exact query plan used one scope-index search
to reach eight rows in 1.619 ms with five shared hits and eight reads. This
was a separate cache state and is not paired with the earlier fixture times.

The read plan supports the scope-index theory on the observed baseline shape.
The concurrent migration can scan the table twice, add I/O load, and
wait for existing transactions; build duration and contention need owner
observation. The exact production planner choice, deployed handler latency, and cold/warm p95 remain unmeasured. No production DDL or setting was
changed. #7242 remains open until the owner deploys and the original API/MCP
arguments meet the under-one-second cold and warm p95 target.

## Observability Evidence:

`repositoryWorkloadNames` now emits a child `postgres.query` span for its SQL
read, with bounded `db.operation=repository_workload_names` and
`db.sql.table=fact_records` attributes. It records query, scan, and row-iteration
errors. Empty-scope calls perform no SQL and emit no dependency span. The
focused span regression was RED before instrumentation and GREEN afterward on
query and scan errors. The existing API request duration metric remains the
request-level signal; this change does not add a Postgres histogram to the raw
`ContentReader` DB path.
