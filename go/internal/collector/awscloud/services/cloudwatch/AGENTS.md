# AGENTS — services/cloudwatch

This package emits CloudWatch metric alarms, composite alarms, dashboards,
Contributor Insights rules, and metric streams as metadata-only facts. Agents
editing this package MUST:

- Read this package's `doc.go`, `README.md`, and the parent agent docs
  (`go/internal/collector/awscloud/AGENTS.md` and the repository root
  `AGENTS.md`).
- Treat the SDK adapter interface as the contract surface: only List/Describe
  shaped methods are allowed. Adding `GetDashboard`, any `Put*`, `Delete*`,
  `Enable*`, `Disable*`, `Start*`, `Stop*`, or `SetAlarmState` method is a
  rule violation and the existing tests assert this.
- NEVER persist a dashboard's body JSON. NEVER persist a Contributor Insights
  rule's definition. Alarm metric dimensions go through the shared redact
  library when the dimension name looks like a customer tag.
- Keep emitted facts metadata-only. Time-series metric data points belong to a
  separate read path; this scanner emits identity and configuration only.

## Layout

- `scanner.go`: the regional scanner that produces `facts.Envelope` lists.
- `relationships.go`: relationship helpers extracted from the scanner.
- `helpers.go`: redaction and tag/dimension cloning helpers.
- `types.go`: scanner-owned models and the `Client` interface.
- `awssdk/`: AWS SDK adapter behind the `apiClient` interface — the contract
  surface that proves forbidden methods are unreachable.
- `runtimebind/`: package-init binder that registers the scanner with
  `awsruntime`.

## Tests

Focused tests live in `scanner_test.go`. They MUST cover:

- All five resource types are emitted with the correct `resource_type`.
- Dashboard observations carry only name + last-modified (no body JSON).
- Contributor Insights rule observations carry only name + state (no
  definition).
- Customer-tag-named alarm metric dimensions are redacted in the observed
  metric relationship.

Adapter tests in `awssdk/` MUST assert that the `apiClient` interface excludes
`GetDashboard` and every mutation API, and that no such call was made during a
ListAlarms/ListDashboards/ListInsightRules/ListMetricStreams flow.
