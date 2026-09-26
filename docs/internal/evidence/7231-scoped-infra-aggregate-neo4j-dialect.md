# #7231 Scoped Infra Resource Aggregates On Neo4j

A scoped token calling `GET /api/v0/infra/resources/count` (MCP
`count_infra_resources`) got `backend_timeout` on Neo4j. The live #5167
scoped-token sweep found it after #7226. The aggregate path still copied the
SHAPE-A inline-map grant predicate into every branch of its 27-label
`CALL { ... UNION ALL ... }`, and the count route sends four such statements
(total, provider, environment, label). `GET /api/v0/infra/resources/inventory`
(MCP `get_infra_resource_inventory`) shares the builders and sends one.

The fix applies the #7226 split to the aggregate builders. On an explicit Neo4j
backend with a scoped caller, `infraResourceAggregateNeo4jScopedCypher` keeps
each branch's property filters, returns `n` through `UNION ALL`, and applies
the list-`EXISTS` predicate once after the `CALL` over `$scope_grants`. It then
counts or groups once. NornicDB, the zero value and unknown backends keep the
SHAPE-A statements byte for byte. Design:
[Scoped Grant Predicates](../../public/reference/cypher-scoped-grant-predicates.md).

## Setup

- Image: `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
  (the `docker-compose.neo4j.yml` pin), private container `perf7231-neo4j`
  with a 512m heap and 512m pagecache, the compose sizes. It is linux/amd64
  under emulation on an Apple M4 Pro, so absolute seconds are inflated. The same
  container serves every row.
- Schema: 137 of 139 Eshu DDL statements (the #7215 extraction; 2 multi-line
  fulltext rows fail to extract and the aggregates do not use them).
- Host 1-minute load average: 6 to 25 during the runs (peer agents active).
- Grants: g5 = 5 repositories + 5 scopes. The cap grant is 70 + 70 = 140
  scalars, past the 128 inline-term cap. The statement-level shim used the
  unit-test grants (`repo-000..`, `scope-000..`). The handler-level run used the
  live fixture's nonce-prefixed grants.
- Cold = `CALL db.clearQueryCaches()` first, plus a unique `/* nonce */`
  prefix on raw statements. Warm = the same text or request again.

## Theory: one statement, before and after

The shim ran one statement at a time outside the handler: SHAPE-A, the exact
statement the handler sent before this change, against the hoisted
list-`EXISTS` candidate. The graph was the schema-only database with no seeded
nodes, so the timings are planning cost. Three runs each at g5, one at the cap.

| Statement | Grant | SHAPE-A chars | SHAPE-A cold (s) | SHAPE-A warm (s) | Hoisted chars | Hoisted cold (s) | Hoisted warm (s) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| total | g5 | 60,591 | 24.67 / 14.18 / 11.55 | 0.16 / 0.10 / 0.10 | 1,966 | 1.33 / 0.43 / 0.33 | 0.02 / 0.01 / 0.02 |
| provider | g5 | 65,567 | 14.88 / 13.67 / 13.08 | 2.12 / 1.87 / 2.43 | 2,150 | 1.65 / 0.88 / 0.19 | 0.10 / 0.01 / 0.01 |
| environment | g5 | 63,380 | 11.34 / 20.13 / 14.54 | 0.25 / 0.11 / 0.58 | 2,069 | 0.87 / 0.19 / 0.15 | 0.01 / 0.01 / 0.01 |
| label | g5 | 61,328 | 10.74 / 10.60 / 11.65 | 0.12 / 0.12 / 0.11 | 1,993 | 0.77 / 0.17 / 0.15 | 0.01 / 0.01 / 0.01 |
| total | cap | 661,827 | DNF: heap OOM | DNF: heap OOM | 1,966 | 1.41 | 0.01 |

At the cap the SHAPE-A total failed with
`Neo.TransientError.General.OutOfMemoryError (Java heap space)`, as SHAPE-A
search and relationships did in #7215. The shim was stopped after that row. The
hoisted text is the same for every grant size, so one plan-cache entry serves
every token.

## Results: the handler, before and after

Performance Evidence: scoped infra count and inventory on Neo4j, wall seconds.
After = the real handler on the real `Neo4jReader` (10 s bounded read
enforced), 3 cold runs, each followed by a warm run.
Before = the SHAPE-A statements the handler sent, run in order outside the
handler after a cache clear, bounded at 120 s per statement.
Driver: `TestLiveInfraScopeNeo4jAggregateColdBudget`
(`go/internal/query/infra_scope_neo4j_aggregate_live_test.go`), with
`ESHU_INFRA_SCOPE_NEO4J_TIMING_BEFORE=1`, on the live fixture.

| Route | Grant | Statements | Before cold (3 runs) | After cold (3 runs) | After warm (3 runs) |
| --- | --- | --- | --- | --- | --- |
| count | g5 | 4 | 44.47 / 45.48 / 44.70 | 3.35 / 0.69 / 0.65 | 0.09 / 0.05 / 0.05 |
| inventory `group_by=provider` | g5 | 1 | 12.67 / 11.76 / 12.65 | 1.04 / 0.78 / 0.21 | 0.13 / 0.01 / 0.01 |
| count | cap | 4 | DNF: heap OOM at 91.8 / 87.0 / 85.0 on statement 1 | 0.72 / 2.06 / 2.71 | 0.07 / 0.09 / 0.12 |
| inventory `group_by=provider` | cap | 1 | DNF: heap OOM at 86.9 / 82.6 / 83.0 | 0.23 / 0.21 / 0.15 | 0.02 / 0.02 / 0.02 |

A before count at g5 needs about 45 s of planning, over four times the 10 s
budget, so the handler returned `backend_timeout`. At the cap every SHAPE-A
statement ran out of the 512m heap. The run's host load average fell from
about 15 to 6 over its 714 s.

Every after run answered 200 inside the budget; the test fails on a 504 or on
any run of 10 s or more. The issue measured the same wall on the e2e stack:
about 5 s to plan each of the four count statements, about 20 s per request.

## Equivalence and authorization

`TestLiveInfraScopeNeo4jAggregateEquivalenceAndNoLeak` seeds the #7215
nonce-prefixed fixture on the same container. It then gives every node a
provider (`provider`, or `source_system` on a `CloudResource`) and an
environment from a fixed Go rule. It drives the real handler for count and for
inventory grouped by provider, environment and label:

- the total and all three rollups equal an oracle computed in Go from the
  fixture design, and equal the SHAPE-A statements reduced by the production
  merge: g1 total 11, g5 total 26, cap total 33. That is one higher each than
  the first run, because the fixture now has a dual-labeled granted node, which
  is counted once per matching label branch. A unit test pins the `UNION ALL`
  join that keeps that count. At the cap the SHAPE-A
  reference hoists its predicate after the `CALL`, because the per-branch
  SHAPE-A statement does not plan on Neo4j at that size;
- inventory buckets equal the matching count rollup and the oracle;
- every negative node carries the provider and environment `ungranted-leak`,
  and that bucket never appears. The negatives are a `CloudResource` used only
  by an ungranted instance, an orphan `CloudResource`, an unmatched and an
  ungranted-matched `TerraformStateResource`, and ungranted-owned
  `K8sResource` and `TerraformResource` nodes. At g1 the g5-only nodes are also
  absent from the total and the oracle.

Default-lane proof (`go/internal/query/infra_scope_aggregate_dialect_test.go`):

- NornicDB, empty and `bogus` backends: statement digests captured at
  f55447c45 before the change, unchanged after it (count: 4 statements;
  inventory: 1);
- Neo4j: every statement carries exactly one grant predicate, after the
  `CALL`, in list form, with no `$scope_grant_<i>` params, and a text that is
  independent of the grant size; branch property filters and the four count
  shapes are kept;
- cap parity: `$scope_grants` equals the 128 scalars SHAPE-A inlines;
- an empty grant makes no graph read;
- the shared guard `TestInfraScopeListExistsNeverSelectedOffNeo4j` now lists
  the count and inventory routes. It went RED when the aggregate access helper
  was planted to always select the Neo4j dialect;
- the Neo4j flag never removes the grant from another branch builder (the
  read-model graph pass): `TestInfraAggregateNeo4jFlagNeverDropsBranchGrant`.

## Observability

Observability Evidence: the count and inventory request spans now carry
`eshu.infra_scope_dialect` (`unscoped`, `shape_a`, `neo4j_list_exists`), like
search and relationships (`TestInfraScopeDialectSpanAttributes`). The existing
`query.graph_read.warning` slow-read log carries the statement fingerprint, and
the #5408 cap counter `eshu_dp_query_scope_grant_inline_capped_total` and its
warning are unchanged. No metric is added.

## Limits

- Emulated amd64 on arm64 under a loaded host; the ratios, not the absolute
  seconds, are the claim.
- The committed fixture is small, so the committed claim is planning cost.
  Execution at data scale is not in the committed evidence. A reviewer-run,
  uncommitted 81,200-node driver (27 infra labels x 3,000, native arm64, 512m
  heap) measured the hoisted form at or below SHAPE-A, with equal answers. With
  the plan cached, g1 took ~0.75 s against ~1.5 s and g5 took ~0.83 s against
  ~5.8 s. Do not read that as ledger evidence.
- NornicDB was not re-measured: its statements are unchanged (digest-pinned).
