# AWS Cloud Map (Service Discovery) Scanner Runtime Binding

## Purpose

`internal/collector/cloud/aws/service/discovery/bind` registers
the Cloud Map scanner with the runtime registry from a package `init()`.
Importing this package for its blank side effect is the only way a runtime
brings the Cloud Map scanner into the production registry.

## Ownership boundary

This package owns one thing: the `runtime.Register` call that wires
`aws.ServiceServiceDiscovery` to the Cloud Map scanner builder. It does not
own AWS API calls, Cloud Map domain types, or fact emission. Those belong to
`internal/collector/cloud/aws/service/discovery` and its `sdk`
adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for
the godoc rendering of that contract.

## Dependencies

- `internal/collector/cloud/aws` for the `ServiceServiceDiscovery` constant.
- `internal/collector/cloud/aws/runtime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/cloud/aws/service/discovery` for the scanner
  struct.
- `internal/collector/cloud/aws/service/discovery/sdk` for the SDK
  adapter constructor.

## Telemetry

This binding emits no telemetry of its own. The Cloud Map scanner and its SDK
adapter emit the per-service counters and spans documented in `../README.md`
and the runtime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- The registration leaves `RequiresRedactionKey` unset. The Cloud Map scanner
  records instance counts only and never reads instance attribute maps, so it
  needs no `ESHU_AWS_REDACTION_KEY`.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the Cloud Map scanner contract.
- `../../../runtime/README.md` for the registry and runtime surface.
- `docs/public/guides/collector-authoring.md` for the AWS scanner registration
  pattern.
