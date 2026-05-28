# AWS SageMaker Scanner Runtime Binding

## Purpose

`internal/collector/awscloud/services/sagemaker/runtimebind` registers the
SageMaker scanner with the awsruntime registry from a package `init()`.
Importing this package for its blank side effect is the only way a runtime
brings the SageMaker scanner into the production registry.

## Ownership boundary

This package owns one thing: the `awsruntime.Register` call that wires
`awscloud.ServiceSageMaker` to the SageMaker scanner builder. It does not own
AWS API calls, SageMaker domain types, or fact emission. Those belong to
`internal/collector/awscloud/services/sagemaker` and its `awssdk` adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for
the godoc rendering of that contract.

## Dependencies

- `internal/collector/awscloud` for the `ServiceSageMaker` constant.
- `internal/collector/awscloud/awsruntime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/awscloud/services/sagemaker` for the scanner struct.
- `internal/collector/awscloud/services/sagemaker/awssdk` for the SDK adapter
  constructor.

## Telemetry

This binding emits no telemetry of its own. The SageMaker scanner and its SDK
adapter emit the per-service counters and spans documented in `../README.md`
and the awsruntime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, surfacing copy-paste bugs at process start instead of at the
  first scan claim.
- The builder takes no optional dependency. SageMaker is metadata-only and
  needs no redaction key or pagination checkpoint store, so the builder body
  is a plain constructor call.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim from the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the SageMaker scanner contract.
- `../../../awsruntime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
