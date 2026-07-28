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
detail, the failing-test-first evidence, and the fix (including the P1-1
case/alias exact-match follow-up and the P2-1 bind-order/coverage follow-up
below) are in `go/internal/storage/postgres/gotchas-and-invariants.md`.

### P1-1 follow-up: exact-match vs. the decode/matcher's case-insensitive, alias-aware contract

The first cut of the new predicate did an exact-match `= ANY(...)` against
the raw payload value. `decodeVulnerabilitySuppressionScope`
(`go/internal/reducer/supply_chain_suppression_decode.go`) canonicalizes
`environment` through `environment.Canonical` (`"production"` maps to
`"prod"`) and only `strings.TrimSpace`s `workload_id`/`service_id`, while the
matcher compares all three with case-insensitive `strings.EqualFold`. A
payload authored as `{"environment":"production"}` could therefore decode
and match correctly once loaded, but could never be SELECTED by an
exact-match `= ANY('{prod}')` predicate against a canonical `"prod"` filter
— silently inert in production. The fix (first cut, later widened by the
F-4 follow-up below): `lower(payload->'scope'->>'environment') =
ANY($14::text[])` with `$14` expanded through `environment.Aliases()` so a
canonical filter value also binds every alias spelling, and `lower(trim(...))`
for `workload_id`/`service_id` with the bind values lowercased the same
way. Failing-test-first proof:
`TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByEnvironmentAliasLive`
(seeds the literal `"production"` payload, filters by `Environments:
["prod"]`) failed against the pre-fix predicate (`len = 0, want 1`) and
passes after the fix.

### F-4 follow-up: `trim()` strips ASCII space only, not Go's Unicode whitespace class

The P1-1 fix above used plain `trim(...)` (Postgres's `btrim(x, ' ')`,
ASCII space only). Go's `strings.TrimSpace` — used by `payloadStr` when
reading `workload_id`/`service_id`, and again inside `environment.Canonical`
for `environment` (`payloadStr`'s trim runs first, so environment is
trimmed twice) — strips the full Unicode `White_Space` property: tab,
newline, vertical tab, form feed, carriage return, NBSP (U+00A0), the
U+2000–200A run, U+2028/2029, U+202F, U+205F, U+3000, and more. Proven live
on Postgres 16.14:

```
lower(trim(' Production '))     -> 'production'          matches ANY['prod','production'] -> t
lower(trim(E'\tProduction\n'))  -> E'\tproduction\n'      matches ANY['prod','production'] -> f
```

So a payload of `{"environment":"\tProduction\n"}` decoded to `"prod"` and
the matcher accepted it, while the prefilter silently never loaded it. This
applied identically to all three predicates ($12/$13/$14), not just
environment — the live test up to this point only seeded space-padding, so
no existing test caught it. Fix: widen every `trim(...)` in this branch to
`btrim(..., E' \t\n\v\f\r')`, an explicit ASCII whitespace character class
(space, tab, newline, vertical tab, form feed, carriage return).
`TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByEnvironmentAliasLive`
gained a third seed, `SUP-ALIAS-TAB`, whose payload is padded with a real
tab and newline (via JSON `\t`/`\n` escapes, which the jsonb parser resolves
to actual control-character bytes); against the pre-`btrim` `trim(...)`
predicate the test failed at `len = 2, want 3` (the plain-ASCII-space-padded
`SUP-ALIAS-WHITESPACE` fact still matched plain `trim()`; only the
tab/newline-padded `SUP-ALIAS-TAB` fact was isolated as unselected), and
passes at `len = 3` after the `btrim` fix.

This closes the gap for realistic operator-authored payloads (tab/newline
padding) but does **not** close it for exotic non-ASCII Unicode whitespace
(NBSP, the U+2000–200A run, U+2028/2029, U+202F, U+205F, U+3000, ...) —
Postgres has no built-in primitive to trim the full Unicode whitespace
class, so a payload padded with one of those codepoints would still
decode/match in Go and not be selected by this SQL. This residual gap is
accepted and documented here, not silently assumed away.

### P2-1 follow-up: $12/$13 load-path coverage and bind-order

`TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByWorkloadAndServiceIDsLive`
adds real load-path proof for `$12` (WorkloadIDs) and `$13` (ServiceIDs),
which no prior live test exercised, seeding two distinct
mixed-case/whitespace-payload facts and querying with both filter fields
populated at once. A hermetic (no DSN required)
`TestListActiveSupplyChainImpactFactsBindsWorkloadAndServiceIDsToDistinctPlaceholders`
asserts the bound argument values at their exact placeholder positions
(`db.queries[0].args[11]`/`args[12]`), so a `filter.WorkloadIDs`/
`filter.ServiceIDs` bind-order swap fails CI even without a live Postgres.

### Index/sargability evidence (re-run with a realistic environment distribution)

Measured against the `lower(payload->'scope'->>'environment')` predicate as
it stood immediately after P1-1 (before the F-4 `btrim` widening below).

```
-- 300,000-row vulnerability.suppression table, darwin/arm64, postgres:16 in
-- Docker. Environment values drawn from a realistic ~7-token closed domain
-- (prod, production, qa, stage, staging, dev, uat -- environment.Aliases()'s
-- token set), round-robin distributed, NOT a single low-cardinality
-- synthetic value.

-- PRE-BTRIM predicate (P1-1 shape): lower(payload->'scope'->>'environment') = ANY(alias-expanded ['prod','production'])
Gather (actual time=0.375..25.202 rows=85715 loops=1)
  Workers Launched: 2
  -> Parallel Seq Scan on fact_records (actual time=0.019..20.091 rows=28572 loops=3)
     Filter: (... AND (lower((payload->'scope')->>'environment') = ANY ('{prod,production}'::text[])))
     Rows Removed by Filter: 71428
Execution Time: 26.572 ms

-- PRE-FIX predicate (exact match, single canonical value): payload->'scope'->>'environment' = ANY(['prod'])
Gather (actual time=0.118..16.292 rows=42857 loops=1)
  Workers Launched: 2
  -> Parallel Seq Scan on fact_records (actual time=0.010..12.367 rows=14286 loops=3)
     Filter: (... AND ((payload->'scope')->>'environment' = ANY ('{prod}'::text[])))
     Rows Removed by Filter: 85714
Execution Time: 17.015 ms

-- EXISTING sibling predicate (already shipped, unchanged): payload->'scope'->>'cve_id'
Gather (actual time=12.364..14.052 rows=0 loops=1)
  Workers Launched: 2
  -> Parallel Seq Scan on fact_records (actual time=9.489..9.489 rows=0 loops=3)
     Filter: (... AND ((payload->'scope')->>'cve_id' = ANY ('{CVE-2026-000001}'::text[])))
     Rows Removed by Filter: 100000
Execution Time: 14.065 ms
```

All three produce the IDENTICAL plan shape (`Parallel Seq Scan` on
`fact_records`, no index used by any of them, 14-27ms at this scale). The
`lower()` wrapper and the wider alias-expanded array do not change the scan
strategy — they return a higher row count by construction (2/7 of rows for
the 2-value alias-expanded array vs. 1/7 for the single-value exact match),
bounded by the total suppression-fact count. This directly supersedes the
prior version of this evidence, which used a single low-cardinality
synthetic environment value (`env-1`, 60/300k rows) — real environment
values are a small closed domain, so the realistic-distribution re-run above
is the accurate selectivity picture, not the original strawman.

### F-4 EXPLAIN re-run: `btrim` vs `trim` plan shape

Re-ran `EXPLAIN (ANALYZE, BUFFERS)` on the identical 300,000-row
realistic-distribution seed above, comparing the shipped
`lower(btrim(payload->'scope'->>'environment', E' \t\n\v\f\r'))` predicate
against the pre-F-4 `lower(trim(payload->'scope'->>'environment'))`
predicate (both with the same alias-expanded `ANY('{prod,production}')`
array):

```
-- SHIPPED: lower(btrim(payload->'scope'->>'environment', E' \t\n\v\f\r')) = ANY('{prod,production}')
Gather (actual time=0.369..29.020 rows=85715 loops=1)
  Workers Launched: 2
  -> Parallel Seq Scan on fact_records (actual time=0.010..25.514 rows=28572 loops=3)
     Filter: (... AND (lower(btrim((payload->'scope')->>'environment', '  \t\n\x0Bv\x0C\r')) = ANY ('{prod,production}'::text[])))
     Rows Removed by Filter: 71428
Execution Time: 30.477 ms

-- PRE-F-4: lower(trim(payload->'scope'->>'environment')) = ANY('{prod,production}')
Gather (actual time=0.071..24.342 rows=85715 loops=1)
  Workers Launched: 2
  -> Parallel Seq Scan on fact_records (actual time=0.006..21.224 rows=28572 loops=3)
     Filter: (... AND (lower(TRIM(BOTH FROM (payload->'scope')->>'environment')) = ANY ('{prod,production}'::text[])))
     Rows Removed by Filter: 71428
Execution Time: 25.706 ms
```

Identical plan shape (`Parallel Seq Scan`, identical cost estimate
`1000.00..11077.00`, identical `rows=28572`/`Rows Removed by Filter: 71428`
per worker) — `btrim` with a literal character-class argument is exactly as
unindexable as `trim`, so the pre-`btrim` EXPLAIN evidence above (the
"FIXED predicate" block, now relabeled pre-`btrim`) stands without needing
a fresh 300k-row re-run for every subsequent whitespace-class change. This
independently reproduces the round-3 review's own re-run (which reported
the same `28572`/`71428` per-worker figures).

### Frequency correction (P2-3)

The original version of this evidence described the active-evidence call as
"once-per-generation." That was wrong. `ListActiveSupplyChainImpactFacts` is
reached **per supply-chain-impact intent**, and
`loadActiveSupplyChainImpactFactsUntilStable`
(`go/internal/reducer/supply_chain_impact_handler_helpers.go`) issues **up to
`maxSupplyChainImpactActiveEvidenceLoads = 8` paginated rounds per intent**,
not once — asserted by
`TestSupplyChainImpactHandlerStopsActiveEvidenceExpansionConservatively`
(`go/internal/reducer/supply_chain_impact_active_test.go`).

Separately, adding `Environments`/`WorkloadIDs`/`ServiceIDs` to
`SupplyChainImpactFactFilter` widened `SupplyChainImpactFactFilter.empty()`
to return `false` for a NEW class of intents: one carrying only deployment
evidence (environment/workload/service) and no package/CVE/digest/
repository anchor at all. Before #5466 that intent's derived filter was
fully empty and the load short-circuited to `nil, nil` with no query issued;
after #5466 it issues a full paginated Seq Scan. **This new-invocation class
is unmeasured** — no benchmark or production metric isolates it yet — but it
is bounded the same way every other invocation is, by the same 8-round cap.
Round-2 review narrowing (conservative in the safe direction): every fact
kind that contributes to `Environments`/`WorkloadIDs`/`ServiceIDs` in
`supplyChainImpactFilter` (`cicdRunCorrelationFactKind`,
`workloadIdentityFactKind`, `serviceCatalogCorrelationFactKind`) also
contributes a `RepositoryIDs` value for that same envelope, so this new
class requires the repository-ID extractor to independently return empty
for the same fact too — a materially rarer trigger than "any
deployment-evidence-only intent," not the broader class the wording above
could be read to imply.

The no-index CONCLUSION still stands regardless of this correction: the
predicate is OR-ed into a query that already performs a `Parallel Seq Scan`
for its other branches at this scale, so an index on this one predicate
would not change the scan strategy chosen for the query as a whole. No index
change is proposed in this PR.
