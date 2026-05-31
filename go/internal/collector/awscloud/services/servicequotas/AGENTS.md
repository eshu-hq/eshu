# AGENTS.md - internal/collector/awscloud/services/servicequotas guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shape, and
   invariants.
2. `types.go` - scanner-owned Service Quotas domain types.
3. `scanner.go` - applied-quota resource emission and the service_kind switch.
4. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Service Quotas API access behind `Client`; do not import the AWS SDK into
  this package.
- Never request, modify, or delete a quota. Never call
  `RequestServiceQuotaIncrease`, any quota-increase template API, or any
  `Put*`/`Delete*` mutation. The SDK surface is `List`-only.
- Emit no relationships. A quota references an AWS service code, not a scanned
  resource, so a cross-service edge would dangle. Record the service code as a
  quota attribute instead. If a future quota field reports a real ARN to a
  scanned resource, verify the target scanner's published resource_id before
  keying any edge, and prefer skipping the edge over dangling it.
- The quota node publishes its resource_id as the quota ARN, falling back to a
  stable `<service_code>/<quota_code>` key. Never synthesize a partition-aware
  ARN here; the API supplies the ARN directly.
- Set `overridden` only when both the applied value and the AWS default are
  known and differ. The join by quota code happens in the `awssdk` adapter.
- Record the CloudWatch usage metric as identity only (namespace, name,
  dimensions, recommended statistic). Never read a metric sample value.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from quota names, service
  codes, or values.
- Keep quota names, values, ARNs, and AWS error payloads out of metric labels.

## Common Changes

- Add a new quota metadata field by extending the scanner-owned `ServiceQuota`
  type, writing a focused scanner or adapter test first, then mapping it through
  the `awscloud` envelope builder. If the field carries usage-sample data or a
  quota-change request, leave it out of the scanner contract.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not request, modify, or delete quotas, or touch quota-increase templates or
  request history.
- Do not invent cross-service edges from a quota's service code.
- Do not resolve Service Quotas names or values into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
