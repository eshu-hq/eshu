# AWS KMS Scanner Runtime Binding

## Purpose

`internal/collector/cloud/aws/service/kms/bind` registers the KMS
scanner with the runtime registry from a package `init()`. Importing
this package for its blank side effect is the only way a runtime brings
the KMS scanner into the production registry.

## Ownership boundary

This package owns one thing: the `runtime.Register` call that wires
`aws.ServiceKMS` to the KMS scanner builder. It does not own AWS
API calls, KMS domain types, redaction policy, or fact emission. Those
belong to `internal/collector/cloud/aws/service/kms` and its `sdk`
adapter.

## Exported surface

None. The package is imported only for its init side effect. See
`doc.go` for the godoc rendering of that contract.

## Dependencies

- `internal/collector/cloud/aws` for the `ServiceKMS` constant.
- `internal/collector/cloud/aws/runtime` for `Register`,
  `ScannerDeps`, and `ScannerRegistration`.
- `internal/collector/cloud/aws/service/kms` for the scanner struct.
- `internal/collector/cloud/aws/service/kms/sdk` for the SDK adapter
  constructor.

## Telemetry

This binding emits no telemetry of its own. The KMS scanner and its SDK
adapter emit the per-service counters and spans documented in
`../README.md` and the runtime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead
  of at the first scan claim.
- Do not perform AWS configuration loading, credential acquisition, or
  client construction at init time. Builders construct clients per
  claim, using the runtime-provided `ScannerDeps`.
- The KMS builder does not require `RedactionKey`. KMS metadata is
  not field-level redacted because the scanner already excludes
  policy bodies, encryption contexts, and key material at the SDK layer.

## Related docs

- `../README.md` for the KMS scanner contract.
- `../../../runtime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the
  user-facing coverage table.
