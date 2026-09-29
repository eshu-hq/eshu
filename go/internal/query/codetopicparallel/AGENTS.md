# Code-topic parallel package

Keep this package downstream of `internal/query`: it may import `codequery`,
but must not import the parent query package. The parent supplies grant and
language filters before any bounded probe runs.

Preserve the shared-snapshot sequence: reserve all four connections under the
per-pool gate, then begin the read-only repeatable-read exporter, export its
snapshot, import that snapshot in each worker before the worker's first read,
join every worker, then run PostgreSQL final assembly while the exporter is
still open. Roll back every transaction and close every connection on all
exits, including canceled partial reservations.

The final SQL owns collation, distinct-term scoring, pool-cap status, and
LIMIT/OFFSET. Keep nullable columns typed in the JSON transfer. A change to
probe predicates, worker count, or assembly SQL needs a failing regression and
matched exactness and latency proof on representative PostgreSQL data.
