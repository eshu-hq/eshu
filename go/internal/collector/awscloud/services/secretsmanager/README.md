# AWS Secrets Manager Scanner

## Purpose

`internal/collector/awscloud/services/secretsmanager` owns the AWS Secrets
Manager scanner contract for the AWS cloud collector. It converts secret
control-plane metadata into `aws_resource` facts and emits relationship
evidence when AWS directly reports KMS key and rotation Lambda dependencies.

## Ownership boundary

This package owns scanner-level Secrets Manager fact selection and identity
mapping. It does not own AWS SDK pagination, STS credentials, workflow claims,
fact persistence, graph writes, reducer admission, workload ownership, or query
behavior.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - minimal Secrets Manager metadata read surface consumed by
  `Scanner`.
- `Scanner` - emits secret metadata and direct KMS/rotation Lambda relationship
  facts for one boundary.
- `Secret` - scanner-owned metadata-only secret representation.

## Dependencies

The scanner imports AWS collector boundaries, resource/relationship constants,
envelope builders, and fact envelope kinds. It depends on a small `Client`
interface rather than the AWS SDK so tests can use fake clients and runtime
adapters can own SDK behavior.

## Telemetry

This scanner emits no spans or logs directly. `awsruntime.ClaimedSource`
records scan duration and emitted resource counts; the `awssdk` adapter records
Secrets Manager API call counts, throttles, and pagination spans.

## Gotchas / invariants

- Secrets Manager facts are metadata only. The scanner must not read secret
  values, version payloads, resource policy JSON, external rotation partner
  metadata, or mutate Secrets Manager resources.
- Secret identity, description presence, KMS key identifier, rotation state,
  rotation Lambda ARN, timestamps, primary region, owning service, type, safe
  rotation schedule fields, and tags are reported control-plane metadata.
- Tags are raw AWS tag evidence. Do not infer environment, owner, workload,
  repository, or deployable-unit truth from tags in this package.
- KMS and rotation Lambda relationships are reported join evidence only.
  Correlation belongs in reducers.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
