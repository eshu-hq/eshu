<!-- docs-catalog
title: Scale And Performance Mechanisms
description: The query and planner mechanisms behind the measured read latency on Eshu routes, with statement-level timings and their limits.
type: reference
audience: practitioner, maintainer
entrypoint: false
landing: false
-->

# Scale and performance mechanisms

This page holds the statement-level timings and mechanisms behind the figures
in the [Scale and performance reference](scale-and-performance-measurements.md).
Statement times are not endpoint latency. The limits in that page's
[Open limits](scale-and-performance-measurements.md#open-limits) apply here.

Each mechanism below has a short explanation and a pointer to its evidence note.

## Planner statistics flip a plan

The scoped, single-term code-topic read feeds `pre-change`,
`developer-change-plan`, and the MCP planning tools. It filters by trigram
indexes on entity names and cached source.

Postgres estimates each trigram match from a sampled histogram. For a common
term such as `decode`, about 1% of 2.67 million rows match. Each `ANALYZE` can
land the estimate on either side of the planner's cost threshold. The sources
give no odds for either side.

On one side the planner skips the repository bitmap. It then reads about 22,000
heap blocks for 1,680 rows, in 1.25 to 1.27 s. On the other side it combines the
repository bitmap and finishes in 0.3 to 0.6 s. The age of the statistics does
not decide the outcome. A bad-regime read occurred minutes after an analyze, at
10:25 UTC, 3 minutes after the 10:22 autoanalyze in the table below.

See `docs/internal/evidence/7246-code-topic-scoped-one-term.md`.

## A materialized CTE hides the literal

The fix marks the statement's `terms` CTE as `MATERIALIZED` for the scoped
one-term shape. The planner then estimates the probe without the literal term.
The plan stops depending on the trigram estimates. Rows are byte-identical in
the 13 measured cases.

The numbers come from read-only statements on the shared QA reader. The three
rows with n=6 each ran one warm-up pair, then six interleaved pairs, in one
session. The first two `decode` rows are ad hoc pairs with two observations per
variant. The bad regime cannot be forced from a read-only session. The planner
state is the autoanalyze time in the second column.

| Case | Planner statistics | Base | Hidden term | n |
| --- | --- | --- | --- | --- |
| `decode`, 10:20 UTC | autoanalyze 10:06, slow plan | 1,250 and 1,273 ms | 897 and 611 ms | 2 each, ad hoc |
| `decode`, 10:25 UTC | autoanalyze 10:22, slow plan | 2,633 (outlier) and 1,265 ms | 601 and 613 ms | 2 each, ad hoc |
| `decode`, about 10:50 UTC | autoanalyze 10:34, fast plan | median 317 ms | median 322 ms | 6 each |
| `showimage` | not recorded | median 101 ms | median 53 ms | 6 each |
| `loadimage` | not recorded | median 673 ms | median 559 ms | 6 each |

Host load was not stated. Reader concurrency was one to four active sessions
per case.

The change has a cost. A term with no corpus hits cannot benefit, and the
repository bitmap is paid anyway.

| Repository (entities) | Base median | Hidden-term median | Delta |
| --- | --- | --- | --- |
| 241,726 | 10.4 ms | 42.0 ms | +31.6 ms |
| 132,715 | 8.7 ms | 25.8 ms | +17.1 ms |
| 21,824 | 10.1 ms | 10.4 ms | +0.3 ms |

Planner statistics were not recorded for the rare-term cases.

A term with no run of three ASCII letters or digits has no trigram to filter on.
Hiding such a term cost 3.6 to 4 times more (n=1 for the hidden case). The
change keeps the plain statement for those terms. A `db_` read on the
241,726-entity repository took 4,133 and 4,577 ms plain and 16,722 ms hidden.
Planner statistics were not recorded. Those terms still take 3 to 4 s on the
plain statement. The change does not address that.

## Node-label tests before `labels()`

The outgoing traversal behind `change-surface` enumerates every path of up to
four hops from the repository. It then ran six `'X' IN labels(impacted)` tests
on each path. For the 12,402-file repository that is 263,186 paths. The
`labels()` filter cost 1,842,302 of 2,251,956 database hits, and the answer was
zero rows.

The fix adds a node-label test (`impacted:Label`) ahead of the `labels()` terms.
On Neo4j 2026.08.1 the profile shows the label test dropping non-matching
paths before the `labels()` calls run. That is an observation on one version,
not a claim about the planner. The change returns the same rows. The `labels()`
terms stay because NornicDB v1.3.3 ignores a label test in the `WHERE` of a
relationship `MATCH`.

These are server-side statement times on Neo4j, n=6 per case, median shown.
Host load on the driving machine was 15 to 41. Planner statistics are not
applicable (graph read). This table uses the source's counts of 12,402 and
7,096 files.

| Repository (files) | Whitelisted paths | Before | After | Faster |
| --- | --- | --- | --- | --- |
| 12,402 | 0 | 552 ms | 145 ms | 3.80x |
| 7,096 | 0 | 306 ms | 89 ms | 3.44x |
| 1,053 | 48,152 | 3,833 ms | 825 ms | 4.65x |
| 369 | 116,510 | 12,444 ms | 2,618 ms | 4.75x |

Nine repositories showed 3.4x to 4.8x. The path sets and the ordered 11-row
outputs were identical. The 369-file repository is a rows-returning case with
a large fan-out. At 12.4 s before, it was past the 10 s graph-read deadline.
After the fix it takes 2.6 s. The expansion itself is unchanged, and it is not
within the argument sets of the original sweep.

Fully whitelisted `CloudResource` anchors pay about 7% more database hits. The
time was under the 1 ms timer resolution.

See `docs/internal/evidence/7246-change-surface-label-test.md`.

## A removed names read replaced by a count

Repository context loaded workload display names from `fact_records`. Those
names are identity hints from reducer intents. They are not materialized
workloads, so they are not a count. The route reports the distinct `Workload`
nodes reached through `DEFINES` edges. After the count-port fix (#7654) it reads
only the platform and dependency count scalars and no longer loads the names.
Story and entity reads still load them.

The names read dominated the slow traces. In the two slowest traces it took
3,741 ms of 3,896 ms and 4,008 ms of 4,988 ms.

Statement timings on the shared QA reader are read-only. The session ran one
warm-up round and four rounds with alternating order. Planner statistics for
`fact_records` were not recorded. The reader's cache was warm. Host load was
not stated.

| Repository (files) | Removed names read (median, range) | Remaining reads, total |
| --- | --- | --- |
| 12,403 | 842.4 ms (834.5 to 850.7) | about 2.3 ms |
| 7,097 | 3,805.3 ms (3,794.6 to 3,837.7) | about 2.8 ms |

These are statement times, not endpoint latency. See
`docs/internal/evidence/7542-materialized-repository-workload-count.md`.

## Buffer-cache first touch

Source: #7250 comment of 2026-09-27 17:34 UTC and #7542 comment of 2026-10-07
15:03 UTC.

In two statements the first run was slower than later runs. Only the first row
shows an order-of-magnitude gap. The first run reads pages from disk. Later runs
find them in the database cache. Planner statistics were not recorded for
either row.

| Statement | First run | Later runs | n | Host load |
| --- | --- | --- | --- | --- |
| `content_files` 5,000-row list, 12,403-file repository | 2,571.311 ms (4,207 shared reads) | 35.920 and 15.689 ms | 3 | not stated |
| `list_repo_entities_by_types`, two repositories | 45 and 58 ms (about 2,000 shared reads) | 32 to 50 ms | 1 first, then repeats | 3.5 |

The second row did not reproduce the 354 ms and 259 ms spans seen in the
route. Its first and later runs overlap. It does not support the size of the
effect.

In the repository-context route the first two calls of run 1 took 354 ms and
259 ms in `list_repo_entities_by_types`. The other 20 calls took 7 to 109 ms.
The plan was the same on every run. The evidence is consistent with cache
misses, made larger by a host load of 12.2. A controlled cold-cache run has not
proven it.

A pod restart does not make the database cache cold. The database process keeps
its cache across an API or MCP pod restart. A database restart empties
`shared_buffers`. The operating-system cache and autoprewarm can refill it.

## Bounded reads and truth markers

Most fixes below change what the response says, not how fast it runs. The
third bullet is a speed change.

- The CI/CD static workflow summary reads the first 5,000 repository files in
  path order. A full page now reports `candidate_pool_status=unknown_at_limit`.
  Zero workloads at the limit means `state=unknown`, not absence. The SQL,
  order, and limit did not change. See
  `docs/internal/evidence/7250-capped-workflow-coverage.md`.
- Change-planning responses report known topic-pool caps. The pool holds up to
  4,000 rows. An empty page at a nonzero offset reports
  `candidate_pool_status=unknown_empty_page`. See
  `docs/internal/evidence/7246-change-planning-coverage-truth.md`.
- On Neo4j the graph-summary packet groups and ranks `CALLS` edges in the
  database. It keeps the 50,000-edge fail-closed result. A read-only profile on
  a 45,495-function graph took 632 and 680 ms before and 458 and 377 ms after
  (n=2 each). Database hits fell from 878,988 to 550,060. Host load was not
  stated. Planner statistics are not applicable to this graph read. See
  `docs/internal/evidence/7251-neo4j-graph-summary-degree.md`.
