# AWS EC2 Scanner

## Purpose

`internal/collector/awscloud/service/ec2` owns scanner-side EC2 network and
volume fact selection for the AWS cloud collector. It converts VPCs, subnets,
security groups, security group rules, network interfaces, and EBS volumes into
`aws_resource` and `aws_relationship` facts. Each security-group rule
additionally emits one normalized `aws_security_group_rule` posture fact
carrying the reachability tuple `(group_id, direction, ip_protocol, from_port,
to_port, source_kind, source_value)` plus metadata-only derived booleans
(`is_internet`, `is_all_protocols`, `is_all_ports`).

For every EC2 instance the scanner emits one metadata-only
`ec2_instance_posture` fact from the existing `DescribeInstances` pass: IMDS
settings (`imds_v2_required`, `imds_http_endpoint`, `imds_http_put_hop_limit`),
user-data PRESENCE (`user_data_present`, a boolean only), detailed monitoring,
EBS optimization, public-IP association, the attached instance-profile ARN,
per-volume block-device metadata, and tenancy / Nitro-enclave state. The
scanner never reads user-data content.

For every EC2 instance the scanner ALSO emits (#5448) one `aws_resource`
identity fact (`resource_type=aws_ec2_instance`) carrying the launch AMI id
(`ami_id`, from the same `DescribeInstances` pass, no new API call), and, when
an AMI id is present, one `aws_relationship` fact recording the
instance-to-AMI usage. This identity fact resolves to the SAME canonical
`cloud_resource_uid` the `ec2_instance_posture` fact's node materialization
uses, but is projected by a SEPARATE, narrow reducer domain
(`EC2InstanceIdentityMaterialization`) that only ever augments the node with
the disjoint `ami_id` property — it never creates the node or touches the
posture domain's base identity/posture properties. See
`go/internal/reducer/ec2instance/ec2_instance_identity_materialization.go`
for the dual-writer safety argument.

The scanner ALSO emits (#5717) one `aws_resource` fact for the AMI itself
(`resource_type=aws_ec2_ami`, `resource_id`=the AMI id), deduplicated across
every instance in the scan that shares the same AMI id — many instances
commonly launch from one AMI, so this is emitted once per distinct AMI id per
scan, not once per instance. This is what lets the instance->AMI relationship
above finally resolve: the AMI materializes as an ordinary `CloudResource`
node through the SAME generic AWS resource node materialization every other
resource type uses (`go/internal/reducer/aws_resource_materialization.go`) —
no new node label, no dedicated node/edge writer. The fact carries ONLY
identity (account/region/resource_id, with Name set to the bare resource id
like every other EC2 resource type); it never carries rich state, owner,
or creation-date AMI metadata, because that requires a separate
`DescribeImages` API call this scanner deliberately does not make (a distinct,
separately costed enrichment follow-up, not a bug).

For every EBS volume the scanner emits one metadata-only `aws_ec2_volume`
resource fact from a single boundary-scoped `DescribeVolumes` pass. It records
reported encryption, KMS key, attachment, and operational shape metadata and
emits a volume-to-KMS relationship only when AWS reports a non-empty KMS key
identifier. Volume contents, snapshots, user-data, and reducer posture
decisions stay out of this package.

The package implements the EC2 network-topology slice from
`docs/public/services/collector-aws-cloud.md`.

## Ownership boundary

This package owns scanner-owned EC2 models and fact-envelope construction. It
does not own AWS SDK calls, credentials, throttling, workflow claims, graph
writes, reducer admission, instance inventory, or query behavior.

```mermaid
flowchart LR
  A["ec2.Client"] --> B["Scanner.Scan"]
  B --> C["VPC / Subnet / SecurityGroup"]
  B --> D["SecurityGroupRule / NetworkInterface"]
  B --> H["Instance"]
  B --> J["Volume"]
  C --> E["aws_resource facts"]
  D --> E
  J --> E
  H --> E
  D --> F["aws_relationship facts"]
  J --> F
  H --> K["aws_relationship: instance USES AMI #5448"]
  H --> L["aws_resource: AMI identity #5717, deduped per scan"]
  L --> K
  D --> G["aws_security_group_rule posture facts"]
  H --> I["ec2_instance_posture facts"]
```

## Exported surface

See `doc.go` for the godoc contract.

- `Scanner` - emits EC2 network topology, EBS volume, instance-posture,
  (#5448) instance-identity, and (#5717) AMI-identity facts for one claimed
  AWS boundary.
- `Client` - scanner-owned read surface implemented by `awssdk.Client`.
- `VPC`, `Subnet`, `SecurityGroup`, `SecurityGroupRule`, `NetworkInterface`,
  `Volume`, and `Instance` - scanner-owned EC2 records.
- `BlockDevice` - one instance block-device mapping entry (device name, volume
  id, delete-on-termination, status); per-volume encryption is not reported by
  `DescribeInstances`, so it stays unset.
- `VolumeAttachment` - one EBS volume attachment entry reported by
  `DescribeVolumes`, including instance id or AWS-managed associated-resource
  metadata when AWS reports it.
- `NetworkInterfaceAttachment` - ENI attachment metadata, including attached
  resource ARN when AWS reports enough data to derive one.

The normalized `aws_security_group_rule` posture fact and its source-kind /
direction constants are owned by `internal/collector/awscloud`
(`NewSecurityGroupRuleEnvelope`, `SecurityGroupRuleObservation`); this scanner
maps each `SecurityGroupRule` into that observation in `scanner.go`. The
`ec2_instance_posture` fact, its `EC2InstancePostureObservation`, and
`NewEC2InstancePostureEnvelope` are likewise owned by
`internal/collector/awscloud`; this scanner maps each `Instance` into that
observation in `posture.go`.

## Dependencies

- `internal/collector/awscloud` for AWS boundaries and fact envelopes.
- `internal/facts` for durable fact envelopes.

## Telemetry

This package emits no metrics or spans directly. The `awssdk` adapter emits
AWS API call counters, throttle counters, and pagination spans.

## Gotchas / invariants

- EC2 instance inventory is INTENTIONALLY MINIMAL, not absent. The scanner
  emits an `aws_ec2_instance` `aws_resource` fact per instance, but it is
  scoped narrowly to identity + the launch AMI id (#5448) — it never carries
  posture, IMDS, user-data-presence, block-device, or any other property the
  `ec2_instance_posture` fact and its CloudResource node materialization own.
  A future change that wants to add another EC2 instance property to the
  `aws_resource` identity fact MUST NOT let it collide with a posture-owned
  property name; see `go/internal/reducer/aws_resource_materialization.go`'s
  `cloudResourceNodeRow` exclusion and
  `go/internal/storage/cypher/cloud/aws/ec2/instance_identity_node_writer.go`'s
  disjointness proof before adding one.
- The AMI (`aws_ec2_ami`) `aws_resource` fact (#5717) is INTENTIONALLY
  identity-only, mirroring the instance fact's minimalism above: Name is the
  bare resource id, and there is no rich
  state, owner, or creation-date property. It exists solely so the
  instance->AMI relationship's edge join resolves; enriching it with real AMI
  metadata requires a `DescribeImages` call this package does not make and is
  a separate, costed follow-up, not something to add inline here. It is also
  DEDUPLICATED per scan by AMI id (`amiResourceEnvelopes`'s `seenAMIIDs` map
  in `scanner.go`) — do not revert to emitting one AMI fact per instance.
- The `ec2_instance_posture` fact carries user-data PRESENCE only. The user-data
  content (which can hold secrets), instance console output, environment
  variables, and any other instance payload are never read or persisted. The
  `DescribeInstances` pass does not include user-data, so `user_data_present`
  stays unset unless a later bounded enrichment fills it; PR1 adds no
  per-instance API fan-out.
- Per-volume block-device `encrypted` is not reported by `DescribeInstances`, so
  it stays unset on the posture fact. The reducer joins each volume id to the
  boundary-wide EBS volume facts and KMS relationship evidence (#1304);
  per-instance `DescribeVolumes` fan-out remains forbidden.
- EBS volume facts are metadata only. They record the volume id, synthesized
  volume ARN, state, availability zone, encrypted flag, KMS key id, attachment
  summaries, and safe operational scalars. They never read or persist volume
  contents, snapshot payloads, CloudWatch metric samples, cost data, or
  user-data content.
- The `ec2_instance_posture` fact emits no graph edges. The USES_PROFILE join to
  the IAM instance profile (#1146), the block-device KMS posture projection
  (#1304), and the derived internet-exposed flag (#1135) are reducer slices.
- Security group rules are child `aws_resource` facts with a security-group to
  rule `aws_relationship` edge, plus one normalized `aws_security_group_rule`
  posture fact per rule.
- The posture fact's `is_internet` boolean is an exact-CIDR normalization
  (`0.0.0.0/0` / `::/0`), not a reachability or exposure claim. Real
  internet-exposure truth needs the reducer reachability slice and the
  exposure query, which are deferred follow-ups in issue #1135.
- The posture fact emits no graph edges. Projecting it into
  `ALLOWS_INGRESS`/`ALLOWS_EGRESS` edges and `:CidrBlock`/`:PrefixList` nodes is
  the reducer PR2 slice, under principal review.
- ENIs emit placement and attachment relationships so reducers can later join
  ECS, EKS, or Lambda runtime evidence to subnet and VPC topology.
- Descriptions and tags are user-controlled text. They are preserved in fact
  payloads, but must never become metric labels.
- This package emits reported AWS evidence only. Do not infer public exposure,
  service ownership, environment, deployable-unit truth, or workload truth here.

## Evidence

### EBS volume metadata facts (#1303)

Collector Performance Evidence: the EBS volume path is one paginated
`DescribeVolumes` stream per claimed account/region boundary, not a
per-instance fan-out. `go test ./internal/collector/awscloud/service/ec2/... -count=1`
covers scanner emission and SDK mapping for the boundary-scoped pass, including
encrypted/KMS, missing-key, unencrypted, attached, and missing-identity cases.
The emitted fact volume is linear in the number of volumes returned by AWS and
adds at most one resource fact plus one optional KMS relationship fact per
volume.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/reference/telemetry/index.md`
