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

The initial actual plan spent 5,554.564 ms in the file candidate probe.
Two content branches used the global content trigram index, then performed
heap rechecks and applied the repository predicate as a filter. Those two
branches together accounted for 5,021.191 ms of the 5,933.388 ms execution.
This identifies expensive branches; it does not establish a pure I/O cause.

The candidate adds a singleton materialized term CTE inside each file branch
and reads that term through a scalar subquery in the content-only predicate.
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

## Limits and remaining acceptance

These are isolated SQL diagnostics on a changing replica corpus, with cache
state uncontrolled. They are not cold measurements, a p95 distribution, a
built-candidate API/MCP measurement, or an immutable-corpus acceptance run.
The application's cached-plan behavior is also not established by fresh
`PREPARE` probes. A separate planner-only `EXPLAIN (GENERIC_PLAN)` returned
serial repository index scans with content filters for both variants; it did
not reproduce the custom-plan bitmap intersection improvement. This check
executed no data query and changed no settings. Generic-plan runtime remains
unmeasured. Inner candidate limits have no SQL-level ordering guarantee;
this one-snapshot comparison proves the observed capped membership only.

The approximately 911 ms SQL sample leaves little room for handler and
transport cost. Deployed same-argument cold/warm endpoint p95 below one
second remains **NOT_CHECKED**. #7246 stays open until its complete endpoint
family, result truth, deployed artifact, and latency acceptance are verified.
