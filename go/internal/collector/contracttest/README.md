# contracttest

## Purpose
Reusable fact-shape contract test helpers that any collector package can import
to assert its emitted facts match the declared contract in
`specs/collector_fact_contract.v1.yaml`.

## Ownership boundary
This package owns the shared `AssertFactShape`, `AssertFactKinds`,
`AssertRequiredPayloadKeys`, `AssertRejectsMismatchedServiceKind`, and
`AssertRequiresClient` helpers. Each collector owns its own contract profile
and service-specific payload assertions.

## Exported surface
- `Contract`, `FactKindShape` — contract model. See `doc.go`.
- `AssertFactShape` — primary entry point: runs fact-kind and payload-key checks.
- `AssertFactKinds` — subset check: every emitted kind must be declared.
- `AssertRequiredPayloadKeys` — key-presence check per declared kind.
- `AssertRejectsMismatchedServiceKind` — wrong-ServiceKind rejection check.
- `AssertRequiresClient` — nil-client rejection check.
- `ValidateCollectorKind` — collector_kind match check.
- `EnvelopeCounts` — diagnostic fact-kind count map.
- `ScanFunc` — shared scanner function signature.

## Dependencies
- `go/internal/facts` — envelope and fact kind constants.
- `go/internal/collector/awscloud` — `Boundary` type for scan helpers.

## Telemetry
No runtime telemetry. This package is test-only.

## Gotchas / invariants
- `AssertRejectsMismatchedServiceKind` and `AssertRequiresClient` take a
  `ScanFunc`, not a scanner struct. The caller wraps the scanner's zero-value
  (nil client) or the correct scanner with a wrong boundary.
- `AssertFactKind` strictly checks subset membership. Any emitted fact kind
  not in the contract triggers a test failure.
- These are opt-in contract checks. A service that does not adopt them still
  has its existing tests.

## Related docs
- `specs/collector_fact_contract.v1.yaml` — the per-collector contract spec.
- `docs/internal/design/awscloud-test-parity.md` — awscloud test depth ADR.
