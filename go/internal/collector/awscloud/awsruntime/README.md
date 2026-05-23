# AWS Cloud Collector Runtime

## Purpose

`internal/collector/awscloud/awsruntime` adapts AWS service scanners to
workflow-claimed collector execution. It turns one authorized AWS claim into
one scanner run and one collected generation.

## Ownership Boundary

This package owns claim parsing, target authorization, per-account concurrency,
claim-scoped credential acquisition and release, pagination checkpoint expiry,
scan-status updates, and production scanner registry introspection. It does not
map AWS responses into facts, persist workflow rows, commit facts, write graph
rows, or decide reducer/query truth.

## Exported Surface

See `doc.go` and `go doc ./internal/collector/awscloud/awsruntime`. Main
exports include `ClaimedSource`, target and credential config types,
credential-provider interfaces, `AccountLimiter`, `DefaultScannerFactory`,
`SupportedServiceKinds`, and `SupportsServiceKind`.

## Telemetry

Key signals include `aws.collector.claim.process`,
`aws.credentials.assume_role`, `aws.service.scan`,
`aws.service.pagination.page`, `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`, `eshu_dp_aws_claim_concurrency`,
`eshu_dp_aws_assumerole_failed_total`, `eshu_dp_aws_budget_exhausted_total`,
`eshu_dp_aws_pagination_checkpoint_events_total`,
`eshu_dp_aws_resources_emitted_total`,
`eshu_dp_aws_relationships_emitted_total`,
`eshu_dp_aws_tag_observations_emitted_total`, and
`eshu_dp_aws_scan_duration_seconds`.

## Gotchas / Invariants

- Claims are parsed from structured JSON; do not infer scope from free-form
  strings or ARNs.
- Unauthorized claims must not acquire credentials.
- Central AssumeRole scopes require same-account role routing and external ID;
  local workload identity scopes must not carry AssumeRole routing fields.
- `CredentialLease.Release` clears temporary credential material after attempts.
- ECS and Lambda scanners require a redaction key.
- Copy the workflow fencing token into emitted fact envelopes.

## Focused Tests

```bash
cd go
go test ./internal/collector/awscloud/awsruntime -count=1
go test ./cmd/collector-aws-cloud -count=1
go run ./cmd/eshu docs verify ../go/internal/collector/awscloud/awsruntime --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `go/internal/collector/awscloud/README.md`
- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
