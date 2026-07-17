# #5283 — infra all-categories by-provider null bucket on NornicDB

## Summary

The default all-categories `/api/v0/infra/resources/count` by-provider grouping
returned a `<null>` bucket for provider-less resources instead of `unknown` on
the pinned NornicDB backend. Root cause: the all-categories
`infraResourceProviderGroupExpression` used a deeply nested
`CASE WHEN n.provider ... THEN CASE WHEN n:CloudResource THEN CASE ... END END`
expression. On the pinned NornicDB build a `n:Label` label test inside a
CASE/projection is echoed as the literal expression text (`"n:CloudResource"`),
so the enclosing nested CASE collapsed to a null group key. The fix flattens the
expression to a single-level CASE and gates the CloudResource source_system
fallback with `'CloudResource' IN labels(n)` (which NornicDB evaluates
correctly).

Classification: **Correctness win** (behavior fix). No hot-path performance
impact.

## Root cause proof (live, pinned NornicDB, Bolt driver, DB `nornic`)

Image `eshu-nornicdb-pr261:149245885258`
(commit `1492458852588c884c32f70d27ea2ee07086769c`), embeddings disabled.

Direct label-test probe in a projection:

| node | `RETURN (n:CloudResource)` | `RETURN 'CloudResource' IN labels(n)` |
| --- | --- | --- |
| CloudResource | `"n:CloudResource"` (literal string) | `true` |
| TerraformResource | `"n:CloudResource"` (literal string) | `false` |

A label test in a projection returns the literal expression text; `IN labels(n)`
evaluates correctly. Nesting CASE inside CASE additionally mangles the whole
expression.

## Before / after (same 4-node seed, same per-label CALL query builder)

Seed (across a CloudResource and a non-cloud label):

| id | label | provider | source_system | intended bucket |
| --- | --- | --- | --- | --- |
| cr-aws | CloudResource | `''` | `aws` | `aws` |
| cr-empty | CloudResource | `''` | `''` | `unknown` |
| tf-none | TerraformResource | `''` | — | `unknown` |
| tf-gcp | TerraformResource | `gcp` | — | `gcp` |

Intended merged buckets: `{aws:1, gcp:1, unknown:2}`.

| expression | merged buckets | verdict |
| --- | --- | --- |
| OLD nested `CASE WHEN n:CloudResource` | `{"":3, "unknown":1}` | BROKEN — null bucket; `gcp` lost |
| NEW flat + `'CloudResource' IN labels(n)` | `{aws:1, gcp:1, unknown:2}` | CORRECT |

The NEW expression matches the intended (Neo4j-equivalent) semantics exactly:
provider-less CloudResource falls back to `source_system`, provider-less
non-cloud nodes bucket as `unknown`, and a real provider is never collapsed.

## Adjacent surfaces checked (no change needed)

- `infraResourceServiceGroupExpression` all-categories path: already a flat
  `coalesce(...)` CASE with no label test and no nesting — live probe returned no
  null bucket. Safe, unchanged.
- Provider **filter** predicate `(n.provider = $provider OR (n:CloudResource AND
  n.source_system = $provider))` (`infraResourceAggregateFilterClauses`): a label
  test in a WHERE clause is evaluated as a boolean by NornicDB (live probe:
  `provider=aws` matched exactly the one CloudResource with `source_system=aws`).
  Safe, unchanged. Only the RETURN-projection label-test-in-CASE was defective.

## Verification

- `go test ./internal/query -run Infra -count=1` — green (unit shape + guard).
- New guard `TestInfraResourceInventoryGroupExpressionsAreNornicDBSafe` fails RED
  on the old nested-label-test expression across every inventory dimension.
- Updated shape test `TestInfraResourceInventoryDefaultScopeCoalescesCloudAnd
  TerraformGroups` requires the `'CloudResource' IN labels(n)` gate.
- Backend-required live proof
  `TestLiveInfraProviderInventoryBucketsNonNull`
  (`ESHU_INFRA_AGG_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687`) drives
  BOTH shipped call sites that consume the expression — the
  `GraphInfraResourceAggregateStore.InfraResourceInventory` by-provider read and
  the `CountInfraResources.ByProvider` rollup that backs the
  `/api/v0/infra/resources/count` endpoint named in the issue — and asserts no
  empty bucket and the corrected `{aws, gcp, unknown}` counts on each. Reverting
  the fix reproduces the null bucket (`{"":3, "unknown":1}`).

No-Regression Evidence: The change edits only the RETURN grouping projection of
the all-categories by-provider inventory read; it does not alter the `MATCH
(n:Label)` per-branch anchors, the branch WHERE predicates, index selection, or
result cardinality (one grouped row per bucket per branch, identical to before).
The projection is evaluated per-row over the already-anchored, bounded per-branch
result set — not a scan predicate — so the hot-path cost proven in #5281
(410ms -> 1.2ms grouped bucket read on the 91k-node corpus) is unchanged. Same
input shape, same query structure, correctness-only delta. Query shape: per-label
`CALL { MATCH (n:Label) ... RETURN <flat CASE> AS bucket, count(n) }` on the
pinned NornicDB (`eshu-nornicdb-pr261:149245885258`); input cardinality bounded
by the infra-label population; anchors use the per-label MATCH from #5281.

No-Observability-Change: No metrics, spans, logs, or status fields are added,
removed, or renamed. The read path emits the same telemetry as before; only the
Cypher group-key expression string changed.
