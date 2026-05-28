# AGENTS.md - internal/collector/awscloud/services/fsx guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned FSx domain types and the read-only `Client`.
3. `scanner.go` - file system, SVM, volume, snapshot, and backup emission.
4. `relationships.go` - VPC, subnet, KMS, AD-directory, backup, SVM, and volume
   relationship observations and ARN-upgrade join logic.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - FSx coverage.

## Invariants

- Keep FSx API access behind `Client`; do not import the AWS SDK into this
  package.
- NEVER add a mutation API (Create/Delete/Update/Restore/Copy/Release) or a
  file-content read to the `Client` interface. A reflection test in the `awssdk`
  adapter enforces the metadata-only SDK seam.
- NEVER persist Active Directory self-managed credentials across any flavor: the
  Windows/SVM self-managed AD `Password`, `UserName`,
  `FileSystemAdministratorsGroup`, `DnsIps`, and `DomainJoinServiceAccountSecret`
  have no field on the scanner-owned types and must never gain one.
- NEVER persist the ONTAP fsxadmin password or the SVM admin password.
- Every relationship sets a non-empty `target_type` and a `target_resource_id`
  that matches the target scanner's `resource_id`. VPC -> `aws_ec2_vpc` (bare
  vpc-id), subnet -> `aws_ec2_subnet` (bare subnet-id), KMS -> `aws_kms_key`
  (key ARN or ID, `target_arn` only when ARN-shaped), AD -> `aws_ds_directory`
  (bare directory ID). FSx-internal edges upgrade to the parent file system or
  SVM ARN when known.
- Emit reported evidence only. Do not infer environment, workload ownership, or
  deployable-unit truth from file system names, mount paths, or tags.
- Keep secrets, mount paths, and tags out of metric labels.

## Common Changes

- Add a new FSx resource or attribute by extending the scanner-owned type,
  writing a focused scanner test first, then mapping it through `awscloud`
  envelope builders.
- Add new file system fields only when the FSx API reports them on the describe
  path and the field is safe for persistence (not a secret).
- Extend SDK pagination and SDK-to-scanner mapping in the `awssdk` adapter, not
  here.

## What Not To Change Without An ADR

- Do not resolve FSx file systems, SVMs, or volumes to source repositories or
  services here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not relax the credential-exclusion invariants; they are the security
  contract for this scanner (issue #735, multi-flavor redaction review).
