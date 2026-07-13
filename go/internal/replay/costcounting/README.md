# replay/costcounting

Deterministic **cost-counting** assertions for the replay framework (epic
#4102, issue #4125, R-16). It asserts the *operation counts* a replayed
scenario produces — not wall-clock — against a committed per-scenario budget,
so an algorithmic regression (N+1 writes, quadratic fan-out) fails the gate on
every PR, credential-free.

## Scenarios (C-14, issue #4367)

One scenario per distinct `reducer_domain`
(`specs/fact-kind-registry.v1.yaml`), each driving that domain's production
graph writer:

| Domain (`projection:<domain>`) | Test file | Writer driven | Primary instrument |
|---|---|---|---|
| `code_graph_projection` | `cost_counting_test.go` | `storage/cypher.CanonicalNodeWriter` | `eshu_dp_canonical_atomic_writes_total` |
| `semantic_entity_materialization` | `semantic_entity_cost_test.go` | `storage/cypher.SemanticEntityWriter` | `eshu_dp_neo4j_batches_executed_total` |
| `documentation_materialization` | `documentation_edges_cost_test.go` | `storage/cypher.EdgeWriter` | `eshu_dp_shared_edge_write_groups_total` |

`code_graph_projection` reuses the existing nested-directory-tree scenario: the
"code" family's `file`/`repository` kinds project through
`CanonicalNodeWriter`, and the repository/directory canonical writes that test
already drives ARE the code-graph canonical projection path, so no second
scenario is needed to honestly claim the domain.

## How it works

Each scenario drives its domain's production writer through:

1. a real `go.opentelemetry.io/otel/sdk/metric.ManualReader` + `MeterProvider`,
2. the production `telemetry.NewInstruments(meter)` registry (so the real
   `eshu_dp_*` counters record), and
3. an in-memory counting executor (no graph backend, no Docker) — either
   wrapped in the production `storage/cypher.InstrumentedExecutor` (semantic
   entity) or passed straight to a writer whose own `Instruments` field the
   production wiring already sets (canonical writer, edge writer).

After the write call, each test `Collect`s the reader and asserts its
**primary** `eshu_dp_*` instrument is within the committed budget, reading it
off the real otel reader — not a hand-counted statement slice — so it cannot
drift from what production records.

## Input data

`code_graph_projection` drives its writer over a committed cassette
materialization
(`testdata/cassettes/replayoffline/nested-directory-tree.json`). The other two
domains' writers (`SemanticEntityWriter`, `EdgeWriter`) operate over flat
reducer rows, not a `CanonicalMaterialization`, so their deterministic input is
an in-package Go literal fixture — the same convention
`semantic_entity_test.go` already uses — defined in each scenario's test file.

## Budget

Each scenario has a `.cost-budget.json` file under
`testdata/cassettes/replayoffline/` that pins the **exact deterministic
counts**:

- `nested-directory-tree.cost-budget.json`: `eshu_dp_canonical_atomic_writes_total: 4`, `statements_executed: 5`.
- `semantic-entity-materialization.cost-budget.json`: `eshu_dp_neo4j_batches_executed_total: 1`, `statements_executed: 12`.
- `documentation-materialization.cost-budget.json`: `eshu_dp_shared_edge_write_groups_total: 1`, `statements_executed: 2`.

The fixture-backed budgets (semantic entity, documentation edges) carry a
`cassette` field explaining there is no cassette, and their `refresh_path` is a
hand edit of the fixture rows and budget file together in the same reviewed
diff, since no credentialed cassette refresh applies. Because every count is
deterministic, an increase trips the gate and must be refreshed deliberately,
keeping the diff reviewable.

## Teeth

Every scenario has an `_N1_ExceedsBudget` mandatory negative control: it
drives the **same** production writer once per input unit (directory / fixture
row) instead of once for the whole batch — the N+1 anti-pattern — and asserts
the accumulated instrument value **exceeds** the budget. If the budget were too
loose, this test fails. False-green guards also fail the positive tests if any
instrument reads 0.

## Relation to Epic B

Complements the B-8/B-9 wall-clock benches: counts here, nanoseconds there. Over
the same deterministic cassette corpus where a cassette exists.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/costcounting/ -count=1 -v
```
