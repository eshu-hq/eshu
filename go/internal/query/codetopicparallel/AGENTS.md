# Code-topic parallel package

Keep this package downstream of `internal/query`: it may import `codequery`,
but must not import the parent query package. The parent supplies grant and
language filters before any bounded probe runs.

Preserve the shared-snapshot sequence through the guarded reader contract:
it reserves and fences four connections, opens one read-only repeatable-read
exporter, imports its snapshot before each worker's first read, and owns all
transaction and connection cleanup. This package joins every worker, runs
PostgreSQL final assembly while the exporter remains open, then closes the
snapshot set on every exit. Never bypass the guarded read store with a raw
SQL pool.

The final SQL owns collation, distinct-term scoring, pool-cap status, and
LIMIT/OFFSET. Keep nullable columns typed in the JSON transfer. A change to
probe predicates, worker count, or assembly SQL needs a failing regression and
matched exactness and latency proof on representative PostgreSQL data.
