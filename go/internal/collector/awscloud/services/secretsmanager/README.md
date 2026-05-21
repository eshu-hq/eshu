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

```mermaid
flowchart LR
  A["Secrets Manager API adapter"] --> B["Client"]
  B --> C["Scanner.Scan"]
  C --> D["aws_resource"]
  C --> E["aws_relationship"]
  D --> F["facts.Envelope"]
  E --> F
```

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - minimal Secrets Manager metadata read surface consumed by
  `Scanner`.
- `Scanner` - emits secret metadata and direct KMS/rotation Lambda relationship
  facts for one boundary.
- `Secret` - scanner-owned metadata-only secret representation.

## Dependencies

- `internal/collector/awscloud` for boundaries, resource constants,
  relationship constants, and envelope builders.
- `internal/facts` for emitted fact envelope kinds.

The package depends on a small `Client` interface rather than the AWS SDK for Go
v2 so tests can use fake clients and runtime adapters can own SDK behavior.

## Telemetry

This scanner emits no spans or logs directly. `awsruntime.ClaimedSource`
records scan duration and emitted resource counts after `Scanner.Scan` returns.
The `awssdk` adapter records Secrets Manager API call counts, throttles, and
pagination spans.

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

## Verification

```bash
go test ./internal/collector/awscloud/services/secretsmanager/... -count=1
go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/... -count=1
go run ./cmd/eshu docs verify ../go/internal/collector/awscloud/services/secretsmanager --limit 1000 \
  --fail-on contradicted,missing_evidence
```

Run the AWS runtime tests when scan warnings or partial-status behavior changes.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
