# Amazon VPC Lattice Scanner Runtime Binding

## Purpose

`internal/collector/cloud/aws/service/vpclattice/bind` registers the VPC
Lattice scanner with the runtime registry from a package `init()`. Importing
this package for its blank side effect is the only way a runtime brings the VPC
Lattice scanner into the production registry.

## Ownership boundary

This package owns one thing: the `runtime.Register` call that wires
`aws.ServiceVPCLattice` to the VPC Lattice scanner builder. It does not own
AWS API calls, VPC Lattice domain types, redaction policy, or fact emission.
Those belong to `internal/collector/cloud/aws/service/vpclattice` and its
`sdk` adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for
the godoc rendering of that contract.

## Dependencies

- `internal/collector/cloud/aws` for the `ServiceVPCLattice` constant.
- `internal/collector/cloud/aws/runtime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/cloud/aws/service/vpclattice` for the scanner struct.
- `internal/collector/cloud/aws/service/vpclattice/sdk` for the SDK adapter
  constructor.

## Telemetry

This binding emits no telemetry of its own. The VPC Lattice scanner and its SDK
adapter emit the per-service counters and spans documented in `../README.md`
and the runtime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the VPC Lattice scanner contract.
- `../../../runtime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
