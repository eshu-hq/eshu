# Scanner Component Admission

## Purpose

`scannercomponent` is the neutral leaf that owns the scanner vocabulary and
the component-admission contract after the #8878 in-tree scanner removal.
The coordinator planner and the component admission callers both import this
package; they never import each other. Execution (claim input, analyzers,
runners, paging, seeding) lives in the `eshu-hq/eshu-scanner-worker`
satellite; this package never executes an analyzer.

## Ownership boundary

This package owns the frozen analyzer, target, and failure vocabularies, the
per-analyzer manifests and required grants, component-ID resolution, and the
seeded-artifact admission check (`VerifySeededArtifact`). It never touches
Postgres, the graph, the reducer, or the API/MCP surfaces, and it invents no
protocol: manifests speak the v1alpha3 shape the host already decodes.

It does not own claim planning (coordinator), claim execution (satellite),
finding admission (reducer), or grant minting (the `eshu component grant`
activation flow defines lifetimes).

## Exported surface

- `AnalyzerKind`, `TargetKind`, `FailureClass`, `FailureDisposition`,
  `ExecutionLane`, `AnalyzerLane` — frozen vocabularies with pinned wire
  strings.
- `IDImage`, `IDSBOM`, `IDOSRuntime`, `Version`,
  `CollectorSDKProtocolV1Alpha3`, `ManifestFor`, `AnalyzerFor`,
  `RequiredGrants` — per-analyzer manifests, grants, and ID resolution.
- `VerifySeededArtifact` — admission check that a seeded manifest artifact
  equals the platform pin.

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/component` — manifest, grant, and registry types the
  admission contract is expressed in.
- `go/internal/facts`, `go/internal/facts/supply/chain` — fact-kind and
  schema-version constants for emitted families.

## Telemetry

This package emits no metrics, spans, or logs of its own. The frozen
vocabulary strings survive as telemetry attribute values: analyzer,
target-kind, and failure-class dimensions recorded by the satellite and the
status writers keep the exact values pinned here.

## Gotchas / invariants

- Every frozen string is asserted verbatim in `vocabulary_test.go`. A rename
  or value change breaks the build on purpose.
- Manifests must declare every kind an analyzer can emit, including
  degraded-path warnings, or the host rejects real output at admission.
- The digest pins, the `Scanner-Publish-SHA` anchor, and the chart
  `seed.image` entries move together in a reviewed follow-up PR after any
  satellite republish; the publish workflow never auto-updates this package.
- No SDK vocabulary is published from here. An SDK addition is a separate
  versioned contract change.

## Related docs

- `docs/public/reference/security-intelligence.md#scanner-worker-boundary`
- `docs/public/reference/standalone-vulnerability-scanner-boundary.md`
