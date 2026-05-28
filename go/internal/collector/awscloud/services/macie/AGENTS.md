# AGENTS.md - internal/collector/awscloud/services/macie guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Macie domain types (identity and counts only).
3. `scanner.go` and `relationships.go` - resource and relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.
6. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage
   and data boundary.

## Invariants

- Macie is the highest-redaction scanner in the collector. Keep Macie API
  access behind `Client`; do not import the AWS SDK into this package.
- Emit reported evidence only. Do not infer environment, deployment, workload,
  ownership, or defender posture truth from Macie names, tags, or accounts.
- Never persist sensitive-data findings. They are the personally identifiable
  information Macie detected. The scanner makes no `GetSensitiveDataOccurrences`,
  `GetSensitiveDataOccurrencesAvailability`, `GetFindings`, or `ListFindings`
  call.
- Never persist custom data identifier regular-expression bodies, keywords, or
  ignore words. The regex IS a description of the sensitive data. Custom data
  identifier facts are identity-only.
- Never persist allow-list contents. Allow-list facts are identity-only.
- Never persist findings filter criteria. Filter facts are identity, name, and
  action only.
- Never persist classification-job bucket-criteria expressions or the explicit
  bucket list. Jobs carry aggregate `target_bucket_count`,
  `target_account_count`, and a `uses_bucket_criteria` boolean only.
- Never persist member email addresses (personal contact data).
- Aggregate finding counts are grouped by severity label only and live as a
  `finding_counts_by_severity` attribute on the session resource. Do not group
  by finding type, bucket name, or job id.
- A standalone (non-administrator) account emits no member relationships. A
  disabled account emits one disabled session resource and stops.
- The member-to-administrator relationship targets the administrator account's
  Macie session resource id and sets a non-empty `target_type`.
- Keep account IDs, job IDs, list IDs, filter IDs, and tags out of metric
  labels.

## Common Changes

- Add a new safe Macie metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. Never add a field able to hold a regex body,
  list contents, finding detail, or criteria.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not call GetSensitiveDataOccurrences, GetSensitiveDataOccurrencesAvailability,
  GetFindings, ListFindings, GetCustomDataIdentifier, BatchGetCustomDataIdentifiers,
  TestCustomDataIdentifier, GetAllowList, GetFindingsFilter,
  DescribeClassificationJob, DescribeBuckets, or any mutation API.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
