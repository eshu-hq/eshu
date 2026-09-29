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

`Investigate` adds probe row count, payload bytes, phase durations, and pool-cap
status to the caller's `postgres.query` span. The caller records errors and the
chosen execution mode on that span.

## Measured scope

Performance Evidence: A read-only SQL assembly shim on an isolated PostgreSQL
18.6 corpus of 984 repositories compared the captured 16-term single statement
with four snapshot-sharing probes and PostgreSQL final assembly. The same
storage snapshot held 155,826 `content_files` rows and 2,808,209
`content_entities` rows. Interleaved baseline/candidate medians were 0.810743
and 0.568569 seconds from dispatch through consumption of the 26-row ordered
page. All measured ordered pages, including cap status, matched. This proves
the theory on that corpus; a built binary and the ops-qa endpoint remain to be
measured after schema bootstrap. It does not establish an endpoint target.

Observability Evidence: The `postgres.query` span records the selected route,
probe row count, JSON bytes, probe and assembly duration, pool-cap status, and
errors. A pool below four open connections uses the single statement and marks
the fallback reason on the same span.

## Gotchas / invariants

- Import an exported snapshot before a worker's first SELECT. Keep the exporter
  transaction open until final assembly completes.
- Compute the candidate cap from the full request before partitioning terms.
- The four transactions must fit in the configured connection pool; smaller
  pools use the single-statement path.
- Preserve the SQL `ORDER BY` and `string_agg(DISTINCT ...)` rules. Go string
  sorting is not a replacement for database collation.

## Related docs

- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
