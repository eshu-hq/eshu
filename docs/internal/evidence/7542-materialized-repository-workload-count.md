# #7542 repository context workload count

`GET /api/v0/repositories/{repo_id}/context` reports `workload_count` from
materialized graph truth: distinct `Workload` nodes connected through a
`Repository` `DEFINES` edge. Repository read-model `WorkloadNames` remain
identity and display hints for story and service fallback. They are not a count
of materialized workloads.

## Performance Evidence: theory PROFILE

A read-only Neo4j PROFILE of the existing count query, anchored by the unique
`Repository.id` index, returned zero materialized workloads on each of two
representative repositories with 12,403 and 7,097 files. Server query time was
5 ms and 0 ms respectively, with 6 and 7 DB hits and about 120 B of allocated memory. This is server-only query timing, not API or MCP latency. The
previous read-model path avoided this graph query when a summary was available;
this change adds one indexed, scalar count read to that path. The count query
text and index are unchanged. This theory measurement preceded implementation.

## Built API/MCP comparison

The baseline (`eda63dc2c7040f3b9d991f633ab8a7967740209d`) and candidate
(`220a118125c37c4a8526b852f8b225e6d44cfc7b`) were built as actual `cmd/api`
and `cmd/mcp-server` processes. The candidate's `cmd/bootstrap-data-plane`
applied production schemas to isolated Postgres 18 and authenticated Neo4j
Community 2026.08.1. Both stores had limits of two CPUs and 2 GiB. The local
macOS arm64 host had 128 GiB and 18 CPUs; Docker had about 62.75 GiB available.

The fixed Postgres fixture contained 12,403 and 7,097 content-file rows, active
generations, repository facts, and one retained workload identity name per
repository. The sparse graph had zero and two materialized workloads; the
second repository had three `DEFINES` edges, including a duplicate. Empty
cloud-resource backfills were marked complete before service startup.

One ABBA comparison made 96 sequential HTTP API/MCP requests through real
production adapters. Each round restarted the service processes, waited for
readiness, and made one first-observed request followed by five warm requests
per repository and surface. Startup was outside request timing; the persisted
stores were shared across rounds. First-observed requests are not storage-cold
measurements. Ten warm samples per variant give an empirical p95 equal to the
maximum observed sample.

| File count | Surface | Baseline warm p95 | Candidate warm p95 |
| --- | --- | --- | --- |
| 12,403 | API | 58.500 ms | 45.787 ms |
| 12,403 | MCP | 66.350 ms | 43.331 ms |
| 7,097 | API | 27.746 ms | 21.690 ms |
| 7,097 | MCP | 21.636 ms | 21.734 ms |

The complete run exited zero in 40.975584 seconds. All 96 responses were HTTP
200. Full canonical envelopes matched across API/MCP and repeated requests;
across variants only `data.workload_count` changed from one to zero or two.
Every response retained its exact file count and the explicit
`repository_context_file_read_truncated_at_5000` reason. Fixture fingerprints
were unchanged. No configured cost flag fired (candidate p95 at least 1,000 ms,
relative increase over 10%, or absolute increase over 100 ms). The medium MCP
delta was +0.098 ms. This passes the predeclared local warm-p95 guard for
this fixture; it does not establish an absence of runtime cost or a speedup.

Round drift was substantial: baseline large-API warm median changed from
45.203 ms in round one to 20.361 ms in round four. Pooled candidate large-API
and MCP medians exceeded baseline medians despite passing the p95 guard.
The added graph read is visible in `summary_counts`: candidate warm median
was about 0.9 ms versus baseline about 0.015 ms, with one candidate
first-observed stage at 45.187 ms. A sufficiently quiet control was not
established to attribute the remaining endpoint timing differences. This
measured read cost is disclosed; the claim is limited to the stated estimator
and fixture.

Private custody retains the raw responses, binary/source hashes, packet hashes,
direct exit receipts, fixture fingerprints, service logs, metrics, and resource
samples under run identifier `7542-20261003T105905Z-73118`. Independent review
parsed all raw responses and recomputed the timings. Cleanup verification found
the owned containers and service processes absent and all four ports bindable.
Two earlier setup attempts made no comparison calls. The second retained
diagnostics that identified a login-shell executable lookup failure, corrected
by invoking the observed absolute `cypher-shell` path.

This sparse synthetic graph is not the production corpus. Original-argument
deployed cold/warm API/MCP p95 remains NOT_CHECKED and requires owner deployment
followed by a qualified sweep. The local result does not complete that latency
acceptance requirement.

## Regression proof

The focused regression first failed with a retained workload name and graph
count zero: context reported one. It also failed when the graph count was two
but the summary had one name, and when a graph deadline was swallowed by the
summary. The same tests pass after context always uses the graph count. The
existing file, platform, and dependency summary behavior remains covered by
the repository context handler regression.

## No-Observability-Change:

The existing `summary_counts` repository query stage logs the four count
fields and graph read errors. The graph read uses the existing query adapter,
spans, and duration metrics. No new metric, span, status, or runtime setting is
introduced.

## Performance Evidence: #7542 count-only context port

On source `306eac09` (a proof-branch commit that is not on `main`) against the PostgreSQL 18 read replica, one read-only
repeatable-read snapshot compared the
original full summary with a count-only path for repositories with 12,403 and
7,097 files. Both AB/BA orderings matched on scope, availability, platform
count (11), and dependency count (0). The workload-names read took
0.924480208 s and 3.038443042 s; the required count reads together took
0.092049124 s and 0.126132124 s. These are one-snapshot query-path samples,
not endpoint latency or a p95 estimate. The full/narrow SQL texts were the
same as the four relevant source files on `ebce60b8` (a proof-branch commit that is
not on `main`; the SQL has not changed since).

The context handler now selects a count-only read-model port. Its adapter
reuses the existing scope, platform, and dependency SQL with no query rewrite,
index, cache, or concurrency change. It leaves the graph `Workload` count in
place. Story and entity loaders still fetch workload names. If the new port
fails or returns unavailable, context uses the graph counts without a full
summary retry. A failure of workload-name hydration alone no longer triggers
context graph fallback; this is an intentional behavior change for a field
context does not consume. The `summary_counts` stage remains; the new bounded
`postgres.query` span identifies `repository_context_counts` and records
errors without repository IDs. Built API/MCP latency and original-argument
cold/warm p95 for this follow-up remain NOT_CHECKED until post-deployment
proof.

The finished `ContentReader` methods were also run through the repository's
FIFO SQL fixture driver in alternating order, 100 calls each. The full
summary made 400 SQL calls in 1.523085 ms of local method time; the count
method made 300 calls in 0.672120 ms. Both returned platform count 11 and
dependency count 0. The fixture has no PostgreSQL server work, so these
times only check local call-path cost and cannot be combined with the
read-only snapshot times or used as endpoint latency evidence.

Observability Evidence: the context `summary_counts` stage still logs all
four counts and graph errors. The new `postgres.query` span records the
`repository_context_counts` operation and required Postgres errors with
bounded attributes; the focused span test proves the operation and error
event without a repository ID attribute. No queue or write path runs here.

## Deployed route and statement timings on `sha-2f0388b` (2026-10-06)

Performance Evidence: before this change, a replay of the original saved
argument sets (cold on set 0, then ten warm, concurrency 1, 22 calls, all HTTP
200) on the ops-qa image `sha-2f0388b` measured `GET /api/v0/repositories/{repo_id}/context`
at cold 2.828 s and warm p95 5.109 s, and MCP `get_repo_context` at cold 1.139 s and
warm p95 4.150 s. The slow sets were the 7,097-file repository (4.0 to 5.1 s) and
the 12,403-file repository (1.1 to 2.8 s); the other seven sets were 0.15 to 0.50 s.
The two slowest `eshu-api` request traces (Tempo trace ids beginning `4af8bc35` and
`3261fa1b`, 2026-10-06 17:51 EDT) spent 3,741 ms of 3,896 ms and 4,008 ms of
4,988 ms in one Postgres query, `repository_workload_names` on `fact_records`; the
remaining 155 ms and about 980 ms are the coverage read (11 and 48 ms) and the
graph and other reads, which this note does not break down further.

Statement timings on the ops-qa physical reader (read-only session, 15 s statement
timeout, `PREPARE` plus `EXPLAIN (ANALYZE, BUFFERS)`, one warm-up round then four
rounds with alternating order): the names read this change removes from context took
a median 842.4 ms (834.5 to 850.7) on the 12,403-file repository and 3,805.3 ms
(3,794.6 to 3,837.7) on the 7,097-file repository; the retained reads took
0.87 and 0.88 ms (scope read, five runs with the first discarded), 1.3 and 1.8 ms
(platform) and 0.1 ms (dependency), about 2.3 and 2.8 ms in total. The earlier
section's 0.092 s and 0.126 s for the required count reads were one-snapshot samples
taken on 3 October on the replica with an unrecorded cache state; I did not
reproduce them (today's warm figures are 40 to 50 times lower), and both sets are
far below the removed names read and are not endpoint latency. The reader's cache had warmed
since its restart at 19:58Z, so these are warm-cache figures; planner statistics for
`fact_records` were not recorded. The statement times add up to the trace cost but
are not endpoint latency: the endpoint figure for the image that carries this
change stays NOT_CHECKED until it is deployed and replayed.
