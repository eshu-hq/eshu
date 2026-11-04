# Canonical terraform-backends reader proof (#8875 lane 1)

## Question

The canonical terraform-backends reader swept ~200,000 rows per match
(3.0M shared-buffer hits, 15.3 s warm) at production scale. The lane-1
change adds the predicate-matched partial index
`fact_records_terraform_backends_active_idx` (migration 273) and marks the
`repo_local_paths` CTE `AS MATERIALIZED`. This note records the
before/after evidence and the contribution split.

## Fixture A/B (reproduced 2026-11-03)

Performance Evidence: 2x2 EXPLAIN (ANALYZE, BUFFERS) on a scratch
PostgreSQL 18.6 database (local Compose host, `pg_dump --schema-only`
of `ingestion_scopes`, `scope_generations`, `fact_records` plus manual
fixture seed: 2 active generations, 3,404 file facts with 3,400 decoys,
ANALYZE before each run; old-SQL variant derived from the shipped
constant by removing only the MATERIALIZED keyword; index toggled by
DROP/CREATE with the migration-273 predicate; 1 warmup + 3 measured
runs per cell, medians reported):

| SQL variant | index present | shared buffers (median) | exec ms (median) |
|---|---|---|---|
| old (CTE inlined) | yes | 163 | 2.68 |
| new (MATERIALIZED) | yes | 163 | 2.71 |
| old (CTE inlined) | no | 310 | 3.61 |
| new (MATERIALIZED) | no | 310 | 3.52 |

Row counts are byte-identical across all four cells (402 = 402); the
committed differential (`TestTF8875CanonicalRowDifferentialLive`)
asserts the exact 404-row full fixture plus both owners of a shared
locator hash and a plan that seeks the new index.

Contribution budget: the index arm accounts for the whole fixture-scale
win (310 to 163 buffers, -47%). MATERIALIZED contributes nothing at
402-outer-row fixture scale (identical buffers with and without it in
both index states), which is expected: the rescan it removes scales
with the outer-row count, and the production EXPLAIN that motivated it
shows 9,188 re-executions with 8,657,033 rows removed by the outer
join filter (recorded in the `backend_canonical.go` comment and the
migration-273 header). The deployed magnitude follows that live
attribution (5.9 s / 1.1M-hit `backend_generations` arm inside the
15.3 s statement); fixture timings are host-specific and are not a
deployed wall-time claim. Production re-measurement is follow-up work.

Why safe: the SQL delta is one keyword (`AS` to `AS MATERIALIZED`);
materializing a side-effect-free single-reference CTE cannot change
the row set, only the rescan. The index is partial to the exact CASE
predicate the served queries filter on, so it carries B-tree entries
only for backends-bearing rows; write cost is one predicate evaluation
per ingested file fact. Exactness is gated, not argued: the derived
COUNT probe is built from the shipped constant (never hand-copied)
with a byte-prefix guard, and the differential runs green on a fresh
bootstrap.

## Observability

Observability Evidence: the lane-1 change itself added no signal (see the
No-Observability-Change record at merge). The follow-up sensor PR adds one:
`eshu_dp_terraform_backend_canonical_read_duration_seconds` (labels
read=canonical|filtered, outcome=success|error), emitted once per
`ListTerraformBackendsByLocator` call and once per
`filteredTerraformStateCandidates` discovery call, with the coverage row in
`telemetry-coverage-reducer-domains.md` and the operator dashboard panel.
Lane-1 production proof is an absolute-budget check on that sensor, not a
before/after: the sensor did not exist before the index, so no
pre-index histogram comparison is possible.

No-Regression Evidence: the sensor PR changes no SQL text, plan shape, or
read contract. The diff carries zero hunks inside any query constant; the
adapters gain only a clock read, a span label, and one SDK
histogram record per call on a seconds-scale path, with nil-safe branches
that keep unwired callers behavior-identical. Proof is the green unit
suites at the sensor head (`go test ./internal/telemetry/`
`./internal/storage/postgres/tfstate/`, RED-first registration and
emission tests included) plus `go vet ./...` and `go build ./...` clean.
No slow-path benchmark was run because there is no slow path to compare:
the timed statements are byte-identical before and after.

## Round-2 S1: outer-leg partial index (migration 276)

Performance Evidence: fixture A/B on a disposable PostgreSQL 18 database
(same tf8875 shape: 2 scopes, 2 active generations, 3,410 file facts with
3,400 decoys, ANALYZE before each run; probe is the byte-exact derived
COUNT of the shipped constant; migration-273 index kept throughout;
3 measured runs per shape):

| Shape | shared buffers | exec ms |
|---|---|---|
| Baseline (273 only) | 1075 | 2.62 |
| S1: partial btree on (scope, generation, repo_id expr) WHERE file/git AND 3-CASE-sum>0 | 439 (-59%) | 1.44 (-45%) |
| S2: S1 + INCLUDE (payload) | 514 | 1.46 |
| S3: CASE-sum expression index | 459 | 1.40 |

Row counts are byte-identical across shapes (404) with both owners of the
shared locator hash surfacing in order. S2 loses on payload bloat with no
index-only plan (observed_at/fact_id absent); S3 matches S1 on ms but not
on buffers. Stop rule met at S1. Fixture timings are host-specific and
are not a deployed wall-time claim; the deployed magnitude follows the
live attribution (outer leg re-reading ~55k heap rows per call on the
production EXPLAIN) and the sensor week-over-week after rollout.

Why safe: the served query text is byte-identical, so the row set cannot
change; the predicate mirrors the outer filter modulo table alias
(verified by normalized comparison in review); the build is a single
CREATE INDEX CONCURRENTLY, rerun-safe, with rollback as a matching DROP;
write cost is one predicate evaluation per ingested file fact with
entries only for rows the outer leg can return. Exactness is gated by
the RED-to-green differential plus the pg_indexes assertion, both green
against a fully migrated disposable database.

No-Regression Evidence: the committed differential
(TestTF8875CanonicalOuterLegExactnessDifferential) plus the row
differential (TestTF8875CanonicalRowDifferentialLive, count 404 and
2-owner order) run green DSN-backed; the hermetic derivation guards
run green everywhere. No query constant changes, so no plan shape can
regress except toward the new index.

No-Observability-Change: S1 adds no signal. The existing
eshu_dp_terraform_backend_canonical_read_duration_seconds histogram
covers the reader; the week-over-week sensor comparison after deploy
is the production confirmation.
