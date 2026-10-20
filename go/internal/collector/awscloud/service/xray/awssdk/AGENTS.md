# AGENTS — services/xray/awssdk

This adapter is the contract surface between the AWS SDK v2 and the
`xray.Client` interface. Agents editing this package MUST:

- Treat the local `apiClient` interface as a security boundary. It MUST list
  exactly `GetGroups`, `GetSamplingRules`, and `GetEncryptionConfig`. Adding any
  trace, service-graph, insight, telemetry, or mutation method
  (`GetTraceSummaries`, `BatchGetTraces`, `GetTraceGraph`, `GetServiceGraph`,
  `GetTimeSeriesServiceStatistics`, `GetInsight*`, `GetSamplingTargets`,
  `GetSamplingStatisticSummaries`, `PutTraceSegments`, `PutTelemetryRecords`,
  `CreateGroup`/`UpdateGroup`/`DeleteGroup`,
  `CreateSamplingRule`/`UpdateSamplingRule`/`DeleteSamplingRule`,
  `PutEncryptionConfig`) is a rule violation; the reflection test in
  `client_test.go` fails if the interface shape changes.
- Persist only configuration. The group filter expression is a config string;
  never expand it into the traces it selects. Never map trace, segment, or
  service-map data onto the scanner-owned model.
- Use pagination through `NextToken` for `GetGroups` and `GetSamplingRules`. Do
  not assume a single page.
- Record every API call through `recordAPICall` so the runtime gets
  per-operation counters, throttle attribution, and tracer spans.

## Layout

- `client.go`: the `Client` adapter, paginator loops, and the narrow
  `apiClient` interface.
- `mapper.go`: SDK-to-scanner-model mapping helpers.
- `client_test.go`: reflection over the `apiClient` interface to prove its
  shape, plus a paginating fake that exercises the mapping.
