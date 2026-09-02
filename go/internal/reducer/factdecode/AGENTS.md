# Reducer fact-decode package instructions

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/factdecode/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- This package must remain below `internal/reducer`. Never import the parent
  reducer package or a family subpackage. Its budget is `internal/facts`,
  `internal/telemetry`, `internal/reducer/contract`, `pkg/log`,
  `go.opentelemetry.io/otel/metric`, the `factschema` SDK, and the standard
  library. A per-family schema package (for example `sdk/go/factschema/aws/v1`)
  is NOT in the budget: the attribute-shape adapters match the
  `AttributeShapeField() string` error contract with `errors.As` and never name
  a family's concrete error type. It does not currently import `payloadcore`;
  adding that is fine, but anything beyond this list is not.
- Mechanism only. A per-fact-kind `Decode*` function belongs to the family that
  owns the fact kind, not here, however many families happen to call it.
- `FactDecodeError.Retryable()` must stay false. A missing or malformed required
  field cannot succeed on replay unchanged; making it retryable turns a dead
  letter into a queue loop.
- The classification string is matched BY VALUE against
  `projector.TriageClassInputInvalid`. The contracts module cannot import
  `go/internal`, so there is no shared constant to lean on — changing the
  spelling here silently breaks triage.
- A quarantine is per-fact and non-fatal by design. A durable-write outage must
  not turn one into a fatal intent failure.

## Common changes

Adding a quarantine reason: extend the classification handling in
`PartitionDecodeFailures` and add the matching sub-signal to
`InputInvalidSubSignals`, together, so the operator-facing breakdown stays
exhaustive.

## Failure modes

- Making `Retryable()` true, or letting a persistence failure propagate, both
  convert a visible dead letter into an invisible retry loop.
- Hoisting a family's `Decode*` function into this package erodes the boundary
  that lets that family be extracted later.
- Renaming `QuarantinedFact`'s exported fields breaks the three construction
  sites outside this package; the compiler catches it, but a partial rename that
  also touches `FactDecodeError.factKind` does not do what you meant.
