# AGENTS.md - internal/collector/awscloud/services/efs guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned EFS domain types.
3. `scanner.go` - file system, access point, mount target, and replication
   resource and relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep EFS API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read file contents.
- Never persist NFS file system policy bodies (`DescribeFileSystemPolicy`
  output) or backup policy bodies.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from file system names or tags.
- Emit the file-system-to-KMS-key relationship only for encrypted file systems
  with a reported KMS key ARN.
- Keep file system ARNs, IP addresses, root directory paths, and tags out of
  metric labels.

## Common Changes

- Add a new EFS metadata field by extending the relevant scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when the EFS API reports both sides
  directly.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read or sample file contents.
- Do not request, read, or persist file system policy bodies or backup policy
  bodies.
- Do not resolve file system names, tags, subnets, or security groups into
  workload ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
