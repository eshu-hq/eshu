# AGENTS.md - internal/collector/awscloud/services/applicationautoscaling guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Application Auto Scaling domain types.
3. `scanner.go` - scalable target, scaling policy, and scheduled action resource
   and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and partition-aware target ARN
   synthesis.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Application Auto Scaling API access behind `Client`; do not import the
  AWS SDK into this package.
- Read only the three Describe APIs. Never register, deregister, put, delete, or
  invoke a scaling action.
- Never persist step-scaling or target-tracking configuration bodies. Keep only
  the bound CloudWatch alarm ARNs.
- Every relationship `TargetType` must be a declared `awscloud.ResourceType*`
  constant, and every `TargetResourceID` must match how the target scanner
  publishes its `resource_id`. Verify against the target scanner before adding a
  new edge; skip rather than dangle.
- Synthesize target ARNs only with the partition from
  `awscloud.PartitionForBoundary`; never hardcode `arn:aws:`.
- Canonicalize `service_kind` by switching on `strings.TrimSpace(...)` and
  writing the canonical constant back on the merged empty/matched case.
- Keep every Go file under 500 lines.

## Verification

```
go test ./internal/collector/awscloud/services/applicationautoscaling/... -count=1
go test ./internal/collector/awscloud/ -run ServiceKind -count=1
golangci-lint run ./internal/collector/awscloud/services/applicationautoscaling/...
```
