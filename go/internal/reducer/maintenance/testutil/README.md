# Maintenance Test Utilities

## Purpose

Shares the metric-reading helpers the maintenance leaf test packages
need in common. Created under #7648 when the flat `maintenance`
package split into leaves: Go test files cannot share unexported
symbols across a package boundary, so one exported leaf replaces nine
copies.

## Ownership boundary

Owns `CounterValue`, `GaugeValue`, and `HasAttrs` only. Does not own
any production behavior, and production code must never import this
package. Family-local helpers (gauge/histogram readers used by one
leaf's tests) stay in that leaf's own test files.

## Exported surface

- `CounterValue` — read one int64-sum data point by attributes
- `GaugeValue` — read one int64-gauge data point by attributes
- `HasAttrs` — exact attribute-set match for histogram points

See `doc.go` for the full contract.

## Dependencies

- `go.opentelemetry.io/otel` — attribute and metricdata shapes

## Telemetry

None. Test helpers emit no signals.

## Gotchas / invariants

- Every helper takes `*testing.T` and fails the test on a missing
  metric or point; none returns an error.
- Keep helpers generic over metric names and attributes; leaf
  specifics (metric names, expected values) stay at the call site.

## Related docs

- `go/internal/reducer/maintenance/README.md`
