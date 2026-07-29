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
   the historical pre-#5466 baseline commit `58f364f68f` (this is the commit
   `#5466`'s branch was originally authored against; it is NOT current
   `origin/main`, which has since advanced through unrelated merges and a
   `#5466`-branch rebase — see the F-14 note under Results below for the
   current `origin/main` SHA) in a throwaway worktree, and once against this
   branch, with the identical benchmark source on both sides.
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
| `LegacyScopeOnly` (identical shape) | `58f364f68f` (historical pre-#5466 baseline) | 10,550.8 | 43,962.6 | 14 |
| `LegacyScopeOnly` (identical shape) | `c9c780dbe8` (#5466 branch, pre-round-4-rebase) | 11,152.6 | 49,727.2 | 14 |
| `WithEnvironmentWorkloadServiceScope` (new fields populated) | `c9c780dbe8` (#5466 branch, pre-round-4-rebase) | 11,529.2 | 49,728.6 | 14 |

**F-14 note (round-5 review):** `origin/main` has since advanced from
`58f364f68f` to `a2a5340a9e` (#5847) through unrelated merges — `58f364f68f`
above is a historical baseline label, not current `origin/main`. Separately,
`c9c780dbe8` is NOT an ancestor of the current branch HEAD
(`git merge-base --is-ancestor c9c780dbe8 HEAD` fails): the round-4 review's
rebase onto the advanced `origin/main` rewrote every commit on this branch,
including the one this SHA identified ("split suppression scope matcher and
tests under the 500-line cap"), which now lives at `30924bad3d`. The
benchmark numbers themselves were not re-run for the rebase — nothing in the
rebased diff touches `EvaluateSupplyChainSuppression` or
`vulnerabilitySuppressionScope`'s field layout, so the measured ns/op/B/op/
allocs/op figures still describe the current code; only the commit SHA used
to label that measurement predates the rebase.

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

# baseline (throwaway worktree at the historical pre-#5466 baseline
# 58f364f68f -- NOT current origin/main, which has since advanced to
# a2a5340a9e; see the F-14 note under Results above)
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
The `payload->'scope'->>'cve_id'` sibling predicate in this measurement is
labelled "already shipped, unchanged" because that was true AT THE TIME
this measurement was taken (before F-6, further below, rewrote it) -- it is
a historical snapshot, not a description of the predicate as it ships
today. See the F-6 follow-up section below for the current shape of that
predicate.

```
-- 300,000-row vulnerability.suppression table, darwin/arm64, postgres:16 in
-- Docker. Environment values drawn from a realistic ~7-token closed domain
-- (prod, production, qa, stage, staging, dev, uat -- a sample of
-- environment.knownTokens, the 12-token union), round-robin distributed,
-- NOT a single low-cardinality synthetic value.

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

### F-6 follow-up: the five sibling suppression-scope predicates had the identical exact-match defect

`scopeAnchorMatches`
(`go/internal/reducer/supply_chain_suppression_scope_match.go`) compares
`scope.CVEID`, `scope.PackageID`, `scope.PURL`, `scope.RepositoryID`, and
`scope.SubjectDigest` with `strings.TrimSpace` + `strings.EqualFold` --
the identical case-insensitive, whitespace-tolerant contract P1-1/F-4
already fixed for `environment`/`workload_id`/`service_id`. Before this
follow-up, the five `->'scope'->>'X'` predicates for these keys (including
the `cve_id` predicate labelled "unchanged" in the historical measurement
above) were still exact-match `= ANY(...)`, so a payload of
`{"cve_id":"cve-2026-1234"}` (lowercase) or a whitespace-padded `purl`
decoded and matched in Go but was never SELECTED by this query.

Fix: new placeholders `$15` (package_id), `$16` (purl), `$17` (cve_id),
`$18` (subject_digest), and `$19` (repository_id) carry
`lower(btrim(fact.payload->'scope'->>'X', E' \t\n\v\f\r'))` REPLACEMENTS
for what were exact-match `->'scope'->>'X' = ANY($1/$2/$3/$4/$7)`
predicates -- there is no exact-match fallback left for these five
`"scope"`-nested comparisons. `$15`-`$19` bind `lowerCleanedStringFilterValues`
of the SAME filter values already bound to `$1`/`$2`/`$3`/`$4`/`$7`, so
every row the old exact-match predicate could select, the normalized one
also selects, plus payloads whose case/whitespace differs from the filter.
New placeholders were required (not reusing `$1`-`$4`/`$7` with lowered
values) because those placeholders are ALSO bound to the top-level
(non-`"scope"`) sibling predicates on the same lines, which serve OTHER
fact kinds (`vulnerability.affected_package`, `sbom.component`, ...) whose
existing exact-match behavior must not change.

Test coverage:
`TestListActiveSupplyChainImpactFactsQueryNormalizesSuppressionScopeSiblings`
and `TestListActiveSupplyChainImpactFactsBindsNormalizedSuppressionScopeSiblings`
(`facts_active_supply_chain_impact_scope_normalize_test.go`) prove the
predicate shape and the `$15`-`$19` bind positions hermetically (no DSN
required). The real load-path proof is
`TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByLowercaseCVEIDAndPaddedPURLLive`
(`facts_active_supply_chain_impact_scope_normalize_live_test.go`, build tag
`integration`): seeds a suppression scoped ONLY by a lowercase CVE ID and
another scoped ONLY by a whitespace-padded PURL, and proves both are
selected when the reducer's derived filter carries the conventional
uppercase/unpadded form.

Index/sargability: `$15`-`$19` are additional `OR`-ed string comparisons in
the same already-`Parallel Seq Scan`ning `WHERE` clause the Index/sargability
evidence above already covers for the shape class (`lower(btrim(jsonb
text extraction)) = ANY(text[])`); no separate EXPLAIN re-run was performed
for `$15`-`$19` specifically, since they are the identical predicate shape
already measured for `$14` (`lower(btrim(...)) = ANY(...)`, no index used,
`Parallel Seq Scan`) -- only the JSON key name and bind values differ.

### F-10 follow-up: `advisory_id` had NO load-path predicate at all

Unlike the five F-6 keys above (which at least had a stale exact-match
predicate before being normalized), `advisory_id` was a dead scope key from
the start: `scopeAnchorMatches`
(`go/internal/reducer/supply_chain_suppression_scope_match.go`) accepts
`scope.AdvisoryID`, `suppressionScopeIsEmpty` treats it as a valid sole
anchor, and the reasons string in
`go/internal/reducer/supply_chain_suppression_reasons.go` advertises it as
sufficient -- but `ListActiveSupplyChainImpactFacts`'s `WHERE` clause had no
predicate that could ever match a suppression scoped ONLY by `advisory_id`.

**Corrected source-of-truth note** (an earlier draft of this investigation
wrongly stated the only distinct raw source was `security_alert.repository_alert`'s
`ghsa_id`/`ghsa_ids` -- that is WRONG and is corrected here; a SECOND wrong
inference below was also caught and corrected in round-5 review F-12/F-13):
`vulnerability.cve` and `vulnerability.affected_package` each carry a raw,
top-level `advisory_id` field. **`vulnerability.affected_product` does
NOT** -- the `AffectedProduct` struct
(`sdk/go/factschema/vulnerability/v1/affected_product.go`) is
CVEID/Criteria/MatchCriteriaID/Vulnerable only, its sole emitter
`newNVDAffectedProductEnvelope`
(`go/internal/collector/vulnerabilityintelligence/nvd_envelope.go`) builds
no `advisory_id` key, and `sdk/go/factschema/vulnerability/v1/README.md`'s
required-fields table lists `AdvisoryID` for `CVE`/`AffectedPackage` only.
`vulnerability.epss_score` and `vulnerability.known_exploited` don't carry
it either -- `EPSSScore` is CVEID/Probability/Percentile/ScoreDate,
`KnownExploited` is CVEID plus KEV catalog fields, and neither struct nor
either emitter sets an `advisory_id` key. `advisory_id` is separately
indexed by:

```
CREATE INDEX IF NOT EXISTS fact_records_vulnerability_active_advisory_lookup_v2_idx
    ON fact_records ((payload->>'advisory_id'), scope_id, generation_id, fact_kind, fact_id ASC)
    WHERE fact_kind IN ('vulnerability.cve', 'vulnerability.affected_package', 'vulnerability.affected_product', 'vulnerability.epss_score', 'vulnerability.known_exploited', 'vulnerability.reference')
      AND is_tombstone = FALSE;
```

(`go/internal/storage/postgres/schema_fact_records_vulnerability_indexes.go`).
**Root cause of the wrong `affected_product`/`epss_score`/`known_exploited`
inference (worth internalizing):** this index's `fact_kind IN (...)` list
constrains which ROWS get indexed, not which payloads carry the
`advisory_id` key -- nothing stops the index expression evaluating to
`NULL` for a kind lacking the field. `vulnerability.reference` genuinely
DOES carry an optional `advisory_id` (set by the OSV and GitLab Gemnasium
emitters, never by NVD's;
`sdk/go/factschema/vulnerability/v1/reference.go`), but that kind is not in
`supplyChainImpactFactKinds()`
(`go/internal/reducer/supply_chain_impact.go`) so this reducer never loads
it at all -- it is not collected by this fix either. Payload-shape claims
must come from `sdk/go/factschema` and the emitter, never from a DDL
predicate list.

`supplyChainCVEID` is `firstNonBlank(payload["cve_id"], payload["advisory_id"])`
(`go/internal/reducer/supply_chain_impact_summary.go`), so whenever a fact
already has a populated `cve_id` -- the common case -- its DISTINCT
`advisory_id` (e.g. a GHSA ID alongside an NVD CVE ID, the exact shape in
the golden-corpus cassette:
`testdata/cassettes/replayschedule/supply-chain-impact.json`, `cve_id:
"CVE-2024-11001"` / `advisory_id: "GHSA-demo-1111-2222"`) never reached
`CVEIDs` and had nowhere else to go. `security_alert.repository_alert`'s
`ghsa_id`/`ghsa_ids` fields exist and are a REAL advisory-shaped field, but
they are not the primary source this fix closes and are NOT collected by
this fix -- only the raw `advisory_id` field on
`vulnerability.cve`/`vulnerability.affected_package` facts, plus
`vulnerability.suppression`'s own `scope.advisory_id` (for
suppression-to-suppression follow-up expansion, matching how the other
five anchors already work). The code line collecting `advisory_id` in the
`vulnerability.affected_product` case of `supplyChainImpactFilter`'s switch
(`supply_chain_impact_active_filter.go`) is a harmless no-op --
`uniqueSortedStrings` drops empty strings -- kept as defensive coverage
against a future emitter starting to set the field on that kind, not
because the field exists today.

Fix: a new `AdvisoryIDs []string` field on `SupplyChainImpactFactFilter`,
collected in `supplyChainImpactFilter`
(`go/internal/reducer/supply_chain_impact_active_filter.go`) SEPARATELY
from `CVEIDs` for `vulnerability.cve`/`affected_package` (and, as the
harmless no-op above, `affected_product`) (so a distinct `advisory_id` is
never dropped by `supplyChainCVEID`'s `firstNonBlank` preference for
`cve_id`) and from `vulnerability.suppression`'s own `scope.advisory_id`;
threaded through `SupplyChainImpactFactFilter.empty()`,
`supplyChainImpactFollowUpFilter`, and `mergeSupplyChainImpactFactFilters`
the same way `Environments`/`WorkloadIDs`/`ServiceIDs` were in P1-1. New SQL
placeholder `$20`: `lower(btrim(fact.payload->'scope'->>'advisory_id', E'
\t\n\v\f\r')) = ANY($20::text[])`, bound via `lowerCleanedStringFilterValues`,
the same normalization shape as `$15`-`$19`.

**What this does NOT cover** (be precise, not exhaustive-sounding):
`security_alert.repository_alert`'s `ghsa_id`/`ghsa_ids` are not collected
into `AdvisoryIDs`; `vulnerability.reference`'s optional `advisory_id` is
not collected either (that fact kind is outside this reducer's fact-kind
universe entirely, see the root-cause note above); and
`SupplyChainImpactFinding.AdvisoryID` (the CLASSIFICATION-time,
provenance-selected value used elsewhere, e.g.
`firstNonBlank(cve.advisoryID, cve.cveID)` in
`go/internal/reducer/supply_chain_impact_product.go`) is unrelated to and
unchanged by this fix -- this fix only affects which facts the
active-evidence PREFILTER can select, not how a finding's own AdvisoryID is
derived at classification time.

Test coverage:
`TestSupplyChainImpactFilterCollectsAdvisoryIDsSeparatelyFromCVEIDs`,
`TestSupplyChainImpactFilterAdvisoryIDOnlyIsNotEmpty`, and
`TestSupplyChainImpactFollowUpFilterTracksAdvisoryIDs`
(`go/internal/reducer/supply_chain_impact_active_filter_test.go`) prove the
collection/empty/follow-up behavior at the reducer layer.
`TestListActiveSupplyChainImpactFactsQueryNormalizesSuppressionScopeAdvisoryID`
and `TestListActiveSupplyChainImpactFactsBindsNormalizedSuppressionScopeAdvisoryID`
(`facts_active_supply_chain_impact_scope_normalize_test.go`) prove the
predicate shape and `$20` bind position hermetically. The real load-path
proof is
`TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByAdvisoryIDOnlyLive`
(`facts_active_supply_chain_impact_scope_normalize_live_test.go`, build tag
`integration`): seeds a suppression scoped ONLY by `advisory_id` and proves
it is selected when the reducer's derived filter carries that same
advisory ID.

Index/sargability: `$20` is the identical predicate shape already measured
for `$14`-`$19` (`lower(btrim(...)) = ANY(...)`, no index used, `Parallel
Seq Scan`); no separate EXPLAIN re-run was performed for `$20` specifically.

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
was originally described here as unmeasured but bounded by the same 8-round
cap every other invocation gets. That bound claim was wrong -- see the
round-7 review P1-B section at the end of this document.** The 8-round cap
bounds the NUMBER OF ROUNDS, not the number of rows any single round's
`ListActiveSupplyChainImpactFacts` call can return; before P1-B a single
round had no row-count bound at all.
Round-2 review narrowing (conservative in the safe direction): every fact
kind that contributes to `Environments`/`WorkloadIDs`/`ServiceIDs` in
`supplyChainImpactFilter` (`cicdRunCorrelationFactKind`,
`workloadIdentityFactKind`, `serviceCatalogCorrelationFactKind`) also
contributes a `RepositoryIDs` value for that same envelope, so this new
class requires the repository-ID extractor to independently return empty
for the same fact too — a materially rarer trigger than "any
deployment-evidence-only intent," not the broader class the wording above
could be read to imply.

**Round-4 review F-10 / round-5 review F-15 addition:** `AdvisoryIDs`
(#5466 round-4 review F-10) widens `empty()` identically -- a suppression
scoped ONLY by `scope.advisory_id` now yields a non-empty follow-up filter
where the load previously short-circuited to `nil, nil`, same 8-round bound
as above, so this is a completeness gap, not a new perf risk. Unlike the
Environments/WorkloadIDs/ServiceIDs case, the "also contributes
RepositoryIDs" narrowing above does NOT apply here: `vulnerability.cve`/
`affected_package` (the fact kinds `AdvisoryIDs` reads from) do not
contribute `RepositoryIDs` in `supplyChainImpactFilter`, and a
`vulnerability.cve`/`affected_package` fact with a populated `cve_id`
already widens `empty()` via `CVEIDs` regardless of `AdvisoryIDs` (per
`supplyChainCVEID`'s `firstNonBlank` preference), so the practical new
class this adds is narrower still: an intent carrying ONLY an
advisory-only-scoped `vulnerability.suppression` fact (`scope.advisory_id`
set, every other scope key and every other anchor absent) -- the exact
mirror of the environment-only-scoped-suppression scenario P1-1 introduced.

The no-index CONCLUSION still stands regardless of this correction: the
predicate is OR-ed into a query that already performs a `Parallel Seq Scan`
for its other branches at this scale, so an index on this one predicate
would not change the scan strategy chosen for the query as a whole. No index
change is proposed in this PR.

### Round-7 review P1-A (codex, PR #5857): multi-anchor Cartesian-product false positive

Both PR #5857 codex findings slipped past seven prior separate-context
reviews and were independently verified true against the code before
fixing, per the mandatory Prove-The-Theory-First rule.

`applySupplyChainRuntimeContext` (`go/internal/reducer/supply_chain_impact_runtime.go`)
populates a finding's `Environments`, `WorkloadIDs`, and `ServiceIDs` from
THREE independently matched, uncorrelated evidence sources:
`supplyChainDeploymentContext` (from `reducer_ci_cd_run_correlation`,
carrying `environment` but no `workload_id`/`service_id`),
`supplyChainWorkloadContext` (from `reducer_workload_identity`, carrying
`workload_id` but no `environment`), and `supplyChainServiceContext` (from
`reducer_service_catalog_correlation`, carrying `service_id`/`workload_id`
but no `environment`) -- verified directly against
`go/internal/reducer/supply_chain_impact_index.go`'s struct definitions and
their sole builder functions
(`supplyChainDeploymentContextFromEnvelope`/`supplyChainWorkloadContextsFromEnvelope`/
`supplyChainServiceContextFromEnvelope`, `supply_chain_impact_deployment_context.go`
and `supply_chain_impact_match.go`). The only field these three structs
share is `repositoryID`, and each is matched against the finding
INDEPENDENTLY (by digest/image-ref/repository, `supply_chain_impact_runtime.go`),
never against each other. So a finding whose repository has two deployments
`(stage, workload-a)` and `(prod, workload-b)` gets
`Environments=[prod,stage]` and `WorkloadIDs=[workload-a,workload-b]` with
NO record anywhere of which environment paired with which workload.

`suppressionScopeMatchesFinding`
(`go/internal/reducer/supply_chain_suppression_scope_match.go`) checked
each of Environment/WorkloadID/ServiceID independently against its own
flattened list, so a suppression scoped `environment=stage,
workload_id=workload-b` matched -- "stage" is in `Environments`,
"workload-b" is in `WorkloadIDs` -- even though that exact combination
never occurred in either real deployment. The whole finding was then
suppressed: an OVER-suppression bug (hides a real, still-visible
vulnerability), exactly the widening the #5466 fail-closed rule (empty
anchor list -> no-match) was designed to prevent, but for a failure mode
one anchor at a time never surfaces.

**Tuple preservation vs. the alternative, surfaced before building either:**
codex suggested preserving correlated deployment tuples or splitting
findings by deployment context. Neither is achievable within the current
fact model without a genuine reducer contract/payload change: as the struct
trace above shows, `reducer_ci_cd_run_correlation` facts carry no
`workload_id`/`service_id`, and `reducer_workload_identity`/
`reducer_service_catalog_correlation` facts carry no `environment` -- there
is no real per-deployment tuple in the evidence to preserve or split by; any
`(environment, workload)` pairing would have to be INVENTED, not derived.
Making the underlying collectors/reducers correlate these dimensions is a
cross-cutting contract change well beyond this fix's scope, and was not
built speculatively here.

**Fix actually shipped (matcher-only, no reducer payload/contract change,
does not disturb #5426's environment-evidence contract):** a new
`suppressionDeploymentContextUnambiguous` check in
`suppressionScopeMatchesFinding`. When a scope names two or more of
{Environment, WorkloadID, ServiceID}, it requires the finding to have AT
MOST ONE distinct value in EVERY dimension the scope references -- with at
most one candidate value per referenced dimension there is only one
possible combination, so the existing independent per-dimension checks are
equivalent to verifying that single combination, exactly satisfying
codex's "matches only when a SINGLE deployment context satisfies all the
anchors it names." A dimension with two or more distinct values makes the
combination unverifiable and fails closed to no-match (same
ambiguity-resolves-to-visible direction as the pre-existing empty-list
case). A scope naming zero or one of the three dimensions has nothing to
combine and is completely unaffected -- **a single-anchor scope keeps
behaving exactly as it did before this fix.**

Failing-test-first proof:
`TestEvaluateSupplyChainSuppressionMultiAnchorScopeDoesNotMatchUnverifiedCombination`
(`go/internal/reducer/supply_chain_suppression_scope_cartesian_test.go`)
constructs the exact scenario above -- `Environments:["prod","stage"]`,
`WorkloadIDs:["workload-a","workload-b"]`, scope
`environment=stage,workload_id=workload-b`. Run against the pre-fix
matcher, this test failed at `State = "not_affected"` (the finding was
incorrectly hidden); after the fix it passes at `State = "scope_mismatch"`
(finding stays visible, suppression preserved for audit). A companion
`TestEvaluateSupplyChainSuppressionMultiAnchorScopeMatchesTheOnlyPossibleCombination`
proves the unambiguous single-value case still matches. The two pre-existing
backward-compat regression tests,
`TestEvaluateSupplyChainSuppressionEnvironmentAndWorkloadCombinationRequiresBoth`
and `TestEvaluateSupplyChainSuppressionWorkloadAndServiceScopeMatchAndFailClosed`,
use single-value lists throughout and pass unchanged, proving the common
case is not regressed.

This is a genuine projected-truth change (a finding's suppression decision
outcome changes for the previously-over-suppressed multi-anchor-ambiguous
case), so the full golden-corpus gate is mandatory for this fix -- see the
combined re-verification note at the end of the P1-B section below.

### Round-7 review P1-B (codex, PR #5857): unbounded environment-only suppression loads

See `go/internal/storage/postgres/gotchas-and-invariants.md` for the full
measurement, fix, and test detail; this section is the narrative summary.
Codex correctly identified that this branch's own committed 300k-row EXPLAIN
evidence (85,715 rows matching a realistic `prod` environment predicate,
documented earlier in this file) demonstrates
`ListActiveSupplyChainImpactFacts` has no bound on total rows per call --
the 8-round expansion cap and the 500-row page `LIMIT` do not compose into
a total bound, since the query paginates to exhaustion of every matching
row within EACH round. MEASURED (per Prove-The-Theory-First, not reasoned
about): the real Go code path against the same 300k-row corpus with
`Environments:["prod"]` returned 85,715 envelopes in ~22.7s. Fixed with a
2,000-row (4-page) cap on `ListActiveSupplyChainImpactFacts`'s own
pagination loop; re-measured with the cap engaged, warm-cache steady state
settled at ~532ms for the same filter -- a ~43x reduction, matching the
4/172 page-count ratio. Truncation fails open (findings stay visible) and
reuses the existing `activeEvidenceTruncated` observability path (finding
marking, evidence-summary log suffix, `active_evidence_truncated`
sub-signal) rather than adding a new one.

This fix changes `ListActiveSupplyChainImpactFacts`'s internal pagination
behavior but not its `WHERE`-clause SQL text or per-page row set; for the
small, curated golden corpus the 2,000-row cap never engages (no golden
fixture approaches that row count), so this fix alone would not be
golden-corpus-observable. P1-A above, however, IS a genuine projected-truth
change, so the full golden-corpus gate was run covering both fixes'
combined diff -- see the command output cited in the PR.
