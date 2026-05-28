# AGENTS.md - internal/collector/awscloud/services/appmesh/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - mesh/virtual-service pagination and the apiClient interface
   that intentionally omits every mutation API.
3. `client_nodes.go`, `client_gateways.go` - virtual node/router/route and
   virtual gateway/gateway route pagination and Describe fan-out.
4. `mappers.go`, `mappers_routes.go` - SDK-to-scanner mapping, including ACM
   trust extraction and header match extraction.
5. `client_test.go` - the reflection-based security gate that fails if any
   App Mesh mutation API appears on the interface.
6. `telemetry.go` - per-API-call instrumentation and throttle classification.
7. `../scanner.go` - scanner-owned App Mesh fact selection.
8. `../README.md` - App Mesh scanner contract.
9. `../../../README.md` - AWS cloud envelope contract.

## Invariants

- Keep App Mesh SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- The `apiClient` interface MUST stay restricted to List, Describe, and
  `ListTagsForResource` operations. Adding any Create/Update/Delete mutation
  method breaks the security gate and the metadata-only contract.
- Extract only ACM Private CA certificate authority ARNs from client TLS
  validation trusts. Never read file or SDS trust certificate chains or secret
  names, and never return a literal certificate body.
- Pass HTTP header match values through verbatim; redaction belongs to the
  scanner.
- Do not call any App Mesh mutation API. The adapter is read-only.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new App Mesh metadata read by extending `appmesh.Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not add any App Mesh mutation API to the apiClient interface under any
  circumstances.
- Do not read or return certificate bodies, certificate chains, or SDS secret
  names.
- Do not infer workload, environment, deployment, or ownership truth from
  resource names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
