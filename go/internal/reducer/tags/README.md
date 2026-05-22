# internal/reducer/tags

## Purpose

`reducer/tags` defines the tag-normalization seam and the helpers that
publish canonical-cloud readiness rows once normalization completes. The
package owns the contract, defensive scaffold copies, and validation shape; it
does not own a concrete normalizer.

## Ownership boundary

The package owns the accepted `RuntimeContract` scaffold, the `Normalizer` seam,
observation/result value shapes, validation, phase-row conversion, and the
publish helper. It does not own a concrete normalizer or graph writes; phase
rows go through `reducer.GraphProjectionPhasePublisher`.

## Exported surface

See `doc.go` and `go doc ./internal/reducer/tags` for the godoc contract.
Callers depend on `RuntimeContract`, `DefaultRuntimeContract`,
`RuntimeContractTemplate`, `Normalizer`, `ObservationBatch`, `ObservedResource`,
`NormalizedResource`, `NormalizationResult`, and `PublishNormalizationResult`.
The accepted scaffold has one component (`normalizer`) and one canonical
keyspace (`cloud_resource_uid`).

## Dependencies

- `go/internal/reducer` — `GraphProjectionPhaseKey`,
  `GraphProjectionPhaseState`, `GraphProjectionPhasePublisher`,
  `GraphProjectionKeyspaceCloudResourceUID`,
  `GraphProjectionPhaseCanonicalNodesCommitted`.

## Telemetry

The package does not emit metrics or spans. Callers wrap
`PublishNormalizationResult` with their own telemetry.

## Gotchas / invariants

- `PhaseStates` publishes only `(cloud_resource_uid,
  canonical_nodes_committed)` rows. Downstream domains that consume
  `resolved_relationships` still need the post-Phase-3 reopen mechanism; this
  package does not own it.
- Duplicate resources in one `NormalizationResult` collapse by
  `CanonicalResourceID`.
- Phase rows are sorted by `AcceptanceUnitID` for deterministic output.
- Blank `scopeID` or `generationID` is invalid. Zero `observedAt` falls back to
  `time.Now().UTC()`.
- `PublishNormalizationResult` is a no-op when the publisher is nil or the
  result produces no rows.

## Focused tests

```bash
cd go
go test ./internal/reducer/tags -count=1
go vet ./internal/reducer/tags
go doc ./internal/reducer/tags
```

## Related docs

- `docs/public/architecture.md`
- `go/internal/reducer/README.md`
- `go/internal/reducer/aws/README.md`
- `go/internal/reducer/dsl/README.md`
