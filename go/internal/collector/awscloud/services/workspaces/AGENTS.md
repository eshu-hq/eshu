# AGENTS.md - internal/collector/awscloud/services/workspaces guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned WorkSpaces domain types.
3. `scanner.go` - workspace, directory, bundle, and IP-group resource and
   relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware ARN synthesis, and
   scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep WorkSpaces API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist passwords, directory registration codes, WorkSpace IP
  addresses, connection state, or any session/credential payload. User names are
  identity metadata and are allowed; secrets are not. Never call any reboot,
  rebuild, start, stop, terminate, modify, or `Create*`/`Delete*` mutation API.
- The WorkSpaces describe APIs return no ARNs. The workspace, directory, bundle,
  and IP-group nodes publish a synthesized partition-aware WorkSpaces ARN as
  their resource_id via `workspacesARN`, which derives the partition with
  `awscloud.PartitionForBoundary` and never hardcodes `arn:aws:`. The bare id is
  the fallback when account or region is missing.
- The internal workspace-in-directory edge targets the directory node's
  synthesized ARN. The directory-to-DS-directory edge targets the BARE directory
  id (`d-...`), which is the resource_id the `ds` scanner publishes. Do not
  collapse these two distinct nodes.
- The directory-to-subnet and directory-to-security-group edges target the BARE
  `subnet-...` / `sg-...` ids the `ec2` scanner publishes.
- The directory-to-IAM-role edge targets the role ARN the `iam` scanner
  publishes; the workspace-to-KMS-key edge targets the reported key reference.
  Set `target_arn` only when the value is ARN-shaped.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from WorkSpace, directory,
  bundle, or group names, or AWS tags.
- Preserve stable identities across repeated observations in the same AWS
  generation. Keep ARNs, names, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new WorkSpaces metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry a credential, registration
  code, IP address, or session detail, leave it out of the scanner contract.
- Add new relationship evidence only when the WorkSpaces API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (bare id for DS directory / subnet / security group, role
  ARN for IAM, key reference for KMS, synthesized WorkSpaces ARN for the internal
  directory/bundle/IP-group nodes).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read session contents, connection status, credentials, or registration
  codes, and do not call any WorkSpaces mutation API.
- Do not resolve WorkSpaces names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
