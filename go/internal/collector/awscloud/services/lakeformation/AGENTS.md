# AGENTS.md - internal/collector/awscloud/services/lakeformation guidance

## Read First

1. `README.md` - package purpose, exported surface, data-sensitivity policy,
   and invariants.
2. `types.go` - scanner-owned Lake Formation domain types.
3. `scanner.go` - settings, registered-resource, permission, and relationship
   emission.
4. `helpers.go` - partition-aware S3 ARN derivation, Glue table id shaping, and
   scanner-side cloning helpers.
5. `relationships.go` - relationship emission rules.
6. `../glue/` - Glue scanner this package mirrors; permission edges reuse the
   Glue database and table `resource_id` shapes.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Lake Formation API access behind `Client`; do not import the AWS SDK
  into this package.
- Never grant, revoke, register, deregister, put settings, or wire any
  `Grant*`, `Revoke*`, `Register*`, `Deregister*`, `Put*`, `Create*`,
  `Update*`, `Delete*`, or `AddLFTags*`/`RemoveLFTags*` API.
- Never persist a permission policy body, a permission condition (LF-Tag)
  expression, an LF-Tag value, an LF-Tag policy expression, or principal
  credentials. Emit grant identities, principal identifiers, and resource ARNs
  only.
- Permission privilege names (`SELECT`, `ALTER`, `ALL`, ...) are a closed AWS
  enum recorded as grant identity. Do not widen this to free-form policy text.
- Derive the S3 bucket ARN from the registered location ARN with
  `awscloud.PartitionFromARN` (or the local `partition(boundary)` fallback);
  never hardcode `arn:aws:`.
- Emit the permission-to-principal edge only when the principal identifier is
  an IAM role ARN; the resource edge still resolves for special principals.
- Emit the registered-location-to-S3 edge only when the registered ARN is an
  S3 location ARN, and the registered-location-to-IAM-role edge only when AWS
  reports an ARN-shaped role identity.
- Every relationship sets a non-empty declared `awscloud.ResourceType*`
  `TargetType` and a matching `TargetResourceID` (the relguard contract).
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from principal, database, or
  table names.
- Keep Lake Formation ARNs, names, and AWS error payloads out of metric labels.

## Common Changes

- Add a new Lake Formation metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry policy, condition, or
  LF-Tag value material, leave it out of the scanner contract until an ADR
  documents a sanitized exception.
- Add new relationship evidence only when the Lake Formation API reports both
  sides directly and the target identity is not sensitive (an ARN or a
  catalog-stable name) and matches how the target scanner publishes its
  `resource_id`.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not grant, revoke, register, deregister, put settings, or call any Lake
  Formation mutation or LF-Tag mutation API.
- Do not call `GetTemporaryGlueTableCredentials`,
  `GetTemporaryGluePartitionCredentials`,
  `GetTemporaryDataLocationCredentials`, `GetTableObjects`, `GetWorkUnits`,
  `GetWorkUnitResults`, `StartQueryPlanning`, or any governed-data or
  credential-vending read.
- Do not persist permission condition expressions, LF-Tag values, LF-Tag policy
  expressions, or `AdditionalDetails` RAM-share payloads.
- Do not resolve Lake Formation principal or resource names into workload
  ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
