# services/xray/awssdk

AWS SDK v2 adapter that satisfies `xray.Client`. The adapter is built around a
narrow `apiClient` interface so the compiler enforces the configuration-only
contract:

- `GetGroups` reads group configuration (name, ARN, filter expression, insights
  flags), paginated through `NextToken`.
- `GetSamplingRules` reads sampling rule configuration (name, ARN, priority,
  reservoir, fixed rate, service match criteria), paginated through `NextToken`.
  Records with no embedded rule are skipped, not mapped empty.
- `GetEncryptionConfig` reads the account-region encryption configuration
  (type, status, KMS key reference) as a single point read.

These three reads are the entire surface. Every observability-payload read
(`GetTraceSummaries`, `BatchGetTraces`, `GetTraceGraph`, `GetServiceGraph`,
`GetTimeSeriesServiceStatistics`, `GetInsight`, `GetInsightSummaries`,
`GetInsightEvents`, `GetInsightImpactGraph`, `GetSamplingTargets`,
`GetSamplingStatisticSummaries`) and every mutation (`PutTraceSegments`,
`PutTelemetryRecords`, `CreateGroup`, `UpdateGroup`, `DeleteGroup`,
`CreateSamplingRule`, `UpdateSamplingRule`, `DeleteSamplingRule`,
`PutEncryptionConfig`) is absent from the `apiClient` interface. Because the
adapter holds an `apiClient` value rather than the concrete `*xray.Client`, any
call to a method not on the interface would not compile.

The companion test reflects the `apiClient` interface at runtime and asserts:

- The interface contains exactly the three allowed config reads and nothing else.
- Pagination across `GetGroups` and `GetSamplingRules` walks every page.
- The mapper carries configuration only and skips empty sampling-rule records.

## Layout

- `client.go`: the `Client` adapter, paginator loops, and the narrow
  `apiClient` interface.
- `mapper.go`: SDK-to-scanner-model mapping helpers.
- `client_test.go`: reflection over `apiClient` to prove its shape, plus a
  paginating fake that exercises the mapping.
