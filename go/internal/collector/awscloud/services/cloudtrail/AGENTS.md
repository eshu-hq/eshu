# AGENTS.md - internal/collector/awscloud/services/cloudtrail guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned CloudTrail domain types and the `Client`
   interface (the security boundary).
3. `scanner.go` - resource and relationship emission.
4. `scanner_test.go` -
   `TestClientInterfaceExcludesEventPayloadAndMutationAPIs` is the guard
   test that enforces the metadata-only contract.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage
   and data boundary.

## Invariants

- CloudTrail is the audit-config service. The protected data class is the
  event payload itself, so the scanner must never call `LookupEvents` and
  must never persist event records.
- The `Client` interface is the security boundary. It must not expose
  `LookupEvents`, `StartQuery`, `GetQueryResults`, `CancelQuery`,
  `DescribeQuery`, or any mutation method (CreateTrail, UpdateTrail,
  DeleteTrail, StartLogging, StopLogging, PutEventSelectors,
  PutInsightSelectors, Create/Update/Delete EventDataStore/Channel/Dashboard,
  StartEventDataStoreIngestion, StopEventDataStoreIngestion,
  StartDashboardRefresh, RestoreEventDataStore).
- Event selectors are persisted as bounded summaries: total count, advanced
  selector count, and per-resource-type counts. Raw selector bodies, field
  filters, and value matchers must not be persisted.
- Insight selectors are persisted as a list of insight type names only.
- Lake event data stores carry retention, multi-region, organization,
  termination protection, KMS, and selector-count metadata; advanced selector
  bodies and Lake query strings or results are never persisted.
- Dashboards carry status, refresh schedule, and a widget count; widget query
  bodies and result rows are never persisted.
- Keep CloudTrail API access behind `Client`; do not import the AWS SDK into
  this package.
- Emit reported evidence only. Do not infer environment, deployment, workload,
  ownership, or deployable-unit truth from trail names, tags, or selectors.
- Keep trail ARNs, store ARNs, bucket names, log group ARNs, key ARNs, SNS
  ARNs, and tags out of metric labels.

## Common Changes

- Add a new safe CloudTrail metadata field by extending the scanner-owned
  type, writing a focused scanner or adapter test first, then mapping it
  through `awscloud` envelope builders.
- Add new relationship evidence only when CloudTrail directly reports both
  sides and the value is metadata.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not call `LookupEvents` from any code path the scanner can reach.
- Do not add Lake query data-plane calls (`StartQuery`, `GetQueryResults`).
- Do not add CloudTrail mutation APIs.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
