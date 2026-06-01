# AWS EC2 Scanner

## Purpose

`internal/collector/awscloud/services/ec2` owns scanner-side EC2 network fact
selection for the AWS cloud collector. It converts VPCs, subnets, security
groups, security group rules, and network interfaces into `aws_resource` and
`aws_relationship` facts. Each security-group rule additionally emits one
normalized `aws_security_group_rule` posture fact carrying the reachability
tuple `(group_id, direction, ip_protocol, from_port, to_port, source_kind,
source_value)` plus metadata-only derived booleans (`is_internet`,
`is_all_protocols`, `is_all_ports`).

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
  C --> E["aws_resource facts"]
  D --> E
  D --> F["aws_relationship facts"]
  D --> G["aws_security_group_rule posture facts"]
```

## Exported surface

See `doc.go` for the godoc contract.

- `Scanner` - emits EC2 network topology facts for one claimed AWS boundary.
- `Client` - scanner-owned read surface implemented by `awssdk.Client`.
- `VPC`, `Subnet`, `SecurityGroup`, `SecurityGroupRule`, and
  `NetworkInterface` - scanner-owned EC2 records.
- `NetworkInterfaceAttachment` - ENI attachment metadata, including attached
  resource ARN when AWS reports enough data to derive one.

The normalized `aws_security_group_rule` posture fact and its source-kind /
direction constants are owned by `internal/collector/awscloud`
(`NewSecurityGroupRuleEnvelope`, `SecurityGroupRuleObservation`); this scanner
maps each `SecurityGroupRule` into that observation in `scanner.go`.

## Dependencies

- `internal/collector/awscloud` for AWS boundaries and fact envelopes.
- `internal/facts` for durable fact envelopes.

## Telemetry

This package emits no metrics or spans directly. The `awssdk` adapter emits
AWS API call counters, throttle counters, and pagination spans.

## Gotchas / invariants

- EC2 instance inventory is explicitly out of scope. ENI attachment metadata
  may carry an instance ARN as target evidence, but this package does not emit
  `aws_ec2_instance` resource facts.
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

### security_group_rule posture fact, PR1 facts-only (#1135)

No-Regression Evidence: `go test
./internal/collector/awscloud/services/ec2/... ./internal/facts -count=1` covers
`TestScannerEmitsNetworkTopologyWithoutInstanceFacts` (now also asserting one
`aws_security_group_rule` fact with `group_id=sg-123`, `direction=ingress`,
`source_kind=cidr_ipv4`, `source_value=0.0.0.0/0`, `is_internet=true`) and the
`aws_resource`/`aws_relationship` counts (5/7) are unchanged, proving the
posture fact is purely additive. The new fact is built from the rule slice the
scanner already fetched via `ListSecurityGroupRules`, so it adds no AWS API call
and no per-resource fan-out; emission is one extra in-memory envelope per rule
inside the existing rule loop.

No-Observability-Change: the scanner emits facts only; it adds no instrument,
span, metric label, or `aws_scan_status` row. The `awssdk` adapter's existing
`DescribeSecurityGroupRules` pagination span and API-call counter already cover
the read that sources the fact.

### Partition-aware ARNs (#866)

No-Regression Evidence: `go test ./internal/collector/awscloud/services/ec2/... -count=1`
covers the new `TestEC2InstanceARNDerivesPartition` (commercial / `aws-us-gov` /
`aws-cn`) alongside the existing commercial assertions. The synthesized EC2
instance ARN used as a network-interface attachment target now derives its
partition from the instance region via `awscloud.PartitionForRegion` instead of
hardcoding `aws`, so the ENI->instance edge resolves in GovCloud and China.
Commercial output (`us-east-1`) is byte-for-byte unchanged; this is a
metadata-only correctness fix with no graph-write, queue, or hot-path behavior
change.

No-Observability-Change: the fix only changes the partition substring of a
synthesized ARN value; no instrument, span, metric label, or `aws_scan_status`
row changes.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/reference/telemetry/index.md`
