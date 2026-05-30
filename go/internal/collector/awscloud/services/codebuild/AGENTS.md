# AGENTS.md - internal/collector/awscloud/services/codebuild guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned CodeBuild domain types.
3. `scanner.go` - project/report-group/build fact emission.
4. `relationships.go` - project relationship derivation and join keys.
5. `observations.go` - resource observation builders.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep CodeBuild API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist buildspec.yml bodies. `ProjectSource` holds only the
  source type, location, and identifier; it has no buildspec field.
- Never persist environment-variable PLAINTEXT values. The SDK adapter redacts
  them; `Scanner.Scan` fails closed when the redaction key is zero.
- Never persist build logs or source-credential tokens. `Build` holds only
  identity, status, and duration metadata.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from CodeBuild names or tags.
- Every relationship target carries a non-empty `target_type` and a target
  resource id that matches the owning scanner's `resource_id`. Do not emit an
  edge whose target type is empty.
- Derive S3 bucket ARNs as `arn:<partition>:s3:::bucket`, taking the partition
  from the scan boundary's region (`partition(boundary)`). S3 ARNs omit region
  and account but DO carry the partition segment, so it must match the S3 bucket
  node identity (`arn:<partition>:s3:::bucket`) or the edge dangles in GovCloud
  and China. Do not hardcode `aws`.

## Common Changes

- Add a new CodeBuild metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when the CodeBuild API reports both sides
  directly and the target names a concrete resource whose owning scanner
  resource_id you can match.
- Extend SDK pagination and mapping in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read buildspec bodies, build logs, environment-variable plaintext
  values, or source credentials.
- Do not resolve CodeBuild names, tags, or source links into workload ownership
  here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
