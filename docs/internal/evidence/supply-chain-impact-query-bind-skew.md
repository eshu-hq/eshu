# Supply-chain impact query: bind list drifted from the SQL it feeds

Date: 2026-08-12

## What was broken

`TestSupplyChainImpactRuntimeFilterPlansLive` cannot run. With a live Postgres
DSN set, it dies on its first EXPLAIN:

```
supply_chain_impact_runtime_filter_plan_live_test.go:242: explain legacy_workload_scalar: pq: got 23 parameters but the statement requires 24
```

The failing side is the caller, not the SQL. `listSupplyChainImpactFindingsQuery`
legitimately needs `$24` — the store-bound clock that read-time suppression
expiry is evaluated against, pinned by
`TestSupplyChainImpactQueriesEvaluateSuppressionExpiryAtStatementTime`. The
production store binds it. The plan test did not, because it kept its own
hand-written copy of the argument list and that copy never picked up the
parameter.

Three copies had drifted the same way, each short by exactly the trailing
read-at clock:

| test helper | bound | query needs |
| --- | --- | --- |
| `supplyChainRuntimeFilterListArgs` | 23 | 24 (`$24::timestamptz`) |
| `supplyChainRuntimeFilterAggregateArgs` | 19 | 20 (`$20::timestamptz`) |
| `supplyChainRuntimeFilterExplainArgs` | 12 | 13 (`$13::timestamptz`) |

The inventory probe was worse in kind. It built its arguments as
`append(supplyChainRuntimeFilterAggregateArgs(filter), 10, 0)`, so the page
limit and offset landed on `$20` and `$21` when the query wanted them on `$21`
and `$22`. Adding a value to the end of that list — the obvious-looking fix —
would have produced a statement with the right *count* and an integer bound to
a `timestamptz` predicate.

## Which side was wrong, and why it matters

The caller. Deleting `$24` instead would undo read-time suppression expiry:
suppression decisions would stop being evaluated against the store's own clock,
which is the behavior that parameter exists to guarantee. Adding the bind keeps
that contract; removing the placeholder would trade a loud failure for a quiet
correctness regression.

## Was any path binding shifted arguments?

No production path was. All four production call sites — list, aggregate
count/priority/severity, inventory, explain — bind the full list in the right
order. Every drifted list lives in the plan test, and each is a clean truncation
of the production order rather than a reordering, so the mismatch surfaced as a
statement-level rejection instead of silently wrong rows.

The inventory probe is the one place where a shift was one step away, as
described above. It never executed, because the arity check rejects the
statement first.

## Second defect, previously masked

Once the arity error cleared, the inventory probe failed again:

```
supply_chain_impact_runtime_filter_plan_live_test.go:328: explain inventory_environment_high_cardinality: pq: column fact.payload does not exist
```

Same root cause, different input. The test hand-wrote the grouping expression as
`COALESCE(fact.payload->>'impact_status', 'unknown')`, but the `canonical_facts`
CTE it groups over projects `impact_status` as a plain column and never carries
`payload`. The store's own
`supplyChainImpactInventoryGroupExpression` returns
`COALESCE(NULLIF(fact.impact_status, ''), 'unknown')`. The probe had been
measuring a statement the store cannot issue. It only surfaced now because the
parameter error killed the test three probes earlier.

## What changed

Every argument list is now written down once, in the production file, and the
plan test calls it:

- `supplyChainImpactFindingListArgs` — `$1..$24`, shared by the direct and
  winners-backed list queries
- `supplyChainImpactAggregateArgs` — `$1..$20` of the shared canonical-facts
  CTE, previously repeated verbatim at four call sites
- `supplyChainImpactInventoryArgs` — the aggregate list plus `$21` limit and
  `$22` offset, so limit and offset can no longer be appended onto a short list
- `supplyChainImpactRuntimeContextArgs` — `$1..$4`
- `supplyChainImpactExplanationQueryArgs` — already existed; the test now uses
  it instead of its copy

The plan test also takes its inventory grouping expression from
`supplyChainImpactInventoryGroupExpression` rather than spelling it out.

## The coverage gap, which is the more useful half

This defect could sit on `main` indefinitely. The only test that would catch it
is gated on `ESHU_POSTGRES_TEST_DSN`, so CI skips it, and nothing else compares
a query against the arguments fed to it.

`TestSupplyChainImpactQueryPlaceholdersMatchBoundArguments` closes that. For
each of the nine queries in this family it takes the shipped SQL and the
production argument builder, and requires the highest `$N` in the SQL to equal
`len(args)` with `$1..$N` all present. It needs no database and runs in under a
second.

`TestSupplyChainImpactPlaceholderCoverageDetectsSkew` keeps that guard honest by
running the same check against the real list query with one argument too few and
one too many, and requiring both to be rejected.

## Evidence

Reproduction and proof ran against a throwaway `postgres:16` container on a
private port (15991), seeded with the repo's embedded bootstrap migrations.

| what | command | result |
| --- | --- | --- |
| reproduce, unmodified `origin/main` | `go test ./internal/query/ -run TestSupplyChainImpactRuntimeFilterPlansLive -count=1` with DSN | `pq: got 23 parameters but the statement requires 24` |
| same test, after the fix | same command | PASS, 20.30s, all 14 probes plan and execute |
| hermetic guard | `go test ./internal/query/ -run 'TestSupplyChainImpactQueryPlaceholders...' -count=1` | pass, 0.85s |
| guard catches a dropped bind | delete `readAt` from `supplyChainImpactFindingListArgs`, rerun | `query uses placeholders up to $24 but the caller binds 23 arguments` |
| package, credential-free | `go test ./internal/query/ -count=1` | pass, 4.25s |
| lint | `golangci-lint run ./internal/query/...` | 0 issues |
| package docs | `scripts/verify-package-docs.sh` | exit 0 |
| performance evidence | `ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main scripts/verify-performance-evidence.sh` | exit 0, no hot files changed |

Pre-existing on `origin/main`: reproduced on a clean worktree at
`4b29552a5`, and `go/internal/query/` is byte-identical at `40310537a`, the
current tip. Commit `39ca9f25e` re-added the entire tree on 2026-08-09, so
`git log` cannot date when the skew was introduced — only that it is there now
and was not introduced by this branch.

The passing run, probe by probe (EXPLAIN ANALYZE execution time against 300,000
seeded runtime facts, 500ms budget per probe):

| probe | execution |
| --- | --- |
| `runtime_context_200_candidates` | 134.9ms |
| `legacy_workload_scalar` | 270.8ms |
| `legacy_workload_entity_key` | 241.2ms |
| `legacy_service` | 21.4ms |
| `legacy_environment` | 27.1ms |
| `legacy_environment_high_cardinality` | 193.6ms |
| `legacy_combined` | 340.5ms |
| `winners_combined` | 345.0ms |
| `winners_environment_high_cardinality` | 193.6ms |
| `aggregate_combined` | 336.9ms |
| `aggregate_environment_high_cardinality` | 184.3ms |
| `inventory_environment_high_cardinality` | 180.6ms |
| `aggregate_no_runtime_filter` | 0.2ms |
| `explain_workload_service` | 308.8ms |

Read those timings with the host in mind. Other sessions were running
full-module Go builds and coverage sweeps on this machine for most of the
session, and the same `runtime_context_200_candidates` probe measured 134.9ms,
137.3ms, 244.9ms, 360.5ms, and 1205.1ms across runs of byte-identical code. Two
intermediate runs failed the 500ms budget purely on load, and one had its
300,000-row seed cancelled by the test's own three-minute context. The passing
run above was taken at 1-minute load average 17.9 — high, but the probes still
cleared. The numbers are good enough to show no plan regression; they are not
precise enough to compare against a stored baseline.

## A neighbouring live test that fails either way

`TestSupplyChainSuppressionPathsPerformanceLive` also fails on this host, and it
is worth writing down why it is not this change. It compares against a hardcoded
table of absolute millisecond baselines calibrated on some other machine, so a
Mac running Docker Postgres alongside other sessions' builds will not reproduce
them.

Run at comparable load (1-minute average 7.0-7.6), branch versus its unmodified
parent `40310537a`, same container, same seeded data:

| measurement | parent median | branch median | table baseline |
| --- | --- | --- | --- |
| `direct_list` | 187.6ms | 191.8ms | 177.2ms |
| `materialized_list` | 20.2ms | 20.3ms | 60.2ms |
| `aggregate_count_and_facets` | 454.8ms | 465.2ms | 465.1ms |

Both fail, on different measurements each run: the parent overshot
`winner_rebuild` (2720.2ms against a 912.1ms baseline, 3x), the branch overshot
`aggregate_count_and_facets` p95 (527.2ms against 468.0ms). The branch and
parent medians are within noise of each other on every path they both reached.
The failure belongs to the host, not to this diff.

No-Regression Evidence: no SQL text changed. The query constants
(`supply_chain_impact_findings_split_queries.go`,
`supply_chain_impact_findings_queries.go`,
`supply_chain_impact_aggregates_queries.go`,
`supply_chain_impact_runtime_filter_sql.go`) are untouched by this diff; only
the Go call sites that assemble the argument slice moved, and they pass the same
values in the same order. Plan shapes measured after the change match the
indexes the test already asserts:
`fact_records_workload_identity_workload_idx` on workload probes,
`fact_records_service_catalog_correlations_service_idx` on service,
`fact_records_ci_cd_run_correlations_environment_lookup_idx` on environment.

No-Observability-Change: no metric, span, log, or status field is added,
removed, or renamed. The operator-visible surface is the query error itself,
which the store already wraps (`list supply chain impact findings: %w`).
