# Scoped one-term code-topic read: hide the term from the entity probe (#7246)

The repository-scoped, single-term `investigate_code_topic` read
(`ContentReader.InvestigateCodeTopic`, `go/internal/query/content_reader_code_topic.go`)
feeds the `/api/v0/impact/pre-change`, `/developer-change-plan` and the MCP
`analyze_pre_change_impact` and `plan_developer_change` tools. For a corpus-common
term on a large repository the entity probe could pick a plan that skips
`content_entities_repo_idx` and reads about 22,000 heap blocks for 1,680 rows,
1.25 to 1.27 s inside Postgres. This change marks the statement's `terms` CTE
`MATERIALIZED` for that one shape, so the planner estimates the probe without the
literal. It changes no query semantics (same predicates, candidate cap, ordering,
limit and hydration); rows were byte-identical in the 13 measured cases, three of
which return no rows and one of which is capped. Which candidates fill a capped
pool was already plan dependent and stays so. It is a statement-level change, and
it does not by itself close #7246.

Performance Evidence: statement `scoped_one_term` (`$1` repository id, `$2` term,
limit, offset), backend PostgreSQL on the ops-qa physical reader, run as
`PREPARE` plus `SET plan_cache_mode=force_custom_plan` plus
`EXPLAIN (ANALYZE, BUFFERS, TIMING) EXECUTE`, which mirrors pgx
`QueryExecModeCacheDescribe`. The baseline text is the statement the `origin/main`
builder emits and the fixed text is the branch builder's; they differ only by the
word `MATERIALIZED` on the terms CTE (both dumped by a throwaway test, not hand
copied). One warm-up pair then six interleaved pairs per case, alternating the
first mover, in one session per case; medians with min to max below. Rows are
byte-identical between base and fixed in all 13 cases, and `pool_truncated`
matches. Two baseline plan regimes were observed, and both are reported.

Observability Evidence: the `postgres.query` span of `InvestigateCodeTopic` now
carries `code_topic.scoped_one_term` (bool), true only for one repository, one
term and no language filter. It is documented in
`docs/public/reference/telemetry/code-topic-probes.md` and pinned by
`TestInvestigateCodeTopicSpanMarksScopedOneTermShape`. An operator reading a slow
trace can tell which statement shape ran without reading SQL. No term,
repository id or SQL text is added.

## Why the plan flips

With a literal term, Postgres estimates each trigram predicate from the sampled
histogram bounds (about 99 usable bounds, so roughly 1% of 2.67 million rows per
bound). `decode` matches 1.0% of rows, so each ANALYZE of `content_entities` can
land the estimate on either side of the planner's cost threshold for ANDing the
repository bitmap. In the bad regime the entity probe is estimated at 85 to
97 rows, the planner skips the repository bitmap and the read takes 1.25 s. In
the good regime the planner ANDs the repository bitmap and the read takes 0.3 to
0.6 s. In the good regime measured here the `source_cache` trigram scan is still
estimated at 1,232 rows (1,504 in the bad regime); what differs is the
`name_trgm` scan, estimated at 23,074 rows against about 230, which pushes the
combined estimate over the threshold. Neither fresh nor stale statistics decide
it. A hidden term gets the planner's default match selectivity for both
predicates, so the plan no longer depends on those estimates. The statistics
refresh settings on the table are not changed here.

## Baseline regimes seen

| Time (UTC, 2026-10-06) | `content_entities` last autoanalyze | `decode` base | `decode` fixed | n per variant |
| --- | --- | --- | --- | --- |
| 10:20 | 10:06 | 1,250 and 1,273 ms | 897 and 611 ms | 2 each, ad hoc |
| 10:25 | 10:22 | 2,633 (outlier) and 1,265 ms | 601 and 613 ms | 2 each, ad hoc |
| about 10:50 | 10:34 | median 317 ms (302 to 567) | median 322 ms (305 to 563) | 6 interleaved |

The first two rows are the bad regime (base skips the repository bitmap; the
entity-heap scan is estimated at 85 to 97 rows). The third is the good regime
(base uses the repository bitmap on its own). The fix is about 2x faster in the
bad regime and neutral in the good one. The bad regime has only two observations
per variant, not six, because the regime cannot be forced from a read-only
session.

## Statement results (good regime for `decode`)

| Case | Class | Base median (range) | Fixed median (range) | Rows |
| --- | --- | --- | --- | --- |
| r_957cd853 `decode` L13 | winner class | 317.2 ms (301 to 567) | 321.8 ms (305 to 563) | 13 |
| r_957cd853 `decode` L15 | winner class | 305.6 ms (302 to 340) | 312.5 ms (307 to 346) | 15 |
| r_8946df89 `showimage` L10 | winner | 101.2 ms (97 to 104) | 53.4 ms (47 to 59) | 10 |
| r_8946df89 `showimage` L11 | winner | 101.4 ms (96 to 105) | 52.0 ms (52 to 55) | 11 |
| r_8946df89 `loadimage` L13 | winner | 672.5 ms (666 to 678) | 558.5 ms (554 to 562) | 13 |
| r_4507b513 `featurestab` L13 | neutral | 34.9 ms | 34.9 ms | 6 |
| r_4ff9d0b5 `checkprerequisites` L13 | neutral | 36.2 ms | 36.2 ms | 3 |
| r_2645123f `createnewversion` L13 | neutral | 50.6 ms | 50.3 ms | 4 |
| r_957cd853 `array` L13 | neutral, capped | 497.9 ms | 502.0 ms | 13, `pool_truncated` true on both |
| r_8946df89 `user` L13 | neutral | 812.2 ms | 815.2 ms | 13 |

Sets 2, 3 and 7 are excluded: they are not the scoped one-term shape.

## The cost: rare terms on large repositories

`zxcvbnm` has no corpus hits, so the hidden term cannot help and the repository
bitmap is paid anyway. This is the loser class and a bounded, accepted cost.

| Repository (entities) | Base median | Fixed median | Delta | `content_entities_repo_idx` node |
| --- | --- | --- | --- | --- |
| r_8946df89 (241,726, largest) | 10.4 ms | 42.0 ms | +31.6 ms | 34.7 ms |
| r_957cd853 (132,715) | 8.7 ms | 25.8 ms | +17.1 ms | 17.8 ms |
| r_0cfe3508 (21,824) | 10.1 ms | 10.4 ms | +0.3 ms | 1.6 ms |

Each delta equals its repository-bitmap node time within 10 ms, and every fixed
median is below the 100 ms ceiling set for this trade. Repositories larger than
the 241,726-entity one are extrapolated, not measured: the node cost scales at
about 0.14 ms per 1,000 repository entities.

## What this does not show

- Deployed endpoint latency. The statement is about 0.3 to 0.6 s in the fixed
  plan; about 0.47 s of the bad-regime fixed plan is the file content branch
  (`content_files_language_repo_idx` with a `content ILIKE` filter), a separate
  misestimate that this change does not touch. #7246 stays open for the deployed
  replay and for that next measured bottleneck.
- The `user` and `array` terms stay at 0.5 to 0.8 s in both variants; they are
  outside this change.
- Reader concurrency was one to four active sessions per case (recorded in the
  raw results), not a controlled idle reader.
- `SET STATISTICS` on the table or autovacuum tuning is not tried: it needs
  cluster DDL and stays estimate dependent.

## Reproduce

Raw plans and results are under `/tmp/eshu-7246-revive/bench/` on the
measuring machine (not committed). The statement text comes from
`codeTopicSQLFor` in `code_topic_entity_probe_test.go` run against `origin/main`
and the branch.
