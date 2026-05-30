# AGENTS.md - internal/collector/awscloud/services/servicecatalog/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Service Catalog SDK pagination, the physical-id index, and
   telemetry.
3. `mapping.go` - SDK-to-scanner type mappers.
4. `../scanner.go` - scanner-owned Service Catalog fact selection.
5. `../README.md` - Service Catalog scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Service Catalog SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe portfolio, product, and provisioned-product metadata. Never
  read or forward provisioning-artifact template bodies, launch-constraint
  policy documents, provisioning parameter values, or record output values.
- Resolve the CloudFormation stack ARN only through the
  `SearchProvisionedProducts` physical-id index. Never call `DescribeRecord` or
  any output-reading API to obtain it.
- Scope provisioned-product scans to the `Account` access level with value
  `self`.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Service Catalog metadata read by extending `Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types via `mapping.go`.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does
  not reveal template bodies, parameter values, or record outputs.

## What Not To Change Without An ADR

- Do not provision, update, terminate, associate, disassociate, share, or
  constrain any Service Catalog resource.
- Do not call `DescribeProvisioningArtifact`, `DescribeRecord`,
  `GetProvisionedProductOutputs`, `DescribeProvisioningParameters`, or any API
  that returns template bodies, parameter values, or stack output values.
- Do not infer workload, environment, deployment, or ownership truth from
  Service Catalog names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
