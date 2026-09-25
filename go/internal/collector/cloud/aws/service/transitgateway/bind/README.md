# AWS Transit Gateway Scanner Runtime Binding

## Purpose

`internal/collector/cloud/aws/service/transitgateway/bind` registers the
Transit Gateway scanner with the runtime registry from a package `init()`.
Importing this package for its blank side effect is the only way a runtime
brings the Transit Gateway scanner into the production registry.

## Ownership boundary

This package owns one thing: the `runtime.Register` call that wires
`aws.ServiceTransitGateway` to the Transit Gateway scanner builder. It does
not own AWS API calls, Transit Gateway domain types, redaction policy, or fact
emission. Those belong to `internal/collector/cloud/aws/service/transitgateway`
and its `sdk` adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for
the godoc rendering of that contract.

## Dependencies

- `internal/collector/cloud/aws` for the `ServiceTransitGateway` constant.
- `internal/collector/cloud/aws/runtime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/cloud/aws/service/transitgateway` for the scanner struct.
- `internal/collector/cloud/aws/service/transitgateway/sdk` for the SDK
  adapter constructor.

## Telemetry

This binding emits no telemetry of its own. The Transit Gateway scanner and its
SDK adapter emit the per-service counters and spans documented in `../README.md`
and the runtime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- The builder needs no redaction key; Transit Gateway metadata is the product,
  so the builder body is a plain constructor call like the SQS binding.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the Transit Gateway scanner contract.
- `../../../runtime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
