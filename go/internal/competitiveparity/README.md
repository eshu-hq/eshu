# competitiveparity

## Purpose

`competitiveparity` owns the offline #3265 gate for shipped Eshu capability
surfaces. It validates whether first-run evidence, operator digest artifacts,
investigation evidence packets, and capability catalog surfaces remain reachable,
documented, locally exercised, and tied back to their proof issues.

## Ownership boundary

This package scores caller-supplied inventories and renders the resulting
`competitive_parity_gate.v1` artifact. It does not read files, call GitHub,
start runtimes, open network connections, inspect graph or Postgres state, or
decide whether a residual gap needs a new issue.

## Exported surface

See `doc.go` for the godoc contract. Callers use `DefaultExpectations`,
`Validate`, `RenderJSON`, and `RenderMarkdown`, plus the report and expectation
types in `types.go`.

## Dependencies

The package imports only the Go standard library. CLI integration in
`go/cmd/eshu` supplies Cobra command paths, generated capability catalog
surfaces, public documentation text, and local exercise results.

## Telemetry

No telemetry is emitted here. The gate is pure validation over local inputs and
does not create spans, metrics, logs, status rows, or graph facts.

## Gotchas / invariants

Missing surfaces and failed exercises are hard failures. Residual gaps must link
to existing issues, while related closed issues record the proof lineage for each
surface family. Keep rendered output deterministic by sorting checks and
surfaces before returning a report.

## Related docs

- `docs/public/reference/local-testing/competitive-parity-gate.md`
- `docs/public/reference/cli-reference.md`
