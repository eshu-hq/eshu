# AGENTS.md - internal/collector/awscloud/services/datasync guidance

## Read First

1. `README.md` - package purpose, exported surface, partition policy, and
   invariants.
2. `types.go` - scanner-owned DataSync domain types.
3. `scanner.go` - task, location, agent, and relationship emission.
4. `relationships.go` - relationship emission rules and the local
   `partition(boundary)` helper.
5. `helpers.go` - log-group ARN trim, ARN-shape check, and time helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DataSync API access behind `Client`; do not import the AWS SDK into this
  package.
- Never start, cancel, create, update, or delete a transfer task, location, or
  agent, and never wire any `Create*`, `Start*`, `Cancel*`, `Update*`, or
  `Delete*` API.
- Never persist the object or record contents a task transfers,
  object-storage access keys, server certificates, SMB/object-storage
  passwords, include/exclude filter patterns, or manifest bodies.
- Use task, location, and agent ARNs from the API directly as `resource_id`.
- Synthesize S3/EFS/FSx backing-storage ARNs partition-aware through
  `partition(boundary)`. Never hardcode `arn:aws:`. The `partitionguard` test
  (#866) fails CI otherwise. Prefer the ARN the API reports directly (FSx for
  NetApp ONTAP `FsxFilesystemArn`) over synthesis.
- Match the resource_id each target scanner publishes:
  `arn:<partition>:s3:::<bucket>` for S3,
  `arn:<partition>:elasticfilesystem:<region>:<account>:file-system/<fs-id>` for
  EFS, and `arn:<partition>:fsx:<region>:<account>:file-system/<fs-id>` for FSx.
  Trim the trailing `:*` wildcard from the CloudWatch log group ARN.
- Emit a location storage or IAM-role edge only when the backing identity is
  present and ARN-shaped where required. NFS, SMB, object-storage, HDFS, and
  Azure Blob locations have no AWS backing resource and emit no storage edge.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from task, location, or
  agent names.

## Common Changes

- Add a new DataSync metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry credential or
  object-content material, leave it out of the scanner contract.
- Add new relationship evidence only when the DataSync API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id.
- Add a new location flavor parser in `helpers.go`/`awssdk` only after
  confirming the URI scheme and backing-identity field from the AWS SDK.

## What Not To Change Without An ADR

- Do not run transfers, mutate tasks, mutate locations, mutate agents, or call
  any DataSync mutation or transfer-control API.
- Do not call `DescribeTaskExecution`, `ListTaskExecutions`, or any execution
  read that exposes transferred-file paths or per-file transfer detail.
- Do not persist object-storage access keys, server certificates,
  SMB/object-storage passwords, or include/exclude filter patterns.
- Do not resolve DataSync names into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
