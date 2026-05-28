# AGENTS.md - internal/collector/awscloud/services/config guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned AWS Config domain types.
3. `scanner.go` and `relationships.go` - resource and relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.
6. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage
   and data boundary.

## Invariants

- Keep AWS Config API access behind `Client`; do not import the AWS SDK into
  this package.
- Emit reported evidence only. Do not infer environment, deployment, workload,
  ownership, or compliance posture truth from Config names, tags, or rule
  scopes.
- Never persist recorded configuration item bodies. A configuration item is a
  full resource snapshot; it is inventory state, not Config metadata. The
  scanner makes no GetResourceConfigHistory, BatchGetResourceConfig,
  GetDiscoveredResourceCounts, or discovered-resource-listing call.
- Never read per-resource compliance detail
  (GetComplianceDetailsByConfigRule, GetComplianceDetailsByResource). Aggregate
  per-rule compliance from DescribeConformancePackCompliance is allowed only to
  derive the member-rule set and count.
- Never fetch custom-rule Lambda code (GetCustomRulePolicy,
  GetOrganizationCustomRulePolicy).
- The rule resource-type scope is a rule attribute, not a relationship. Do not
  emit an edge to a synthetic resource-type node; it would dangle.
- Every relationship sets a non-empty `TargetType` and a `TargetResourceID`
  that matches the target scanner's resource_id convention: rule edges target
  `config-rule/<name>`, custom-rule-to-Lambda edges target the Lambda function
  ARN (`aws_lambda_function`), and aggregator-to-account edges target the
  account root ARN (`aws_account`).
- Derive the partition from a source ARN (the aggregator ARN) before
  synthesizing an account root ARN. Never hardcode `arn:aws:`. Skip the edge
  when the partition cannot be derived.

## Common Changes

- Add a new safe Config metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when AWS Config directly reports both sides
  and the target is a real emitted node or a cross-service account/Lambda target
  following the existing convention.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not call GetResourceConfigHistory, BatchGetResourceConfig,
  GetDiscoveredResourceCounts, GetComplianceDetailsByConfigRule,
  GetComplianceDetailsByResource, GetCustomRulePolicy, GetStoredQuery, or any
  mutation API (Put/Delete/Start/Stop/Tag/Untag).
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
