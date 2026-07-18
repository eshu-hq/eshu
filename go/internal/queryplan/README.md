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
- `non_hot_reason` records why an inventory-only support or bounded read is not
  independently registered as a hot query.
- `non_hot` records a machine-checked closed classification, source digest, and
  applicable key/result bounds for newly audited support reads.

The production-source test discovers this inventory with the Go parser. A new
file, symbol, or call, a changed call count, a stale registration, an unknown hot
entry, or a missing disposition fails the gate. A developer adding graph-query
execution must therefore update the inventory and either register its hot query
shape or explain the non-hot classification in the same change.

`testdata/handler-hot-cypher.yaml` holds handler-owned hot shapes. Its required
entries must be linked back to production execution callsites. The manifest
stores no copied Cypher. Instead, each entry records an exact-text SHA-256 and a
`query_fragment` owned by its production builder. The query-package binding
test supplies the actual builder output, verifies the fingerprint, and then
runs the full anchor, traversal, ordering, schema, and plan validation against
those production-owned bytes. The older `testdata/hot-cypher.yaml` continues to
hold cross-service static shapes.

The inventory still contains 102 pre-existing `non_hot_reason` entries. They
remain accepted during the staged typed-disposition migration so this issue
does not invent behavioral limits for unrelated callsites. Source-digest
revalidation applies to entries using the typed `non_hot` form, including the
bounded workload repository-name hydration helper; the migration is not
presented as complete.

## Live Plan Proof

Live backend calls remain outside this package. The build-tagged test
`internal/query/queryplan_profile_live_test.go` applies only the schema names
required by the handler manifest to an isolated Neo4j database, binds every
entry to its exact production builder output, runs `PROFILE`, and fails on
`AllNodesScan` or a missing bounded anchor operator. Entries whose production
predicate is not indexable must explicitly declare their accepted bounded
label or relationship-type anchor operator; generic scans remain forbidden.
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
- Every handler hot entry must bind its exact production-builder SHA-256 and
  anchor fragment to the declared builder symbol.

No-Regression Evidence: `scripts/verify-query-plan-regression.sh` exercises the
static fixtures, exhaustive production callsite inventory, deliberately bad
query/plan fixtures, and the query-package production-builder binding test.
With the documented Neo4j environment variables and
`ESHU_QUERYPLAN_PROFILE_ISOLATED=1` set, run the isolated live promotion proof
as:

```bash
go test -tags queryplan_profile_live ./internal/query \
  -run TestHandlerQueryplanProfilesRejectWholeGraphScans -count=1
```

No-Observability-Change: this package performs static validation only. It adds
no API route, graph query, graph write, metric, span, runtime knob, queue work,
or provider call.
