# AWS Cognito Scanner Runtime Binding

## Purpose

`internal/collector/awscloud/services/cognito/runtimebind` registers the Cognito
scanner with the awsruntime registry from a package `init()`. Importing this
package for its blank side effect is the only way a runtime brings the Cognito
scanner into the production registry.

## Ownership boundary

This package owns one thing: the `awsruntime.Register` call that wires
`awscloud.ServiceCognito` to the Cognito scanner builder. The builder validates
the runtime redaction key and constructs the SDK adapter per claim. It does not
own AWS API calls, Cognito domain types, redaction policy, or fact emission.
Those belong to `internal/collector/awscloud/services/cognito` and its `awssdk`
adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for
the godoc rendering of that contract.

## Dependencies

- `internal/collector/awscloud` for the `ServiceCognito` constant.
- `internal/collector/awscloud/awsruntime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/awscloud/services/cognito` for the scanner struct.
- `internal/collector/awscloud/services/cognito/awssdk` for the SDK adapter
  constructor.

## Telemetry

This binding emits no telemetry of its own. The Cognito scanner and its SDK
adapter emit the per-service counters and spans documented in `../README.md`
and the awsruntime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- The builder returns a typed error when `ScannerDeps.RedactionKey.IsZero()`.
  Cognito is in the command's redaction-required service set, so a claim without
  `ESHU_AWS_REDACTION_KEY` fails configuration before any scan.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the Cognito scanner contract.
- `../../../awsruntime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
