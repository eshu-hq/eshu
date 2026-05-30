# AGENTS.md - internal/collector/awscloud/services/elb guidance

## Read First

1. `README.md` - package purpose, flow, synthesized ARN, and invariants.
2. `types.go` - scanner-owned Classic ELB records and client contract.
3. `scanner.go` - fact selection and resource envelope.
4. `relationships.go` - synthesized ARN and graph-join edges.
5. `helpers.go` - target-type constants and ARN helpers.
6. `awssdk/README.md` - AWS SDK pagination and response mapping.

## Invariants

- Do not call AWS APIs from this package. The `awssdk` adapter owns AWS SDK
  calls and telemetry.
- Synthesize the load balancer ARN with `partition(boundary)`. Never hardcode
  `arn:aws:`; GovCloud and China must resolve. The `partitionguard` and
  `relguard` tests fail otherwise.
- Do not persist live instance health. Registered instance ids come from
  `DescribeLoadBalancers`; `DescribeInstanceHealth` is never called.
- Do not read or persist certificate bodies or private keys. Only the public
  `SSLCertificateId` ARN survives.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant or a documented
  `relguard.KnownTargetTypeAllowlist` entry, and a `target_resource_id` matching
  how the target scanner publishes its `resource_id` (bare `i-`/`subnet-`/`sg-`/
  `vpc-` id, or certificate ARN).
- Do not infer application ownership, environment, service identity, or public
  exposure from names, DNS names, or tags here.

## Common Changes

- Add a new relationship in `relationships.go` only when a durable topology join
  needs it; add its constant to `../../constants_elb.go`.
- Add a new resource attribute in `scanner.go` only when it supports routing,
  network placement, or later correlation.
- When adding a target type that is not yet a scanned resource, add a documented
  entry to `relguard.KnownTargetTypeAllowlist`.

## What Not To Change Without An ADR

- Do not add `DescribeInstanceHealth` or live instance state to the fact stream.
- Do not make the scanner write graph rows directly.
- Do not put ARNs, DNS names, certificate ARNs, tags, or instance ids in metric
  labels.
