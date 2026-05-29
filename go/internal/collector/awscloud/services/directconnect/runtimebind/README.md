# AWS Direct Connect Scanner Runtime Binding

## Purpose

`internal/collector/awscloud/services/directconnect/runtimebind` registers the
Direct Connect scanner with the awsruntime registry from a package `init()`.
Importing this package for its blank side effect is the only way a runtime
brings the Direct Connect scanner into the production registry.

## Ownership boundary

This package owns one thing: the `awsruntime.Register` call that wires
`awscloud.ServiceDirectConnect` to the Direct Connect scanner builder. It does
not own AWS API calls, Direct Connect domain types, or fact emission. Those
belong to `internal/collector/awscloud/services/directconnect` and its `awssdk`
adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for
the godoc rendering of that contract.

## Dependencies

- `internal/collector/awscloud` for the `ServiceDirectConnect` constant.
- `internal/collector/awscloud/awsruntime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/awscloud/services/directconnect` for the scanner struct.
- `internal/collector/awscloud/services/directconnect/awssdk` for the SDK
  adapter constructor.

## Telemetry

This binding emits no telemetry of its own. The Direct Connect scanner and its
SDK adapter emit the per-service counters and spans documented in `../README.md`
and the awsruntime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- The registration leaves `RequiresRedactionKey` unset (false). Direct Connect
  drops the BGP authentication key and MACsec key material by never mapping
  them, so it has no redaction dependency. `TestDirectConnectRuntimeBindRegisters`
  proves the builder succeeds with a zero redaction key, and
  `TestDirectConnectRuntimeBindDoesNotRequireRedactionKey` pins the requirement
  flag off.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the Direct Connect scanner contract.
- `../../../awsruntime/README.md` for the registry and runtime surface.
- `docs/public/guides/collector-authoring.md` for the AWS scanner registration
  pattern.
