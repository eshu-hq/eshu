# #7246 repository-scoped topic SQL probe

## Scope

On 2026-10-02, a read-only investigation of the deployed change-planning
request on the 12,403-file repository identified the topic SQL as the dominant
stage. The HTTP request took 6.929330 seconds; the correlated server trace
took 6.826926 seconds, including 6.742461 seconds in
`postgres.query` with `db.operation=investigate_code_topic`.
Correlation used the route, time window, request and response byte counts,
and user agent; the incoming client trace ID did not resolve in Tempo.

The deployed source was `483d0d2`. SQL was rendered from that source's actual
templates, with three terms, an explicit repository predicate, a per-term
candidate cap of 1,333, and the final `LIMIT 11 OFFSET 0`.
The configured API reader points to the read-replica service. The SQL probes
verified recovery mode, database identity, and read-only mode on that replica.
No DDL, standalone `ANALYZE`, data mutation, or server setting change was made.

## Measured theory

Performance Evidence: the bounded replica probes below establish the measured
custom-plan SQL change and fixed-case candidate equality. They do not establish
application cached-plan or endpoint p95 performance.

The initial actual plan spent 5,554.564 ms in the file candidate probe.
Two content branches used the global content trigram index, then performed
heap rechecks and applied the repository predicate as a filter. Those two
branches together accounted for 5,021.191 ms of the 5,933.388 ms execution.
This identifies expensive branches; it does not establish a pure I/O cause.

On the single-statement route, requests with a nonempty trimmed explicit
repository ID add a singleton materialized term CTE inside each file branch
and read that term through a scalar subquery in the content-only predicate.
Requests without an explicit repository ID retain the original SQL, including
searches bounded by a repository grant list. The three-term request measured
here uses this route.

The candidate was rebased onto main `6d0c1d81` on 2026-10-03. Main's eligible
16-term shared-snapshot route runs before the single-statement builder and
retains its existing file-branch SQL. The measurements below do not validate
that parallel route or the rebased candidate's deployed latency.
The measured plans then intersected the repository and content indexes
before heap rechecks. Path-first quotas, path exclusion, repository predicates,
entity candidates, grouping, scoring, pagination, and truncation metadata
were preserved. No terms are special-cased.

Planner-only comparisons in one connection confirmed that the diagnostic
observers retained each variant's scan types, parallel awareness, and child
indexes. The observers add downstream CTE consumers, so their timing is
reported separately from the uninstrumented query.

## Complete candidate-pool comparison

One `REPEATABLE READ READ ONLY` transaction executed the uniform candidate
first and the baseline second, with an 8-second statement timeout and a
22-second client bound. The completed output acknowledged `ROLLBACK`.

| Compared output | Baseline rows | Candidate rows | Equality |
| --- | ---: | ---: | --- |
| File candidate pool, including capped membership and captured order | 1,355 | 1,355 | Exact |
| Entity candidate pool and captured order | 1,351 | 1,351 | Exact |
| Complete grouped output before final pagination | 2,701 | 2,701 | Exact multiset and captured order |
| Final page, including truncation metadata | 11 | 11 | Exact ordered tuples |

The capped file branch contained the same 1,333 candidates in order.
An independent offline read of the retained output confirmed these results.
The initial controller failed while parsing psql's timing banner after both
queries completed; output was recovered offline without rerunning either
database query. The diagnostic timings were 5,509.066 ms for baseline and
1,037.740 ms for candidate, including the diagnostic JSON construction.

## Uninstrumented SQL measurement

A separate bounded, same-snapshot pair used
`EXPLAIN (ANALYZE, BUFFERS, VERBOSE, FORMAT JSON)` on the original query shapes,
candidate first. Each query had an 8-second statement timeout.
The direct client exited 0, acknowledged rollback, and finished in
7.053570 seconds within its 22-second bound.

| Variant | Planning ms | Execution ms | Returned rows |
| --- | ---: | ---: | ---: |
| Baseline | 4.093 | 5,196.368 | 11 |
| Uniform candidate | 5.534 | 910.986 | 11 |

An earlier baseline comparison timed out at 8 seconds before its candidate
started. That censored result is retained separately and is not a passing
sample. The successful pairs do not replace it.

## Compiled handler proof status

The baseline and candidate compiled locally using Go 1.27.1 on Darwin arm64,
with the production content reader, graph reader, impact handler, default
code-surface backend, and production profile. The planned experiment used a
synthetic scoped post-auth context granting only the fixed repository.
Original caller headers, enforcement mode, and grant class were not retained;
original authentication parity is **NOT_CHECKED**.

The one-shot experiment stopped during its initial replica identity check,
before graph connection or any handler request. Its direct exit was 1.
The baseline did not start; both owned forwards were terminated. Total runtime,
including setup and cleanup, was 4.082883 seconds within the 120-second bound.
This is a failed setup, not a latency sample. No database or handler retry was
performed. The helper retained the failing stage but not the driver cause, so
the connection failure's root cause is **NOT_CHECKED**.

A corrected, separately reviewed experiment retained the original failure,
mapped all approved pgx transport fallbacks to the tunnel, and removed the
unused session timeout change. It kept the production driver's statement
cache capacity of 512 and cache-statement mode. The existing server
statement timeout was zero and was left unchanged. Requests used a
7-second client context within the 8-second request ceiling; actual backend
termination was checked separately from client exit.

Six candidate requests completed with HTTP 200 on one physical replica
session. Handler timings include actual database and graph driver waits and
in-process HTTP serialization, but exclude external HTTP transport. Topic
timings are pgx client elapsed times, not per-node EXPLAIN runtimes. The driver's query fingerprints matched the prepared statement, and
its counters identify the transition from custom to generic planning:

| Call | Complete handler seconds | Topic SQL seconds | Custom plans | Generic plans |
| --- | ---: | ---: | ---: | ---: |
| 1 | 1.336928 | 1.057195 | 1 | 0 |
| 2 | 1.038395 | 0.889141 | 2 | 0 |
| 3 | 1.056282 | 0.900708 | 3 | 0 |
| 4 | 1.051849 | 0.902089 | 4 | 0 |
| 5 | 1.070759 | 0.906071 | 5 | 0 |
| 6 | 3.073575 | 2.893725 | 5 | 1 |

All six complete response byte hashes matched. Their parsed response also
matched the saved original response, which does not establish the unknown
original caller's authorization. The baseline's first request was censored
at 7.002068 seconds, so full baseline/candidate response comparison remains
**NOT_CHECKED**. No remaining baseline request ran. The controller stopped,
reaped both owned forwards, and finished in 22.232589 seconds.

The candidate backend was absent before baseline dispatch. The baseline
backend remained present during the three immediate cleanup observations;
a later read-only metadata check on the sole ready replica endpoint
confirmed it absent. This follow-up executed no handler or topic query.
No planner mode, server timeout, schema, cache, or deployment setting changed.

The compiled result disproves sufficiency of the custom-plan win for the
one-second endpoint target: even the custom-plan handler samples exceeded
one second, and the observed generic-plan sample took over three seconds.
This is not a p95 distribution or deployed acceptance. Publication remains
on hold for a measured improvement that holds through the actual cached
driver path and meets the endpoint budget.

Focused generated-SQL, explicit-repository language, grant-list language, and
scoped grant/deny tests passed after the implementation. Source-template checks
confirmed that unscoped branches match the original SQL and the explicit
repository branch matches the measured shim after whitespace normalization.
The interrupted selected static run has no passing aggregate. Its five
unfinished floor commands were subsequently run separately and passed:
the FIPS seeded test, hot-Cypher source coverage and its validator tests,
performance-evidence verifier, and verifier self-tests. Deferred gates
remain **NOT_CHECKED** unless their separate retained proof covers this change.

## Limits and remaining acceptance

No-Observability-Change: the change preserves the existing
`postgres.query` span, `investigate_code_topic` operation, candidate-cap and
truncation attributes, HTTP route tracing, and response truth metadata.
The existing spans identified the SQL bottleneck; no new public fields,
metric labels, or telemetry transport are introduced.

These are isolated SQL diagnostics on a changing replica corpus, with cache
state uncontrolled. They are not cold measurements, a p95 distribution, a
deployed API/MCP measurement, or an immutable-corpus acceptance run.
Fresh PREPARE probes do not establish application cached-plan behavior;
the separate compiled-handler experiment above observes that behavior only
for its fixed case and declared scoped authorization. A separate planner-only `EXPLAIN (GENERIC_PLAN)` returned
serial repository index scans with content filters for both variants; it did
not reproduce the custom-plan bitmap intersection improvement. This check
executed no data query and changed no settings. The generic-plan runtime observed above does not attribute
individual scan-node costs to that planner-only shape. Inner candidate limits have no SQL-level ordering guarantee;
this one-snapshot comparison proves the observed capped membership only.

The approximately 911 ms SQL sample leaves little room for handler and
transport cost. Deployed same-argument cold/warm endpoint p95 below one
second remains **NOT_CHECKED**. #7246 stays open until its complete endpoint
family, result truth, deployed artifact, and latency acceptance are verified.
