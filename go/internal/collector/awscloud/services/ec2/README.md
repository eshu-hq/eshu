# AWS EC2 Scanner

## Purpose

`internal/collector/awscloud/services/ec2` owns scanner-side EC2 network fact
selection for the AWS cloud collector. It converts VPCs, subnets, security
groups, security group rules, and network interfaces into `aws_resource` and
`aws_relationship` facts.

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
```

## Exported surface

See `doc.go` for the godoc contract.

- `Scanner` - emits EC2 network topology facts for one claimed AWS boundary.
- `Client` - scanner-owned read surface implemented by `awssdk.Client`.
- `VPC`, `Subnet`, `SecurityGroup`, `SecurityGroupRule`, and
  `NetworkInterface` - scanner-owned EC2 records.
- `NetworkInterfaceAttachment` - ENI attachment metadata, including attached
  resource ARN when AWS reports enough data to derive one.

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
  rule `aws_relationship` edge.
- ENIs emit placement and attachment relationships so reducers can later join
  ECS, EKS, or Lambda runtime evidence to subnet and VPC topology.
- Descriptions and tags are user-controlled text. They are preserved in fact
  payloads, but must never become metric labels.
- This package emits reported AWS evidence only. Do not infer public exposure,
  service ownership, environment, deployable-unit truth, or workload truth here.

## Evidence

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
