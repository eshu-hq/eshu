# Amazon OpenSearch Serverless Scanner Runtime Binding

## Purpose

`internal/collector/awscloud/services/opensearchserverless/runtimebind` registers
the OpenSearch Serverless scanner with the awsruntime registry from a package
`init()`. Importing this package for its blank side effect is the only way a
runtime brings the OpenSearch Serverless scanner into the production registry.

## Ownership boundary

This package owns one thing: the `awsruntime.Register` call that wires
`awscloud.ServiceOpenSearchServerless` to the OpenSearch Serverless scanner
builder. It does not own AWS API calls, OpenSearch Serverless domain types,
policy-body handling, or fact emission. Those belong to
`internal/collector/awscloud/services/opensearchserverless` and its `awssdk`
adapter.

## Exported surface

None. The package is imported only for its init side effect. See `doc.go` for the
godoc rendering of that contract.

## Dependencies

- `internal/collector/awscloud` for the `ServiceOpenSearchServerless` constant.
- `internal/collector/awscloud/awsruntime` for `Register`, `ScannerDeps`, and
  `ScannerRegistration`.
- `internal/collector/awscloud/services/opensearchserverless` for the scanner
  struct.
- `internal/collector/awscloud/services/opensearchserverless/awssdk` for the SDK
  adapter constructor.

## Telemetry

This binding emits no telemetry of its own. The OpenSearch Serverless scanner and
its SDK adapter emit the per-service counters and spans documented in
`../README.md` and the awsruntime README.

## Gotchas / invariants

- `init()` must register exactly once. The registry panics on duplicate
  registrations, which surfaces copy-paste bugs at process start instead of at
  the first scan claim.
- Do not perform AWS configuration loading, credential acquisition, or client
  construction at init time. Builders construct clients per claim, using the
  runtime-provided `ScannerDeps`.

## Related docs

- `../README.md` for the OpenSearch Serverless scanner contract.
- `../../../awsruntime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
