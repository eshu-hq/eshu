<!-- docs-catalog
title: Scale And Performance Reference
description: Measured query latency on Eshu read routes at scale, with host load, process state, and planner state beside each figure, and the limits of the evidence.
type: reference
audience: practitioner, maintainer
entrypoint: false
landing: false
-->

# Scale and performance reference

This page records how query latency on Eshu read routes behaves on a large
indexed corpus. It lists the measured figures and the limits of the evidence.
The [mechanisms page](scale-and-performance-mechanisms.md) explains the causes.

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
| Cold or warm | Cold is the first call of a sweep, on argument set 0. Warm is a later call. |
| n | The number of samples behind the figure. |
| Host load | The load note the source recorded, or "not stated". |
| Planner statistics | The state the source recorded for the Postgres planner. |

A cold call is not always the first call on a fresh process. Each section says
whether the process was fresh: yes, no, or not stated. A fresh process had
served no earlier business request.

The planner-statistics column uses these values.

- **Autoanalyze time**: the source gave the last autoanalyze time and the
  changes since. The row says which plan followed.
- **Estimate off**: the source later showed that the planner's row estimate
  was far from the real count. This describes the run. It is not a cause.
- **Not recorded**: the source did not state the planner-statistics state.

The age of the statistics does not predict the plan. See
[Planner statistics flip a plan](scale-and-performance-mechanisms.md#planner-statistics-flip-a-plan).

Graph reads run on Neo4j. Postgres planner statistics do not affect them. Those
rows say "not applicable (graph read)".

Some routes mix graph reads and Postgres reads. Their rows say "not recorded"
unless the source states a value.

The environment is a shared QA environment with a large indexed corpus. A
repository is named by its file count only. Two repositories appear under two
counts each: 12,402 and 12,403 files, and 7,096 and 7,097 files. The sources do
not explain the one-file difference. This page keeps each source's count.

Each section names its source by issue and comment date (UTC). Issue comments
can be edited, so the dates identify the version read.

## Baseline sweep

Source: the bodies of #7246, #7250, and #7251 (2026-09-26). Process fresh: not
stated.

The baseline sweep ran on 2026-09-26, after a read-path build with the
call-graph backlog fully drained. Each route got one cold call on the largest
repository. Then it got ten warm calls across eight real argument sets.

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

Source: #7246 comments of 2026-10-05 18:21 UTC (first replay) and 19:36 UTC
(second replay). Process fresh: yes for the first replay (each route counter
read 0 before and 11 after on both pods). No for the second replay, which used
the same pods about 75 minutes later. It lists warm p95 only.

The next table shows the first replay after the topic-search fix (#7617). Four
routes were still over 1 s at warm p95 on the 7,097-file repository. The source
first blamed stale planner statistics. The later evidence note shows that either
statistics age can produce either plan. See
[Planner statistics flip a plan](scale-and-performance-mechanisms.md#planner-statistics-flip-a-plan).

| Route | Cold (n=1) | Warm p95 (n=10) | Host load | Planner statistics |
| --- | --- | --- | --- | --- |
| `POST /api/v0/impact/developer-change-plan` | 0.499 s | 2.192 s | control probe 0.09 to 0.45 s early | not recorded; estimate off |
| `POST /api/v0/impact/pre-change` | 0.699 s | 1.158 s | same | same |
| MCP `analyze_pre_change_impact` | 0.228 s | 1.286 s | same | same |
| MCP `plan_developer_change` | 0.283 s | 1.184 s | same | same |

For the term `decode`, the estimate was about 1,000 trigram matches against a
real count of about 25,000. The source found this after the run.

About 75 minutes later the same sets ran again. The `content_entities` table
had an autoanalyze at 19:31 UTC in between. No code changed.

| Route | Warm p95 (n=10) | Host load | Planner statistics |
| --- | --- | --- | --- |
| `POST /api/v0/impact/developer-change-plan` | 0.513 s | control probe 0.087 to 0.090 s | autoanalyze 19:31 UTC; good plan |
| `POST /api/v0/impact/pre-change` | 0.507 s | same | same |
| MCP `analyze_pre_change_impact` | 0.452 s | same | same |
| MCP `plan_developer_change` | 0.490 s | same | same |

Both runs used the original saved argument sets. The control probe is a
`/health` call used as a load check.

The same two sweeps had other routes at or over 1 s. The `change-surface` rows
are graph reads, so planner statistics do not apply. The source did not record
planner statistics for `deployment-config-influence`.

| Route and replay | Cold | Warm p95 |
| --- | --- | --- |
| `POST /api/v0/impact/change-surface`, first replay | 1.0003 s | 0.926 s |
| `POST /api/v0/impact/deployment-config-influence`, first replay | 1.255 s | 0.718 s |
| MCP `find_change_surface`, first replay | 0.943 s | 0.951 s |
| `POST /api/v0/impact/change-surface`, second replay | 1.373 s | 1.387 s |
| MCP `find_change_surface`, second replay | 1.159 s | 1.013 s |
| `POST /api/v0/impact/deployment-config-influence`, second replay | not listed | 0.758 s |

The `change-surface` traversal is a graph read. The label fix addresses it. See
[Node-label tests before `labels()`](scale-and-performance-mechanisms.md#node-label-tests-before-labels).

## Replay on a fresh process

Source: #7246 comment of 2026-10-06 17:55 UTC. Process fresh: yes.

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
`decode` on the 7,097-file repository. The hinted statement ran in 327 and
329 ms. The unhinted statement ran in 959 and 1,588 ms (n=2 each, read-only,
same reader).

## Change-surface after the label fix

Source: #7246 comment of 2026-10-06 11:59 UTC. Process fresh: run 1 ran on pods
that started at 10:16 UTC, and the source calls its cold call a fresh-pod call.
It states no route counter. No for run 2: it ran about 20 minutes later on the
same pods, so its set-0 call is not process-cold.

The change-surface fix (#7631) was deployed before the replay above. These two
runs came from that deployment.

| Route and run | Cold (n=1) | Warm p95 (n=10) | Host load | Planner statistics |
| --- | --- | --- | --- | --- |
| `POST /api/v0/impact/change-surface`, run 1 | 0.460 s | 0.522 s | not stated | not applicable (graph read) |
| `POST /api/v0/impact/change-surface`, run 2 | 0.452 s | 1.134 s | control probe 0.24 to 0.31 s | not applicable (graph read) |
| MCP `find_change_surface`, run 1 | 0.478 s | 0.464 s | not stated | not applicable (graph read) |
| MCP `find_change_surface`, run 2 | 0.454 s | 0.440 s | control probe 0.24 to 0.31 s | not applicable (graph read) |

In run 2 one sample took 1.134 s. The other nine took 0.23 to 0.55 s. The
client link was degraded in that run.

## Graph summary

Source: #7251 comments of 2026-09-29 18:51, 2026-10-02 16:31, 2026-10-04 20:40,
2026-10-05 19:10, 2026-10-06 20:09, 2026-10-07 12:44, and 2026-10-07 17:01 UTC.

The graph-summary route reads from Neo4j and Postgres. Rows are grouped by
cohort. Each cohort is a different deployed build. The sources do not pool
cohorts across builds. Host load was not stated, except for epoch 2 of cohort 6
(load 7.2). Process fresh: "cold, process-first" rows are yes. The sources
checked route counters before and after. "First observed" rows are not
independently cold, so the source does not call them process-cold.

| Cohort (date UTC), surface, kind | Value | n | Planner statistics |
| --- | --- | --- | --- |
| 1 (2026-09-29), API, first observed | 1.0922 s | 1 | not recorded |
| 1 (2026-09-29), API, warm p95 | 0.8161 s | 10 | not recorded |
| 1 (2026-09-29), MCP, first observed | 0.6843 s | 1 | not recorded |
| 1 (2026-09-29), MCP, warm p95 | 0.7753 s | 10 | not recorded |
| 2 (2026-10-02), API, first observed | 0.769 s | 1 | not recorded |
| 2 (2026-10-02), API, warm p95 | 0.559 s | 10 | not recorded |
| 2 (2026-10-02), MCP, first observed | 0.559 s | 1 | not recorded |
| 2 (2026-10-02), MCP, warm p95 | 0.552 s | 10 | not recorded |
| 3 (2026-10-04), API, warm p95 | 0.667 s | 20 | not recorded |
| 3 (2026-10-04), MCP, warm p95 | 0.652 s | 20 | not recorded |
| 4 (2026-10-05), API, cold, process-first | 0.758 s | 1 | not recorded |
| 4 (2026-10-05), MCP, cold, process-first | 0.564 s | 1 | not recorded |
| 5 (2026-10-06), API, cold, process-first | 1.0006 s | 1 | not recorded, not controlled |
| 5 (2026-10-06), MCP, cold, process-first | 0.581 s | 1 | not recorded, not controlled |
| 6 (2026-10-07), epoch 1, API, cold, process-first | 0.857 s | 1 | not recorded |
| 6 (2026-10-07), epoch 1, MCP, cold, process-first | 0.649 s | 1 | not recorded |
| 6 (2026-10-07), epoch 2, MCP, cold, process-first | 0.649 s | 1 | not recorded |
| 6 (2026-10-07), epoch 2, API, cold, process-first | 0.567 s | 1 | not recorded |

Surfaces: API is `POST /api/v0/ecosystem/graph-summary`. MCP is
`get_graph_summary_packet`.

Five notes on this table.

- The 1.0006 s API value is 0.6 ms over the 1 s line. Its server-side time was
  0.895 s.
- The Postgres reader had been restarted before that sample. Its
  `shared_buffers` started empty.
- In cohorts 4 to 6, epoch 1, the MCP sample ran about 15 s after the API
  sample. The shared backends were already warm, so those MCP values likely run
  low.
- In epoch 2 of cohort 6 the order reversed. MCP ran first at 0.649 s. API ran
  second, 11 s later, at 0.567 s. The two epochs used the same build after one
  rollout restart.
- Cohorts 1 to 5 have n=1 per surface. Cohort 6 has n=2 per surface (one per
  epoch). None of these rows is a cold p95.

## Repository context

Source: #7542 comments of 2026-10-06 21:53, 2026-10-07 12:44, 2026-10-07 13:16,
2026-10-07 15:03, and 2026-10-07 17:01 UTC.

The repository-context route (`GET /api/v0/repositories/{repo_id}/context` and
MCP `get_repo_context`) reads a workload count. Before the count-port fix
(#7654), it also ran a Postgres names read. The table shows two builds and four
runs: one before the fix and three after it.

| Route and run | First call (n=1) | Warm p50 | Warm p95 (n=10) | Host load | Planner statistics |
| --- | --- | --- | --- | --- | --- |
| API context, before fix, warm pods | 2.828 s | 0.421 s | 5.109 s | 5.9 at start | not recorded |
| MCP `get_repo_context`, before fix, warm pods | 1.139 s | 0.311 s | 4.150 s | 5.9 at start | not recorded |
| API context, after fix, run 1, fresh pods | 1.265 s | 0.250 s | 1.413 s | 12.2 at start | not recorded |
| MCP `get_repo_context`, after fix, run 1 | 0.264 s | 0.166 s | 0.261 s | 12.2 at start | not recorded |
| API context, after fix, run 2, warm pods | 0.601 s | 0.389 s | 0.847 s | 4.5 at start | not recorded |
| MCP `get_repo_context`, after fix, run 2 | 0.459 s | 0.208 s | 0.401 s | 4.5 at start | not recorded |
| API context, after fix, run 3, fresh pods | 0.463 s | 0.207 s | 0.371 s | 6.7 at start | not recorded |
| MCP `get_repo_context`, after fix, run 3 | 0.273 s | 0.180 s | 0.269 s | 6.7 at start | not recorded |

In the after-fix runs the control probe stayed near 0.09 s (0.085 s in run 3).
In the before-fix run it read 0.106 to 0.117 s. The before-fix pods were already
warm, so that first call is not a fresh-process sample. The MCP first call in
run 1 was not process-cold either, because the API and MCP wrappers had already
run on those pods. In run 2 the pods were warm.

Run 3 followed one rollout restart of the API and MCP pods. A one-call
graph-summary wrapper ran on those pods first, so the first calls in run 3 are
not process-cold. The database cache was not cold. The database primary had
restarted earlier and autoprewarm had run, and a pod restart does not clear the
cache. The 5-minute and 15-minute load averages were still about 14 to 16.
Traces showed `repository_context_counts` spans and no
`repository_workload_names` span.

The run 1 warm p95 of 1.413 s was the first call on the 7,097-file repository.
The second visit to that repository took 0.250 s. Run 3 did not reproduce the
slow first call. At a start load of 6.7, the first call on the 12,403-file
repository took 0.463 s. See
[Buffer-cache first touch](scale-and-performance-mechanisms.md#buffer-cache-first-touch).

### Local fixture comparison

A separate comparison ran built API and MCP processes against isolated local
stores. The stores held a sparse synthetic graph, not the production corpus. It
made 96 sequential requests in an ABBA order. Planner statistics were not
recorded. Host load was not stated for the request timings.

| Repository and surface | Baseline warm p95 | Candidate warm p95 | n |
| --- | --- | --- | --- |
| 12,403 files, API | 58.500 ms | 45.787 ms | 10 |
| 12,403 files, MCP | 66.350 ms | 43.331 ms | 10 |
| 7,097 files, API | 27.746 ms | 21.690 ms | 10 |
| 7,097 files, MCP | 21.636 ms | 21.734 ms | 10 |

The source calls the round drift substantial. It does not claim a speedup. The
added graph read shows in the `summary_counts` stage as a warm median of about
0.9 ms, against about 0.015 ms before.

## Mechanisms and statement timings

The mechanisms, statement-level timings, and truth-marker fixes live on
[Scale and performance mechanisms](scale-and-performance-mechanisms.md).

## Open limits

Read the figures with these limits in mind.

- **n=10 per route is a spot check.** It is not a distribution.
- **The p95 equals the maximum at n=10.** One slow sample sets the value.
- **Cold samples are n=1 per sweep.** A cold p95 is not established for any
  route. Cohorts for different builds are not pooled. Several cold values are
  not process-cold. The tables say which.
- **A pod restart does not make the database cache cold.** A cold process still
  reads from a warm database cache, unless the database itself restarted.
- **The page claims no acceptance against the 1 s target.** Per-route facts from
  the tables follow.
  - Baseline sweep: warm p95 was over 1 s on all 10 rows (1.04 to 3.75 s).
  - Change planning, first replay: warm p95 was over 1 s on four routes
    (1.158 to 2.192 s). The second replay and the fresh-process replay were
    under 1 s on those routes (0.404 to 0.642 s).
  - Change surface: the second change-planning replay was over 1 s (1.387 s for
    the API, 1.013 s for MCP). After the label fix, warm p95 was under 1 s
    except one sample on a degraded client link (1.134 s).
  - Graph summary: warm p95 was 0.552 to 0.816 s. First calls were 0.559 to
    1.0922 s. Two were over 1 s: 1.0922 s (cohort 1, first observed) and
    1.0006 s (cohort 5, restarted reader).
  - Repository context: warm p95 was 5.109 s (API) and 4.150 s (MCP) before the
    fix. After it, API run 1 was 1.413 s, over 1 s at host load 12.2. API runs
    2 and 3 and all three MCP runs were under 1 s. First calls were over 1 s
    before the fix (2.828 s and 1.139 s) and for API run 1 (1.265 s).
- **Planner statistics were often not recorded.** Where a table says "not
  recorded", the plan regime is unknown.
- **Host load varied.** Some runs started above the load limit of 9 that the
  sources set for timing runs. Repository context run 1 started at 12.2.
- **Statement times are not endpoint latency.** The statement tables on the
  [mechanisms page](scale-and-performance-mechanisms.md) measure the database
  call alone.
- **Some cases were not measured.** Examples are a common three-letter term such
  as `set`, and `TerraformModule` and `DataAsset` anchors.

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
| Sweep, replay, and cold-epoch figures | Comments on GitHub issues #7246, #7250, #7251, and #7542. Each section names its comments by date. |
