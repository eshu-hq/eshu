# Incident-context anchor lookup (#7244)

The ops-qa read sweep on 2026-09-28, against API image `sha-83cbf62`, measured
7.064 s for the first API incident-context call and 0.166 s warm p95 over eight
calls. The first MCP call followed the API call for the same incident and took
0.184 s. These are endpoint observations, not a before/after comparison of
this change. The read-only sweep returned the same 12 rows for the first and
repeated calls; response size does not explain the first-call difference.

The anchor SQL used an `OR` between `provider_incident_id` and its legacy
`source_record_id` fallback. On the real ops-qa scope and incident ID,
`EXPLAIN (ANALYZE, BUFFERS)` showed an index scan constrained by source system
and scope, followed by 16,236 rows removed by the identity filter. Its first
measured execution took 353.401 ms and read 16,522 shared blocks. That SQL
plan shows a costly cold-cache path, but does not prove how much of the
observed first endpoint call this statement consumed.

A read-only, same-snapshot candidate split the mutually exclusive identities
into `UNION ALL` branches and retained active-generation, scope, kind,
tombstone, ordering, and limit predicates. Both branches then used existing
incident lookup indexes. The candidate and baseline produced the same ordered
rows for scoped and unscoped matches, a richer incident, and a missing ID
(forward and reverse differences were empty). On the same scoped incident with
warm cache, baseline execution was 27.768 ms and 16,942 shared hits; candidate
execution was 0.372 ms and 298 shared hits. This is a query-plan comparison,
not an endpoint speedup or a cold-cache paired measurement. The candidate was
also run for a richer incident (22.013 to 0.264 ms), missing incident (24.130
to 0.220 ms), and unscoped incident (0.353 to 0.373 ms). The unscoped case
was slightly slower at this scale.
The timings are observations on a shared host, not a quiet-host wall-time gate;
the changed plan shape and shared-block counts are the primary cost evidence.

The final Go query matched the measured candidate SQL ignoring whitespace. A
disposable PostgreSQL 18 fixture compared all projected rows and their order
against the old SQL for scoped, unscoped, limit-two, alternate ID, and missing
ID reads. The fixture included absent and empty provider IDs, a nonempty
provider ID that must not use source-record fallback, an inactive generation,
a tombstone, a different kind, and a different source system. Both queries
returned identical output. The fixture was removed after the comparison.

A second read-only EXPLAIN pass on the same incident found the timeline,
change, declared-routing, applied-routing, observed-routing, and warning reads
at 3.594, 0.108, 4.111, 0.149, 6.776, and 4.162 ms respectively. These later
measurements used a warmer cache and do not reconstruct the 7.064 s first
request. The source route has a single handler span and route-duration metric;
its raw `*sql.DB` reads had no per-statement timings. The store now records a
bounded stage event on that handler span for each completed anchor, timeline,
changes, routing, runtime, and review read. Each event carries duration and an
error flag, without incident IDs, URLs, payloads, or SQL. On a recording
span, these events attribute time within the six store reads; compare their
sum with route duration to see whether the remainder lies elsewhere.

Verification: `go test ./internal/query/incident/sql ./internal/query/incident/store
./internal/query/incident -count=1` passed after the edit. Both regressions
first failed on the old behavior and then passed: one checks the indexable
identity branches; the other exercises the production read flow and its stage
events. An added error-path test observes the anchor and failing timeline only;
a planted false error flag made it fail before the restored code passed. This
change does not yet prove a deployed cold response under one second; that
requires a fresh owner deployment and endpoint sweep.
