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

The production-source test discovers this inventory with the Go parser. A new
file, symbol, or call, a changed call count, a stale registration, an unknown hot
entry, or a missing disposition fails the gate. A developer adding graph-query
execution must therefore update the inventory and either register its hot query
shape or explain the non-hot classification in the same change.

`testdata/handler-hot-cypher.yaml` holds handler-owned hot shapes. Its required
entries must be linked back to production execution callsites. Every handler
entry also declares a `query_fragment`; the gate parses the owning Go symbol
and fails if that anchor fragment drifts out of production source. The older
`testdata/hot-cypher.yaml` continues to hold cross-service hot shapes; neither
manifest weakens the existing unlabeled-anchor, traversal, ordering, schema, or
forbidden-operator checks.

## Live Plan Proof

Live backend calls remain outside this package. The build-tagged test
`internal/query/queryplan_profile_live_test.go` applies only the schema names
required by the handler manifest to an isolated Neo4j database, runs `PROFILE`
for every handler entry, and fails on `AllNodesScan` or a missing bounded anchor
operator. NornicDB builds that do not expose a plan over Bolt remain covered by
the shared static shape and schema contract; the live proof must not be pointed
at retained data because it creates schema objects.

## Evidence Contract

- Cypher entries must be anchored on declared label/property pairs.
- Variable-length traversals must have an upper bound.
- Paginated paths using `SKIP` must order before offsetting.
- Required schema names must exist in the supplied schema statement list.
- SQL/read-model entries must carry caveats so the gate does not pretend they
  have graph plans.
- Every production query execution callsite must have an exact inventory entry
  and an explicit hot or non-hot disposition.
- Every handler hot entry must bind its anchor fragment to the declared source
  symbol.

No-Regression Evidence: `go test ./internal/queryplan -count=1` exercises the
accepted hot-path shapes, the exhaustive production callsite inventory, and
deliberately bad query/plan fixtures. With the documented Neo4j environment
variables and `ESHU_QUERYPLAN_PROFILE_ISOLATED=1` set, run the isolated live
promotion proof as:

```bash
go test -tags queryplan_profile_live ./internal/query \
  -run TestHandlerQueryplanProfilesRejectWholeGraphScans -count=1
```

No-Observability-Change: this package performs static validation only. It adds
no API route, graph query, graph write, metric, span, runtime knob, queue work,
or provider call.
