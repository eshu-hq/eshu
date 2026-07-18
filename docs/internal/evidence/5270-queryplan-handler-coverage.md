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

After the first inventory implementation, the new production-binding
regression also failed all 14 original handler entries with `manifest must not copy
production Cypher`. That second RED proved that a live test loading copied YAML
could still profile a query different from the one the handler executes.

After rebasing onto `2209892807`, the source inventory failed again on two
newly merged workload-resolution symbols containing three direct graph calls.
That third RED proves the guardrail catches query execution added by another PR
instead of preserving a stale green snapshot.

## Static Coverage Proof

The gate now parses non-test Go files recursively beneath `internal/query`. It
records every execution site by file, enclosing function or method, and exact
call count. Each site must link to hot entry IDs or state a non-hot reason. It
fails for a new file or symbol, an added or removed call, a stale registration,
an unknown hot ID, a duplicate, or a missing disposition.

Each handler plan entry contains no Cypher copy. It records an exact-text
SHA-256 and binds its query anchor fragment to the production builder symbol.
The query-package gate supplies the actual builder output, rejects a fingerprint
mismatch, and validates the populated manifest. Changing the production query
or removing its anchor therefore fails before the live plan test can run.

Seven handler query shapes previously assembled Cypher inline. Their query construction was
extracted without changing emitted bytes. Production-capture tests compare
handler execution with the builder output and with SHA-256 baselines derived
from pre-extraction commit `50d13be62c`. The two workload-resolution shapes
merged later are frozen against their pre-extraction source at `2209892807`.
The proof covers repository-anchored, all-scope, scoped, workload-property,
workload-relationship, resource-workload, instance-workload, repository-name
hydration, and both repository-path directions.

The handler manifest registers 16 plan shapes spanning the repository-anchored
entity and code reads corrected under #5244 and the import, entity-map, cloud
resource, call-graph, graph-entity, workload-resolution, and
resource-investigation handler families.
The sibling issue's still-unfixed unlabeled resolver branches remain visible in
the exhaustive inventory; this change does not weaken the unlabeled-anchor rule
to admit them.

The exhaustive inventory retains 102 existing prose `non_hot_reason`
dispositions during the staged typed-disposition migration. This issue does not
invent unsupported key bounds or result limits for unrelated callsites. Typed
source-digest enforcement now covers the newly merged workload repository-name
hydration helper as a batch bounded to 101 keys/results. A source change forces
that typed disposition to be re-audited.

## Isolated PROFILE Proof

Backend: isolated `neo4j:2026-community`, reporting Neo4j 2026.05.0. The test
binds the 16 exact production-builder outputs, applies only schema objects named
by the handler manifest, waits for the indexes, then runs every bound entry with
`PROFILE`.

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

Result: 16/16 subtests passed. Entity resolution, code search, import reads,
entity-map traversal, call-graph metrics, and resource-investigation reads used
`NodeUniqueIndexSeek` or `NodeIndexSeek` where the production predicate is
indexable. Graph-entity counts used
`NodeCountFromCountStore`; its substring list used the explicitly label-bounded
`NodeByLabelScan`. The resource workload and repository-path shapes used their
explicitly admitted `DirectedRelationshipTypeScan` and `NodeByLabelScan`
anchors. No plan contained `AllNodesScan`, `CartesianProduct`, or an unbounded
expansion.

The pinned NornicDB Bolt/HTTP surface returned no plan object for `PROFILE`, so
the NornicDB side remains enforced through the shared Cypher shape and exact
schema-name contract. The live test fails rather than treating a missing plan
as success on backends that claim plan support.

No-Regression Evidence: the focused production-binding and byte-preservation
tests, `go test ./internal/queryplan -count=1`, the queryplan verification
script, and the isolated 16-query PROFILE test above all pass.

Performance Evidence: the prior gate profiled 14 copied fixture shapes and did
not prove the bytes executed by production handlers. The finished gate profiles
16 exact production-builder shapes on isolated Neo4j 2026.05.0 with an empty
proof graph (0 terminal result rows per entry). All 16 completed in 0.14 seconds
after schema warm-up; the two newly merged workload queries used
`NodeIndexSeek`, and no entry used `AllNodesScan`, `CartesianProduct`, or an
unbounded expansion. This is planner-regression evidence, not a retained-corpus
latency claim; production query text and runtime call counts are byte-for-byte
unchanged.

No-Observability-Change: this is a static/test guardrail. It adds no production
query, API request, graph write, metric, span, log, runtime knob, or queue work.
