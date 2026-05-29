# AWS Route 53 Resolver Scanner Runtime Binding

## Purpose

`internal/collector/awscloud/services/route53resolver/runtimebind` registers
the Route 53 Resolver scanner with the awsruntime registry from a package
`init()`. Importing this package for its blank side effect is the only way a
runtime brings the Route 53 Resolver scanner into the production registry.

## Ownership boundary

This package owns one thing: the `awsruntime.Register` call that wires
`awscloud.ServiceRoute53Resolver` to the Route 53 Resolver scanner builder. It
does not own AWS API calls, Route 53 Resolver domain types, or fact emission.
Those belong to `internal/collector/awscloud/services/route53resolver` and its
`awssdk` adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for
the godoc rendering of that contract.

## Dependencies

- `internal/collector/awscloud` for the `ServiceRoute53Resolver` constant.
- `internal/collector/awscloud/awsruntime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/awscloud/services/route53resolver` for the scanner
  struct.
- `internal/collector/awscloud/services/route53resolver/awssdk` for the SDK
  adapter constructor.

## Telemetry

This binding emits no telemetry of its own. The Route 53 Resolver scanner and
its SDK adapter emit the per-service counters and spans documented in
`../README.md` and the awsruntime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- The registration sets no `RequiresRedactionKey`: the scanner persists no
  sensitive metadata that needs HMAC redaction (DNS Firewall domain list
  contents are dropped, not redacted).
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the Route 53 Resolver scanner contract.
- `../../../awsruntime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
