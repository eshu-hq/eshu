# PagerDuty Evidence Contract

This page defines the evidence contract for PagerDuty incident-routing context.
It covers the path from infrastructure declarations to applied state to live
PagerDuty observations. It keeps the existing PagerDuty incident collector
behavior separate from optional live configuration validation, and it does not
add a Terraform provider loader.

Eshu already packages the PagerDuty Terraform provider schema. The missing work
is the staged evidence path that lets collectors and reducers explain
how a PagerDuty service, integration, escalation policy, or alert route relates
to an incident without treating any one source as automatic truth.

## Scope

PagerDuty incident-routing evidence answers these questions:

- Which PagerDuty service, escalation policy, team, integration, or event
  orchestration was declared for a service?
- Was the declaration applied in Terraform state?
- Does the live PagerDuty API still show the same routing shape?
- Is there an AWS alerting route, such as SNS, Lambda, SSM, EventBridge, or
  CloudWatch, that can deliver alerts into PagerDuty?
- Can an incident be connected to runtime artifact, image, build, commit, pull
  request, or Jira work-item evidence through reducer-owned joins?

The existing #1025 `incident.record`, `incident.lifecycle_event`, and
`change.record` facts remain observed incident evidence. They must not be
overloaded with configuration truth.

## Evidence Classes

Every PagerDuty incident-routing fact MUST declare exactly one source class.

| Source class | Source | Contract |
| --- | --- | --- |
| `declared` | Terraform source, modules, tfvars, and declared AWS routing resources | Preferred when present and current. Declarations describe intended routing, but they do not prove the route was applied or still exists. |
| `applied` | Terraform state and provider-binding facts for PagerDuty and AWS routing resources | Confirms, contradicts, or fills declaration gaps. Applied evidence must preserve Terraform address, module path, provider binding, workspace, and state generation. |
| `observed` | Live PagerDuty API state plus existing incident, lifecycle-event, and change facts | Confirms current provider state, supports no-IaC teams, detects drift, and records incidents without requiring Jira or Terraform evidence. |

Precedence evaluates current declared evidence first, applied evidence second,
and observed evidence third. Stale declarations cannot overwrite current state
or live provider observations. No source class silently overwrites another.
Reducers must record contradictions as explicit outcomes and keep enough
provenance for operators to inspect the path.

## Source Identity

All new PagerDuty incident-routing fact payloads MUST carry the common source
identity fields below unless the fact is an existing incident fact.

| Field | Requirement |
| --- | --- |
| `source_class` | One of `declared`, `applied`, or `observed`. |
| `source_kind` | Stable source family such as `terraform_source`, `terraform_state`, `pagerduty_api`, `aws_sns`, `aws_lambda`, `aws_ssm`, `aws_eventbridge`, or `aws_cloudwatch`. |
| `source_instance_id` | Stable configured collector or parser instance. |
| `scope_id` | Eshu scope that owns the evidence. |
| `generation_id` | Active generation that produced the evidence. |
| `observed_at` | Time the source was parsed, collected, or fetched. |
| `freshness_state` | `current`, `stale`, `partial`, or `unknown`. |
| `provenance` | File path and Terraform address for declared evidence, state snapshot identity for applied evidence, or provider object reference for observed evidence. |
| `redaction_version` | Version of the redaction policy applied before persistence. |
| `outcome` | One of the incident-context outcomes in this page. |

PagerDuty identity fields MUST be stable and joinable without storing secrets.
Supported identity keys include service ID, sanitized service URL, service name
fingerprint, escalation-policy ID, team ID, integration ID, integration type,
event-orchestration ID, Terraform resource address, Terraform module path,
Terraform provider address, workspace, account scope, AWS resource ARN, and
sanitized alert target reference.

## Fact Families

The first implementation slices start with these fact families. Terraform-state
applied PagerDuty and alert-route facts are emitted by the Terraform-state
parser. Live PagerDuty service and service-integration facts are emitted only
when a PagerDuty target opts into live configuration validation. Declared-source
reducers and broader live resource classes remain separate follow-up slices.

| Fact family | Source class | Identity keys | Payload boundaries |
| --- | --- | --- | --- |
| `incident_routing.declared_pagerduty_service` | `declared` | Terraform address, module path, provider address, service name fingerprint, optional service ID | Service description intent, escalation policy reference, team references, alert-creation behavior, urgency rules, and sanitized source path. |
| `incident_routing.declared_pagerduty_escalation_policy` | `declared` | Terraform address, module path, escalation-policy name fingerprint, optional policy ID | Rule count, schedule/user references as fingerprints, handoff timing, and team references. |
| `incident_routing.declared_pagerduty_team` | `declared` | Terraform address, module path, team name fingerprint, optional team ID | Team metadata needed for service ownership joins. User identities and contact methods are excluded. |
| `incident_routing.declared_pagerduty_integration` | `declared` | Terraform address, module path, service reference, integration summary or type, optional integration ID | Integration type, vendor summary, service reference, and whether the integration expects events. Routing keys are excluded. |
| `incident_routing.declared_pagerduty_event_orchestration` | `declared` | Terraform address, module path, orchestration name fingerprint, optional orchestration ID | Rule-set shape, service route references, and sanitized condition metadata. Raw event rules and private endpoints are excluded when sensitive. |
| `incident_routing.declared_alert_route` | `declared` | AWS resource ARN or Terraform address, route type, sanitized PagerDuty target reference | AWS alert path from SNS, Lambda, SSM, EventBridge, CloudWatch, or related resources into PagerDuty. Secret-bearing URLs and parameter values are excluded. |
| `incident_routing.applied_pagerduty_resource` | `applied` | Terraform state resource address, resource type, provider address, state generation, provider object ID | Applied PagerDuty service, team, escalation policy, integration, or orchestration state. Sensitive state attributes are redacted before persistence. |
| `incident_routing.applied_alert_route` | `applied` | Terraform state resource address, AWS ARN, provider address, state generation, sanitized target reference | Applied AWS routing resources that can deliver alerts into PagerDuty. Secret values, endpoint tokens, and payload templates are excluded. |
| `incident_routing.observed_pagerduty_service` | `observed` | PagerDuty service ID, sanitized service URL, account scope | Optional live service status, escalation policy reference, team references, name fingerprint, comparison state, and update timestamp. Raw service names are excluded. |
| `incident_routing.observed_pagerduty_integration` | `observed` | PagerDuty integration ID, service ID, integration type | Optional live integration state, vendor reference, service reference, name fingerprint, comparison state, and redaction flags. Integration keys and routing keys are excluded. |
| `incident_routing.coverage_warning` | any | Scope, source instance, warning reason, related identity key | Missing provider permission, unsupported resource type, stale source, ambiguous match, rejected sensitive value, or incomplete route proof. |

Existing `incident.record`, `incident.lifecycle_event`, and `change.record`
facts continue to describe provider-reported incidents, incident timeline
entries, and related change events. They are observed incident facts, not
configuration facts.

## Outcomes

PagerDuty incident-routing reducers and reads MUST classify evidence with the
same vocabulary used by incident context.

| Outcome | Meaning |
| --- | --- |
| `declared` | Intended routing exists in source but has not been compared with state or live API evidence. |
| `applied` | Terraform state confirms a declared or state-only PagerDuty or alert-route resource. |
| `observed` | Live PagerDuty API confirms provider state or supplies no-IaC evidence. |
| `drifted` | Declared, applied, and observed evidence disagree on a meaningful identity or route field. |
| `stale` | Source evidence is older than the accepted freshness window or belongs to an inactive generation. |
| `permission_hidden` | The collector lacks permission to observe a resource that source or state evidence references. |
| `unsupported` | Eshu recognizes the source but does not yet model the specific resource or route type. |
| `rejected` | Evidence was intentionally dropped because it was invalid, unsafe, secret-bearing, or outside the configured scope. |
| `exact` | Evidence resolves to one unambiguous incident-routing object or downstream incident-context path. |
| `derived` | Evidence supports a likely path but depends on a weaker join, such as a tag, name fingerprint, or route summary. |
| `ambiguous` | Evidence resolves to more than one candidate and cannot promote truth. |
| `missing` | A required evidence slot is absent. Missing Jira or Terraform evidence is valid for no-IaC PagerDuty incidents. |

Reducers may expose both a source-class state and a resolution outcome. For
example, an applied Terraform resource can produce an `applied` source state
and a `drifted` comparison outcome when the live PagerDuty API disagrees.

## Redaction Rules

PagerDuty incident-routing evidence MUST never persist or label:

- PagerDuty tokens, user tokens, OAuth client secrets, webhook secrets, routing
  keys, integration keys, or event orchestration credentials.
- Secret-bearing URLs, API URLs with embedded credentials, webhook payloads,
  request bodies, or response bodies.
- Responder email addresses, phone numbers, contact methods, user names, user
  summaries, schedule membership details, or notification rules.
- Incident titles, log-entry summaries, private incident links, or copied
  provider text as metric labels.
- SSM parameter values, Lambda environment variable values, SNS subscription
  tokens, or private endpoint query strings.
- Real tenant names, production account IDs, or private service names in
  synthetic fixtures.

Store stable IDs only when they are safe for the configured deployment. When a
field may reveal tenant-private naming, store a deterministic fingerprint and
preserve the original only inside an operator-controlled private environment.

## Freshness

Freshness is evaluated per source class.

| Source class | Freshness signal |
| --- | --- |
| `declared` | Repository revision, parsed file path, module version, tfvars source, parser generation, and active branch or bundle identity. |
| `applied` | Terraform state snapshot digest, backend identity, workspace, serial, lineage, and collection generation. |
| `observed` | PagerDuty API fetch time, provider updated timestamp, collector generation, rate-limit state, and permission coverage. |

Webhook deliveries are freshness triggers only. A PagerDuty webhook can wake the
configured collector target, but it does not produce incident-routing facts by
itself.

## Fixture Matrix

Fixtures MUST be synthetic and free of real PagerDuty IDs, tokens, integration
keys, private URLs, responder identities, incident titles, tenant names, account
IDs, or production tag values.

| Fixture | Required assertion |
| --- | --- |
| Terraform-declared PagerDuty service confirmed by state and live API | Declared, applied, and observed evidence converge to `exact`. |
| Terraform-declared PagerDuty service missing from state | Declared evidence remains source truth and comparison emits `missing` or `stale` applied evidence. |
| Terraform state resource with no source declaration | Applied evidence is retained with source-only provenance and no fabricated declaration. |
| Live PagerDuty service with no IaC declaration | Observed evidence supports no-IaC mode and records missing declared/applied slots explicitly. |
| Private module wrapping PagerDuty service config | Module path and call-site provenance are preserved without requiring module internals to leak. |
| tfvars-driven service name, escalation policy, urgency, and event orchestration | Resolved declaration carries tfvars provenance and deterministic identity fingerprints. |
| AWS SNS, Lambda, SSM, EventBridge, or CloudWatch route into PagerDuty | Alert route records sanitized AWS identity and PagerDuty target reference without endpoint secrets. |
| PagerDuty service integration drift | Applied or declared integration disagrees with live provider state and emits `drifted`. |
| Escalation policy drift | Policy identity or rule shape disagrees across source, state, and live evidence. |
| Permission-hidden live PagerDuty config | Collector records `permission-hidden` without treating absent live data as deletion. |
| Stale Terraform source revision | Stale declared evidence cannot overwrite current applied or observed evidence. |
| Sensitive token, integration key, private URL, or responder identity | Secret-bearing values are redacted or rejected and covered by fixture assertions. |

## Test Expectations

Implementation PRs that adopt this contract MUST add tests before the
implementation code. At minimum, tests must prove:

- Typed Terraform provider resources are classified into the declared
  PagerDuty and alert-route fact families.
- Module and tfvars declarations preserve provenance and stable identity.
- Terraform state resources emit applied evidence without leaking sensitive
  attributes.
- Live PagerDuty API fixtures can confirm, fill gaps, or report
  permission-hidden state.
- Existing `incident.record`, `incident.lifecycle_event`, and `change.record`
  fixtures still behave as observed incident evidence.
- Reducer, API, and MCP reads agree on exact, derived, ambiguous, missing,
  drifted, stale, permission_hidden, unsupported, and rejected outcomes.

Operator-facing runtime work must also expose bounded metrics, spans, logs,
status, retry, dead-letter, fact-count, and freshness evidence before any new
collector lane is treated as production-ready.

## Readiness

The shipped PagerDuty collector emits provider-reported incident and change
evidence. Terraform-state applied PagerDuty and alert-route evidence,
Terraform-source PagerDutyDeclaration content rows, and optional live PagerDuty
service/integration observations now exist as source lanes. The incident-context
API/MCP read model compares declared, applied, and observed PagerDuty service
evidence for the incident service and reports intended, applied, and live
routing slots without promoting root cause, service health, blast radius,
deployable, image, commit, pull-request, or work-item truth.

Broader live PagerDuty config classes, alert-route-to-service comparison, and
graph materialization remain staged follow-up work.

Do not add Helm production-readiness claims for the full incident-routing
surface until the collector, reducer, fixtures, telemetry, status, and API/MCP
reads all exist.

Observability Evidence: optional live PagerDuty config validation records
bounded provider request, emitted fact, config resource observed, drift
candidate, partial failure, redaction, fetch duration, rate-limit, and
generation-lag metrics without putting incident IDs, service names, integration
names, routing keys, token env names, token values, or private URLs into metric
labels.
