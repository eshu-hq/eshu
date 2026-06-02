# AGENTS.md - internal/collector/awscloud/services/ec2 guidance

## Read First

1. `README.md` - package purpose, flow, and invariants.
2. `types.go` - scanner-owned EC2 records and client contract.
3. `scanner.go` - fact selection and resource envelope mapping.
4. `posture.go` - EC2 instance posture observation derivation.
5. `volume.go` - EBS volume metadata and KMS relationship fact construction.
6. `relationships.go` - EC2 topology relationship construction.
7. `awssdk/README.md` - AWS SDK pagination and response mapping.

## Invariants

- Do not call AWS APIs from this package. The `awssdk` adapter owns AWS SDK
  calls and telemetry.
- Do not emit an `aws_ec2_instance` resource (inventory) fact. The scanner emits
  one metadata-only `ec2_instance_posture` fact per instance; ENI attachment
  target evidence is metadata only.
- EBS volume facts come only from a boundary-scoped `DescribeVolumes` pass. Do
  not use per-instance `DescribeVolumes` calls to fill block-device posture
  inline, and do not let volume facts become reducer posture decisions.
- The `ec2_instance_posture` fact carries user-data PRESENCE only. Never read or
  persist user-data content, console output, environment variables, or any other
  instance payload, and never add a per-instance API fan-out (`UserDataPresent`
  and per-volume `Encrypted` stay nil from the `DescribeInstances` pass; reducers
  resolve them).
- Preserve VPC, subnet, security-group, security-group-rule, and ENI identity
  as AWS reports it.
- Emit topology edges as `aws_relationship` facts only.
- The `aws_security_group_rule` posture fact normalizes one rule into a single
  `(source_kind, source_value)` pair plus metadata-only derived booleans. Its
  `is_internet` flag is an exact open-CIDR match only; do not turn it into a
  reachability or exposure claim, and do not emit graph edges or `:CidrBlock`
  nodes from this scanner (that is the reducer PR2 slice for issue #1135).
- Keep ENI IDs, security group IDs, subnet IDs, VPC IDs, descriptions, tags,
  and attached resource ARNs out of metric labels.

## Common Changes

- Add a new EC2 field in `types.go`, `scanner.go`, and `awssdk/mapper.go`
  together.
- Add a focused scanner test before changing emitted resource or relationship
  shapes.
- Add EBS volume metadata fields in `types.go`, `volume.go`, and
  `awssdk/mapper.go` together.
- Keep instance inventory, live reachability, exposure analysis, the
  USES_PROFILE/KMS joins, and per-volume encryption resolution in later
  reducer/query slices, not this scanner package.

## What Not To Change Without An ADR

- Do not add EC2 write APIs.
- Do not make the scanner write graph rows directly.
- Do not broaden scope to instance inventory, route tables, NACLs, VPC
  endpoints, or transit gateways without updating issue scope and downstream
  data-use expectations.
