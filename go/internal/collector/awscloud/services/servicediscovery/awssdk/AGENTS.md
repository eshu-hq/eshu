# AGENTS.md - internal/collector/awscloud/services/servicediscovery/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - namespace and service pagination plus the apiClient interface
   that intentionally omits every mutation and instance-reader API.
3. `mappers.go` - SDK-to-scanner mapping (namespace, service, DNS records, tags).
4. `client_test.go` - the reflection-based security gate that fails if any Cloud
   Map mutation API or instance-reader API appears on the interface.
5. `telemetry.go` - per-API-call instrumentation and throttle classification.
6. `helpers.go` - timestamp normalization and compile-time interface assertions.
7. `../scanner.go` - scanner-owned Cloud Map fact selection.
8. `../README.md` - Cloud Map scanner contract.
9. `../../../README.md` - AWS cloud envelope contract.

## Invariants

- Keep Cloud Map SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- The `apiClient` interface MUST stay restricted to `ListNamespaces`,
  `ListServices`, and `ListTagsForResource`. Adding any Create/Update/Delete
  mutation method, any tag mutation, or any instance discovery/read API
  (`ListInstances`, `GetInstance`, `GetInstancesHealthStatus`,
  `DiscoverInstances`, `DiscoverInstancesRevision`) breaks the security gate and
  the metadata-only contract.
- Record the instance COUNT only, from `ServiceSummary.InstanceCount`. Never
  fetch an instance, because instance attribute maps can carry caller-defined
  secrets.
- Scope `ListServices` per namespace with a `NAMESPACE_ID` filter and carry the
  parent namespace id/name into each service; the service summary has no
  namespace id field.
- Do not call any Cloud Map mutation API. The adapter is read-only.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Cloud Map metadata read by extending `servicediscovery.Client`,
  writing a scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not add any Cloud Map mutation API or instance-reader API to the apiClient
  interface under any circumstances.
- Do not read or return instance attribute maps.
- Do not infer workload, environment, deployment, or ownership truth from
  resource names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
