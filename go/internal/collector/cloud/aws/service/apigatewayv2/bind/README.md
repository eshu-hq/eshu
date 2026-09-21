# AWS API Gateway v2 Scanner Runtime Binding

## Purpose

`internal/collector/cloud/aws/service/apigatewayv2/bind` registers the
API Gateway v2 scanner with the runtime registry from a package `init()`.
Importing this package for its blank side effect is the only way a runtime
brings the API Gateway v2 scanner into the production registry.

## Ownership boundary

This package owns one thing: the `runtime.Register` call that wires
`aws.ServiceAPIGatewayV2` to the API Gateway v2 scanner builder. It does
not own AWS API calls, API Gateway v2 domain types, redaction policy, or fact
emission. Those belong to `internal/collector/cloud/aws/service/apigatewayv2`
and its `sdk` adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for
the godoc rendering of that contract.

## Dependencies

- `internal/collector/cloud/aws` for the `ServiceAPIGatewayV2` constant.
- `internal/collector/cloud/aws/runtime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/cloud/aws/service/apigatewayv2` for the scanner struct.
- `internal/collector/cloud/aws/service/apigatewayv2/sdk` for the SDK adapter
  constructor.

## Telemetry

This binding emits no telemetry of its own. The API Gateway v2 scanner and its
SDK adapter emit the per-service counters and spans documented in `../README.md`
and the runtime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- The registration leaves `RequiresRedactionKey` unset. The scanner drops
  templates and secrets by never mapping them, so it needs no redaction key. The
  command derives its `ESHU_AWS_REDACTION_KEY` requirement from the registry, so
  setting the flag here would force an unnecessary operator key.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the API Gateway v2 scanner contract.
- `../../../runtime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
