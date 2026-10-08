<!-- docs-catalog
title: Scale And Performance Reference
description: Measured query latency on Eshu read routes at scale, the mechanisms behind it, and the limits of the evidence.
type: reference
audience: practitioner, maintainer
entrypoint: false
landing: false
-->

# Scale and performance reference

This page records how query latency on Eshu read routes behaves on a large
indexed corpus. It lists the measured figures, the mechanisms behind them, and
the limits of the evidence.

It is a record of measurements, not a service-level promise. The targets live in
the [Performance SLO contract](performance-slo-contract.md). The slot taxonomy
lives in [Scale slots and the perf contract](scale-slots-and-perf-contract.md).

The target in this work is 1 second for both the first call and the warm 95th
percentile. The page does not claim acceptance against it. See
[Open limits](#open-limits).

## How to read the figures

Every latency figure in the tables carries the same fields.

| Field | Meaning |
| --- | --- |
| Route | The HTTP route or MCP tool that was called. |
| Cold or warm | Cold is the first call on a process. Warm is a later call. |
| n | The number of samples behind the figure. |
| Host load | The load note the source recorded, or "not stated". |
| Planner statistics | The state the source recorded for the Postgres planner. |

The planner-statistics column uses these values.

- **Fresh**: the source recorded an `ANALYZE` just before the run and a plan
  that matched real counts.
- **Stale**: the source later showed an out-of-date estimate for the run.
- **Autoanalyze time**: the source gave the last autoanalyze time and the
  changes since. The row says which plan regime followed.
- **Not recorded**: the source did not state the planner-statistics state.

Graph reads run on Neo4j. Postgres planner statistics do not affect them. Those
rows say "not applicable (graph read)".

Some routes mix graph reads and Postgres reads. Their rows say "not recorded"
unless the source states a value.

The environment is a shared QA environment with a large indexed corpus. A
repository is named by its file count only. Two sources count the same
repository as 12,402 and 12,403 files. This page keeps each source's count.

## Baseline sweep

The baseline sweep ran on 2026-09-26, after a read-path build with the
call-graph backlog fully drained. Each route got one cold call on the largest repository. Then it got
ten warm calls across eight real argument sets.

| Route | Cold | Warm p50 | Warm p95 (n=10) | Host load | Planner statistics |
| --- | --- | --- | --- | --- | --- |
| `POST /api/v0/impact/developer-change-plan` | 5.65 s | 0.80 s | 3.75 s | not stated | not recorded |
| MCP `analyze_pre_change_impact` | 5.17 s | 0.52 s | 3.65 s | not stated | not recorded |
| MCP `plan_developer_change` | 3.19 s | 0.22 s | 3.30 s | not stated | not recorded |
| `POST /api/v0/impact/pre-change` | 3.23 s | 0.20 s | 3.03 s | not stated | not recorded |
| `POST /api/v0/impact/deployment-config-influence` | 0.30 s | 0.10 s | 1.74 s | not stated | not recorded |
| `POST /api/v0/impact/change-surface` | 0.84 s | 0.16 s | 1.35 s | not stated | not recorded |
| MCP `find_change_surface` | 0.70 s | 0.12 s | 1.21 s | not stated | not recorded |
| `GET /api/v0/ci-cd/run-correlations` | 2.70 s | 0.11 s | 1.75 s | not stated | not recorded |
| `POST /api/v0/ecosystem/graph-summary` | 1.14 s | 0.10 s | 1.04 s | not stated | not recorded |
| MCP `get_graph_summary_packet` | 1.11 s | 0.10 s | 1.08 s | not stated | not recorded |

The cold value is n=1. The planning routes took 3.0 to 5.7 s on a 12,403-file
repository and 1.8 to 3.7 s on a 7,097-file repository. Small repositories were
fast. Cost followed repository size, not result size. `change-surface` took
1.2 to 1.35 s on the 12,403-file repository and returned 0 rows.

## Change-planning routes after the fixes

The next table shows the first replay after the topic-search fix. Four routes
were still slow on the 7,097-file repository. Later analysis tied the slowdown
to stale planner statistics. See [Planner statistics flip a plan](#planner-statistics-flip-a-plan).

| Route | Cold (n=1) | Warm p95 (n=10) | Host load | Planner statistics |
| --- | --- | --- | --- | --- |
| `POST /api/v0/impact/developer-change-plan` | 0.499 s | 2.192 s | control probe 0.09 to 0.45 s early | stale (inferred later) |
| `POST /api/v0/impact/pre-change` | 0.699 s | 1.158 s | same | stale (inferred later) |
| MCP `analyze_pre_change_impact` | 0.228 s | 1.286 s | same | stale (inferred later) |
| MCP `plan_developer_change` | 0.283 s | 1.184 s | same | stale (inferred later) |

About 75 minutes later the same sets ran again. The `content_entities` table
had an `ANALYZE` at 19:31 UTC in between. No code changed.

| Route | Warm p95 (n=10) | Host load | Planner statistics |
| --- | --- | --- | --- |
| `POST /api/v0/impact/developer-change-plan` | 0.513 s | control probe 0.087 to 0.090 s | fresh (autoanalyze 19:31 UTC) |
| `POST /api/v0/impact/pre-change` | 0.507 s | same | fresh |
| MCP `analyze_pre_change_impact` | 0.452 s | same | fresh |
| MCP `plan_developer_change` | 0.490 s | same | fresh |

Both runs used the original saved argument sets. The control probe is a
`/health` call used as a load check.

## Replay on a fresh process

This replay ran after the scoped code-topic fix (#7649) was deployed. The API
and MCP processes were fresh. The route counter read 0 before and 11 after on
every route, so each cold value is the first call on that process. All 99 calls
returned HTTP 200.

| Route | Cold (n=1) | Warm p50 | Warm p95 (n=10) |
| --- | --- | --- | --- |
| `POST /api/v0/impact/pre-change` | 0.155 s | 0.159 s | 0.447 s |
| `POST /api/v0/impact/developer-change-plan` | 0.224 s | 0.186 s | 0.594 s |
| MCP `analyze_pre_change_impact` | 0.193 s | 0.156 s | 0.642 s |
| MCP `plan_developer_change` | 0.164 s | 0.152 s | 0.404 s |
| `POST /api/v0/impact/change-surface` | 0.274 s | 0.133 s | 0.261 s |
| MCP `find_change_surface` | 0.258 s | 0.117 s | 0.257 s |
| `POST /api/v0/impact/deployment-config-influence` | 0.485 s | 0.287 s | 0.507 s |
| `GET /api/v0/ci-cd/run-correlations` | 0.153 s | 0.107 s | 0.142 s |
| MCP `list_ci_cd_run_correlations` | 0.119 s | 0.116 s | 0.357 s |

Host load for the whole table: 1-minute load average 6.8 at the start. The
control probe read 0.097 s before and after.

Planner statistics for the whole table, as autoanalyze time: `content_files` at
17:31 UTC with 9,645 changes since, and `content_entities` at 17:48 UTC with
36,522 changes since.

With those statistics the unhinted statement was in the slow plan for the term
`decode` on the 7,097-file repository. The hinted statement ran in 327 and 329 ms. The
unhinted statement ran in 959 and 1,588 ms (n=2 each, read-only, same reader).

## Change-surface after the label fix

The change-surface fix (#7631) was deployed before the replay above. These two
runs came from that deployment, with pods started 10:16 UTC. The runs were about
20 minutes apart.

| Route | Run | Cold (n=1) | Warm p95 (n=10) | Host load | Planner statistics |
| --- | --- | --- | --- | --- | --- |
| `POST /api/v0/impact/change-surface` | 1 | 0.460 s | 0.522 s | not stated | not applicable (graph read) |
| `POST /api/v0/impact/change-surface` | 2 | 0.452 s | 1.134 s | control probe 0.24 to 0.31 s | not applicable (graph read) |
| MCP `find_change_surface` | 1 | 0.478 s | 0.464 s | not stated | not applicable (graph read) |
| MCP `find_change_surface` | 2 | 0.454 s | 0.440 s | control probe 0.24 to 0.31 s | not applicable (graph read) |

In run 2 one sample took 1.134 s. The other nine took 0.23 to 0.55 s. The
client link was degraded in that run.

## Graph summary

The graph-summary route reads from Neo4j and Postgres. Rows are grouped by
date. Each date is a different deployed build. The sources do not pool cohorts
across builds. Host load was not stated for any row.

| Date (UTC) | Surface | Kind | Value | n | Planner statistics |
| --- | --- | --- | --- | --- | --- |
| 2026-09-29 | API `graph-summary` | first call | 1.0922 s | 1 | not recorded |
| 2026-09-29 | API `graph-summary` | warm p95 | 0.8161 s | 10 | not recorded |
| 2026-09-29 | MCP `get_graph_summary_packet` | first call | 0.6843 s | 1 | not recorded |
| 2026-09-29 | MCP `get_graph_summary_packet` | warm p95 | 0.7753 s | 10 | not recorded |
| 2026-10-02 | API `graph-summary` | first call | 0.769 s | 1 | not recorded |
| 2026-10-02 | API `graph-summary` | warm p95 | 0.559 s | 10 | not recorded |
| 2026-10-02 | MCP `get_graph_summary_packet` | first call | 0.559 s | 1 | not recorded |
| 2026-10-02 | MCP `get_graph_summary_packet` | warm p95 | 0.552 s | 10 | not recorded |
| 2026-10-04 | API `graph-summary` | warm p95 | 0.667 s | 20 | not recorded |
| 2026-10-04 | MCP `get_graph_summary_packet` | warm p95 | 0.652 s | 20 | not recorded |
| 2026-10-05 | API `graph-summary` | cold, process-first | 0.758 s | 1 | not recorded |
| 2026-10-05 | MCP `get_graph_summary_packet` | cold, process-first | 0.564 s | 1 | not recorded |
| 2026-10-06 | API `graph-summary` | cold, process-first | 1.0006 s | 1 | not recorded, not controlled |
| 2026-10-06 | MCP `get_graph_summary_packet` | cold, process-first | 0.581 s | 1 | not recorded, not controlled |
| 2026-10-07 | API `graph-summary` | cold, process-first | 0.857 s | 1 | not recorded |
| 2026-10-07 | MCP `get_graph_summary_packet` | cold, process-first | 0.649 s | 1 | not recorded |

Four notes on this table.

- The 1.0006 s API value is 0.6 ms over the 1 s line. Its server-side time was
  0.895 s.
- The Postgres reader had been restarted before that sample. Its buffer cache
  started empty.
- The MCP samples ran about 15 s after the API sample. The shared backends were
  already warm, so the MCP values likely run low.
- With n=1 per cohort, none of these rows is a cold p95.

## Repository context

The repository-context route (`GET /api/v0/repositories/{repo_id}/context` and
MCP `get_repo_context`) reads a workload count. Before the count-port fix (#7654),
it also ran a Postgres names read. The table shows two builds and two runs.

| Route | Build and run | First call (n=1) | Warm p50 | Warm p95 (n=10) | Host load | Planner statistics |
| --- | --- | --- | --- | --- | --- | --- |
| API context | before fix, warm pods | 2.828 s | 0.421 s | 5.109 s | 5.9 at start | not recorded |
| MCP `get_repo_context` | before fix, warm pods | 1.139 s | 0.311 s | 4.150 s | 5.9 at start | not recorded |
| API context | after fix, run 1, fresh pods | 1.265 s | 0.250 s | 1.413 s | 12.2 at start | not recorded |
| MCP `get_repo_context` | after fix, run 1 | 0.264 s | 0.166 s | 0.261 s | 12.2 at start | not recorded |
| API context | after fix, run 2, warm pods | 0.601 s | 0.389 s | 0.847 s | 4.5 at start | not recorded |
| MCP `get_repo_context` | after fix, run 2 | 0.459 s | 0.208 s | 0.401 s | 4.5 at start | not recorded |

In the after-fix runs the control probe stayed near 0.09 s. In the before-fix
run it read 0.106 to 0.117 s. The before-fix pods were already warm, so that
first call is not a fresh-process sample. The MCP first call in run 1 was not
process-cold either, because the API and MCP wrappers had already run on those
pods. In run 2 the pods were warm.

The run 1 warm p95 of 1.413 s was the first call on the 7,097-file repository.
The second visit to that repository took 0.250 s. See
[Buffer-cache first touch](#buffer-cache-first-touch).

### Local fixture comparison

A separate comparison ran built API and MCP processes against isolated local
stores. The stores held a sparse synthetic graph, not the production corpus. It
made 96 sequential requests in an ABBA order. Planner statistics were not
recorded. Host load was not stated for the request timings.

| File count | Surface | Baseline warm p95 | Candidate warm p95 | n |
| --- | --- | --- | --- | --- |
| 12,403 | API | 58.500 ms | 45.787 ms | 10 |
| 12,403 | MCP | 66.350 ms | 43.331 ms | 10 |
| 7,097 | API | 27.746 ms | 21.690 ms | 10 |
| 7,097 | MCP | 21.636 ms | 21.734 ms | 10 |

The source calls the round drift substantial. It does not claim a speedup. The
added graph read shows in the `summary_counts` stage as a warm median of about
0.9 ms, against about 0.015 ms before.

## Mechanisms

Each mechanism below has a short explanation and a pointer to its evidence note.

### Planner statistics flip a plan

The scoped, single-term code-topic read feeds `pre-change`,
`developer-change-plan`, and the MCP planning tools. It filters by trigram
indexes on entity names and cached source.

Postgres estimates each trigram match from a sampled histogram. For a common
term such as `decode`, about 1% of 2.67 million rows match. Each `ANALYZE` can
land the estimate on either side of the planner's cost threshold. This is a
coin flip.

On one side the planner skips the repository bitmap. It then reads about 22,000
heap blocks for 1,680 rows, in 1.25 to 1.27 s. On the other side it combines the
repository bitmap and finishes in 0.3 to 0.6 s. Neither fresh nor stale
statistics decide the outcome. The estimate can flip either way.

See `docs/internal/evidence/7246-code-topic-scoped-one-term.md`.

### A materialized CTE hides the literal

The fix marks the statement's `terms` CTE as `MATERIALIZED` for the scoped
one-term shape. The planner then estimates the probe without the literal term.
The plan stops depending on the trigram estimates. Rows are byte-identical in
the 13 measured cases.

The numbers come from read-only statements on the shared QA reader. Each case
ran as one warm-up pair, then six interleaved pairs, in one session. The
planner state is the autoanalyze time in the second column.

| Case | Planner statistics | Base | Hidden term | n |
| --- | --- | --- | --- | --- |
| `decode`, 10:20 UTC | autoanalyze 10:06, slow plan | 1,250 and 1,273 ms | 897 and 611 ms | 2 each |
| `decode`, 10:25 UTC | autoanalyze 10:22, slow plan | 2,633 and 1,265 ms | 601 and 613 ms | 2 each |
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

### Node-label tests before `labels()`

The outgoing traversal behind `change-surface` enumerates every path of up to
four hops from the repository. It then ran six `'X' IN labels(impacted)` tests
on each path. For the 12,402-file repository that is 263,186 paths. The
`labels()` filter cost 1,842,302 of 2,251,956 database hits, and the answer was
zero rows.

The fix adds a node-label test (`impacted:Label`) ahead of the `labels()` terms.
On Neo4j 2026.08.1 the profile shows the label test dropping non-matching
paths before the `labels()` calls run. That is an observation on one version,
not a claim about the planner. The change returns the same rows. The `labels()` terms stay
because NornicDB ignores a label test in this position.

These are server-side statement times on Neo4j, n=6 per case, median shown.
Host load on the driving machine was 15 to 41. Planner statistics are not
applicable (graph read).

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

### A removed names read replaced by a count

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

### Buffer-cache first touch

A repeated statement can run tens of times faster than its first run. The
first run reads pages from disk. Later runs find them in the database cache.

Two measurements show this. Planner statistics were not recorded for either.

| Statement | First run | Later runs | n | Host load |
| --- | --- | --- | --- | --- |
| `content_files` 5,000-row list, 12,403-file repository | 2,571.311 ms (4,207 shared reads) | 35.920 and 15.689 ms | 3 | not stated |
| `list_repo_entities_by_types`, two repositories | 45 and 58 ms (about 2,000 shared reads) | 32 to 50 ms | 1 first, then repeats | 3.5 |

In the repository-context route the first two calls of run 1 took 354 ms and
259 ms in `list_repo_entities_by_types`. The other 20 calls took 7 to 109 ms.
The plan was the same on every run. The evidence is consistent with cache
misses, made larger by a host load of 12.2. A controlled cold-cache run has not
proven it.

A pod restart does not make the database cache cold. The database process keeps
its cache across an API or MCP pod restart. A database restart empties it.

## Bounded reads and truth markers

Some fixes in this work change what the response says, not how fast it runs.

- The CI/CD static workflow summary reads the first 5,000 repository files in
  path order. A full page now reports `candidate_pool_status=unknown_at_limit`.
  Zero workloads at the limit means `state=unknown`, not absence. The SQL, order,
  and limit did not change. See
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

## Open limits

Read the figures with these limits in mind.

- **n=10 per route is a spot check.** It is not a distribution.
- **The p95 equals the maximum at n=10.** One slow sample sets the value.
- **Cold samples are n=1 per fresh process.** A cold p95 is not established for
  any route. Cohorts for different builds are not pooled.
- **A pod restart does not make the database cache cold.** A cold process still
  reads from a warm database cache, unless the database itself restarted.
- **The 1 s target is met on warm pods.** One run exceeded it on a degraded
  client link. The target is not yet shown for the first call on a fresh pod.
  The fresh-process replay is n=1 per route. Two first calls were over 1 s:
  repository context API at 1.265 s and graph-summary API at 1.0006 s.
- **Planner statistics were often not recorded.** Where a table says "not
  recorded", the plan regime is unknown.
- **Host load varied.** Some runs started above the load limit of 9 that the
  timing rule asks for.
- **Statement times are not endpoint latency.** The statement tables in
  [Mechanisms](#mechanisms) measure the database call alone.
- **Some cases were not measured.** Examples are a common three-letter term such
  as `set`, `TerraformModule` and `DataAsset` anchors, and a fresh-pod run at low
  host load for repository context.

This page does not claim acceptance against the 1 s target for any route.

## Evidence sources

| Topic | Source |
| --- | --- |
| Change-planning coverage truth | `docs/internal/evidence/7246-change-planning-coverage-truth.md` |
| Change-surface label test | `docs/internal/evidence/7246-change-surface-label-test.md` |
| Scoped one-term code topic | `docs/internal/evidence/7246-code-topic-scoped-one-term.md` |
| Capped workflow coverage | `docs/internal/evidence/7250-capped-workflow-coverage.md` |
| Graph-summary degree page | `docs/internal/evidence/7251-neo4j-graph-summary-degree.md` |
| Repository workload count | `docs/internal/evidence/7542-materialized-repository-workload-count.md` |
| Sweep, replay, and cold-epoch figures | Comments on GitHub issues #7246, #7250, #7251, and #7542 |
