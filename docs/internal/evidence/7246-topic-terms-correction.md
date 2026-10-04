# #7246 change-planning topic terms

## Accepted result change

The caller approved removing file and entity results that match only the internal
`change_surface` workflow words. A topic of `showImage` must search for
`showimage`, not for `change` or `surface`. Explicit caller topics of `change`
and `surface` still search those words. The public code-topic route still uses
its caller-supplied intent when it forms search terms.

The three change-planning routes use one production code-surface backend:
change-surface investigation, pre-change impact, and developer change plan.
They retain repository authorization, the topic reader's bounded pool, limit,
offset, truncation and coverage markers, and answer truth metadata. The changed
selection is intentional, so old and new row equality is not claimed for this
one correction.

## Theory evidence and limits

A read-only candidate query on the current ops-qa PostgreSQL read replica used
the same `showImage` repository argument and a single repeatable-read snapshot.
The candidate selected only the requested topic term. All 11 returned rows
matched `showimage`; zero matched only `change` or `surface`. The complete
`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` execution took 186.691 ms, and the
separate result query took 95.860 ms. The candidate pool reported no cap.
These are SQL observations, not endpoint p95 or a controlled old/new speedup.
The original SQL's separate custom plan took 991.278 ms in a different
connection and storage state, so those times are not paired comparison evidence.

No-Observability-Change: the existing query span, topic read operation,
repository scope, candidate-pool status, truncation and response truth fields
remain in place. No new metric, span, graph read or database write is added.

Performance Evidence: the bounded representative candidate above proved the
selection theory before production code changed. The built route's API and MCP
cold/warm p95 below one second remains NOT_CHECKED. The owner deploy and a
same-argument sweep are still required for issue closure.

## New compiled handler observation

On 2026-10-04, the topic-only candidate at `79bdb7ec3` was compiled with
Go 1.27.1 for Darwin arm64. The binary SHA-256 was
`139d2dba18f7eae38244a458e1c5789179203b216396c3c8ed34d8f5a777311f`.
The existing helper mounted the production impact handler and used a synthetic
repository-scoped post-auth context with a raw cached pgx content reader.
It made six new developer-change-plan requests on one read-replica session.
The original baseline calls and timings were not repeated.

| Call | Handler seconds | Topic SQL seconds | Custom / generic plans |
| --- | ---: | ---: | --- |
| 1 | 0.484786500 | 0.220560708 | 1 / 0 |
| 2 | 0.281960875 | 0.127664750 | 2 / 0 |
| 3 | 0.275823791 | 0.125956417 | 3 / 0 |
| 4 | 0.277799625 | 0.125277917 | 4 / 0 |
| 5 | 0.274579500 | 0.124540417 | 5 / 0 |
| 6 | 1.763523083 | 1.580887083 | 5 / 1 |

All six responses matched the independently retained topic-only page and the
handler projection: seven affected entities, one normalized changed-path input,
no candidate-pool cap, and explicit page truncation and partial coverage.
The missing changed-path evidence remained visible and unsafe patch guidance
remained blocked. The same topic SQL hash and backend session were retained
across all calls. The sixth call recorded the first generic-plan execution.

The controller and helper exited zero. Backend absence was observed after
the helper closed, both owned port forwards terminated, and the local ports
were free. The complete run took 9.241 seconds, about nine seconds, inside
its 120-second bound. A prior local setup attempt stopped before any forward,
database connection, or handler call because its PATH omitted a required tool;
that failed receipt is retained separately. No old baseline was replayed.

The generic cached-plan path still exceeds one second. This candidate is not
a completed latency fix. The measured helper bypasses production Reader Access,
authentication middleware, and external HTTP transport. It does not establish
deployed API or MCP cold/warm p95, a cold storage state, or a controlled speedup.
The next investigation is the new candidate generic plan; the issue remains open.
