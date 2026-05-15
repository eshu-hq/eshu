# ADR: Observability Collector

**Date:** 2026-05-15
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- Issue: `#19`
- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `2026-05-14-service-story-dossier-contract.md`
- `2026-05-15-ci-cd-run-collector.md`
- `2026-05-15-service-catalog-collector.md`

---

## Context

Eshu already emits its own metrics, traces, logs, and status surfaces. This ADR
is not about changing Eshu's internal telemetry. Issue `#19` asks for a future
collector that observes external observability control planes: OpenTelemetry
Collector configs, Prometheus scrape jobs and rules, Grafana dashboards and
datasources, Datadog monitors and service definitions, and related metadata.

The collector must not turn Eshu into a second metrics, logs, traces, or
profiles database. Raw telemetry streams are high-volume, time-dependent,
privacy-sensitive, and already owned by observability systems. Eshu needs the
map of what observes a service, which labels and service names are used, which
dashboards and monitors depend on those signals, and where coverage is missing
or stale.

This ADR is design-only. Runtime implementation should wait until the current
collector deployment lane has stable proof for hosted credentials, pagination,
rate limits, redaction, and status reporting. Fixture-backed parsing for
checked-in configs and dashboards can start earlier.

## Source References

This ADR was checked against the current public contracts for the first source
set:

- OpenTelemetry specification:
  <https://opentelemetry.io/docs/specs/otel/>
- OTLP specification:
  <https://opentelemetry.io/docs/specs/otlp/>
- OpenTelemetry Collector configuration:
  <https://opentelemetry.io/docs/collector/configuration/>
- OpenTelemetry semantic conventions:
  <https://opentelemetry.io/docs/specs/otel/semantic-conventions/>
- Prometheus overview and data model:
  <https://prometheus.io/docs/>
- Prometheus configuration:
  <https://prometheus.io/docs/prometheus/latest/configuration/configuration/>
- Prometheus HTTP API:
  <https://prometheus.io/docs/prometheus/latest/querying/api/>
- Grafana dashboard JSON model:
  <https://grafana.com/docs/grafana/latest/reference/dashboard/>
- Grafana provisioning:
  <https://grafana.com/docs/grafana/latest/administration/provisioning/>
- Grafana dashboard API:
  <https://grafana.com/docs/grafana/latest/http_api/dashboard/>
- Datadog metrics API:
  <https://docs.datadoghq.com/api/latest/metrics/>
- Datadog monitors API:
  <https://docs.datadoghq.com/api/latest/monitors/>
- Datadog dashboards API:
  <https://docs.datadoghq.com/api/latest/dashboards/>
- Datadog service definition API:
  <https://docs.datadoghq.com/api/latest/service-definition/>

## Source Contracts

The first implementation must preserve source-native identifiers and query
contracts without ingesting raw signal streams.

| Source | Source truth | Contract notes |
| --- | --- | --- |
| OpenTelemetry Collector config | Receivers, processors, exporters, connectors, extensions, service pipelines, signal types, and endpoint references | Config proves intended telemetry routing. It does not prove data was received unless runtime/status evidence corroborates it. |
| OTLP and OTel semantic conventions | Signal names, resource attributes, instrumentation scope, schema URLs, service identity fields, and protocol shape | These are normalization contracts. The collector should record signal contracts and schema versions, not OTLP payload streams. |
| Prometheus config | Scrape jobs, scrape intervals, timeouts, service discovery, relabeling, rule files, remote write/read, limits, and alertmanager wiring | Config proves scrape intent and rule loading intent. It does not prove target health or time-series presence unless API metadata supports it. |
| Prometheus HTTP API | Targets, rules, alerts, metric metadata, labels, series metadata, and runtime build/status where permitted | API reads must be bounded. Full series or range queries are out of scope by default. |
| Grafana provisioning | Datasources, dashboards, folders, alerting/contact points, and notification policy config | Provisioning files are observed config evidence and may differ from live Grafana API state. Preserve both. |
| Grafana API | Dashboard UID/version, folders, datasource refs, panels, variables, alert rules, and query expressions | Dashboard JSON can expose sensitive URLs, variables, and query text. Treat as customer evidence. |
| Datadog APIs | Metric metadata, monitors, dashboards, service definitions, tags, teams, docs/runbooks, and query expressions | Datadog metric and query APIs can become high-volume. Default collection is metadata only. |

## Decision

Add a future collector family named `observability`.

The collector owns:

- parsing checked-in observability configs and dashboard artifacts
- fetching configured observability API metadata
- preserving source-native IDs such as dashboard UID, datasource UID, monitor
  ID, Prometheus job name, OTel pipeline name, Datadog `dd-service`, and
  metric name
- normalizing scrape jobs, pipelines, datasources, dashboards, rules, monitors,
  service definitions, metric metadata, label contracts, and warning facts
- recording freshness, partial coverage, API permission gaps, rate limits, and
  redaction events

The collector does not own:

- raw metrics, log, trace, or profile ingestion
- long range queries over time series
- canonical graph writes
- deciding that a dashboard or monitor observes a service without reducer-owned
  corroboration
- alert evaluation or notification delivery
- changing dashboards, rules, monitors, or datasources
- SLO/SLA policy enforcement

Reducers own correlation and observability coverage findings.

## Scope And Generation Model

The bounded acceptance unit should be the observability source object that can
be refreshed independently.

Suggested scope IDs:

```text
otel-config://<repo-id>/<path>
otel-collector-instance://<cluster-or-env>/<instance-id>
prometheus-config://<repo-id>/<path>
prometheus-server://<tenant-id-or-host>/<server-id>
grafana-instance://<tenant-id-or-host>
grafana-dashboard://<tenant-id-or-host>/<dashboard-uid>
datadog-site-org://<site>/<org-id>
datadog-monitor://<site>/<org-id>/<monitor-id>
datadog-service-definition://<site>/<org-id>/<dd-service>
```

`<tenant-id-or-host>` must be canonicalized before use in scope IDs: strip URL
scheme, query, fragment, user info, and trailing slashes; lowercase the host;
and preserve only an operator-approved base path when multiple tenants share
one host. Raw URLs stay in facts as source locators, not scope IDs.

Suggested generation IDs:

- Config-backed sources: `<git-generation-id>:<config-content-sha>`.
- API-backed objects: provider object version, ETag, updated timestamp, or
  normalized response digest plus observed timestamp.
- Runtime/source instances: observed build/version plus response digest.

When a provider cannot produce a transactionally consistent snapshot, the
collector must preserve page/cursor coverage and mark the generation as
partial or eventually consistent.

## Fact Families

Initial fact kinds should use `collector_kind=observability`.

| Fact kind | Purpose |
| --- | --- |
| `observability.source_instance` | One observability source with tool kind, endpoint/site, org/tenant, version/build, auth mode redacted, capabilities, and source mode. |
| `observability.otel_collector_pipeline` | One OTel Collector pipeline with receivers, processors, exporters, connectors, extensions, signal types, endpoint refs, and config hash. |
| `observability.otel_signal_contract` | One signal contract with signal type, resource attributes, instrumentation scope, schema URL, semantic convention version, and service identity fields. |
| `observability.prometheus_scrape_job` | One scrape job with job name, interval, timeout, service discovery kind, relabel rules, target label contracts, limits, protocols, and source refs. |
| `observability.prometheus_metric_family` | One metric family with name, type/unit/help where available, label keys, exemplar/native histogram support, and metadata source. |
| `observability.prometheus_rule` | One recording or alerting rule with group, expression digest, labels, annotations, `for`, `keep_firing_for`, and rule file/source refs. |
| `observability.grafana_datasource` | One datasource with UID, name, type, URL redacted, access mode, provisioning source, default flag, and plugin metadata. |
| `observability.grafana_dashboard` | One dashboard with UID, title, tags, schema version, folder, panels, variables, datasource refs, query-expression digests, and version metadata. |
| `observability.grafana_alert_rule` | One alert rule with UID, title, folder, datasource refs, expression digests, labels, contact/notification refs, and evaluation settings. |
| `observability.datadog_service_definition` | One Datadog service definition with `dd-service`, schema version, team/contact refs, repo/docs/runbook/dashboard links, and source metadata. |
| `observability.datadog_monitor` | One monitor with ID, name, type, query digest, tags, thresholds, notification refs redacted, state metadata, and creator/modified timestamps where available. |
| `observability.datadog_dashboard` | One dashboard with ID, title, tags, widgets, query digests, template variables, layout metadata, and source refs. |
| `observability.datadog_metric_metadata` | One metric metadata record with metric name, type/unit/tags where available, description, integration/source, and usage refs where exposed. |
| `observability.warning` | Unsupported config version, parse failure, API permission gap, partial page, redaction event, high-cardinality drop, query skipped, rate limit, or auth denial. |

`source_confidence` should use:

- `observed` for repo-hosted configs and dashboard artifacts read by Eshu from
  Git.
- `reported` for provider API facts returned by Prometheus, Grafana, Datadog,
  or a collector runtime/status endpoint.
- `derived` only for normalized helper facts computed from already stored
  observability facts.

## Identity And Correlation Rules

Observability identity is drift-prone. `service.name`, Prometheus labels,
Grafana variables, Datadog tags, dashboard titles, and repo names can all
disagree.

Rules:

1. Dashboard, datasource, monitor, and service-definition facts must preserve
   provider-native IDs.
2. Query expressions are source evidence, but query text should be stored as a
   digest plus bounded parsed anchors unless raw query storage is explicitly
   enabled.
3. A dashboard, monitor, scrape job, or rule observes a service only when a
   reducer-owned rule matches stable evidence such as `service.name`,
   workload labels, metric label contracts, Datadog `dd-service`, source repo
   links, or catalog/service identity.
4. Metric names alone are not enough to attach observability evidence to a
   service unless the metric family or query carries a stable service/workload
   label.
5. Prometheus target health and Datadog monitor state are point-in-time API
   evidence. They must carry freshness and cannot erase stale config facts.
6. Missing API permissions must produce partial coverage, not a claim that no
   dashboard, monitor, rule, or metric exists.

## Reducer Correlation Contract

Reducers should admit observability joins only when the evidence path is
explicit:

```text
Observability source object
  -> stable service/workload/repo/cloud/catalog anchor
  -> canonical service, workload, deployable unit, API, or cloud resource
  -> coverage finding or read-side observability context
```

Candidate outcomes:

| Outcome | Meaning |
| --- | --- |
| `exact` | One observability object matched one canonical target through stable IDs, labels, or service definition refs. |
| `derived` | A deterministic rule matched through bounded query parsing, dashboard variables, or provider tag conventions. |
| `ambiguous` | Multiple canonical targets matched, or label/tag identity is shared across services. |
| `unresolved` | The observability object is valid but cannot be attached to current graph truth. |
| `stale` | Observability evidence conflicts with fresher code, catalog, deployment, cloud, or runtime evidence. |
| `rejected` | A title-only, raw metric-name-only, high-cardinality, stale, or unsafe signal was suppressed. |

Coverage findings should answer whether a service has dashboards, monitors,
scrape jobs, telemetry pipelines, or paging alerts. They must not claim that a
service is healthy or unhealthy from historical telemetry values.

## Freshness And Backfill

Normal freshness should use bounded updates:

- repo descriptor/config changes observed through Git refresh
- provider API updated timestamps, versions, ETags, or cursors
- scheduled reconciliation with small lookback overlap
- targeted object refresh from service-story, incident, or deployment workflows

Full tenant scans are backfill and recovery tools. They must require explicit
operator limits: maximum dashboards, monitors, datasource pages, metrics,
rules, targets, query expressions, and request budget.

Raw telemetry queries are disabled by default. If a future diagnostic mode runs
a live PromQL or Datadog query, it must require an explicit operator scope,
timeout, result limit, and no-persistence contract.

## Query And MCP Contract

Future read surfaces should be bounded and service-first:

- show dashboards, monitors, scrape jobs, and telemetry pipelines for a service
- show which observability objects depend on a metric or label before a rename
- show services with no dashboard, no monitor, or no paging alert evidence
- show OTel collectors and exporters that route signals for a service
- show Prometheus jobs that scrape a workload or endpoint
- show Datadog service definitions linked to repo/catalog/service evidence
- explain observability coverage for vulnerability or incident impact context

Responses must include provider, source object ID, generation ID,
truth/confidence label, freshness, warning summaries, deterministic ordering,
`limit`, and `truncated`. Normal use must not require raw Cypher.

## Observability Requirements

The hosted runtime must expose:

- collect duration by source kind and provider
- API request counts by provider, operation, and bounded result
- rate-limit/throttle counts by provider and operation
- configs parsed and source objects observed by provider and object kind
- facts emitted by provider and fact kind
- query expressions parsed, skipped, and redacted by provider
- high-cardinality guard drops by provider and reason
- partial generation counts by provider and reason
- source freshness lag by provider and scope
- reducer correlation outcomes for dashboard, monitor, scrape job, rule,
  datasource, service definition, and metric metadata
- collector claim duration, processing duration, retry counts, and dead-letter
  counts

Spans should cover scope discovery, config parse, API page fetch, object detail
fetch, query-expression parse, fact batch emission, and reducer correlation.

Metric labels must not include dashboard names, monitor names, datasource URLs,
query text, metric names, label values, team names, emails, notification
handles, service names, repository names, target URLs, endpoint paths, or
credential references. Those values belong in facts, spans, or structured logs
with redaction.

## Security And Privacy

Observability metadata can expose private service names, network topology,
internal URLs, runbooks, notification channels, incident contacts, query text,
metric labels, dashboard variables, auth headers, and cloud/resource tags.

Rules:

- Provider credentials must be read-only.
- Raw metrics, logs, traces, and profiles are not stored by default.
- Query text is redacted or stored as a digest plus bounded parsed anchors by
  default.
- Datasource URLs, headers, tokens, basic auth, notification targets, and
  webhook URLs must be stripped from logs, metrics, and status output.
- Status output must not include raw target URLs, full label sets, dashboard
  names, monitor names, emails, team membership, or query bodies unless
  explicitly allowed.
- The collector must not write back to Prometheus, Grafana, Datadog,
  Alertmanager, OTel Collector configs, Git, Slack, PagerDuty, or any
  observability system.

## Implementation Gate

The first implementation should be split into small PRs:

1. Fact contracts and fixtures for OTel Collector, Prometheus, Grafana, and
   Datadog.
2. OTel Collector config parser with pipeline and signal-contract fixtures.
3. Prometheus config/rule parser with scrape job, metric metadata, and rule
   fixtures.
4. Grafana dashboard/datasource parser with provisioning fixtures.
5. Datadog service-definition and monitor parser with fixture-backed API tests.
6. Reducer correlation tests for exact, derived, ambiguous, unresolved, stale,
   and rejected outcomes.
7. Hosted runtime with credentials, budgets, pagination, redaction, health,
   readiness, metrics, admin/status, and ServiceMonitor proof.

Implementation must not start with graph writes, raw telemetry ingestion, or
query shortcuts. Facts and reducer contracts come first.

## Acceptance Criteria

- Collector emits versioned facts only; no direct graph writes.
- Fixtures cover OTel Collector config, Prometheus config/rules/metadata,
  Grafana dashboard/datasource/provisioning, and Datadog service
  definition/monitor/dashboard/metric metadata.
- Facts are idempotent under duplicate collection.
- Reducer preserves exact, derived, ambiguous, unresolved, stale, and rejected
  states.
- Query output uses truth/freshness labels for observability evidence.
- Tests prove secrets, raw URLs, notification targets, query text, and
  high-cardinality label values are redacted from logs, metrics, and status.
- API failure, permission gaps, and rate limits produce visible partial
  generations and do not silently produce authoritative truth.
- Hosted runtime proof includes request-budget, rate-limit, health, readiness,
  metrics, admin/status, and ServiceMonitor evidence before production use.

## Rejected Alternatives

### Ingest Raw Telemetry Streams

Rejected. Metrics, logs, traces, and profiles are high-volume operational data
owned by observability backends. Eshu needs control-plane metadata and coverage
evidence, not another telemetry store.

### Treat Dashboard Or Monitor Existence As Service Health

Rejected. A dashboard or monitor proves configured observation intent. It does
not prove current service health or incident state.

### Attach By Metric Name Alone

Rejected. Metric names are often shared across services. Attachment requires
stable service, workload, repo, catalog, cloud, or label evidence.

### Store Raw Queries Everywhere

Rejected. Query text can contain service names, customer IDs, internal URLs,
tokens, and high-cardinality labels. Default storage is digest plus bounded
parsed anchors.
