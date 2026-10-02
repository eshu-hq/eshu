# Code-topic parallel reads

## Purpose

This package runs the measured 16-term code-topic read through four PostgreSQL
sessions that share one exported snapshot. PostgreSQL assembles the final page
from typed JSON probe rows so its text collation and aggregate rules remain the
source of truth.

## Ownership boundary

`query.ContentReader` chooses this path, supplies repository and language
filters, owns the outer span, and scans the final response. This package owns
the partition SQL, worker cancellation, and final assembly. The guarded
PostgreSQL reader owns connection reservation, session fencing, and snapshot
export/import/cleanup.
It does not decide caller grants or change the response contract.

## Exported surface

- `Eligible` and `Partitions` bound use to the measured term count and pool size.
- `FileBranch`, `ProbeSQL`, and `AssemblySQL` build the existing candidate rules
  and PostgreSQL final grouping.
- `ProbeRow` preserves SQL NULLs across the JSON transfer.
- `RunPartitions` joins all workers after an error; `Investigate` closes the
  guarded snapshot set after final assembly.

See `doc.go` for the package contract.

## Dependencies

- `internal/query/codequery` supplies request and evidence-row types.
- `internal/storage/postgres/db` supplies a query-only snapshot-set contract;
  no raw SQL pool or transaction escapes into this package.

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

A built API at source commit `147971b860e0fd1e6df3e39697be715f52bd865e`
then ran the same unscoped 16-term request on a dedicated Neo4j/PostgreSQL test
instance with the same preserved corpus and storage state for both variants.
Eight interleaved ABBA rounds (16 timed requests per variant) had baseline and
candidate medians of 0.768627 and 0.415212 seconds. Every timed request
returned HTTP 200, the complete canonical JSON response matched, and the
content and queue fingerprint was unchanged. This is a built endpoint result
on the dedicated instance, not a deployed ops-qa acceptance result. Those
measurements remain bound to the pre-rebase source commit. The later
guarded-reader integration has its own [historical pre-permit built endpoint
comparison](../../../../docs/internal/evidence/7033-exact-code-topic-parallel.md):
on the same dedicated corpus, baseline and candidate medians were 0.765572
and 0.464944 seconds, with matching full responses. That result is bound to
candidate `9b96896d595fecd700d8e4bdbc32435dee2cad1b`, before the
permit/fallback changes. The current guarded-reader source
`5d44dda539ad5e2e136aa25e738aaca4910d4e50` has its own fixed-corpus
ABBA comparison against baseline `575ef287bfe13785639a8e4548593ccb92cf3c27`:
16 timed requests per variant yielded medians of 0.797406 and 0.484409
seconds, with matching complete JSON and unchanged content/index fingerprints.
Deployed ops-qa readiness and the `<1 s` endpoint budget are also not yet
validated and are tracked in #7516.

Observability Evidence: The `postgres.query` span records the selected route,
reservation wait and cancellation, connection count, probe row count, JSON
bytes, probe and assembly duration, pool-cap status, and errors. A pool below
four open connections uses the single statement and marks the fallback reason
on the same span.

## Gotchas / invariants

- The guarded reader imports the exported snapshot before each worker's first
  SELECT. Keep its exporter open until final assembly completes.
- Compute the candidate cap from the full request before partitioning terms.
- The guarded reader reserves four connections under a per-pool acquisition
  gate before starting transactions and releases partial reservations on
  cancellation. A pool of eight can run two requests concurrently. A reported
  capacity below four, including zero, or a store without snapshot-set support
  uses the single statement. If the guarded reader's permit reservation
  expires with a live request and clean partial release, the caller makes one
  single-statement attempt through that same fenced store. Fencing, snapshot
  setup, probe, assembly, and caller-cancellation errors remain failures.
- Preserve the SQL `ORDER BY` and `string_agg(DISTINCT ...)` rules. Go string
  sorting is not a replacement for database collation.

## Related docs

- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
