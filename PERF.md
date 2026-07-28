# #5466 Perf Evidence: environment/workload/service suppression scope matcher

## Method

`EvaluateSupplyChainSuppression` (`go/internal/reducer/supply_chain_suppression.go`)
runs the matcher (`suppressionAdjacent` / `suppressionScopeMatchesFinding`,
`go/internal/reducer/supply_chain_suppression_scope_match.go`) once per
finding against every suppression fact in scope — the hot path this change
adds three field comparisons to. Benchmarked with
`go/internal/reducer/supply_chain_suppression_bench_test.go`:
`benchmarkSuppressionSet` builds 50 suppressions (a realistic per-finding
fan-out), 49 of which are adjacent-but-scope-mismatched — the worst case for
the matcher, since it must walk every scope key before rejecting — plus one
active match, so the benchmark exercises `pickPreferredSuppression` and
`decisionFromActiveOperatorSuppression` too, not only the reject path.

Two comparisons, both on `darwin/arm64`, Apple M5 Max, `go test -bench
BenchmarkEvaluateSupplyChainSuppression -benchmem -benchtime=500000x
-count=5`:

1. **OLD vs NEW, identical input shape.** `BenchmarkEvaluateSupplyChainSuppression_LegacyScopeOnly`
   uses a finding and suppressions that never touch Environment/WorkloadID/
   ServiceID (all zero value) — the literal pre-#5466 shape. Run once against
   `origin/main` (commit `58f364f68f`, the commit this branch forked from) in
   a throwaway worktree, and once against this branch, with the identical
   benchmark source on both sides.
2. **NEW with the new fields populated.** `BenchmarkEvaluateSupplyChainSuppression_WithEnvironmentWorkloadServiceScope`
   sets Environment/WorkloadID/ServiceID on every suppression and on the
   finding, so it measures the added fields' actual comparison cost, not only
   their zero-value fast path. This benchmark cannot run on `origin/main`
   (the fields do not exist there).

Five samples per benchmark; reporting the mean, since a busy shared build
machine makes any single sample noisy (visible in the run-to-run spread
below) but the byte/alloc counts are deterministic per run.

## Results

| Benchmark | Commit | ns/op (mean of 5) | B/op | allocs/op |
|---|---|---:|---:|---:|
| `LegacyScopeOnly` (identical shape) | `58f364f68f` (origin/main, baseline) | 10,550.8 | 43,962.6 | 14 |
| `LegacyScopeOnly` (identical shape) | `c9c780dbe8` (#5466 branch) | 11,152.6 | 49,727.2 | 14 |
| `WithEnvironmentWorkloadServiceScope` (new fields populated) | `c9c780dbe8` (#5466 branch) | 11,529.2 | 49,728.6 | 14 |

Raw samples (ns/op), baseline `LegacyScopeOnly`: 12463, 12325, 9561, 9271, 9134.
Raw samples (ns/op), branch `LegacyScopeOnly`: 10750, 10624, 12440, 11208, 10741.
Raw samples (ns/op), branch `WithEnvironmentWorkloadServiceScope`: 10275, 9809, 12240, 12352, 12970.

## Analysis

- **ns/op: no regression outside noise.** The baseline and branch
  `LegacyScopeOnly` sample ranges overlap almost entirely (baseline
  9134-12463, branch 10624-12440); the mean delta is +5.7%, smaller than the
  spread within either sample set on this shared machine. Populating the new
  fields (`WithEnvironmentWorkloadServiceScope`) costs no additional time
  over the unpopulated case on the same branch (11,529.2 vs 11,152.6, within
  the same noise band) — the three added comparisons per scope key are
  `strings.TrimSpace` plus an early-return on an empty string
  (`scopeListAnchorMatches`), the same short-circuit cost class as the six
  pre-existing scope keys.
- **B/op: a real, fully explained +13.1% increase, not an algorithmic
  regression.** `vulnerabilitySuppressionScope` grew from 6 string fields to
  9 (adding `Environment`, `WorkloadID`, `ServiceID`), so
  `vulnerabilitySuppression` (which embeds it by value) grew by 3 string
  headers (16 bytes each on arm64) = 48 bytes per suppression.
  `EvaluateSupplyChainSuppression` buckets every suppression by copying it
  by value into one of `activeMatches`/`providerMatches`/`expiredMatches`/
  `scopeMismatched` via `append` (`go/internal/reducer/supply_chain_suppression.go`);
  with 49 of 50 suppressions landing in `scopeMismatched` in this benchmark,
  that is 49 extra 48-byte copies plus the larger backing-array growth steps
  — accounting for the full ~5,765 byte increase (49,727.2 - 43,962.6). This
  is the same constant-factor cost every prior scope key already paid when
  it was added; it does not change the matcher's asymptotic shape
  (`O(suppressions × scope keys)`, unchanged).
- **allocs/op: unchanged (14 in every run).** No new allocation site was
  introduced; the extra bytes land in the same allocations that already
  existed for the match/mismatch bucket slices.
- **Bound check.** `go/internal/reducer/README.md`'s suppression-evidence
  section documents the additional decode-and-evaluate cost staying "under
  one millisecond per finding" for the largest CI fixture fan-out. At
  ~11.5 microseconds/op for a 50-suppression fan-out here, this change stays
  roughly two orders of magnitude under that budget even with the byte-count
  increase.

## Commands run

```bash
# branch (this worktree)
cd go && go test ./internal/reducer/ -run xxx \
  -bench BenchmarkEvaluateSupplyChainSuppression -benchmem -benchtime=500000x -count=5

# baseline (throwaway worktree at origin/main 58f364f68f)
git worktree add <scratch-path> 58f364f68f
# copy in a LegacyScopeOnly-only variant of the bench file (the new-field
# variant cannot compile against origin/main's struct)
cd <scratch-path>/go && go test ./internal/reducer/ -run xxx \
  -bench BenchmarkEvaluateSupplyChainSuppression_LegacyScopeOnly -benchmem -benchtime=500000x -count=5
git worktree remove <scratch-path>
```

## Addendum: Postgres active-evidence prefilter (index/sargability)

A separate, non-hot-path SQL change was required as a follow-up: the
in-memory matcher above only ever sees a `vulnerability.suppression` fact
that `FactStore.ListActiveSupplyChainImpactFacts`
(`go/internal/storage/postgres/facts_active_supply_chain_impact.go`) actually
loads. Before this fix, a suppression scoped ONLY by
environment/workload_id/service_id (no cve_id/advisory_id/package_id/purl/
subject_digest/repository_id) could never be selected by that query's WHERE
clause — the scope struct and matcher accepted it, but the loader never
fetched it, so the suppression silently never applied in production. Full
detail, the failing-test-first evidence, and the fix are in
`go/internal/storage/postgres/gotchas-and-invariants.md`. Index/sargability
summary, per eshu-postgres-rigor's Index Doctrine (add an index only with
evidence the query is hot enough and the shape benefits):

```
-- 300,000-row vulnerability.suppression table, darwin/arm64, postgres:16 in Docker
-- NEW predicate: payload->'scope'->>'environment'
Gather (actual time=0.582..18.015 rows=60 loops=1)
  Workers Launched: 2
  -> Parallel Seq Scan on fact_records (actual time=0.283..14.879 rows=20 loops=3)
     Filter: (... AND ((payload->'scope')->>'environment' = ANY ('{env-1}'::text[])))
Execution Time: 18.031 ms

-- EXISTING sibling predicate (already shipped): payload->'scope'->>'cve_id'
Gather (actual time=0.138..17.418 rows=1 loops=1)
  Workers Launched: 2
  -> Parallel Seq Scan on fact_records (actual time=9.231..14.438 rows=0 loops=3)
     Filter: (... AND ((payload->'scope')->>'cve_id' = ANY ('{CVE-2026-000001}'::text[])))
Execution Time: 17.427 ms
```

Both predicates produce the identical plan shape (`Parallel Seq Scan`, no
index, ~17-18ms at 300k rows) — the new predicate carries the same
already-accepted unindexed cost as its four siblings, not a new category of
scan. This is a bounded, once-per-generation active-evidence expansion call
(paginated, not a per-finding hot loop), so no index was added; per Index
Doctrine, that would need evidence this specific query is hot enough to
justify one. No index change is proposed in this PR.
