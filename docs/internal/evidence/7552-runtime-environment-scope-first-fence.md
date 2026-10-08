# Runtime-Environment Evidence Scope-First Plan Evidence (#7552)

`selectSupplyChainImpactRuntimeEnvironmentEvidenceQuery`
(`go/internal/query/supply/chain/impact/runtime_environment_store.go`, served by
`GET /api/v0/supply-chain/impact/findings`, `.../impact/explain`, and their MCP
tools; 200 candidates max) joins each candidate's `fact_records` rows to
`ingestion_scopes` and `scope_generations` inside `CROSS JOIN LATERAL`.

## Classification: scope-first reproduces on populated, analyzed data

Fixture: local `postgres:18.6`, production schema via `ApplyBootstrap`,
901 active scope/generation pairs (900 seeded + the `eshu:global` seed pair),
200 candidate digests x 10 matching `reducer_ci_cd_run_correlation` facts each,
200,000 background facts, all three tables `ANALYZE`d. This exceeds ops-qa's
819 pairs from the issue.

Before (unfenced production query), `EXPLAIN (ANALYZE, BUFFERS)`, 3 runs:

- Plan: per candidate, hash the 901 scope pairs, then nested-loop probe
  `fact_records_scope_generation_keyset_idx` once per pair: 180,200 loops
  (200 x 901). The artifact index is not used.
- Execution: 21,850.7 / 21,187.4 / 21,175.9 ms (shared host; shape, not time,
  is the signal). Buffers: shared hit 41,188,757, read 2,460.
- The scope-pair hash join estimates rows=1 for 901 actual rows (901x
  misestimate, same defect as ops-qa's rows=1 for 819).

A concentrated variant (all facts in one scope) also plans scope-first:
180,200 artifact-index probes, ~1,009 ms, shared hit 545,817.

## Root cause

The planner derives transitively implied equalities between the two scope
tables from their fact joins and multiplies the two join-clause selectivities
assuming independence:

- `ingestion_scopes JOIN scope_generations ON generation_id =
  active_generation_id` alone estimates rows=901 (correct).
- Adding the implied second clause `scope_id = scope_id` drops the estimate
  to rows=1 (measured on the fixture).

The scope-first nest loop then looks cheaper than the fact-first probe.

Rejected: extended statistics. `CREATE STATISTICS (dependencies)` on
`ingestion_scopes(scope_id, active_generation_id)` and
`scope_generations(generation_id, scope_id)` plus `ANALYZE` leaves the
two-clause join estimate at rows=1: PostgreSQL does not apply functional
dependencies across join clauses.

## Fix: fenced fact probe

The fact-only predicates moved into an `OFFSET 0` subquery inside the lateral
leg, so the artifact-index probe stays on the outer side of the nest loop and
the scope tables are reached by indexed lookup per matched fact row. Same
placeholders ($1..$4), same predicates, no new index, no migration.

After (fenced query), same fixture, `EXPLAIN (ANALYZE, BUFFERS)`:

- Plan: `Bitmap Index Scan` on
  `fact_records_ci_cd_run_correlations_artifact_lookup_idx`, 200 loops (one
  per candidate), then `ingestion_scopes_active_generation_idx` and
  `scope_generations_active_scope_idx` lookups per matched row (2,000 loops).
- Execution 634.3 ms, shared hit 14,544, read 61 (same-host back-to-back with
  the 21.8 s before run; absolute time is host-bound, see
  `docs/internal/timing-proof-rules.md`).

Row-set differential: old vs new query over the 200-candidate fixture,
including a `deploy_event` fold case, are byte-identical (`cmp` clean, both
md5 `18684171f96d39574767eec13ef8c514`, 200 rows).

## Regression cover

- `TestRuntimeEnvironmentEvidenceManyPairsStayFactFirstLive` (901 pairs,
  asserts 200 artifact-index loops, 200/200 confirmed, `deploy_event` fold):
  RED on the unfenced query ("plan did not use ..._artifact_lookup_idx"),
  GREEN after the fence.
- `TestRuntimeEnvironmentEvidenceHotDigestUsesArtifactIndexLive` (2 pairs) and
  `TestRuntimeEnvironmentEvidenceCurrentAuthorizedTruthMatrixLive` pass
  unchanged: the fence does not regress the small-scale shape or the
  admit/deny truth.
- `TestRuntimeEnvironmentEvidenceQueryUsesCurrentAuthorizedExactPairs` pins
  the `OFFSET 0` fence textually.

Performance Evidence: plan shape before/after above; buffer touches 41.19M
to 14.5k shared hits on the same fixture and host.
No-Observability-Change: the fix changes only the plan shape of an existing
read; no new code path, metric, or log. The live plan test is the regression
signal.
