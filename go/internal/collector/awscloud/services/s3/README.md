# AWS S3 Scanner

## Purpose

`internal/collector/awscloud/services/s3` owns the Amazon S3 scanner contract
for the AWS cloud collector. It converts bucket control-plane metadata into
`aws_resource` facts and emits relationship evidence when S3 reports a bucket
server-access-log target bucket.

## Ownership boundary

This package owns scanner-level S3 fact selection and identity mapping. It does
not own AWS SDK pagination, STS credentials, workflow claims, fact persistence,
graph writes, reducer admission, or query behavior.

## Exported surface

See `doc.go` and the exported comments in `types.go` and `scanner.go` for the
godoc contract. Keep bucket model field details in source comments, not in this
README.

## Dependencies

- `internal/collector/awscloud` for boundaries, resource constants,
  relationship constants, and envelope builders.
- `internal/facts` for emitted fact envelope kinds.

The package depends on a small `Client` interface rather than the AWS SDK for Go
v2 so tests can use fake clients and runtime adapters can own SDK behavior.

## Telemetry

This scanner emits no spans or logs directly. `awsruntime.ClaimedSource`
records scan duration and emitted resource counts after `Scanner.Scan` returns.
The `awssdk` adapter records S3 API call counts, throttles, and pagination
spans.

## Gotchas / invariants

- S3 facts are metadata only. The scanner must not read objects, list object
  keys, mutate buckets, or persist object inventory.
- Bucket policy JSON, ACL grants, replication rules, lifecycle rules,
  notification configuration, inventory configuration, analytics configuration,
  and metrics configuration are not persisted.
- Website configuration is reduced to status flags, redirect host, and routing
  rule count. Index and error document object keys are not persisted.
- Logging target grants and object-key format are not persisted. The scanner
  records only the target bucket and target prefix needed for relationship
  evidence.
- Tags are raw AWS tag evidence. Do not infer environment, owner, workload, or
  deployable-unit truth from tags in this package.

## Verification

```bash
go test ./internal/collector/awscloud/services/s3/... -count=1
go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/... -count=1
go run ./cmd/eshu docs verify ../go/internal/collector/awscloud/services/s3 --limit 1000 \
  --fail-on contradicted,missing_evidence
```

Run the AWS runtime tests when scan warnings or partial-status behavior changes.

## Related docs

- `docs/public/services/collector-aws-cloud.md`
- `docs/public/services/collector-aws-cloud-scanners.md`
- `docs/public/guides/collector-authoring.md`
