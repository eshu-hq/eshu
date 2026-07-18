# Issue #5270 Queryplan Handler Coverage Evidence

## Scope

This change closes the guardrail gap without changing production query text or
runtime behavior. It adds an exhaustive production execution inventory, handler
query-plan registrations, and isolated live plan assertions.

## False-Green Reproduction

Before the change, both existing gates passed while handler-owned graph queries
were absent from the queryplan fixture:

```text
cd go
go test ./internal/queryplan -count=1
bash ../scripts/verify-query-plan-regression.sh
```

The new source-discovery test initially failed with 130 unregistered
`Run`/`RunSingle`-owning symbols and 147 direct call expressions under
`go/internal/query`. That was the proven gap: the old validator inspected only
hand-copied fixture entries and never compared them with production query
execution sites.

## Static Coverage Proof

The gate now parses non-test Go files recursively beneath `internal/query`. It
records every execution site by file, enclosing function or method, and exact
call count. Each site must link to hot entry IDs or state a non-hot reason. It
fails for a new file or symbol, an added or removed call, a stale registration,
an unknown hot ID, a duplicate, or a missing disposition.

Each handler plan entry also binds a query anchor fragment to its declared Go
source symbol. Changing or removing the production anchor without updating the
plan registration now fails instead of leaving the copied fixture silently
green.

The handler manifest registers 14 plan shapes spanning the repository-anchored
entity and code reads corrected under #5244 and the import, entity-map, cloud
resource, call-graph, graph-entity, and resource-investigation handler families.
The sibling issue's still-unfixed unlabeled resolver branches remain visible in
the exhaustive inventory; this change does not weaken the unlabeled-anchor rule
to admit them.

## Isolated PROFILE Proof

Backend: isolated `neo4j:2026-community`, reporting Neo4j 2026.05.0. The test
applied only schema objects named by the handler manifest, waited for the
indexes, then ran every entry with `PROFILE`.

```text
ESHU_QUERYPLAN_PROFILE_LIVE=1 \
ESHU_QUERYPLAN_PROFILE_ISOLATED=1 \
ESHU_NEO4J_URI=bolt://127.0.0.1:17688 \
ESHU_NEO4J_USERNAME=neo4j \
ESHU_NEO4J_PASSWORD=<ephemeral-proof-password> \
ESHU_NEO4J_DATABASE=neo4j \
go test -tags queryplan_profile_live ./internal/query \
  -run TestHandlerQueryplanProfilesRejectWholeGraphScans -count=1 -v
```

Result: 14/14 subtests passed. Entity resolution, code search, import reads,
entity-map traversal, call-graph metrics, and resource-investigation reads used
`NodeUniqueIndexSeek` or `NodeIndexSeek`. Graph-entity counts used
`NodeCountFromCountStore`; its substring list used the explicitly label-bounded
`NodeByLabelScan`. No plan contained `AllNodesScan`, `CartesianProduct`, or an
unbounded expansion.

The pinned NornicDB Bolt/HTTP surface returned no plan object for `PROFILE`, so
the NornicDB side remains enforced through the shared Cypher shape and exact
schema-name contract. The live test fails rather than treating a missing plan
as success on backends that claim plan support.

No-Regression Evidence: `go test ./internal/queryplan -count=1`, the queryplan
verification script, and the isolated 14-query PROFILE test above all pass.

No-Observability-Change: this is a static/test guardrail. It adds no production
query, API request, graph write, metric, span, log, runtime knob, or queue work.
