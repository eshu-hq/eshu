# deployable_unit_edges materialized-edge dispatch (#5993)

`resolveDeployableUnitMaterializedEdges` was fully implemented and reached by
nothing but its own tests. Three layers were unwired: `deployableUnitFamilyOdu()`
was built but never added to `catalogSeed`, `MaterializedEdgeOduResolver.Resolve`
had no `deployable_unit_edges` case, and two waiver rows kept the coverage gate
from asking either question. This change wires all three and drops the waivers.

`perf-evidence` selects this change because
`go/internal/ifa/materialized_edges_deployable_unit.go` is on its hot-file list.
This note records why that file's change carries no runtime cost, and does so by
derivation rather than by measurement, because a measurement here would be
weaker than the derivation.

## Measurements

Benchmark Evidence: none required, and a benchmark would be the wrong
instrument. The change to the hot file is a compile-time identity, provable by
reading it rather than by timing it:

```
-	expected, err := LoadExpectedEdges(expectedEdgesPath, "deployable_unit_edges")
+	expected, err := LoadExpectedEdges(expectedEdgesPath, deployableUnitEdgesFamily)
-	registry, err := MaterializedEdgeDomainEdgeTypes("deployable_unit_edges")
+	registry, err := MaterializedEdgeDomainEdgeTypes(deployableUnitEdgesFamily)

const deployableUnitEdgesFamily = "deployable_unit_edges"
```

The const's value is byte-identical to both literals it replaces, and Go
resolves it at compile time, so the emitted code is unchanged. Verify with the
operation rather than trusting this sentence:

```
rg -n 'deployableUnitEdgesFamily\s*=' go/internal/ifa/materialized_edges_deployable_unit.go
rg -n 'deployableUnitEdgesFamily|"deployable_unit_edges"' go/internal/ifa/materialized_edges_deployable_unit.go
```

Both call sites take the const; no literal remains in that file.

## Regression assessment

No-Regression Evidence: the hot-file change is a constant substitution with an
identical value, so there is no runtime delta to measure — the compiled behavior
is the same program. The two genuinely new behaviors in this PR are both
**gate-time only**, never on a request, write, or reducer path:

- `catalog_seed.go` gains one `deployableUnitFamilyOdu()` call. `catalogSeed` is
  read only by `Catalog()`/`CatalogByName()` (`catalog.go:20-31`), whose only
  production consumer is `go/cmd/ifa/coverage.go:103`. The live gate drivers take
  cassettes by path (`-cassette`) and never the compiled catalog, so no live
  digest can shift. Derive it:
  `rg -n 'CatalogByName\(\)|Catalog\(\)' go/cmd go/internal --glob '!*_test.go'`
- `materialized_edges.go` gains one `case` in a `switch` that previously fell to
  `default`. It runs inside `ifa-materialized-edge-coverage`, an in-process Go
  test over committed fixtures.

Neither adds a query, a lock, an I/O call, a graph write, or a Postgres round
trip. No Cypher, no worker claim, no lease, no batching knob, no concurrency
setting, and no runtime Compose/Helm value is touched — the categories this
gate's own message names.

## Operator surface

No-Observability-Change: this PR adds no metric, span, log line, or status
field, and changes none. Everything it touches executes under `go test` in the
gate binaries, where the reader is a failing assertion rather than a dashboard.

The one reader-facing addition is a test log line —
`TestDeployableUnitFamilyResolvesThroughTheManifestResolver` emits the guard's
own detail via `t.Logf`, matching the sibling families' convention:

```
odù "odu:ifa-deployable-unit-family": DiscoveredEvidence -> relationships.Resolve
-> ExtractDeployableUnitCorrelationRows reproduces the expected 1-edge set exactly
across all 1 registry types, proves both AdmittedDeployableUnitRows drop reasons,
and matches the admitted edge's non-identity properties
```

That line exists because the guard's covered detail was previously invisible on a
green run: `OK:true` renders only inside `goldengate.Finding` failure messages, so
a passing coverage check printed nothing an operator or agent could cite.
