# Scoped Grant Predicates

A scoped token may read only the graph nodes its grants authorize. The infra
search (`POST /api/v0/infra/resources/search`), infra relationships
(`POST /api/v0/infra/relationships`), and infra aggregate
(`GET /api/v0/infra/resources/count` and `/inventory`, MCP
`count_infra_resources` and `get_infra_resource_inventory`) reads enforce that
with a grant predicate in their Cypher. This page covers why that predicate
has two dialects, and what each backend receives.

## The SHAPE-A predicate

`infraResourceScopePredicate` (`go/internal/query/infra_scope_grant.go`) admits
a node through five families, OR-joined:

1. direct ownership: `n.repo_id` or `n.id` in the grant arrays;
2. `CloudResource` via `USES` from a granted `WorkloadInstance`;
3. `TerraformStateResource` via `MATCHES_STATE` from a granted
   `TerraformResource`;
4. a forward `EXISTS` over `DEPLOYMENT_SOURCE` to a granted `Repository`;
5. `DEFINES` from a granted `Repository` (name-collision workloads).

Families 2, 3 and 5 are inline-map pattern terms, one per grant scalar, for
example `(n)<-[:USES]-(:WorkloadInstance {repo_id:$scope_grant_0})`. NornicDB
needs this form: a backward-anchored `EXISTS {}` with an `IN $list` filter
evaluates always-true there, a whole-graph leak (see
[NornicDB Pitfalls](nornicdb-pitfalls.md), "EXISTS {} Subquery Correctness
Depends On Anchor Direction"). The terms are capped at 128 scalars, fail-closed.

## Why SHAPE-A is slow on Neo4j

Two costs multiply on Neo4j:

- **O(grant) inline terms.** A token with N repositories and N scopes renders
  3 x 2N pattern terms. Each is a separate pattern expression to plan.
- **Per-branch repetition.** The search is a `CALL { ... UNION ... }` over 27
  single-label branches, and SHAPE-A copied the whole predicate into every
  branch. The relationships route copies it into three aliases (`n`, `target`,
  `source`) on each of up to 15 anchor statements. The aggregate reads copy
  it into every branch of a 27-label `CALL { ... UNION ALL ... }`, and the
  count route sends four such statements (total, provider, environment,
  label).

Measured on `neo4j:2026-community` (#7215): a 5 + 5 grant planned the search in
22-34 s cold and ran 2.6 s warm even with the plan cached; a 25 + 25 grant did
not plan inside 240 s. PROFILE shows the repetition directly: 837 `Expand`
operators at g5 = 27 branches x (30 inline terms + 1 `EXISTS`). The scoped
count hit the same wall (#7231): each of its four statements took 10.6-24.7 s
to plan cold at g5, so the route timed out at the 10 s bounded read.

## The Neo4j dialect

`InfraHandler.GraphBackend` selects the dialect. Only
`querycontract.GraphBackendNeo4j` selects the Neo4j form; NornicDB, the zero
value and any other value keep SHAPE-A byte for byte, pinned by
`TestInfraScopeNornicDBStatementsByteIdentical` and, for the aggregates,
`TestInfraAggregateScopeNornicDBStatementsByteIdentical`.

Anti-pattern on Neo4j: copying a grant predicate into every `UNION` branch, or
expanding it into O(grant) inline pattern terms.

On Neo4j (`go/internal/query/infra_scope.go`):

- each inline-map family becomes one list `EXISTS`, for example
  `EXISTS { MATCH (n)<-[:USES]-(i:WorkloadInstance) WHERE i.repo_id IN $scope_grants }`.
  `$scope_grants` is the same capped slice SHAPE-A inlines, so admission and
  the 128 cap are unchanged, and the statement text no longer depends on the
  grant count: one plan-cache entry serves every token;
- search branches return `n`, the predicate runs once after the `CALL`, and the
  row is projected once;
- the `category=argocd` shortcut (`searchArgoCDCategoryRows`) uses the same
  dialect: it reads `ArgoCDApplication` and `ArgoCDApplicationSet` as two
  single-label statements, so each carries the list-`EXISTS` predicate once,
  bound to `$scope_grants` with no `$scope_grant_<i>` params. There is no
  `UNION` to hoist across, and the text is still independent of the grant
  count. NornicDB and unknown backends keep the SHAPE-A statements;
- the count and inventory aggregates
  (`infraResourceAggregateNeo4jScopedCypher`,
  `go/internal/query/infra_resource_aggregates_cypher.go`) keep each label
  branch's property filters, return `n` from each branch through `UNION ALL`,
  apply the predicate once after the `CALL`, and count or group once. `UNION
  ALL` keeps SHAPE-A's per-branch counting, and the Go merge reduces the
  one-row-per-bucket result to the same answer SHAPE-A's per-branch rows
  give. The outer aggregation is safe here only because this form never
  reaches NornicDB, where aggregation over a `CALL` result collapses (see
  [NornicDB Pitfalls](nornicdb-pitfalls.md));
- relationships run an unscoped `MATCH (n:<Label>) WHERE n.id = $entity_id
  RETURN 1 AS hit LIMIT 1` probe per anchor label and run the scoped statement
  only for labels that hit, then the scoped unlabeled fallback. The probe
  result never reaches the caller: an ungranted anchor still gets the same 404
  as a missing id.

### Residual: probe timing

The response for an ungranted anchor is byte-identical to the response for a
missing id: same status, body, errors and logs. The work is not identical. An
ungranted anchor on a probed label runs one more scoped statement than a
missing id (14 probes, one scoped labeled read and the scoped fallback, against
14 probes and the fallback), a few milliseconds warm. A caller with a valid
scoped token who measures latency statistically over the network could infer
that an id exists on one of the probed labels. This is rated P3 and accepted;
it discloses existence only, never a name, id or edge of another tenant.

### Residual: backend must match the configuration

`ESHU_GRAPH_BACKEND` must match the Bolt backend actually behind the graph
connection. Setting it to `neo4j` while pointing at NornicDB selects the
list-`EXISTS` form, which leaks on NornicDB. This is the same trust boundary the
other backend branches in the query layer already rely on; there is no runtime
cross-check.

The span attribute `eshu.infra_scope_dialect` records which dialect a read used;
see [Graph-read safety](telemetry/graph-read-safety.md).

## Proof

- Default-lane pins of the Neo4j predicate text
  (`infra_scope_predicate_pin_test.go`) and of probe/scoped-read error,
  fail-closed and shared-deadline behavior (`infra_scope_dialect_errors_test.go`).
- Row-set equality with SHAPE-A and with an oracle computed from the fixture,
  at g1, g5 and the cap, plus the negative authorization cases:
  `go/internal/query/infra_scope_neo4j_equivalence_live_test.go`.
- The `category=argocd` path: byte-identity digests of the NornicDB statements
  and a Neo4j statement-shape pin (`infra_argocd_search_dialect_test.go`), and a
  live row-set equality test against SHAPE-A (g1, g5) and the oracle (g1, g5,
  cap) with a dual-labeled node and negatives
  (`infra_scope_neo4j_argocd_live_test.go`).
- The aggregates: digests of the NornicDB statements, per-statement Neo4j
  shape pins, cap parity and the empty-grant no-read case
  (`infra_scope_aggregate_dialect_test.go`); the shared backend guard
  (`TestInfraScopeListExistsNeverSelectedOffNeo4j`) lists the count and
  inventory routes; the `UNION ALL` join count (a bare `UNION` would
  de-duplicate a dual-labeled node); a live equality test of the total and the
  provider, environment and label rollups against SHAPE-A and an oracle at g1,
  g5 and the cap, with a dual-labeled node and negative nodes that must never
  be counted, plus a cold under-10 s budget test
  (`infra_scope_neo4j_aggregate_live_test.go`). At g1 and g5 the SHAPE-A
  reference is the per-branch NornicDB statement. At the cap the per-branch
  form runs out of heap on Neo4j, so the reference is the hoisted statement
  with the SHAPE-A predicate substituted after the `CALL`: a predicate
  equivalence check inside the new structure, not a comparison against the
  per-branch statement.
- Before/after timings, cold and warm:
  `docs/internal/evidence/7215-scoped-infra-neo4j-dialect.md` and
  `docs/internal/evidence/7231-scoped-infra-aggregate-neo4j-dialect.md`.

Do not move the list-`EXISTS` form onto NornicDB without a live NornicDB proof
that it no longer leaks.
