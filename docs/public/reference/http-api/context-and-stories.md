# HTTP Context And Story Routes

Use these routes when the caller has a fuzzy name or canonical entity and needs
context, catalog navigation, a narrative story, or an investigation packet. The
route list is verified against `go/internal/query`.

## Route Map

| Area | Routes |
| --- | --- |
| Entity resolution | `POST /api/v0/entities/resolve` |
| Incident context | `GET /api/v0/incidents/{incident_id}/context` |
| Work-item evidence | `GET /api/v0/work-items/evidence` |
| Context | `GET /api/v0/entities/{entity_id}/context`, `GET /api/v0/workloads/{workload_id}/context`, `GET /api/v0/services/{service_name}/context`, `GET /api/v0/repositories/{repo_id}/context` |
| Catalog | `GET /api/v0/catalog` |
| Stories | `GET /api/v0/repositories/{repo_id}/story`, `GET /api/v0/workloads/{workload_id}/story`, `GET /api/v0/services/{service_name}/story`, `POST /api/v0/impact/trace-deployment-chain`, `POST /api/v0/impact/deployment-config-influence` |
| Investigation | `GET /api/v0/investigations/services/{service_name}` |

OpenAPI remains canonical for full request and response schemas.

## Entity Resolution

`POST /api/v0/entities/resolve` accepts `name`, optional `type`, optional
`repo_id`, and optional `limit`. `name` is required. The response includes
`entities`, `count`, normalized `limit`, and `truncated`.

Use this route before context or story routes when the caller starts with a
fuzzy name, alias, or partial resource description.

## Context

Context routes are canonical-ID oriented:

- entity context requires `entity_id`
- workload context requires `workload_id`
- repository context requires `repo_id`
- service context is an alias over workload context and adds
  `requested_as=service`

When a repository has workload identity facts but no materialized `Workload`
node, service context can fall back to the repository read model. Those
responses use `materialization_status=identity_only`,
`query_basis=repository_read_model`, an empty `instances` array, and a
`limitations` entry of `workload_identity_not_materialized`.

Entity context may include semantic narrative fields when normalized semantic
metadata exists: `semantic_summary`, `semantic_profile`, and `story`.

## Incident Context

`GET /api/v0/incidents/{incident_id}/context` returns a bounded incident
packet from collected source facts. `provider` defaults to `pagerduty`;
`scope_id` disambiguates duplicate provider incident IDs; `service_id`,
`since`, and `until` bound fallback change candidates.

The response always includes an ordered evidence path for incident, service,
intended PagerDuty routing, applied PagerDuty routing, live PagerDuty routing,
deployable, runtime artifact, image, build/deploy record, commit, pull request,
and work item slots. Missing Jira, pull-request, runtime, image, build,
deployable, routing, or commit evidence is reported explicitly instead of
omitted.

Routing slots preserve source class. `intended_routing` comes from
Terraform-source `PagerDutyDeclaration` content rows. `applied_routing` comes
from active Terraform-state `incident_routing.applied_pagerduty_resource`
facts. `live_routing` comes from optional live
`incident_routing.observed_pagerduty_service` facts or scoped
`incident_routing.coverage_warning` gaps such as permission-hidden provider
state. These slots explain whether the incident service is declared, applied,
or currently visible in PagerDuty; they do not prove root cause, service
health, blast radius, deployable identity, image identity, commit, pull request,
or Jira work-item truth.

When a service-catalog operational link exactly names the PagerDuty service
URL, the read model can use reducer-owned catalog, container-image, and
Kubernetes correlation facts to fill deployable, image, and runtime artifact
slots. When CI/CD run correlation evidence names the selected image digest,
build/deploy and commit slots can be exact; tag-only image-reference matches
remain derived unless a later reducer fact proves an immutable artifact digest.
When a GitHub merged-pull-request trigger names the selected commit, the pull
request slot is exact provider evidence. Jira remote links to that
provider-verified PR, direct PagerDuty incident links, or issue keys in the PR
title can enrich the work-item slot, but Jira-only PR URLs do not verify
pull-request identity. Fallback change candidates are labeled separately from
exact provider evidence and from derived reducer edges, and name-only service or
tag matches are not promoted.

No-Regression Evidence:

```bash
cd go && go test ./internal/query -run 'TestIncidentContext(ChangeCandidateQueryCastsNullableTimeParametersEverywhere|QueriesStayBoundedToActiveFacts|HandlerUsesBoundedStore|HandlerReturnsAmbiguousCandidates|HandlerRequiresIncidentIDAndLimit|RuntimeQueriesStayBoundedToExplicitEvidence)' -count=1
```

This proves incident context reads keep bounded active-fact queries and cast
nullable fallback-change time parameters everywhere they are used.

No-Observability-Change: the route still runs under `query.incident_context`
with stable `http.route` and `eshu.capability` span attributes, existing
Postgres query instrumentation, envelope error reporting, and explicit
missing-evidence slots. No graph write, collector call, queue worker, metric
instrument, or deployment knob changes.

## Work-Item Evidence

`GET /api/v0/work-items/evidence` lists active Jira/work-item source facts.
Requests must include `limit` and at least one scope anchor: `scope_id`,
`project_key`, `work_item_key`, `provider_work_item_id`, `external_url`,
`url_fingerprint`, or `observed_after`. The route returns redacted evidence
rows, `missing_evidence`, state summaries, and `next_cursor.after_fact_id` when
the page is truncated.

The route is ticket-first evidence, not an incident or deployment verifier.
External URLs are converted to sanitized fingerprints and raw URLs are not
returned. Jira facts can show exact provider evidence, unsupported link types,
stale evidence, permission-hidden evidence, missing evidence, or rejected unsafe
payloads, but PR, commit, deployment, runtime artifact, image, service, and
incident truth require provider or reducer evidence outside Jira.

## Catalog

`GET /api/v0/catalog` is the bounded navigation surface for Console and MCP
clients. It returns repository, workload, and service handles plus counts,
`limit`, `truncated`, and limitations when the runtime can only return
repository handles.

The optional `limit` caps each returned collection. The default is 2000 and the
maximum accepted value is 5000.

## Stories

Story routes return structured narrative first and drilldown handles second.
They are the right entry point for onboarding, support, service explanation,
and documentation generation prompts.

Service story supports disambiguation with:

- `service_id` for an exact workload/service ID
- `repo` for repository-scoped disambiguation
- `environment` for environment-scoped disambiguation

When a service name matches multiple workloads, service story returns HTTP 409
with envelope `error.code=ambiguous`, `data=null`, and candidate details. It
does not choose the first match.

Deployment trace responses are evidence-first and may include deployment,
GitOps, controller, runtime, cloud, Kubernetes, image, relationship, and
fact-summary sections. Mapping modes are:

- `controller`
- `iac`
- `evidence_only`
- `none`

`POST /api/v0/impact/deployment-config-influence` accepts `service_name` or
`workload_id`, optional `environment`, and optional `limit`. Use it when the
caller asks which repositories and files influence image tags, runtime
settings, resource limits, values layers, or rendered Kubernetes resources.

## Service Investigation

`GET /api/v0/investigations/services/{service_name}` accepts optional
`environment`, `intent`, and `question`.

It returns an investigation packet rather than a polished story: repositories
considered, repositories with evidence, evidence families found, coverage
summary, findings, and recommended next calls. Use it when the caller should
not need to know which deployment, GitOps, Terraform, workflow, support, or
documentation repositories to inspect first.

## Documentation Generation Flow

For generated docs or support prose:

1. Call a story or service investigation route first.
2. Use deployment trace or deployment config influence when deployment details
   matter.
3. Use content routes only after the story identifies exact files, snippets, or
   entity handles worth citing.
