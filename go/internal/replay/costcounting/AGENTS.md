# replay/costcounting — agent scope

## Owned surface

- `go/internal/replay/costcounting/` — the R-16 deterministic cost-counting gate.
- `testdata/cassettes/replayoffline/*.cost-budget.json` — the per-scenario
  operation-count budgets.

## Non-negotiable invariants

- The PRIMARY assertion MUST read a real `eshu_dp_*` instrument off the
  `sdkmetric.ManualReader` (via the production `telemetry.NewInstruments`
  registry), NOT a hand-counted statement slice. A re-implemented counter is a
  false green — the whole point is to assert what production actually records.
- The gate MUST drive the production `cypher.CanonicalNodeWriter` (or the real
  intent/query path for other counters), never a re-implementation of the
  projection.
- Keep the N+1 negative control and prove it EXCEEDS the budget. If a refactor
  makes it fit within budget, the budget is too loose — tighten it, do not
  delete the control.
- Budgets are the EXACT deterministic counts. Because the scenario is
  deterministic, do not pad the budget "to absorb evolution" — a legitimate
  count change must refresh the budget deliberately (R-6 path) so the diff is
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
