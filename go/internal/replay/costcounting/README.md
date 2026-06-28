# replay/costcounting

Deterministic **cost-counting** assertions for the replay framework (epic
#4102, issue #4125, R-16). It asserts the *operation counts* a replayed
scenario produces — not wall-clock — against a committed per-scenario budget,
so an algorithmic regression (N+1 writes, quadratic fan-out) fails the gate on
every PR, credential-free.

## How it works

The gate drives the production `storage/cypher.CanonicalNodeWriter` over a
committed cassette materialization through:

1. a real `go.opentelemetry.io/otel/sdk/metric.ManualReader` + `MeterProvider`,
2. the production `telemetry.NewInstruments(meter)` registry (so the real
   `eshu_dp_*` counters record), and
3. an in-memory counting `cypher.Executor` (no graph backend, no Docker).

After `Write`, it `Collect`s the reader and asserts each `eshu_dp_*` value is
within the committed budget. The **primary** assertion reads
`eshu_dp_canonical_atomic_writes_total` off the real otel reader — not a
hand-counted statement slice — so it cannot drift from what production records.

## Budget

`testdata/cassettes/replayoffline/nested-directory-tree.cost-budget.json` pins
the **exact deterministic counts** for the scenario (`atomic_writes: 4`,
`statements_executed: 5`). Because the counts are deterministic, the budget is
the exact value: any increase trips the gate and must be refreshed deliberately
(R-6 cassette-refresh path), keeping the diff reviewable.

## Teeth

`TestCostBudget_N1_ExceedsBudget` is the mandatory negative control: it drives
the **same** production writer once per directory (the N+1 anti-pattern),
accumulating 12 atomic writes, and asserts the count **exceeds** the budget. If
the budget were too loose, this test fails. False-green guards also fail the
positive test if any instrument reads 0.

## Relation to Epic B

Complements the B-8/B-9 wall-clock benches: counts here, nanoseconds there, over
the same deterministic cassette corpus.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/costcounting/ -count=1 -v
```
