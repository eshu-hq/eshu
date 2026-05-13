# AGENTS.md - internal/collector/awscloud/services/ec2 guidance

## Read First

1. `README.md` - package purpose, flow, and invariants.
2. `types.go` - scanner-owned EC2 records and client contract.
3. `scanner.go` - fact selection and resource envelope mapping.
4. `relationships.go` - EC2 topology relationship construction.
5. `awssdk/README.md` - AWS SDK pagination and response mapping.

## Invariants

- Do not call AWS APIs from this package. The `awssdk` adapter owns AWS SDK
  calls and telemetry.
- Do not emit EC2 instance resource facts. ENI attachment target evidence is
  metadata only.
- Preserve VPC, subnet, security-group, security-group-rule, and ENI identity
  as AWS reports it.
- Emit topology edges as `aws_relationship` facts only.
- Do not infer public exposure from CIDR blocks or ports in the scanner.
- Keep ENI IDs, security group IDs, subnet IDs, VPC IDs, descriptions, tags,
  and attached resource ARNs out of metric labels.

## Common Changes

- Add a new EC2 field in `types.go`, `scanner.go`, and `awssdk/mapper.go`
  together.
- Add a focused scanner test before changing emitted resource or relationship
  shapes.
- Keep instance inventory, live reachability, and exposure analysis in later
  reducer/query slices, not this scanner package.

## What Not To Change Without An ADR

- Do not add EC2 write APIs.
- Do not make the scanner write graph rows directly.
- Do not broaden scope to instance inventory, route tables, NACLs, VPC
  endpoints, or transit gateways without updating issue scope and downstream
  data-use expectations.
