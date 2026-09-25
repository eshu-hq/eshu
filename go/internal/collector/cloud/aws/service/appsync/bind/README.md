# AWS AppSync Scanner Runtime Binding

## Purpose

`internal/collector/cloud/aws/service/appsync/bind` registers the
AppSync scanner with the runtime registry from a package `init()`.
Importing this package for its blank side effect is the only way a runtime
brings the AppSync scanner into the production registry.

## Ownership boundary

This package owns one thing: the `runtime.Register` call that wires
`aws.ServiceAppSync` to the AppSync scanner builder. It does not own AWS
API calls, AppSync domain types, or fact emission. Those belong to
`internal/collector/cloud/aws/service/appsync` and its `sdk` adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go`
for the godoc rendering of that contract.

## Dependencies

- `internal/collector/cloud/aws` for the `ServiceAppSync` constant.
- `internal/collector/cloud/aws/runtime` for `Register`, `ScannerDeps`,
  and `ScannerRegistration`.
- `internal/collector/cloud/aws/service/appsync` for the scanner struct.
- `internal/collector/cloud/aws/service/appsync/sdk` for the SDK adapter
  constructor.

## Telemetry

This binding emits no telemetry of its own. The AppSync scanner and its SDK
adapter emit the per-service counters and spans documented in `../README.md`
and the runtime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- The builder needs no redaction key. AppSync descriptions are AWS-controlled
  metadata, and secret-bearing fields are excluded by not mapping them, so the
  builder does not check `ScannerDeps.RedactionKey`.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the AppSync scanner contract.
- `../../../runtime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
