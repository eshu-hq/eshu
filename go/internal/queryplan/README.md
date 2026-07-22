# internal/queryplan

`internal/queryplan` validates the static query-plan regression manifests for
hot graph read paths and inventories every production graph-query execution
site under `internal/query`. It checks that each registered Cypher path declares
its source owner, selective anchors, schema evidence, bounds, ordering, and
optional backend plan-operator fixtures.

The package does not connect to graph backends. It is a deterministic CI gate
for query shape regressions and backend caveat drift; live `EXPLAIN` or
`PROFILE` capture remains separate runtime evidence.

## Source Coverage Contract

`testdata/query-source-coverage.yaml` records every direct `Run` or
`RunSingle` call in non-test Go source recursively beneath `internal/query`. Each
record is keyed by source file and enclosing function or method, includes the
exact call count, and has exactly one disposition:

- `entry_ids` links a registered hot callsite to one or more query-plan entries.
  Every hot callsite freezes the full production execution symbol with
  `source_sha256`, so a same-count reroute to a different query forces review.
- `non_hot_reason` records why an inventory-only support or bounded read is not
  independently registered as a hot query.
- `non_hot` records a machine-checked closed classification, source digest, and
  applicable key/result bounds for newly audited support reads.

The production-source test discovers this inventory with the Go parser. A new
file, symbol, or call, a changed call count, a stale registration, an unknown hot
entry, changed hot-callsite source digest, or a missing disposition fails the gate. A developer adding graph-query
execution must therefore update the inventory and either register its hot query
shape or explain the non-hot classification in the same change.

`testdata/handler-hot-cypher.yaml` and `testdata/hot-cypher.yaml` hold the
handler-owned and legacy cross-service hot shapes. Their required entries must
be linked back to production execution callsites, and neither manifest stores a
copied Cypher query. Each entry records its production source owner, source
SHA-256, and exact-text query SHA-256. Handler entries also bind a
`query_fragment` to the owning builder. Query-package binding tests supply direct builder
output or capture the query emitted by the production execution path, verify the
fingerprints, and run the full anchor, traversal, ordering, schema, and plan
validation against those production-owned bytes.

The inventory still contains 102 pre-existing `non_hot_reason` entries, but they
are immutable migration debt rather than an open classification path. Their
exact source digests are frozen to a named main baseline; new prose entries,
source drift, or a different baseline fail validation and require a typed audit.
Source-digest revalidation also applies to entries using the typed `non_hot`
form, including the bounded workload repository-name hydration helper.

## Live Plan Proof

Live graph calls remain outside this package. The build-tagged test
`internal/query/queryplan_profile_live_test.go` applies only the schema names
required by both manifests to an isolated Neo4j database, binds every Cypher
entry to its exact production builder or execution-path output, and profiles 21
handler entries, 26 legacy Cypher entries, and 423 hash-frozen safe production
variants. It also
includes 140 distinct import-dependency queries mapped from all 244 valid API
and MCP request shapes. All 470 profiled shapes must avoid `AllNodesScan` and
expose an admitted bounded anchor operator.
The accepted label and relationship-type scan exceptions are a closed Go policy;
manifest data cannot expand that allowlist. Cloud-resource browsing now has one
UID-bounded graph hydration plan plus a separately hash-frozen 64-variant
Postgres page family (32 filter/cursor combinations for all-scope and scoped
access). `cloud_resource_list_store_live_test.go` executes that SQL family on a
20,000-row disposable corpus, requires an ordered owner-ledger index, and
enforces the 2-second interactive SLO.
The formerly reachable global entity/code graph variants are fail-closed under
#5318. Only repository-selected variants remain in the registered PROFILE
family; global name reads are bounded Postgres content-index queries with their
own exact SQL/plan proof.
NornicDB builds that do not
expose a plan over Bolt remain covered by the shared production shape and
schema contract; the live proof must not be pointed at retained data because it
creates schema objects.

## Evidence Contract

- Cypher entries must be anchored on declared label/property pairs.
- Variable-length traversals must have an upper bound.
- Paginated paths using `SKIP` must order before offsetting.
- Required schema names must exist in the supplied schema statement list.
- SQL/read-model entries must carry caveats so the gate does not pretend they
  have graph plans.
- Every production query execution callsite must have an exact inventory entry
  and an explicit hot or non-hot disposition.
- Every Cypher hot entry must bind its exact production query and source SHA-256
  to its declared builder or execution-path owner. Handler entries additionally
  bind an anchor fragment to the declared builder symbol.

No-Regression Evidence: `scripts/verify-query-plan-regression.sh` exercises the
static fixtures, exhaustive production callsite inventory, deliberately bad
query/plan fixtures, production-builder binding, and the required hermetic live
PROFILE proof. Run the live proof independently as:

```bash
scripts/verify-query-plan-profile.sh
```

No-Observability-Change: this package performs static validation only. It adds
no API route, graph query, graph write, metric, span, runtime knob, queue work,
or provider call.
