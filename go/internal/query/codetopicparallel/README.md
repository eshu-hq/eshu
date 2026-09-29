# Code-topic parallel reads

## Purpose

This package runs the measured 16-term code-topic read through four PostgreSQL
sessions that share one exported snapshot. PostgreSQL assembles the final page
from typed JSON probe rows so its text collation and aggregate rules remain the
source of truth.

## Ownership boundary

`query.ContentReader` chooses this path, supplies repository and language
filters, owns the outer span, and scans the final response. This package owns
the partition SQL, snapshot lifetime, worker cancellation, and final assembly.
It does not decide caller grants or change the response contract.

## Exported surface

- `Eligible` and `Partitions` bound use to the measured term count and pool size.
- `FileBranch`, `ProbeSQL`, and `AssemblySQL` build the existing candidate rules
  and PostgreSQL final grouping.
- `ProbeRow` preserves SQL NULLs across the JSON transfer.
- `RunPartitions` joins all workers after an error; `Investigate` owns their
  read-only transactions.

See `doc.go` for the package contract.

## Dependencies

- `internal/query/codequery` supplies request and evidence-row types.
- `database/sql` pins each worker to one PostgreSQL transaction.

## Telemetry

`Investigate` adds reservation wait, connection count, probe row count, payload
bytes, phase durations, reservation cancellation, and pool-cap status to the
caller's `postgres.query` span. The caller records errors and the chosen
execution mode on that span.

## Measured scope

Performance Evidence: A read-only SQL assembly shim on an isolated PostgreSQL
18.6 corpus of 984 repositories compared the captured 16-term single statement
with four snapshot-sharing probes and PostgreSQL final assembly. The isolated
corpus had unchanged storage state across samples: 155,826 `content_files`
rows and 2,808,209 `content_entities` rows. Interleaved baseline/candidate
medians were 0.810743 and 0.568569 seconds from dispatch through consumption
of the 26-row ordered page. All measured ordered pages, including cap status,
matched. This proves the query-shape theory on that corpus, not the endpoint
target.

A built API at source commit `917922a0831cf6991e3dbe1dc528b0d7683edb47`
then ran the same unscoped 16-term request on a dedicated Neo4j/PostgreSQL test
instance with the same preserved corpus and storage state for both variants.
After two warmups per variant, eight interleaved baseline/candidate HTTP pairs
had medians of 0.780149 and 0.423014 seconds. Every timed request returned
HTTP 200, the complete canonical JSON response matched in every pair, and the
content-table fingerprint was unchanged. This is a built endpoint result on
the dedicated instance; the deployed ops-qa endpoint remains unmeasured for
this commit.

Observability Evidence: The `postgres.query` span records the selected route,
reservation wait and cancellation, connection count, probe row count, JSON
bytes, probe and assembly duration, pool-cap status, and errors. A pool below
four open connections uses the single statement and marks the fallback reason
on the same span.

## Gotchas / invariants

- Import an exported snapshot before a worker's first SELECT. Keep the exporter
  transaction open until final assembly completes.
- Compute the candidate cap from the full request before partitioning terms.
- Reserve four connections under a per-pool acquisition gate before starting
  any transaction. On cancellation, release each partial reservation. This
  prevents competing requests from filling the pool with exporters and waiting
  for their own workers. The gate is released after all four connections are
  reserved, so a pool of eight can run two requests concurrently. Smaller
  pools use the single-statement path.
- Preserve the SQL `ORDER BY` and `string_agg(DISTINCT ...)` rules. Go string
  sorting is not a replacement for database collation.

## Related docs

- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
