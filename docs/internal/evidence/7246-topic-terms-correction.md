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

## New per-query mode theory trial

The cached-handler run above retained the first generic-plan transition. We
then ran only three new candidate reads with pgx CacheDescribe; we did not
repeat the baseline. The SQL digest was unchanged, and all four business
arguments retained their original positions.

| New read | SQL seconds | Rows | Named topic statements |
| --- | ---: | ---: | ---: |
| U1 | 0.204045042 | 11 | 0 |
| U2 | 0.126354083 | 11 | 0 |
| U3 | 0.126036000 | 11 | 0 |

The read-only replica transaction used repeatable-read isolation. Every read
matched the independently retained 11 rows across all 12 fields, row order,
multiplicity, and column types. Each named-statement counter stayed at zero.
The helper closed its reader and observer; its observer confirmed the reader
PID was absent. The helper, controller, and owned port-forward exited, and
the forwarding port was free. The controller completed in 4.994 seconds
within its 60-second limit.

Replica replay advanced during this trial. These are new diagnostic samples,
not a controlled storage comparison, a speedup claim, or endpoint p95. The
trial proved the one-term query mode. The next section records the
regression tests and compiled handler proof. Deployed API and MCP cold/warm
p95 remains **NOT_CHECKED** before the latency issue can close.

## Three-call compiled handler proof and current limit

The reviewed binary built from source head `5d6a0274900729d74619b285dbee2837912efcb5`
had SHA-256 `5a69370135a587e16755663cf568129a32b39cfda5254f3a3dcb2f6fc154dea9`.
Its one-shot controller made three **new** developer-change-plan calls with
the retained request body SHA-256
`10840ce1b51a417e52dfdb32b20e8b8b207d49371955e25d97b46b088f643dc5`.
It made no old-mode or baseline business call.

| New call | Handler ms | Topic SQL ms | Named topic statements |
| --- | ---: | ---: | ---: |
| 1 | 380.353916 | 196.123708 | 0 |
| 2 | 286.865042 | 123.212833 | 0 |
| 3 | 273.335125 | 121.601209 | 0 |

Each topic read used SQL SHA-256
`ad7367332c67f9073c78bf27fe07ac129f959e25633f23f7f3fd8a258b2b0131`.
The pgx trace saw six arguments: the standard-library result-format option,
`CacheDescribe`, then the four original business arguments. The independently
retained eleven-row topic truth and seven-entity response projection matched;
all three responses matched each other. The result retained partial coverage,
missing changed-path evidence, and blocked patch guidance. The reader and graph
close acknowledgments were recorded. A read-only observer checked that the
reader PID was absent on the same pinned replica pod and postmaster. Both
owned port forwards and the controller terminated. This is diagnostic proof
through the mounted production impact handler with a synthetic scoped
post-auth context and a raw one-connection content reader. Production Reader
Access, authentication middleware, external transport, other API/MCP routes,
and deployed cold/warm p95 remain **NOT_CHECKED**.

The final source boundary restricts both the measured file branch and
`CacheDescribe` to a nonempty explicit repository ID, exactly one term, and
an empty trimmed language filter. Three-term and language-filtered explicit
repository searches use the upstream file branch and existing driver mode.
Grant-list-only and unscoped searches retain their prior behavior, as does
the eligible sixteen-term shared-snapshot path. Boundary regressions failed
before this restriction and passed after it. The binary above predates the
boundary restriction. A final-source, offline interception of the production
content reader captured the same SQL SHA-256, `pgx.QueryExecModeCacheDescribe`,
and ordered typed arguments: repository ID and `showimage` as strings, then
limit 11 and offset 0 as integers. The retained request-body SHA-256 matched.
The capture made one intercepted read and no backend call. Together with the
boundary regressions and changed-package build, this links the prior measured
one-term handler path to the final source for that exact input. It does not
measure any other route or a new deployed artifact.

Performance Evidence: the three new calls above measured only the diagnostic
handler path. No paired old/new speedup or deployed endpoint p95 is claimed.
No-Observability-Change: the existing topic query span, execution-mode
attribute, repository scope, pool/truncation status, and response truth fields
remain in place. No new runtime signal is introduced.
