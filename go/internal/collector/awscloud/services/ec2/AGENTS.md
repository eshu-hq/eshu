# AGENTS.md - internal/collector/awscloud/services/ec2 guidance

## Read First

1. `README.md` - package purpose, flow, and invariants.
2. `types.go` - scanner-owned EC2 records and client contract.
3. `scanner.go` - fact selection and resource envelope mapping.
4. `posture.go` - EC2 instance posture observation derivation.
5. `identity.go` - EC2 instance identity fact + instance->AMI relationship (#5448).
6. `volume.go` - EBS volume metadata and KMS relationship fact construction.
7. `relationships.go` - EC2 topology relationship construction.
8. `awssdk/README.md` - AWS SDK pagination and response mapping.

## Invariants

- Do not call AWS APIs from this package. The `awssdk` adapter owns AWS SDK
  calls and telemetry.
- The scanner emits an `aws_ec2_instance` resource fact (#5448), but it is
  SCOPED NARROWLY to identity + the launch `ami_id`. Never add a posture, IMDS,
  user-data, block-device, or any other property to this fact that the
  `ec2_instance_posture` fact and its CloudResource node materialization
  already own — the two facts resolve to the SAME graph node
  (`cloud_resource_uid`) via two SEPARATE reducer domains whose Cypher SET
  clauses are proven disjoint
  (`TestEC2InstanceIdentityWriterDisjointFromEC2InstancePostureWriter` in
  `go/internal/storage/cypher`); adding a colliding property name breaks that
  proof and reintroduces the dual-writer clobber #5448 closed. ENI attachment
  target evidence stays metadata only.
- The instance->AMI `aws_relationship` fact stays Postgres-only. Do not add an
  AMI/MachineImage `aws_resource` fact or a graph node/edge writer for it in
  this package without also registering the new node label under #5472
  (retractable_edge_types, replay-depth spec, relguard, graph schema uid
  constraint) — see the tracked follow-up
  (https://github.com/eshu-hq/eshu/issues/5717) for the AMI node class.
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
