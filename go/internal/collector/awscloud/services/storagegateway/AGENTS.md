# AGENTS.md - internal/collector/awscloud/services/storagegateway guidance

## Read First

1. `README.md` - package purpose, exported surface, join contract, and
   invariants.
2. `types.go` - scanner-owned Storage Gateway domain types.
3. `scanner.go` - gateway, volume, file-share, and relationship emission.
4. `relationships.go` - relationship emission rules and resource-id helpers.
5. `helpers.go` - S3 bucket-ARN derivation, VPC-endpoint-id detection, and
   cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Storage Gateway API access behind `Client`; do not import the AWS SDK
  into this package.
- Never activate, delete, shut down, reboot, or update a gateway; never refresh
  a file-share cache; never create or delete volumes or shares; never wire any
  `Activate*`, `Delete*`, `Shutdown*`, `Reboot*`, `Update*`, `Refresh*`,
  `Create*`, or tape API.
- Never persist file-share object contents, NFS client allow lists, SMB
  admin/user lists, local-console passwords, or SMB guest passwords.
- The gateway node's `resource_id` is its ARN. Volume-to-gateway and
  file-share-to-gateway edges must key the gateway by that ARN so the join
  resolves.
- Emit file-share-to-S3-bucket edges only when `LocationARN` is an S3 bucket
  ARN. Reduce it to the bucket-only ARN the S3 scanner publishes and derive the
  partition from the source ARN with `awscloud.PartitionFromARN`. Never hardcode
  `arn:aws:`. Skip S3 access-point `LocationARN`s.
- Emit file-share-to-IAM-role, file-share-to-KMS-key, and
  file-share-to-CloudWatch-log-group edges only when AWS reports an ARN-shaped
  identity. Key the KMS target by ARN to match the EFS/FSx convention and the
  KMS node's ARN correlation anchor.
- Emit gateway-to-VPC-endpoint edges only when the reported VPCEndpoint value is
  the bare `vpce-` identifier the VPC scanner publishes as its `resource_id`.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from gateway, volume, or
  file-share names or AWS tags.
- Preserve stable gateway, volume, and file-share identities across repeated
  observations in the same AWS generation.
- Keep Storage Gateway ARNs, names, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new Storage Gateway metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. Leave any field that can carry object contents,
  client identity lists, or credential material out of the contract.
- Add new relationship evidence only when the Storage Gateway API reports both
  sides directly and the target identity matches the resource_id the target
  scanner publishes (ARN-equality or bare-id, verified by reading that
  scanner).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not call any gateway lifecycle, cache-refresh, volume/share mutation, tape,
  or credential API.
- Do not persist NFS client allow lists, SMB admin/user lists, or any object
  content.
- Do not resolve Storage Gateway names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
