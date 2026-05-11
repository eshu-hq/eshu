# HTTP API Reference

The HTTP API is versioned under `/api/v0` and shares the same query model as CLI and MCP. It is intended for AI agents, automation, and internal tools that need a stable contract.

## OpenAPI is the source of truth

The live OpenAPI spec is always canonical. If this page and the spec disagree, the spec wins.

- `GET /api/v0/openapi.json` - machine-readable schema
- `GET /api/v0/docs` - Swagger UI for trying requests in a browser
- `GET /api/v0/redoc` - ReDoc reference for reading the API contract

These documentation routes are intentionally public, like the OpenAPI JSON
schema. API data routes still follow the configured API auth policy.

For the mounted Go runtime admin surface, the checked-in OpenAPI contract lives
in `docs/openapi/runtime-admin-v1.yaml`. That contract is separate from the
public `/api/v0` schema because it belongs to the long-running runtime admin
endpoints, not the public query API.

## Response Envelope Contract

Responses use a canonical envelope when the client opts in by sending:

```http
Accept: application/eshu.envelope+json
```

Without that header, handlers keep emitting the legacy payload shape for
backwards compatibility. The envelope is the canonical contract for programmatic
clients, MCP, and CLI `--json` mode.

### Envelope shape

```json
{
  "data": { ... },
  "truth": {
    "level": "derived",
    "capability": "code_search.exact_symbol",
    "profile": "local_lightweight",
    "basis": "content_index",
    "freshness": { "state": "fresh" },
    "reason": "resolved from indexed entity and content tables"
  },
  "error": null
}
```

- `data` carries the response payload. `null` on error responses.
- `truth` carries the truth label for the response. `null` on error responses.
- `error` is `null` on success. On failure it carries a structured error
  envelope (see below).

### Truth levels

| Level | Meaning |
| --- | --- |
| `exact` | Authoritative graph or durable semantic truth. |
| `derived` | Deterministic result computed from indexed entities, content, or other structured relational state. |
| `fallback` | Exploratory result — useful but not strong enough to claim full authority for the requested capability. |

High-authority capabilities (transitive callers/callees, call-chain paths,
dead-code, cross-repo impact) do not silently downgrade to `fallback` in a
profile that cannot answer them correctly. They return a structured
`unsupported_capability` error instead.

### Freshness states

| State | Meaning |
| --- | --- |
| `fresh` | Answer reflects current indexed truth for the requested scope. |
| `stale` | Previously indexed truth exists but backlog or lag means the answer may be behind source. |
| `building` | Initial or replacement indexing is still in progress; authoritative data is not ready. |
| `unavailable` | Required backend or authoritative source is currently unavailable. |

Clients that cache responses MUST invalidate on changes to `truth.level` or
`truth.freshness.state`. ETags or cache keys should vary on both fields.

### Runtime profiles

`truth.profile` is one of:

- `local_lightweight` — single-binary `eshu` host with embedded Postgres, no
  authoritative graph backend.
- `local_authoritative` — local `eshu` service with embedded Postgres and
  NornicDB, authoritative graph available for the indexed workspace.
- `local_full_stack` — full Docker Compose stack, authoritative graph available.
- `production` — deployed multi-runtime platform.

Set the runtime profile via the `ESHU_QUERY_PROFILE` environment variable at
process start. Invalid values are rejected at startup; there is no silent
default.

### Capability IDs

`truth.capability` references the capability matrix at
`specs/capability-matrix.v1.yaml`. Full semantics live in
`reference/capability-conformance-spec.md` and
`reference/truth-label-protocol.md`.

### Structured error codes

On failure, `error` carries a structured envelope:

```json
{
  "error": {
    "code": "unsupported_capability",
    "message": "transitive callers require authoritative graph mode",
    "capability": "call_graph.transitive_callers",
    "profiles": {
      "current": "local_lightweight",
      "required": "local_authoritative"
    }
  }
}
```

Initial error code set:

| Code | When |
| --- | --- |
| `unsupported_capability` | Capability not supported in the current runtime profile. Returned as HTTP 501. |
| `unauthenticated` | Authentication is missing or invalid. Returned as HTTP 401. |
| `invalid_argument` | Request parameters are invalid or malformed. |
| `not_found` | Requested finding, packet, entity, repo, or workspace scope does not exist. |
| `permission_denied` | Caller cannot view the requested source, document, or evidence. |
| `backend_unavailable` | Authoritative backend (Neo4j / Postgres) is unreachable. |
| `index_building` | Initial indexing is in progress; authoritative data not ready. |
| `scope_not_found` | Requested entity, repo, or workspace scope does not exist. |
| `capability_degraded` | Capability supported but running under reduced fidelity (e.g. reducer lag). |
| `overloaded` | Runtime is saturated; request rejected rather than queued unboundedly. |
| `internal_error` | Eshu failed unexpectedly while serving a request. |
| `documentation_read_model_unavailable` | Documentation packet routes are mounted without the Postgres documentation read model. |

Details, freshness semantics, and MCP embedding live in
`reference/truth-label-protocol.md`.

## Scope

The public HTTP API exposes three operator-relevant surfaces:

- a read/query surface for context, code, infra, and content retrieval
- a small ingester control surface for runtime status and manual scan requests
- a checkpoint surface for index completeness and admin reindex requests

Use it to resolve entities, fetch context, search code, trace infra, compare environments, and inspect the deployed ingester state.

Use the CLI for local indexing workflows. Use the Helm runtime for
deployment-managed repository ingestion and steady-state sync.

## Health, Status, And Completeness

Health checks answer whether a process can serve. Completeness checks answer
whether the latest published Go checkpoint is finished.

- `GET /health` reports API process health after dependency
  initialization. It does not prove the latest index run finished.
- The hosted API runtime also mounts the shared service-local admin surface on
  the same listener:
    - `GET /healthz`
    - `GET /readyz`
    - `GET /admin/status`
    - `GET /metrics`
  Those routes are documented by `docs/openapi/runtime-admin-v1.yaml`, not the
  public `/api/v0` OpenAPI schema.
- `GET /api/v0/status/index` returns the current checkpoint summary.
- `GET /api/v0/index-status` returns the same checkpoint summary.
- `GET /api/v0/status/ingesters` is the canonical ingester-status list route.
- `GET /api/v0/status/ingesters/{ingester}` is the canonical ingester-status
  detail route.
- `GET /api/v0/ingesters` and `GET /api/v0/ingesters/{ingester}` return the
  same ingester-status payloads.
- `GET /api/v0/repositories/{repo_id}/coverage` returns durable repository
  coverage rows for one repository.
- Run-scoped completeness routes such as `/api/v0/index-runs/{run_id}` are not
  part of the shipped public contract. Do not assume the
  repository coverage route is run-scoped.
- `POST /api/v0/admin/refinalize` re-enqueues active scope generations for
  re-projection through the durable Go work queue.
- `POST /api/v0/admin/reindex` persists an asynchronous reindex request; the
  API process does not run the full reindex inline.
- `GET /api/v0/admin/shared-projection/tuning-report` returns the operator
  tuning report for shared-projection backlog behavior.
- `POST /api/v0/admin/replay`, `POST /api/v0/admin/dead-letter`,
  `POST /api/v0/admin/skip`, `POST /api/v0/admin/backfill`,
  `POST /api/v0/admin/work-items/query`, `POST /api/v0/admin/decisions/query`,
  and `POST /api/v0/admin/replay-events/query` expose the durable admin queue
  and decision controls.
- The service-local runtime admin surface remains separate from the public
  `/api/v0` contract even when it is mounted on the same listener. Use
  `/admin/status` when you need the runtime-local probe/status surface
  described by `docs/openapi/runtime-admin-v1.yaml`. Use `/admin/replay` and
  `/admin/refinalize` only on runtimes that mount the recovery handler, such
  as the ingester.

## Model Basics

- `workload` is the canonical deployable compute model.
- `service` is a convenience alias over workloads with `kind=service`.
- environment-scoped calls return the logical workload plus the resolved `WorkloadInstance`.
- canonical entity IDs are required on path-based context routes.
- repository identity is remote-first when a git remote exists.
- repository objects expose `repo_slug`, `remote_url`, and `local_path`.
- `local_path` is server-local metadata, not a portable client filesystem path.
- file-bearing query results should be interpreted using `repo_id + relative_path`, not an absolute server path.
- `repo_access` indicates whether the caller may need to ask the user for a local checkout path or clone decision.
- documentation-oriented clients should resolve canonical graph identity first, then use `repo_id + relative_path` or `entity_id` for exact evidence reads.
- repository-oriented context, summary, story, stats, and file routes accept a repository selector at the public boundary and normalize it to the canonical `repo_id` server-side.

### Deployment Evidence Pointers

Repository, workload, service, and deployment-trace responses may include
`deployment_evidence`. This object is intentionally compact: it returns counts
and grouped pointers instead of embedding full Postgres evidence payloads.

- `artifacts[]` carries the inspectable evidence pointer for one deployment,
  CI, IaC, or config signal.
- `artifacts[].resolved_id` is the durable lookup key for the
  `resolved_relationships` row in Postgres.
- `artifacts[].generation_id` identifies the relationship generation that
  produced the row.
- `artifacts[].source_location` identifies where the signal came from with
  `repo_id`, `repo_name`, `path`, and `start_line` / `end_line` when the
  extractor produced line data.
- `evidence_index.lookup_basis` is `resolved_id`.
- `evidence_index.relationship_types`, `evidence_index.artifact_families`, and
  `evidence_index.evidence_kinds` group artifact counts with the unique
  `resolved_ids` and `generation_ids` needed for drilldown.

`GET /api/v0/evidence/relationships/{resolved_id}` dereferences one pointer
into the durable relationship evidence row. The response includes
`lookup_basis: "resolved_id"`, source and target repo metadata, relationship
type, confidence, evidence count, evidence kinds, rationale, resolution source,
generation metadata, `evidence_preview`, and the decoded `details` JSON stored
with the resolved relationship. Use this endpoint when an API or MCP client
needs to explain why an edge exists without embedding the full evidence payload
in every graph-facing response.

### Documentation Truth Evidence

Documentation updater services should use the documentation truth routes
instead of reading graph internals directly.

`GET /api/v0/documentation/findings` lists read-only findings such as
`service_deployment_drift`. The endpoint accepts filters for `finding_type`,
`source_id`, `document_id`, `status`, `truth_level`, `freshness_state`,
`updated_since`, `limit`, and `cursor`. Each item includes the stable finding
identity, document and section identity, status, truth labels, summary, and an
`evidence_packet_url`.

`GET /api/v0/documentation/findings/{finding_id}/evidence-packet` returns the
bounded packet an external updater can snapshot before it plans a diff. The
packet includes finding identity, document and section metadata, bounded
excerpt, linked entities, current truth, evidence references, truth state,
permission state, and explicit `states` for stale, ambiguous, unsupported, or
ready findings. Eshu still does not draft text or write documentation; this
route only gives the updater the evidence it is allowed to use.

`GET /api/v0/documentation/evidence-packets/{packet_id}/freshness` lets an
updater check a saved packet before publishing a diff. If the packet is stale,
the updater should fetch the latest packet and restart planning from the new
snapshot.

## Context API

### Resolve fuzzy input into canonical entities

`POST /api/v0/entities/resolve`

Use this before context lookups when the caller has a fuzzy name, alias, or partial resource description.

```json
{
  "query": "payments prod rds",
  "types": ["workload", "cloud_resource"],
  "environment": "prod",
  "limit": 5
}
```

### Get canonical entity context

`GET /api/v0/entities/{id}/context`

Examples:

- `GET /api/v0/entities/workload:payments-api/context`
- `GET /api/v0/entities/workload-instance:payments-api:prod/context`

Entity context responses may also include semantic narrative fields when the
entity carries normalized semantic metadata:

- `semantic_summary`
- `semantic_profile`
- `story`

### Get workload context

`GET /api/v0/workloads/{id}/context`

Logical view:

- `GET /api/v0/workloads/workload:payments-api/context`

Environment-scoped view:

- `GET /api/v0/workloads/workload:payments-api/context?environment=prod`

### Get service context

`GET /api/v0/services/{id}/context`

This is an alias route. It still accepts a canonical workload ID:

- `GET /api/v0/services/workload:payments-api/context`
- `GET /api/v0/services/workload:payments-api/context?environment=prod`

Service alias responses include `requested_as=service`.

When a repository has workload identity facts but no materialized `Workload`
node yet, service context can fall back to the repository read model. Those
responses use `materialization_status=identity_only`,
`query_basis=repository_read_model`, an empty `instances` array, and a
`limitations` entry of `workload_identity_not_materialized`. Deployment
evidence and delivery-family paths may still be present when parser or
relationship evidence proves them.

## Story API

Use the story routes when the caller wants a structured narrative first and
evidence second.

Repository story responses now expose a structured narrative contract. Workload
and service story responses stay narrative-first today. Use the deployment
trace route when you need the richer deployment-mapping contract.

Repository story responses are shaped around:

- `subject`
- `story`
- `story_sections`
- optional `semantic_overview`
- `deployment_overview`
- `gitops_overview`
- `documentation_overview`
- `support_overview`
- `coverage_summary`
- `limitations`
- `drilldowns`

Within `deployment_overview`, repository story responses may also include:

- `delivery_family_paths`
- `delivery_family_story`
- `delivery_paths`
- `delivery_workflows`
- `shared_config_paths`

When repository entities carry semantic signals, repository story responses
also:

- add a `semantics` entry into `story_sections`
- embed semantic coverage text into the top-level `story`
- expose aggregated semantic counts in `semantic_overview`

Workload and service story responses are still shaped around:

- `subject`
- `story`
- optional lightweight identifiers such as `subject`

Deployment-oriented trace responses are shaped around:

- `subject`
- `story`
- `story_sections`
- `deployment_overview`
- `gitops_overview`
- `controller_overview`
- `runtime_overview`
- `deployment_sources`
- `cloud_resources`
- `k8s_resources`
- `image_refs`
- `k8s_relationships`
- `controller_driven_paths`
- `delivery_paths`
- `deployment_fact_summary`
- `drilldowns`

When repository-backed delivery-family synthesis is available, trace responses
may also surface those grouped summaries through `deployment_overview`, such
as `delivery_family_paths`, `delivery_family_story`, `delivery_workflows`, and
`shared_config_paths`.

When controller evidence is recoverable from the deployment repositories,
`controller_overview` may also include concrete controller entity records in
`entities`.

The current deployment-oriented trace route is:

- `POST /api/v0/impact/trace-deployment-chain`

Deployment-oriented trace responses may also include:

- `deployment_facts`
- `deployment_fact_summary`

Use those when you need a stable, evidence-first contract instead of prose.

`deployment_fact_summary` reports:

- `mapping_mode`
- `overall_confidence`
- `overall_confidence_reason`
- `evidence_sources`
- fact types grouped by confidence
- `fact_thresholds`
- deployment-specific limitations such as `deployment_controller_unknown`

Mapping modes are intentionally controller-agnostic:

- `controller` for explicit controller evidence such as ArgoCD or Flux
- `iac` for explicit infrastructure-as-code evidence such as Terraform or CloudFormation
- `evidence_only` when delivery/runtime evidence exists but no trusted controller/IaC adapter was found
- `none` when no deployment evidence cleared the evidence thresholds and Eshu can only report missing inputs

That lets the same story contract work across GitOps, IaC-driven, and controller-free estates without fabricating deployment tooling.

HTTP story routes stay canonical-ID based. If the caller starts with a fuzzy
name or alias, resolve first and then call the story route. Deployment traces
start from service names because they are the operator-facing entrypoint.

### Get repository story

`GET /api/v0/repositories/{id}/story`

Example:

- `GET /api/v0/repositories/repository:r_ab12cd34/story`

### Get workload story

`GET /api/v0/workloads/{id}/story`

Examples:

- `GET /api/v0/workloads/workload:payments-api/story`
- `GET /api/v0/workloads/workload:payments-api/story?environment=prod`

### Get service story

`GET /api/v0/services/{id}/story`

Examples:

- `GET /api/v0/services/workload:payments-api/story`
- `GET /api/v0/services/workload:payments-api/story?environment=prod`

Treat the story routes as the top-level contract for repo/service/workload
narratives. Use the context routes, trace routes, and content routes named in
`drilldowns` for follow-up evidence.

For documentation generation, use this HTTP flow:

1. call a story route first
2. if it is a repository story, read `story_sections`, `deployment_overview`, `gitops_overview`, `documentation_overview`, `support_overview`, `coverage_summary`, and `limitations`
3. if it is a workload or service story, pair it with `trace_deployment_chain` before you expect deployment overviews
4. only then call content routes for exact file or snippet evidence

For cross-repo documentation or support flows, phrase the caller intent the same
way you would through MCP: tell Eshu to scan all related repositories,
deployment sources, and indexed documentation for the service or workload before asking
for the final narrative.

## Investigation API

Use this route when the caller wants Eshu to plan the repo widening and evidence
search for them instead of manually chaining story, trace, and content calls.

### Investigate a service

`GET /api/v0/investigations/services/{service_name}`

Optional query params:

- `environment`
- `intent`
- `question`

The response is investigation-first rather than story-first. Key fields:

- `repositories_considered`
- `repositories_with_evidence`
- `evidence_families_found`
- `coverage_summary`
- `investigation_findings`
- `recommended_next_calls`

Coverage fields are meant to be truthful, not optimistic:

- `complete` means the indexed repository coverage explicitly reported complete
- `partial` means the indexed context or story limitations reported partial coverage
- `unknown` means Eshu cannot currently prove complete or partial coverage from indexed evidence alone

This route is the HTTP inspection mode for operators:

- story/context routes remain the canonical-first truth surface
- investigation widens into evidence families and related repos on purpose
- inspection mode should explain gaps and widening decisions, not silently act like a second canonical graph

Use it for prompts like:

- "Explain the deployment flow for sample-service-api using Eshu only."
- "Explain the network flow for sample-service-api using Eshu only."
- "What depends on sample-service-api and what does it depend on?"

This route is designed for non-expert users who should not have to know which
deployment, GitOps, Terraform, workflow, or support repositories to inspect
next.

## Code API

Use these routes when you only need code relationships and do not need the full code-to-cloud graph.

- `POST /api/v0/code/search`
- `POST /api/v0/code/relationships`
- `POST /api/v0/code/dead-code`
- `POST /api/v0/code/complexity`

Public code-query requests accept a repository selector in the `repo_id` field
when a repository scope is part of the request. The selector may be the
canonical repository ID, repository name, repo slug, or indexed path. The
server resolves that selector to the canonical repository ID before querying.
Results should still be interpreted using canonical `repo_id + relative_path`,
not absolute server-local paths.

`POST /api/v0/code/relationships` prefers `entity_id` when the caller already
has a canonical entity. It also accepts `name` for fallback lookup, plus
optional `direction` (`incoming` or `outgoing`) and `relationship_type`
filters when the caller only needs one edge class. Set `transitive=true` with
`relationship_type=CALLS` to ask for indirect callers or callees, and use
`max_depth` to cap the traversal. Lightweight mode refuses those transitive
graph traversals with a structured `unsupported_capability` envelope instead
of returning degraded call-graph guesses.

Example transitive-caller workflow:

```json
{
  "name": "process_payment",
  "direction": "incoming",
  "relationship_type": "CALLS",
  "transitive": true,
  "max_depth": 7
}
```

Example code-only workflow:

`POST /api/v0/code/search`

```json
{
  "query": "process_payment",
  "repo_id": "payments",
  "exact": false,
  "limit": 10
}
```

Example dead-code workflow:

`POST /api/v0/code/dead-code`

```json
{
  "repo_id": "payments",
  "language": "go",
  "limit": 200,
  "exclude_decorated_with": ["@route", "@app.route"]
}
```

`repo_id` is optional. `language` is optional; pass it when validating one
parser language family such as `sql`, `go`, `python`, `java`, `javascript`,
`typescript`, `tsx`, or `rust`. For SQL, the language filter narrows the
candidate scan to `SqlFunction` routines so mixed repositories with many
application functions do not starve SQL routine evidence. When `repo_id` is
omitted, the Go API returns the first page of
dead-code candidates across indexed repositories, applies the current default
Go entrypoint/test/generated exclusions plus direct Go Cobra, stdlib HTTP, and
controller-runtime framework-root signatures and Go exported public-package
roots, and uses content metadata to filter any decorator exclusions. Exported
Go symbols under `internal/`, `cmd/`, and `vendor/` remain candidates; only
public-package exports are treated as default roots. The current dead-code
response is intentionally `derived`, not `exact`, until the broader framework,
public-API, reflection, and user-configured root registry from the
reachability spec is implemented. The response body now also includes an
`analysis` object that reports the root categories currently modeled, the
specific Go framework-root signatures currently recognized, the per-language
dead-code maturity table, named exactness blockers for non-exact languages such
as Rust, observed exactness blockers carried by returned candidates, and whether
tests/generated code were excluded. Returned candidates with observed exactness
blockers classify as `ambiguous`, not cleanup-ready `unused`.
`analysis.roots_skipped_missing_source`
counts Go candidates where the framework-root checks could not run because the
content store did not have source cached. `analysis.framework_roots_from_parser_metadata`
and `analysis.framework_roots_from_source_fallback` show whether the excluded
Go framework roots came from parser-emitted metadata or the legacy query-time
source heuristic path. `limit` defaults to `100` and is capped at `500`. The
response includes `display_truncated=true` when filtered results were clipped
to `limit`, `candidate_scan_truncated=true` when the raw graph scan reached
`candidate_scan_limit` before exclusions ran, and top-level `truncated=true`
when either condition is true.

## IaC Quality API

Use these routes when you need infrastructure-as-code cleanup candidates:

- `POST /api/v0/iac/dead`

The dead-IaC route requires an explicit `repo_id` or bounded `repo_ids` scope.
When reducer-materialized reachability rows exist, the route returns those rows
with `analysis_status=materialized_reachability`; bootstrap and
`local_authoritative` graph runs materialize these rows after source-local
content projection drains. Otherwise it falls back to bounded indexed-content
analysis for Terraform modules, Helm charts, Kustomize bases/overlays, Ansible
roles/playbooks, and Docker Compose services. Used artifacts are omitted from
cleanup findings; unreferenced artifacts are returned as `candidate_dead_iac`,
and variable or template-selected artifacts are returned as
`ambiguous_dynamic_reference` when `include_ambiguous=true`.
Findings expose the canonical `repo_id` plus `repo_name` when the repository
catalog can resolve it. `findings_count` reports the number of rows returned
on the current page; `total_findings_count`, `truncated`, and `next_offset`
report whether more materialized or derived findings are available.

Example dead-IaC workflow:

```json
{
  "repo_ids": ["terraform-stack", "terraform-modules", "helm-controller", "helm-charts", "kustomize-controller", "kustomize-config", "compose-controller", "compose-app"],
  "families": ["terraform", "helm", "kustomize", "compose"],
  "include_ambiguous": true,
  "limit": 100,
  "offset": 0
}
```

The content fallback is intentionally bounded and derived. Exact cleanup
support should prefer reducer-materialized IaC usage rows so operators can
explain every finding from persisted evidence instead of broad graph anti-joins.

## Content API

Use these routes when a caller needs source text or indexed content search without relying on raw server filesystem paths.

- `POST /api/v0/content/files/read`
- `POST /api/v0/content/files/lines`
- `POST /api/v0/content/entities/read`
- `POST /api/v0/content/files/search`
- `POST /api/v0/content/entities/search`

Rules:

- portable file lookup uses `repo_id + relative_path`
- content routes accept repository selectors in `repo_id` and `repo_ids`: canonical IDs, repository names, repo slugs, or indexed paths
- portable entity lookup uses `entity_id`
- deployed API runtimes are PostgreSQL-first and PostgreSQL-only for direct content reads
- if PostgreSQL is disabled or missing a cached row, deployed HTTP reads return `source_backend=unavailable` instead of reading from a server workspace checkout
- local CLI and non-deployed helper flows may still use workspace or graph-cache fallbacks
- file and entity read responses include `source_backend` so callers can see whether the result came from `postgres`, `workspace`, `graph-cache`, or `unavailable`
- content search routes require the PostgreSQL content store and return an error payload when it is disabled
- content retrieval should not trigger `repo_access` prompting when the server already has the checkout
- documentation and runbook workflows should expect exact evidence to come from PostgreSQL-backed reads or search when available

Example file read:

```json
{
  "repo_id": "payments",
  "relative_path": "src/payments.py"
}
```

Example entity read:

```json
{
  "entity_id": "content-entity:e_ab12cd34ef56"
}
```

Example file-content search:

```json
{
  "pattern": "shared-payments-prod",
  "repo_ids": ["payments", "eshu-hq/eshu"]
}
```

## Infra API

- `POST /api/v0/infra/resources/search`
- `POST /api/v0/infra/relationships`
- `GET /api/v0/ecosystem/overview`
- `POST /api/v0/traces/resource-to-code`
- `POST /api/v0/impact/explain-dependency-path`
- `POST /api/v0/impact/change-surface`
- `POST /api/v0/environments/compare`

These routes are for tracing shared infrastructure, blast radius, dependency explanation, and environment drift.

`POST /api/v0/infra/resources/search` accepts `query`, `category`, `kind`,
`provider`, `resource_service`, `resource_category`, and `limit`. Terraform AWS
resource and data-source nodes preserve provider classification in both graph
and content-backed responses, so callers can narrow a search to families such
as `provider=aws`, `resource_service=s3`, or `resource_category=storage`.
Free-text `query` also checks typed resource identifiers such as Terraform
resource types and CloudFormation/SAM `resource_type` values. CloudFormation
type identifiers such as `AWS::Serverless::Function` use an exact
resource-type predicate so graph backends do not have to scan broad text fields
to answer a typed resource lookup.

Example infrastructure search:

```json
{
  "query": "aws_s3",
  "category": "terraform",
  "provider": "aws",
  "resource_service": "s3",
  "resource_category": "storage",
  "limit": 10
}
```

## Repository API

- `GET /api/v0/repositories`
- `GET /api/v0/repositories/{id}/context`
- `GET /api/v0/repositories/{id}/story`
- `GET /api/v0/repositories/{id}/stats`

Repository routes accept a repository selector in the `{id}` path segment. The
selector may be the canonical repository ID, repository name, repo slug, or
indexed path. The server resolves that selector to the canonical repository ID
before querying.

Repository responses should be treated as:

- canonical identity: `id`
- remote identity: `repo_slug`, `remote_url`
- server-local checkout metadata: `local_path`

If a downstream workflow needs local file operations on a user machine, use `repo_access` or ask the user for a local checkout path instead of assuming the server path exists locally.

For local or deployed indexing workflows, use the CLI and deployment runtime:

- local: `eshu index <path>`
- Kubernetes: repository ingestion is deployment-managed through the ingester runtime

## Ingester Status API

Use these routes to inspect the deployed ingester runtime without reaching into
Kubernetes directly.

- Canonical:
  - `GET /api/v0/status/ingesters`
  - `GET /api/v0/status/ingesters/{ingester}`
- Legacy `GET` aliases:
  - `GET /api/v0/ingesters`
  - `GET /api/v0/ingesters/{ingester}`

The default ingester is `repository`.

Status responses are designed for remote operation and include:

- ingester identity
- current status
- active run id
- last attempt / last success
- next retry timing
- repository progress counts
- failure counts and last error details

The shipped public API does not include a `POST /api/v0/ingesters/{ingester}/scan`
route in the shipped platform. Use `POST /api/v0/admin/reindex` or deployment-managed
ingestion instead of assuming a per-ingester public scan endpoint exists.

## Bundle Import API

Use this route when you want to load dependency or library internals explicitly
without indexing vendored source trees as part of the normal repository scan.

- `POST /api/v0/bundles/import`

Request contract:

- `multipart/form-data`
- file field: `bundle`
- optional form field: `clear_existing=true|false`

The route imports the uploaded `.eshu` bundle into the active graph database and
returns a success/message response describing the import result.
