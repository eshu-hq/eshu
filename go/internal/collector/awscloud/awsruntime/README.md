# internal/collector/awscloud/awsruntime

## Purpose

`internal/collector/awscloud/awsruntime` adapts AWS service scanners to
workflow-claimed collector execution. It turns one authorized AWS work claim
into one scanner run and one `collector.CollectedGeneration`.

## Ownership boundary

This package owns claim parsing, target authorization, per-account concurrency,
claim-scoped credential acquisition and release, pagination checkpoint expiry,
scanner-side status updates, and production scanner registry introspection.

It does not map AWS responses into facts, persist workflow rows, commit facts,
write graph rows, or decide reducer/query truth.

## Exported surface

Use `go doc ./internal/collector/awscloud/awsruntime` for the complete contract.
The main surface is:

- `ClaimedSource`, `Config`, `Target`, `TargetScope`, and credential config
  types.
- `CredentialProvider`, `CredentialLease`, `AWSConfigLease`, and SDK-backed
  implementations.
- `AccountLimiter` for per-account concurrency.
- `ScannerFactory`, `ServiceScanner`, `DefaultScannerFactory`,
  `SupportedServiceKinds`, and `SupportsServiceKind`.
- `ScanStatusStore` and checkpoint store aliases used by runtime wiring.

## Dependencies

- `internal/collector/awscloud` supplies fact contracts and scan-status types.
- AWS service scanner packages under `services/*` provide production scanners.
- `internal/telemetry` supplies claim, credential, pagination, and scan signals.
- `internal/workflow` supplies claim work items through command/runtime wiring.

## Telemetry

Key spans and metrics include `aws.collector.claim.process`,
`aws.credentials.assume_role`, `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_claim_concurrency`,
`eshu_dp_aws_assumerole_failed_total`,
`eshu_dp_aws_budget_exhausted_total`,
`eshu_dp_aws_pagination_checkpoint_events_total`,
`eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`,
`eshu_dp_aws_tag_observations_emitted_total`, and
`eshu_dp_aws_scan_duration_seconds`.

Keep resource names, ARNs, page tokens, policy text, credential material, and
raw AWS errors out of metric labels.

## Gotchas / invariants

- Claim parsing uses structured JSON. Do not infer scope from ARNs or free-form
  strings.
- Unauthorized claims must not acquire credentials.
- Central AssumeRole scopes require same-account role routing and external ID;
  local workload-identity scopes must not carry AssumeRole routing fields.
- `CredentialLease.Release` must clear temporary credential material after scan
  attempts.
- ECS and Lambda scanners require a redaction key.
- Add full-scan services through `DefaultScannerFactory` and
  `SupportedServiceKinds`; do not branch scanner selection in commands.

## Focused tests

```bash
go test ./internal/collector/awscloud/awsruntime -count=1
go test ./cmd/collector-aws-cloud -count=1
go doc ./internal/collector/awscloud/awsruntime
```

## Related docs

- `go/internal/collector/awscloud/README.md`
- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
- `docs/public/deployment/service-runtimes.md`
