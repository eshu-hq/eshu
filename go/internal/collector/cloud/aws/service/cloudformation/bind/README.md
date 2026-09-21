# AWS CloudFormation Scanner Runtime Binding

## Purpose

`internal/collector/cloud/aws/service/cloudformation/bind` registers the
CloudFormation scanner with the runtime registry from a package `init()`.
Importing this package for its blank side effect is the only way a runtime
brings the CloudFormation scanner into the production registry.

## Ownership boundary

This package owns one thing: the `runtime.Register` call that wires
`aws.ServiceCloudFormation` to the CloudFormation scanner builder. It does
not own AWS API calls, CloudFormation domain types, redaction policy, or fact
emission. Those belong to
`internal/collector/cloud/aws/service/cloudformation` and its `sdk` adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for
the godoc rendering of that contract.

## Dependencies

- `internal/collector/cloud/aws` for the `ServiceCloudFormation` constant.
- `internal/collector/cloud/aws/runtime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/cloud/aws/service/cloudformation` for the scanner struct.
- `internal/collector/cloud/aws/service/cloudformation/sdk` for the SDK
  adapter constructor.

## Telemetry

This binding emits no telemetry of its own. The CloudFormation scanner and its
SDK adapter emit the per-service counters and spans documented in `../README.md`
and the runtime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- The builder requires a non-zero `ScannerDeps.RedactionKey` and returns a typed
  error when it is zero, because the scanner redacts secret-like stack output
  values. The registration also sets `RequiresRedactionKey: true`, which is how
  the command derives the `ESHU_AWS_REDACTION_KEY` requirement; `config.go` no
  longer hardcodes a service list.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the CloudFormation scanner contract.
- `../../../runtime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
