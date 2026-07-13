# replay/costcounting — agent scope

## Owned surface

- `go/internal/replay/costcounting/` — the R-16 deterministic cost-counting gate.
- `testdata/cassettes/replayoffline/*.cost-budget.json` — the per-scenario
  operation-count budgets.

## Scenarios (C-14, issue #4367)

One scenario per distinct `reducer_domain` (`specs/fact-kind-registry.v1.yaml`):
`code_graph_projection` (`cost_counting_test.go`, drives
`cypher.CanonicalNodeWriter`), `semantic_entity_materialization`
(`semantic_entity_cost_test.go`, drives
`cypher.SemanticEntityWriter.WriteSemanticEntities` through
`cypher.InstrumentedExecutor`), and `documentation_materialization`
(`documentation_edges_cost_test.go`, drives `cypher.EdgeWriter.WriteEdges` with
`EdgeWriter.Instruments` set). Shared test helpers (the group-counting
executor, the path-parameterized budget loader) live in
`cost_scenario_helpers_test.go`. See README.md for the full instrument/budget
table before adding a fourth scenario.

## Non-negotiable invariants

- The PRIMARY assertion MUST read a real `eshu_dp_*` instrument off the
  `sdkmetric.ManualReader` (via the production `telemetry.NewInstruments`
  registry), NOT a hand-counted statement slice. A re-implemented counter is a
  false green — the whole point is to assert what production actually records.
- The gate MUST drive a production writer for the domain's real projection
  hook (`cypher.CanonicalNodeWriter`, `cypher.SemanticEntityWriter`,
  `cypher.EdgeWriter`, or the real intent/query path for other counters),
  never a re-implementation of the projection.
- Keep the N+1 negative control and prove it EXCEEDS the budget. If a refactor
  makes it fit within budget, the budget is too loose — tighten it, do not
  delete the control. When the writer batches same-key rows together (as
  `SemanticEntityWriter` does per entity label), the N+1 fixture MUST share a
  batching key across its rows — distinct keys already emit one statement each
  regardless of call count and would make the negative control a no-op.
- Budgets are the EXACT deterministic counts. Because the scenario is
  deterministic, do not pad the budget "to absorb evolution" — a legitimate
  count change must refresh the budget deliberately (R-6 path for
  cassette-backed scenarios; a reviewed hand edit of the fixture rows and
  budget file together for the in-package-fixture scenarios) so the diff is
  reviewed, which is the gate's value.
- Keep the false-green guards: a 0 instrument value MUST fail (the instrument
  isn't recording).
- Stay credential-free: no Postgres, no graph backend, no Docker.

## Skill routing

- `eshu-diagnostic-rigor` for the instrument/throughput reasoning.
- `golang-engineering` for Go edits and tests.
- `telemetry-coverage-discipline` if you add a new `eshu_dp_*` instrument to assert.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/costcounting/ -count=1
```
