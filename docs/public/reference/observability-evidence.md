# Observability Evidence Contract

This page defines the source-fact contract for Grafana-stack observability
evidence. It is the implementation contract for the first observability
collector-family slice; it does not by itself make Grafana, Prometheus, Mimir,
Loki, or Tempo production-ready collector lanes.

Eshu must support both IaC/GitOps-managed environments and teams that configure
observability directly in provider UIs. The precedence is explicit:

1. `declared` evidence from source-controlled configuration is preferred when
   present and current.
2. `applied` evidence confirms or contradicts declared evidence.
3. `observed` evidence fills gaps, detects drift, proves freshness, or supports
   no-IaC users.

Reducers and query surfaces own correlation and user-facing truth. Collectors
emit source facts only.

Implementation status: the Git collector emits declared Grafana and
Prometheus/Mimir IaC/GitOps facts from Helm values, GrafanaFolder and
GrafanaDashboard resources, dashboard ConfigMaps, folder provisioning,
datasource provisioning, alert provisioning, Prometheus Operator
ServiceMonitor, PodMonitor, PrometheusRule, and ScrapeConfig resources,
kube-prometheus-stack and Mimir Helm values, OTel metric pipelines, and chart
Prometheus receiver scrape configs and ServiceMonitor settings. Missing
Prometheus discovery labels are emitted as coverage warnings rather than
silently accepted. Terraform Grafana folder, dashboard, datasource, and
rule-group resources are also supported. Applied-state, live-provider, reducer,
API, and MCP coverage work remains separate.

Declared Prometheus/Mimir facts are intent evidence. Live Prometheus and Mimir
API collection is still required when a team does not use source-controlled
configuration, when Eshu must prove drift between declared and effective
targets or rules, when target/rule freshness matters, or when rendered
Prometheus Operator selection turns Helm and CRD inputs into effective scrape
jobs.

## Evidence Classes

| Class | Source | Proves | Does not prove |
| --- | --- | --- | --- |
| `declared` | Repositories, Helm values, Kustomize overlays, Argo CD config, rendered manifests, CRs, ConfigMaps, and collector pipeline config. | Intended observability coverage and routing. | That the config was applied or is live in the provider. |
| `applied` | Argo CD and Kubernetes state, including synced resources and deployed config objects. | A declared or in-cluster resource exists, is synced, degraded, pruned, stale, or permission-hidden. | That live provider state still matches the applied resource. |
| `observed` | Live Grafana, Prometheus, Mimir, Loki, and Tempo APIs. | Effective provider state, recent signal presence, no-IaC coverage, drift, or freshness. | That the state is source-controlled or intentionally managed. |

No class overwrites another. Reducers must preserve agreement, disagreement,
missing evidence, and drift so API and MCP users can see how an answer was
formed.

## Fact Families

The first implementation wave uses these source fact families. Names may gain
minor additive fields as provider-specific issues land, but the source class,
scope, generation, provenance, freshness, and redaction fields are mandatory.

| Fact kind | Class | Purpose |
| --- | --- | --- |
| `observability.source_instance` | all | Identifies a configured IaC, applied-state, or live-provider source. |
| `observability.declared_folder` | `declared` | Declared Grafana folder UID and title fingerprint. |
| `observability.declared_dashboard` | `declared` | Declared Grafana dashboard identity, folder, datasource refs, and service hints. |
| `observability.declared_datasource` | `declared` | Declared Grafana datasource UID/type/backend refs, including Mimir, Loki, Tempo, and Prometheus. |
| `observability.declared_alert_rule` | `declared` | Declared alert rule identity and safe service/route hints. |
| `observability.declared_scrape_config` | `declared` | Declared Prometheus/Mimir scrape intent from ServiceMonitor, PodMonitor, ScrapeConfig, Helm values, or equivalent config. |
| `observability.declared_metric_rule` | `declared` | Declared Prometheus/Mimir rule group and alert rule metadata. |
| `observability.declared_metric_route` | `declared` | Declared metric pipeline route, for example OTel to Mimir. |
| `observability.declared_log_route` | `declared` | Declared log pipeline route, for example Promtail or OTel to Loki. |
| `observability.declared_trace_route` | `declared` | Declared trace pipeline route, for example OTel to Tempo. |
| `observability.applied_resource` | `applied` | Applied Argo CD or Kubernetes resource metadata for dashboards, datasources, rules, scrape config, collectors, services, and ConfigMaps. |
| `observability.applied_sync_state` | `applied` | Argo CD or Kubernetes sync, health, generation, pruned, missing, stale, or permission-hidden state. |
| `observability.observed_dashboard` | `observed` | Effective Grafana dashboard, folder, datasource, or alert identity observed from live APIs. |
| `observability.observed_target` | `observed` | Effective Prometheus/Mimir target metadata and freshness. |
| `observability.observed_rule` | `observed` | Effective Prometheus/Mimir/Loki rule metadata. |
| `observability.observed_log_signal` | `observed` | Bounded Loki label, series, or ruler metadata. Never log lines. |
| `observability.observed_trace_signal` | `observed` | Bounded Tempo service, tag, or search metadata. Never spans. |
| `observability.coverage_warning` | all | Rejected, stale, ambiguous, drifted, permission-hidden, unsupported, unsafe, or partial evidence. |

## Required Fields

Every fact in this family must carry:

- `source_class`: `declared`, `applied`, or `observed`
- `source_kind`: provider or source family, such as `grafana`, `prometheus`,
  `mimir`, `loki`, `tempo`, `argocd`, `kubernetes`, `helm`, or `kustomize`
- `source_instance_id`: stable configured source identity
- `scope_id`: tenant, org, repo, cluster, namespace, or environment scope
- `generation_id`: collector or parser generation
- `observed_at`: source observation time
- `freshness_state`: `current`, `stale`, `unknown`, or `permission_hidden`
- `provenance`: repo path, source revision, overlay, cluster, namespace,
  resource identity, provider UID, or tenant scope where available
- `redaction_version`: version of the redaction policy used for the payload
- `outcome`: source-local outcome, such as `exact`, `derived`, `ambiguous`,
  `unresolved`, `stale`, `rejected`, `drifted`, `permission_hidden`, or
  `unsupported`

Stable fact keys must include source class, source instance, scope,
provider-native identity or resource identity, and generation. Duplicate pages,
overlapping windows, retries, and repeated source scans must converge on the
same stable keys.

## Redaction Boundary

Observability facts are metadata-only. They must not persist:

- raw metric samples, exemplars, or profile data
- log lines
- spans, traces, raw trace IDs, or request attributes
- dashboard screenshots
- full dashboard JSON as user-facing facts
- datasource credentials, tokens, webhook secrets, or tenant secrets
- contact addresses, email addresses, or private notification routes
- private URLs or URLs containing credentials
- raw PromQL, LogQL, TraceQL, or dashboard query bodies as user-facing facts
- unbounded label or tag values
- Kubernetes Secret values or rendered secret data

Collectors may parse source files or provider responses only enough to extract
safe identity, routing, freshness, and provenance fields. Unsafe fields must be
redacted, fingerprinted, summarized, or rejected with a
`observability.coverage_warning` fact.

## Coverage Outcomes

Reducers map source facts into the observability coverage read model. The
existing #391 outcomes stay intact and gain explicit class-aware drift handling.

| Outcome | Meaning | Graph edge allowed |
| --- | --- | --- |
| `exact` | A provider-native or declared identity resolves to exactly one target. | yes, when the reducer has a canonical target node |
| `derived` | A structured signal needs interpretation but is corroborated by other evidence. | no, unless a later reducer stage proves a canonical target |
| `ambiguous` | Multiple targets match the evidence. | no |
| `unresolved` | No target matches yet. | no |
| `stale` | Evidence exists only for stale, tombstoned, or expired state. | no |
| `rejected` | Evidence is unsafe, malformed, unsupported, or outside bounds. | no |
| `drifted` | Declared, applied, and observed evidence disagree. | no until reconciled |
| `permission_hidden` | The source reports or implies data exists but Eshu cannot read it. | no |

Health and root-cause claims are out of scope. A dashboard, alert, target, log
signal, or trace signal can prove coverage evidence; it does not prove the
service is healthy or that an incident was caused by the observed signal.

## Fixture Matrix

Provider-specific issues must add fixtures that cover these scenario IDs before
shipping runtime behavior:

All fixture records must be synthetic. They must not contain real tenant names,
tokens, private URLs, contact addresses, query bodies, log lines, spans,
dashboard payloads, label values outside an allowlist, or trace/tag values from
production systems.

| Scenario | Required evidence |
| --- | --- |
| `declared_applied_observed_match` | IaC declares coverage, applied state confirms it, and live provider state agrees. |
| `declared_not_applied` | IaC declares coverage, but Argo CD or Kubernetes does not show the resource as applied. |
| `declared_observed_missing` | IaC declares coverage, but live provider state is missing or stale. |
| `observed_only_no_iac` | No IaC exists; live provider state is the only evidence. |
| `manual_provider_drift` | Live provider state has a dashboard, alert, rule, route, or signal not declared in source. |
| `stale_declared_revision` | Declared source revision is older than the active deployment or source freshness window. |
| `permission_hidden_applied` | Argo CD or Kubernetes scope cannot be read. |
| `permission_hidden_observed` | Provider API reports hidden or forbidden data. |
| `unsupported_resource_kind` | Source uses a provider, CRD, or resource kind outside the supported contract. |
| `duplicate_generation` | Duplicate scans or retries emit the same stable fact keys. |
| `redaction_required` | Secrets, contacts, private URLs, raw queries, or unbounded labels/tags are redacted or rejected. |

## Test Expectations

Each provider-specific implementation must prove:

- source parsing or provider normalization for all relevant fact families
- stable keys under duplicate pages, retries, and overlapping generations
- redaction of every forbidden field listed above
- declared, applied, observed, drifted, stale, permission-hidden, unsupported,
  rejected, exact, derived, ambiguous, and unresolved outcomes
- reducer/API/MCP agreement before any user-facing truth claim
- no graph `COVERS` edge for derived, ambiguous, unresolved, stale, rejected,
  drifted, or permission-hidden evidence

No-Regression Evidence: the declared Grafana and Prometheus/Mimir IaC
source-fact slices add bounded parser buckets and Git fact emission only. They
do not add provider calls, graph writes, queue workers, reducer stages, query
handlers, metrics, spans, logs, or status output.

No-Observability-Change: declared Grafana and Prometheus/Mimir source facts use
the existing Git collector snapshot, parse, and fact-commit telemetry.
Operators diagnose these slices through existing file parse counts, generation
fact counts, fact commit counts, and collector observe duration.

## Related Work

- [Collector And Reducer Readiness](collector-reducer-readiness.md)
- [Fact Schema Versioning](fact-schema-versioning.md)
- [Fact Envelope Reference](fact-envelope-reference.md)
- [Relationship Mapping Observability](relationship-mapping-observability.md)
