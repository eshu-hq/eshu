# services/cloudwatch

Amazon CloudWatch metadata-only scanner for the `collector-aws-cloud` runtime.
The package observes a single claimed `(account, region, service_kind=cloudwatch)`
boundary and emits Eshu facts for:

- Metric alarms (`aws_cloudwatch_alarm`).
- Composite alarms (`aws_cloudwatch_composite_alarm`).
- Dashboards, identity only (`aws_cloudwatch_dashboard`). The dashboard body
  JSON is never persisted because widget bodies often reveal internal
  infrastructure naming and KPI thresholds.
- Contributor Insights rules, identity only (`aws_cloudwatch_insight_rule`).
  The rule definition is never persisted because the SQL-like grammar may
  encode customer query patterns.
- Metric streams (`aws_cloudwatch_metric_stream`).

Relationships emitted from the same scan:

- `cloudwatch_alarm_notifies_sns_topic` for each SNS ARN in an alarm's
  AlarmActions, OKActions, or InsufficientDataActions.
- `cloudwatch_composite_alarm_has_child_alarm` for each child alarm referenced
  by a composite alarm's AlarmRule expression.
- `cloudwatch_metric_stream_delivers_to_firehose` for the metric stream's
  reported Kinesis Data Firehose destination.
- `cloudwatch_alarm_observes_metric` carrying the metric's namespace, metric
  name, and a dimension summary. Customer-tag-named dimension values are
  routed through the shared redact library before persistence.

## Boundary

The scanner is regional. CloudWatch Logs log groups are emitted by the
sibling `services/cloudwatchlogs` package and are not part of this contract.

## Forbidden APIs

The scanner never calls any mutation API. Forbidden surface includes
`PutMetricAlarm`, `DeleteAlarms`, `PutCompositeAlarm`, `PutDashboard`,
`DeleteDashboards`, `EnableAlarmActions`, `DisableAlarmActions`,
`SetAlarmState`, `PutInsightRule`, `DeleteInsightRules`, `StartMetricStreams`,
`StopMetricStreams`, and `PutMetricData`. The SDK adapter interface
intentionally omits `GetDashboard` so the dashboard body cannot be fetched.

## Telemetry

Each fact rides the standard
`eshu_dp_aws_resources_emitted_total{service="cloudwatch"}` counter. SDK
adapter calls record through `awscloud.RecordAPICall` and the runtime's
`AWSAPICalls` / `AWSThrottles` instruments.

## Registration

The scanner self-registers through `runtimebind/`. Importing
`github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudwatch/runtimebind`
or the `awsruntime/bindings` aggregate package is the only step required.

## Performance and Observability Evidence

This scanner adds read-only AWS Describe/List calls on the per-claim scan path;
it introduces no new Cypher, graph writes, reducer admission, queue pressure,
lease, batching, or concurrency knob. Each claim runs the bounded paginated
CloudWatch read surface (alarms, composite alarms, dashboards metadata,
Contributor Insights rules metadata, metric streams) once and emits typed
source facts; the reducer continues to own canonical graph writes downstream.

No-Regression Evidence: `cd go && go test ./internal/collector/awscloud/services/cloudwatch/... -count=1 -race`
and `go test ./internal/collector/awscloud/awsruntime/... -count=1 -race`
cover the scanner, the SDK adapter, and registry resolution. `golangci-lint
run ./internal/collector/awscloud/... ./cmd/collector-aws-cloud/...` reports
zero issues. The scan surface is bounded by the AWS account's alarm/dashboard
inventory and uses the shared paginator, so worst-case fan-out matches the
existing Phase 2 metadata scanners (e.g. CloudWatch Logs, SNS) already in the
repo-scale performance contract.

No-Observability-Change: facts ride the existing
`eshu_dp_aws_resources_emitted_total{service="cloudwatch"}` counter and SDK
calls record through `awscloud.RecordAPICall` into the runtime's `AWSAPICalls`
/ `AWSThrottles` instruments and the `aws.service.scan` span. No new metric,
span, or status field is introduced; label cardinality is bounded by the
`service` value and resource-type attribute.
