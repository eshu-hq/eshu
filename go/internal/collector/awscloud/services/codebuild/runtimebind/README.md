# AWS CodeBuild Scanner Runtime Binding

## Purpose

`internal/collector/awscloud/services/codebuild/runtimebind` registers the
CodeBuild scanner with the awsruntime registry from a package `init()`.
Importing this package for its blank side effect is the only way a runtime
brings the CodeBuild scanner into the production registry.

## Ownership boundary

This package owns one thing: the `awsruntime.Register` call that wires
`awscloud.ServiceCodeBuild` to the CodeBuild scanner builder. It does not own
AWS API calls, CodeBuild domain types, redaction policy, or fact emission.
Those belong to `internal/collector/awscloud/services/codebuild` and its
`awssdk` adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for
the godoc rendering of that contract.

## Dependencies

- `internal/collector/awscloud` for the `ServiceCodeBuild` constant.
- `internal/collector/awscloud/awsruntime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/awscloud/services/codebuild` for the scanner struct.
- `internal/collector/awscloud/services/codebuild/awssdk` for the SDK adapter
  constructor.

## Telemetry

This binding emits no telemetry of its own. The CodeBuild scanner and its SDK
adapter emit the per-service counters and spans documented in `../README.md`
and the awsruntime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- The builder returns a typed error when `ScannerDeps.RedactionKey` is zero
  because CodeBuild redacts PLAINTEXT environment-variable values. The command
  enforces the matching `ESHU_AWS_REDACTION_KEY` requirement.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the CodeBuild scanner contract.
- `../../../awsruntime/README.md` for the registry and runtime surface.
- `docs/public/guides/collector-authoring.md` for the AWS scanner registration
  pattern.
