# AGENTS — services/xray

This package emits AWS X-Ray **configuration** as metadata-only facts: trace
groups, sampling rules, and the account-region encryption configuration. Agents
editing this package MUST:

- Read this package's `doc.go`, `README.md`, and the parent agent docs
  (`go/internal/collector/awscloud/AGENTS.md` and the repository root
  `AGENTS.md`).
- Treat the SDK adapter `apiClient` interface and the scanner-owned `Client`
  interface as the contract surface: only `GetGroups`, `GetSamplingRules`, and
  `GetEncryptionConfig` are allowed. Adding ANY trace, service-graph, insight,
  telemetry, or mutation method (`GetTraceSummaries`, `BatchGetTraces`,
  `GetTraceGraph`, `GetServiceGraph`, `GetTimeSeriesServiceStatistics`,
  `GetInsight*`, `GetSamplingTargets`, `GetSamplingStatisticSummaries`,
  `PutTraceSegments`, `PutTelemetryRecords`, `CreateGroup`/`UpdateGroup`/
  `DeleteGroup`, `CreateSamplingRule`/`UpdateSamplingRule`/`DeleteSamplingRule`,
  `PutEncryptionConfig`) is a rule violation; the exclusion reflection tests in
  `scanner_test.go` and `awssdk/client_test.go` assert this.
- NEVER read or persist X-Ray observability payload — traces, trace summaries,
  segments, or service-graph (service-map) data. That is monitoring data, not
  configuration truth. The group filter expression is persisted as a
  configuration string, never expanded into the traces it selects.
- Keep relationships graph-join safe: the KMS edge keys the reported key
  reference (ARN-keyed only when ARN-shaped, never a fabricated ARN for a bare
  id/alias), and the service-correlation edge targets the synthetic
  `aws_xray_service_correlation` anchor by `<service_name>/<service_type>`.
- Derive any synthesized ARN's partition from the boundary or a source ARN.
  Never hardcode `arn:aws:`. This scanner synthesizes no ARN today (the only
  synthetic id is the ARN-less encryption-config resource id).
- Require no `ESHU_AWS_REDACTION_KEY`: X-Ray configuration carries no
  secret-shaped fields. Do not add a redaction-key requirement without a real
  secret-bearing field and the `RequiresRedactionKey` flag in `runtimebind`.

## Layout

- `scanner.go`: the regional scanner that produces `facts.Envelope` lists.
- `relationships.go`: the KMS-key and service-correlation relationship helpers.
- `helpers.go`: id-synthesis and small value helpers.
- `types.go`: scanner-owned models and the configuration-only `Client` interface.
- `awssdk/`: AWS SDK adapter behind the `apiClient` interface — the contract
  surface that proves observability/mutation methods are unreachable.
- `runtimebind/`: package-init binder that registers the scanner with
  `awsruntime` (no redaction key).

## Tests

Focused tests live in `scanner_test.go`. They MUST cover:

- Group, sampling-rule, and encryption-config resources are emitted with the
  correct `resource_type`.
- The encryption-config-to-KMS-key edge joins the KMS key family (ARN-keyed and
  bare-id keyed), and emits no edge for NONE encryption.
- The sampling-rule-to-service correlation anchor is emitted for a named service
  and skipped for a wildcard-only rule.
- The configuration-only exclusion reflection test proves the `Client` interface
  exposes exactly the three config reads and no trace/service-map method.
- The graph-join contract via `relguard.AssertObservations`.

Adapter tests in `awssdk/` MUST assert that the `apiClient` interface is exactly
the three config reads, that pagination walks every page, and that the mapper
carries configuration only.
