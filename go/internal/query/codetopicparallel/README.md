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
