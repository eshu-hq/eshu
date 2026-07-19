# Issue #5270 Queryplan Handler Coverage Evidence

## Scope

This change closes the guardrail gap without changing valid production query
text. It adds an exhaustive production execution inventory, handler query-plan
registrations, and isolated live plan assertions. One invalid runtime branch is
now fail-closed: a resolved resource without a whitelisted infrastructure label
returns an error before any graph read instead of issuing an unlabeled scan.

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
call count. Each hot site must link to entry IDs and freeze the full execution
symbol with a source SHA-256; non-hot sites state their bounded disposition. It
fails for a new file or symbol, an added or removed call, a same-count source
reroute, a stale registration, an unknown hot ID, a duplicate, or a missing
disposition. A failing-first regression proves changing a hot callsite digest
while retaining its single graph call is rejected.

Each handler plan entry contains no Cypher copy. It records exact query and
builder-source SHA-256 values and binds its query anchor fragment to the
production builder symbol. The 22 legacy Cypher entries also contain no query
copy: each records its declared production owner plus exact source and query
fingerprints.
The query-package gates supply direct builder output or capture the query emitted
by the production execution path, reject a fingerprint mismatch, and validate
the populated manifests. Changing a production query or removing its anchor
therefore fails before the live plan test can run.

Seven handler query shapes previously assembled Cypher inline. Their query construction was
extracted without changing emitted bytes. Production-capture tests compare
handler execution with the builder output and with SHA-256 baselines derived
from pre-extraction commit `50d13be62c`. The two workload-resolution shapes
merged later are frozen against their pre-extraction source at `2209892807`.
The proof covers repository-anchored, all-scope, scoped, workload-property,
workload-relationship, resource-workload, instance-workload, repository-name
hydration, and both repository-path directions. A safe-variant family digest
also freezes 324 reachable shapes: repository entity type filters; exact,
substring, and language code filters; all/scoped workload resolution; every
whitelisted resource label, identity property, path direction, and minimum or
maximum traversal depth; and the 31 cloud-resource list shapes not already
represented by its registered resource-type-only entry. Together with that
entry, the proof covers all 32 combinations of optional provider, resource type,
region, account, and keyset cursor predicates.

The handler manifest registers 16 plan shapes spanning the repository-anchored
entity and code reads corrected under #5244 and the import, entity-map, cloud
resource, call-graph, graph-entity, workload-resolution, and
resource-investigation handler families.
The legacy manifest contributes 22 production-bound Cypher shapes across supply
chain, deployable catalog, service context, relationship story, readiness,
change-surface, relationship catalog, and infrastructure reads. Its remaining
entry is a Postgres read-model declaration and is intentionally excluded from
Cypher PROFILE counts.
The 18 unsafe global entity/code variants are a separate, immutable family tied
to #5318. Isolated PROFILE proved `AllNodesScan` for all/scoped entity resolution
and `DirectedRelationshipTypeScan` for all/scoped code search. #5270 therefore
does not register or claim those variants as safe; their exact/substring,
optional-language, type-filter, and access-scope branches remain source- and
family-digest guarded until #5318 replaces their scan shapes.

The exhaustive inventory retains 102 existing prose `non_hot_reason`
dispositions without leaving a writable escape hatch. Their exact source
digests are frozen to main baseline `2209892807`; a new prose disposition, a
different baseline, or any source change fails validation and forces a typed
hot/non-hot audit. Typed source-digest enforcement also covers the workload
repository-name hydration helper as a batch bounded to 101 keys/results.

## Isolated PROFILE Proof

Backend: the hermetic script pins Neo4j by image digest
`sha256:6c162e2432f861f2c4e3da77a6ba478e7f10e2160b870541f85294532bc6ff5f`
(Neo4j 2026.05.0 in the proof run). It starts a uniquely named isolated
container on an ephemeral port, applies only schema objects named by the handler
and legacy manifests, waits for indexes, profiles the 16 handler entries, 22
legacy entries, and all 324 safe production variants, and removes the container
through an exit trap.

```text
scripts/verify-query-plan-profile.sh
```

Result: 362/362 registered and safe production plan shapes passed in the tagged
test. Entity resolution, code search, import reads,
entity-map traversal, call-graph metrics, and resource-investigation reads used
`NodeUniqueIndexSeek` or `NodeIndexSeek` where the production predicate is
indexable. Graph-entity counts used
`NodeCountFromCountStore`; its substring list used the explicitly label-bounded
`NodeByLabelScan`. The resource workload and repository-path shapes used their
explicitly admitted `DirectedRelationshipTypeScan` and `NodeByLabelScan`
anchors. The eight cloud-resource list shapes without a resource-type predicate
or cursor used the explicitly admitted `NodeByLabelScan`; the other 24
cloud-resource shapes used `NodeIndexSeek` or `NodeIndexSeekByRange`. No plan
contained `AllNodesScan`, `CartesianProduct`, or an unbounded expansion. The
closed operator policy lives in Go code; manifest data cannot add arbitrary
accepted operators.

The pinned NornicDB Bolt/HTTP surface returned no plan object for `PROFILE`, so
the NornicDB side remains enforced through the shared Cypher shape and exact
schema-name contract. The live test fails rather than treating a missing plan
as success on backends that claim plan support.

No-Regression Evidence: the focused handler and legacy production-binding and
byte-preservation tests, `go test ./internal/queryplan -count=1`, the queryplan
verification script, and the isolated 362-shape PROFILE test above all pass. The
exact final regression command passed with the tagged PROFILE package in 11.464
seconds and the full source-binding, unit, container-start, readiness, PROFILE,
and cleanup command in 22.99 seconds.

Performance Evidence: the prior gate profiled 14 copied fixture shapes and did
not prove the bytes executed by production handlers. The finished gate profiles
16 exact handler shapes, 22 production-bound legacy shapes, and 324 hash-frozen
safe variants on isolated Neo4j 2026.05.0 with an empty proof graph (0 terminal
result rows per shape). A one-off production-builder probe first profiled all 32
cloud-resource combinations on the same pinned planner: eight shapes without a
resource-type predicate or cursor used `NodeByLabelScan`, while the other 24
used the resource-type index through `NodeIndexSeek` or `NodeIndexSeekByRange`.
The workload variants used `NodeIndexSeek`, and no accepted shape used
`AllNodesScan`, `CartesianProduct`, or an
unbounded expansion. This is planner-regression evidence, not a retained-corpus
latency claim; production query text and runtime call counts are byte-for-byte
unchanged for valid resolved resources. The invalid unlabeled resource branch
now executes zero graph calls by design.

No-Observability-Change: this adds no production query, API request, graph write,
metric, span, log, runtime knob, or queue work. The fail-closed resource-label
invariant reuses the existing handler request span and error response path.
