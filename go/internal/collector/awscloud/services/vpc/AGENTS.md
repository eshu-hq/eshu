# AGENTS.md - internal/collector/awscloud/services/vpc guidance

## Read First

1. `README.md` - package purpose, EC2/VPC ownership table, and invariants.
2. `types.go` - scanner-owned VPC topology domain types.
3. `scanner.go` - resource and relationship emission orchestration.
4. `observations.go` - per-resource attribute payload builders.
5. `relationships.go` - per-resource relationship builders, including the
   cross-package edges back to EC2-owned `aws_ec2_vpc`, `aws_ec2_subnet`,
   `aws_ec2_network_interface`, and `aws_ec2_instance` identifiers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `../ec2/README.md` - the EC2-owned half of the VPC fabric surface. Read
   this before considering whether a new resource belongs in this package or
   the EC2 package.
8. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep VPC API access behind `Client`; do not import the AWS SDK into this
  package.
- Never expose mutation operations on `Client`. Methods MUST be named `List*`.
  `scanner_test.go::TestClientInterfaceIsReadOnly` enforces this.
- Never read or persist VPN tunnel pre-shared keys, IAM policy JSON, or any
  data-plane payload.
- Never emit `aws_ec2_vpc`, `aws_ec2_subnet`, `aws_ec2_security_group`,
  `aws_ec2_security_group_rule`, or `aws_ec2_network_interface` resources.
  Those identities belong to the EC2 scanner.
  `scanner_test.go::TestVPCResourceTypesDisjointFromEC2` pins the boundary.
- Cross-package relationship edges MUST reference the EC2-owned target type
  by `awscloud.ResourceTypeEC2Xxx`, not by re-emitting the resource here.
- Preserve stable resource identities (allocation IDs, gateway IDs, route
  table IDs, endpoint IDs) across repeated observations in the same AWS
  generation.
- Keep IDs, ARNs, tags, route destinations, and CIDR blocks out of metric
  labels.

## Common Changes

- Add a new VPC metadata field by extending the scanner-owned record in
  `types.go`, writing a focused scanner or adapter test first, then mapping
  it through `awscloud` envelope builders.
- Add new relationship evidence only when the AWS API reports both sides
  directly and the target type already exists (or you add a new
  `awscloud.RelationshipVPCXxx` constant alphabetically).
- Extend SDK pagination, mutation guards, and the apiClient interface only in
  the `awssdk` adapter, never here.

## What Not To Change Without An ADR

- Do not add new resource ownership that overlaps with EC2 or any other
  service scanner.
- Do not emit synthetic deployment, workload, or repository ownership facts
  from VPC tags or relationships; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
